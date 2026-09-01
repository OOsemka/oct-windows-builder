package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestInstallVMDisksBootOrder(t *testing.T) {
	disks := installVMDisks("")
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
	withVirtio := installVMDisks("quay.io/example/virtio-win:latest")
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
	if d["autounattend.xml"] != "<unattend/>" || d["Autounattend.xml"] != "<unattend/>" || d["unattend.xml"] != "<unattend/>" {
		t.Fatalf("%v", d)
	}
}

func TestEvaluateGuestExit(t *testing.T) {
	tooShort := evaluateGuestExit(guestExitEvidence{Uptime: 7 * time.Minute})
	if tooShort == nil || !strings.Contains(tooShort.Error(), "guest shut down before install finished") {
		t.Fatalf("7m failed Setup must be Error, got %v", tooShort)
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: 45 * time.Minute}); err != nil {
		t.Fatalf("long uptime should clone: %v", err)
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: 3 * time.Minute, SawAgent: true}); err != nil {
		t.Fatalf("guest agent means FirstLogon ran: %v", err)
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: 16 * time.Minute, SawGuestOS: true}); err != nil {
		t.Fatalf("guest OS after 15m is Setup evidence: %v", err)
	}
	if err := evaluateGuestExit(guestExitEvidence{Uptime: 2 * time.Minute, SawGuestOS: true}); err == nil {
		t.Fatal("guest OS with 2m uptime is still too short")
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
	if interpretVMINotFound(http.StatusNotFound, "", ran) != waitExited {
		t.Fatal("VM+VMI NotFound after a running guest must not poll 4h")
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
