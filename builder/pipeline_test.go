package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestInstallVMDisksBootOrder(t *testing.T) {
	disks := installVMDisks("", false)
	if len(disks) != 3 {
		t.Fatalf("len=%d", len(disks))
	}
	root := disks[0].(map[string]interface{})
	iso := disks[1].(map[string]interface{})
	if root["name"] != "rootdisk" || root["bootOrder"] != 1 {
		t.Fatalf("rootdisk=%v", root)
	}
	if iso["name"] != "installiso" || iso["bootOrder"] != 2 {
		t.Fatalf("installiso=%v", iso)
	}
	if _, ok := iso["cdrom"]; !ok {
		t.Fatal("ISO must be a SATA CD-ROM")
	}
	if _, ok := disks[2].(map[string]interface{})["bootOrder"]; ok {
		t.Fatal("sysprep must not have bootOrder")
	}
	withVirtio := installVMDisks("quay.io/example/virtio-win:latest", false)
	if len(withVirtio) != 4 {
		t.Fatalf("virtio len=%d", len(withVirtio))
	}
	v := withVirtio[2].(map[string]interface{})
	if v["name"] != "virtio" {
		t.Fatalf("virtio=%v", v)
	}
	if _, ok := v["bootOrder"]; ok {
		t.Fatal("virtio must not have bootOrder")
	}
}

func TestVirtioStorageProbe(t *testing.T) {
	winPE := `<settings pass="windowsPE"><component name="Microsoft-Windows-PnpCustomizationsWinPE"><DriverPaths>` +
		`<PathAndCredentials wcm:action="add" wcm:keyValue="1"><Path>E:\\viostor\\2k22\\amd64</Path></PathAndCredentials>` +
		`</DriverPaths></component><component name="Microsoft-Windows-Setup"></component></settings>`
	// Specialize-pass drivers (NonWinPE) mention viostor too; only WinPE counts.
	noWinPE := `<settings pass="windowsPE"><component name="Microsoft-Windows-Setup"></component></settings>` +
		`<settings pass="specialize"><component name="Microsoft-Windows-PnpCustomizationsNonWinPE"><DriverPaths>` +
		`<Path>E:\\viostor\\w11\\amd64</Path></DriverPaths></component></settings>`
	const img = "quay.io/example/virtio-win:latest"
	if needsVirtioStorageProbe(img, winPE) {
		t.Fatal("WinPE loads viostor: no probe (Setup would see a second disk)")
	}
	if !needsVirtioStorageProbe(img, noWinPE) {
		t.Fatal("WinPE skips viostor: probe needed so viostor becomes a boot driver")
	}
	if needsVirtioStorageProbe("", noWinPE) {
		t.Fatal("no virtio-win image: no probe")
	}

	disks := installVMDisks(img, true)
	last := disks[len(disks)-1].(map[string]interface{})
	if last["name"] != virtioProbeName || nestedString(last, "disk", "bus") != "virtio" {
		t.Fatalf("probe must be the last disk, on virtio: %v", last)
	}
	if _, ok := last["bootOrder"]; ok {
		t.Fatal("probe must not be bootable")
	}
	if prev := disks[len(disks)-2].(map[string]interface{}); prev["name"] != "sysprep" {
		t.Fatalf("CD order must not change: %v", prev)
	}

	vm := installVMManifest("ns", "wb-install-win11", "wb-iso-win11", "wb-sysprep-win11", StartBuildRequest{
		VirtioImage: img, Autounattend: noWinPE, Cores: 2, Memory: "8Gi",
	})
	vols, _ := nestedMap(vm, "spec", "template", "spec")["volumes"].([]interface{})
	found := false
	for _, v := range vols {
		vm := v.(map[string]interface{})
		if vm["name"] == virtioProbeName && nestedString(vm, "emptyDisk", "capacity") == "1Gi" {
			found = true
		}
	}
	if !found {
		t.Fatalf("probe emptyDisk volume missing: %v", vols)
	}
}

