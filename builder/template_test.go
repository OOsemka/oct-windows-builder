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
