package main

import "testing"

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
