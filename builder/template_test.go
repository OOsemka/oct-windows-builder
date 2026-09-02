package main

import "testing"

func TestApplyTemplateDataSource(t *testing.T) {
	tpl := map[string]interface{}{
		"parameters": []interface{}{
			map[string]interface{}{"name": "NAME", "value": ""},
			map[string]interface{}{"name": "DATA_SOURCE_NAME", "value": "old"},
			map[string]interface{}{"name": "DATA_SOURCE_NAMESPACE", "value": "kubevirt-os-images"},
		},
	}
	applyTemplateDataSource(tpl, "win2k19", "openshift-virtualization-os-images")
	params, _ := tpl["parameters"].([]interface{})
	got := map[string]string{}
	for _, raw := range params {
		p := raw.(map[string]interface{})
		got[p["name"].(string)] = p["value"].(string)
	}
	if got["DATA_SOURCE_NAME"] != "win2k19" {
		t.Fatalf("DATA_SOURCE_NAME=%q", got["DATA_SOURCE_NAME"])
	}
	if got["DATA_SOURCE_NAMESPACE"] != "openshift-virtualization-os-images" {
		t.Fatalf("DATA_SOURCE_NAMESPACE=%q", got["DATA_SOURCE_NAMESPACE"])
	}

	empty := map[string]interface{}{}
	applyTemplateDataSource(empty, "win11", "openshift-virtualization-os-images")
	params, _ = empty["parameters"].([]interface{})
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(params))
	}
}

func TestStripTemplateSysprep(t *testing.T) {
	tpl := map[string]interface{}{
		"objects": []interface{}{
			map[string]interface{}{
				"kind": "VirtualMachine",
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"domain": map[string]interface{}{
								"devices": map[string]interface{}{
									"disks": []interface{}{
										map[string]interface{}{"name": "rootdisk", "disk": map[string]interface{}{"bus": "sata"}},
										map[string]interface{}{"name": "sysprep", "cdrom": map[string]interface{}{"bus": "sata"}},
									},
								},
							},
							"volumes": []interface{}{
								map[string]interface{}{"name": "rootdisk", "dataVolume": map[string]interface{}{"name": "${NAME}"}},
								map[string]interface{}{"name": "sysprep", "sysprep": map[string]interface{}{"configMap": map[string]interface{}{"name": "wb-sysprep-win2k19"}}},
							},
						},
					},
				},
			},
		},
	}
	stripTemplateSysprep(tpl)
	spec := nestedMap(tpl["objects"].([]interface{})[0].(map[string]interface{}), "spec", "template", "spec")
	vols, _ := spec["volumes"].([]interface{})
	if len(vols) != 1 {
		t.Fatalf("volumes=%v", vols)
	}
	disks, _ := nestedMap(spec, "domain", "devices")["disks"].([]interface{})
	if len(disks) != 1 {
		t.Fatalf("disks=%v", disks)
	}
}

func TestContainerDiskImageFromVirtioWinCM(t *testing.T) {
	img := "registry.redhat.io/container-native-virtualization/virtio-win-rhel9@sha256:abc"
	got := containerDiskImageFromVirtioWinCM(map[string]interface{}{
		"virtio-win-image":              img,
		"virtio-win-image-download-url": "https://hyperconverged-cluster-cli-download.example.invalid/virtio-win/virtio-win.iso",
	})
	if got != img {
		t.Fatalf("got %q", got)
	}
	if containerDiskImageFromVirtioWinCM(map[string]interface{}{
		"virtio-win-image-download-url": "https://example.invalid/virtio-win.iso",
	}) != "" {
		t.Fatal("must ignore HTTP download URLs")
	}
	if isContainerDiskRef("https://example.invalid/virtio-win.iso") {
		t.Fatal("HTTP URL is not a containerDisk")
	}
}

func TestVirtioWinImageFromConfigMapList(t *testing.T) {
	img := "registry.redhat.io/container-native-virtualization/virtio-win-rhel9@sha256:abc"
	obj := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"metadata": map[string]interface{}{"namespace": "other"},
				"data":     map[string]interface{}{"virtio-win-image": "quay.io/example/other-virtio:latest"},
			},
			map[string]interface{}{
				"metadata": map[string]interface{}{"namespace": "openshift-cnv"},
				"data":     map[string]interface{}{"virtio-win-image": img},
			},
		},
	}
	if got := virtioWinImageFromConfigMapList(obj); got != img {
		t.Fatalf("got %q", got)
	}
}