func TestLastUpdateStatus(t *testing.T) {
	log := "BdsDxe: loading Boot0001\r\nwb-update: pass 1/8: checking for updates\r\n" +
		"noise\r\nwb-update: pass 1/8: installing 3 update(s)\r\n\r\n"
	if got := lastUpdateStatus(log); got != "pass 1/8: installing 3 update(s)" {
		t.Fatalf("got %q", got)
	}
	if !isGuestUpdateError(lastUpdateStatus("wb-update: ERROR: sysprep failed after 2 attempts: x\r\n")) {
		t.Fatal("ERROR lines must fail the build")
	}
	if isGuestUpdateError("sysprep did not finish (attempt 1/2): x") {
		t.Fatal("a first sysprep failure is retried, not fatal")
	}
	script, _ := sysprepAnswerFiles("<unattend/>")["wb-update.ps1"].(string)
	del := strings.Index(script, "schtasks.exe /Delete")
	run := strings.Index(script, "Sysprep\\sysprep.exe")
	if del < 0 || run < 0 || del > run {
		t.Fatal("the startup task must be deleted before sysprep runs (never sealed into the image)")
	}
	if got := lastUpdateStatus("no progress yet\n"); got != "" {
		t.Fatalf("got %q", got)
	}
	vm := installVMManifest("ns", "wb-install-x", "wb-iso-x", "wb-sysprep-x", StartBuildRequest{Cores: 2, Memory: "8Gi"})
	dev := nestedMap(vm, "spec", "template", "spec", "domain", "devices")
	if dev["logSerialConsole"] != true {
		t.Fatal("install VM must log its serial console (update progress)")
	}
}

func TestResumeAfterRestart(t *testing.T) {
	for st, want := range map[BuildStatus]bool{
		StatusInstalling: true, StatusSysprep: true,
		StatusPending: false, StatusReady: false, StatusError: false,
	} {
		if canResume(st) != want {
			t.Fatalf("canResume(%s) = %v", st, !want)
		}
	}
	// A builder that resumes after the guest shut down only sees the finished
	// VMI: guestOSInfo (from qemu-guest-agent) counts as the agent connecting.
	var st vmiWaitState
	st.observeVMI(map[string]interface{}{
		"status": map[string]interface{}{
			"phase":       "Succeeded",
			"guestOSInfo": map[string]interface{}{"id": "mswindows", "name": "Microsoft Windows"},
			"phaseTransitionTimestamps": []interface{}{
				map[string]interface{}{"phase": "Running", "phaseTransitionTimestamp": "2026-01-01T00:00:00Z"},
			},
		},
	})
	if !st.sawAgent || !st.sawRunning {
		t.Fatalf("resume evidence: %+v", st)
	}
	if err := evaluateGuestExit(guestExitEvidence{SawAgent: st.sawAgent, RequireAgent: true, SawSucceeded: true, SawRunning: true}); err != nil {
		t.Fatal(err)
	}
}

func TestRedactAutounattend(t *testing.T) {
	if got := redactAutounattend("Job started"); got != "Job started" {
		t.Fatal(got)
	}
	if got := redactAutounattend("see Autounattend.xml"); got == "see Autounattend.xml" {
		t.Fatal("expected redaction")
	}
}

func TestSysprepAnswerFiles(t *testing.T) {
	d := sysprepAnswerFiles("<unattend/>")
	if d["autounattend.xml"] != "<unattend/>" || d["Autounattend.xml"] != "<unattend/>" {
		t.Fatalf("%v", d)
	}
	if _, ok := d["unattend.xml"]; ok {
		t.Fatal("install Autounattend must not also be published as unattend.xml")
	}
	script, _ := d["wb-update.ps1"].(string)
	if !strings.Contains(script, "Microsoft.Update.Session") || !strings.Contains(script, "sysprep.exe") {
		t.Fatal("wb-update.ps1 must install updates and then run sysprep")
	}
}

func TestEvaluateGuestExit(t *testing.T) {
	tooShort := evaluateGuestExit(guestExitEvidence{Uptime: 30 * time.Second, SawRunning: true})
	if tooShort == nil || !strings.Contains(tooShort.Error(), "guest shut down before install finished") {
		t.Fatalf("immediate crash with no guest OS must be Error, got %v", tooShort)
	}
	neverBooted := evaluateGuestExit(guestExitEvidence{Uptime: 7 * time.Minute})
	if neverBooted == nil {
		t.Fatal("uptime without a running VMI must be Error")
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: 7 * time.Minute, SawRunning: true, SawSucceeded: true}); err != nil {
		t.Fatalf("ACPI after a ~7m sysprep must clone: %v", err)
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: 7 * time.Minute, SawRunning: true}); err != nil {
		t.Fatalf("Stopped after a running guest (~7m) must clone: %v", err)
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: 45 * time.Minute, SawRunning: true}); err != nil {
		t.Fatalf("long uptime should clone: %v", err)
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: 3 * time.Minute, SawAgent: true}); err != nil {
		t.Fatalf("guest agent means FirstLogon ran: %v", err)
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: 7 * time.Minute, SawRunning: true, SawSucceeded: true, RequireAgent: true, SawAgent: true}); err != nil {
		t.Fatalf("agent then ACPI must clone: %v", err)
	}
	noAgent := evaluateGuestExit(guestExitEvidence{Uptime: 7 * time.Minute, SawRunning: true, SawSucceeded: true, RequireAgent: true})
	if noAgent == nil || !strings.Contains(noAgent.Error(), "qemu-guest-agent") {
		t.Fatalf("virtio install without qemu-ga must not clone: %v", noAgent)
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: 2 * time.Minute, SawGuestOS: true}); err != nil {
		t.Fatalf("guest OS is Setup evidence: %v", err)
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: time.Minute, DiskUsed: minInstallDiskUsed}); err != nil {
		t.Fatalf("disk growth is Setup evidence: %v", err)
	}
}

func TestInterpretVMINotFound(t *testing.T) {
	ran := vmiWaitState{sawRunning: true, notFoundN: 1}
	if interpretVMINotFound(http.StatusOK, "Stopped", ran) != waitExited {
		t.Fatal("Stopped after a running guest must proceed (or Error on uptime), not poll")
	}
	if interpretVMINotFound(http.StatusNotFound, "", ran) != waitMissing {
		t.Fatal("VM deleted while the guest ran: fail (never clone an unsealed disk), and do not poll 4h")
	}
	if interpretVMINotFound(http.StatusOK, "Starting", ran) != waitPoll {
		t.Fatal("RerunOnFailure recreating the VMI should keep polling")
	}
	if interpretVMINotFound(http.StatusOK, "Running", vmiWaitState{sawSucceeded: true, notFoundN: 1}) != waitExited {
		t.Fatal("Succeeded even briefly must be treated as guest exited")
	}
	early := vmiWaitState{notFoundN: 2}
	if interpretVMINotFound(http.StatusNotFound, "", early) != waitPoll {
		t.Fatal("VMI NotFound before the VM exists is still starting")
	}
	gone := vmiWaitState{notFoundN: 15}
	if interpretVMINotFound(http.StatusNotFound, "", gone) != waitMissing {
		t.Fatal("both gone for ~2m without a guest must Error, not hang")
	}
	if interpretVMINotFound(http.StatusOK, "Unknown", vmiWaitState{sawRunning: true, notFoundN: 3}) != waitExited {
		t.Fatal("NotFound after a guest ran must not poll 4h")
	}

	stale := vmiWaitState{notFoundN: 1}
	if interpretVMINotFound(http.StatusOK, "Stopped", stale) != waitPoll {
		t.Fatal("Stopped before seeing Running must be ignored (stale cached event)")
	}
}

func TestInstallVMEvictionStrategyNone(t *testing.T) {
	vm := installVMManifest("oct-windows-builder", "wb-install-win2k19", "wb-iso-win2k19", "wb-sysprep-win2k19", StartBuildRequest{
		Cores:  2,
		Memory: "4Gi",
	})
	spec, _ := nestedMap(vm, "spec", "template", "spec")["evictionStrategy"].(string)
	if spec != "None" {
		t.Fatalf("evictionStrategy=%q", spec)
	}
}

func TestVmiRunningSince(t *testing.T) {
	obj := map[string]interface{}{
		"status": map[string]interface{}{
			"phase": "Succeeded",
			"phaseTransitionTimestamps": []interface{}{
				map[string]interface{}{"phase": "Running", "phaseTransitionTimestamp": "2026-09-01T19:53:43Z"},
				map[string]interface{}{"phase": "Succeeded", "phaseTransitionTimestamp": "2026-09-01T20:00:30Z"},
			},
		},
	}
	got := vmiRunningSince(obj)
	want, _ := time.Parse(time.RFC3339, "2026-09-01T19:53:43Z")
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
