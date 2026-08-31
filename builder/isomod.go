package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const eltoritoAnnotation = "oct-windows-builder/eltorito"

func (m *Manager) patchISOForUnattendedEFI(isoName string) error {
	if m.alreadyNoprompt(isoName) {
		logf("ISO %s already annotated noprompt", isoName)
		return nil
	}
	img, err := m.builderImage()
	if err != nil {
		return err
	}
	jobName := "wb-noprompt-" + isoName
	if strings.HasPrefix(isoName, "wb-iso-") {
		jobName = "wb-noprompt-" + strings.TrimPrefix(isoName, "wb-iso-")
	}
	if len(jobName) > 63 {
		jobName = jobName[:63]
		jobName = strings.Trim(jobName, "-")
	}

	jobPath := fmt.Sprintf("/apis/batch/v1/namespaces/%s/jobs/%s", m.workNS, jobName)
	_ = m.k8s.Delete(jobPath)
	for i := 0; i < 30; i++ {
		_, code, _ := m.k8s.Get(jobPath)
		if code == http.StatusNotFound {
			break
		}
		time.Sleep(2 * time.Second)
	}

	mode := m.pvcVolumeMode(isoName)
	job := m.nopromptJob(jobName, isoName, img, mode)
	if _, err := m.k8s.Create(fmt.Sprintf("/apis/batch/v1/namespaces/%s/jobs", m.workNS), job); err != nil {
		return fmt.Errorf("create ISO patch Job: %w", err)
	}
	if err := m.waitJob(jobName, 15*time.Minute); err != nil {
		return err
	}
	m.annotateDV(isoName, eltoritoAnnotation, "noprompt")
	return nil
}

func (m *Manager) alreadyNoprompt(isoName string) bool {
	obj, code, err := m.k8s.Get(m.dvPath(m.workNS, isoName))
	if err != nil || code == http.StatusNotFound || obj == nil {
		return false
	}
	meta, _ := obj["metadata"].(map[string]interface{})
	if meta == nil {
		return false
	}
	anns, _ := meta["annotations"].(map[string]interface{})
	v, _ := anns[eltoritoAnnotation].(string)
	return v == "noprompt"
}

func (m *Manager) pvcVolumeMode(name string) string {
	obj, code, err := m.k8s.Get(fmt.Sprintf("/api/v1/namespaces/%s/persistentvolumeclaims/%s", m.workNS, name))
	if err != nil || code == http.StatusNotFound || obj == nil {
		return "Filesystem"
	}
	mode := nestedString(obj, "spec", "volumeMode")
	if mode == "" {
		return "Filesystem"
	}
	return mode
}

func (m *Manager) builderImage() (string, error) {
	if img := strings.TrimSpace(os.Getenv("BUILDER_IMAGE")); img != "" {
		return img, nil
	}
	pod := strings.TrimSpace(os.Getenv("POD_NAME"))
	if pod == "" {
		h, _ := os.Hostname()
		pod = h
	}
	if pod == "" {
		return "", fmt.Errorf("cannot determine builder image (set POD_NAME)")
	}
	obj, code, err := m.k8s.Get(fmt.Sprintf("/api/v1/namespaces/%s/pods/%s", m.workNS, pod))
	if err != nil {
		return "", err
	}
	if code == http.StatusNotFound || obj == nil {
		return "", fmt.Errorf("builder pod %s not found", pod)
	}
	spec := nestedMap(obj, "spec")
	if spec == nil {
		return "", fmt.Errorf("builder pod has no spec")
	}
	containers, _ := spec["containers"].([]interface{})
	for _, raw := range containers {
		c, _ := raw.(map[string]interface{})
		if c == nil {
			continue
		}
		name, _ := c["name"].(string)
		img, _ := c["image"].(string)
		if img != "" && (name == "windows-builder" || len(containers) == 1) {
			return img, nil
		}
	}
	return "", fmt.Errorf("builder pod has no container image")
}

func (m *Manager) nopromptJob(jobName, pvcName, image, volumeMode string) map[string]interface{} {
	isoArg := "/iso"
	var volumes []interface{}
	var mounts []interface{}
	var devices []interface{}
	if volumeMode == "Block" {
		isoArg = "/dev/iso"
		devices = []interface{}{
			map[string]interface{}{"name": "iso", "devicePath": "/dev/iso"},
		}
		volumes = []interface{}{
			map[string]interface{}{
				"name": "iso",
				"persistentVolumeClaim": map[string]interface{}{
					"claimName": pvcName,
				},
			},
		}
	} else {
		mounts = []interface{}{
			map[string]interface{}{"name": "iso", "mountPath": "/iso"},
		}
		volumes = []interface{}{
			map[string]interface{}{
				"name": "iso",
				"persistentVolumeClaim": map[string]interface{}{
					"claimName": pvcName,
				},
			},
		}
	}
	container := map[string]interface{}{
		"name":            "modify-iso",
		"image":           image,
		"imagePullPolicy": "IfNotPresent",
		"args":            []interface{}{"--modify-iso", isoArg},
		"securityContext": map[string]interface{}{
			"allowPrivilegeEscalation": false,
			"runAsNonRoot":             true,
			"seccompProfile":           map[string]interface{}{"type": "RuntimeDefault"},
			"capabilities":             map[string]interface{}{"drop": []interface{}{"ALL"}},
		},
		"resources": map[string]interface{}{
			"requests": map[string]interface{}{"cpu": "50m", "memory": "64Mi"},
			"limits":   map[string]interface{}{"cpu": "1", "memory": "256Mi"},
		},
	}
	if len(mounts) > 0 {
		container["volumeMounts"] = mounts
	}
	if len(devices) > 0 {
		container["volumeDevices"] = devices
	}
	return map[string]interface{}{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]interface{}{
			"name":      jobName,
			"namespace": m.workNS,
			"labels": map[string]interface{}{
				"app.kubernetes.io/part-of": "oct-windows-builder",
				"oct-windows-builder/iso":   pvcName,
			},
		},
		"spec": map[string]interface{}{
			"backoffLimit":            1,
			"ttlSecondsAfterFinished": 1800,
			"activeDeadlineSeconds":   900,
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"app.kubernetes.io/part-of": "oct-windows-builder",
					},
				},
				"spec": map[string]interface{}{
					"restartPolicy":      "Never",
					"serviceAccountName": "windows-builder",
					"securityContext": map[string]interface{}{
						"runAsNonRoot":   true,
						"seccompProfile": map[string]interface{}{"type": "RuntimeDefault"},
					},
					"containers": []interface{}{container},
					"volumes":    volumes,
				},
			},
		},
	}
}

func (m *Manager) waitJob(name string, timeout time.Duration) error {
	path := fmt.Sprintf("/apis/batch/v1/namespaces/%s/jobs/%s", m.workNS, name)
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		obj, code, err := m.k8s.Get(path)
		if err != nil {
			return err
		}
		if code == http.StatusNotFound {
			last = "NotFound"
			time.Sleep(3 * time.Second)
			continue
		}
		st := nestedMap(obj, "status")
		if st != nil {
			if n, ok := asInt(st["succeeded"]); ok && n >= 1 {
				return nil
			}
			if n, ok := asInt(st["failed"]); ok && n >= 1 {
				msg := m.jobLogSnippet(name)
				return fmt.Errorf("ISO patch Job failed: %s", msg)
			}
			last = nestedString(obj, "status", "conditions")
			if last == "" {
				last = "Pending"
			}
		}
		time.Sleep(4 * time.Second)
	}
	return fmt.Errorf("timeout waiting for ISO patch Job (last %s)", last)
}

func (m *Manager) jobLogSnippet(jobName string) string {
	sel := url.QueryEscape("job-name=" + jobName)
	obj, _, err := m.k8s.Get(fmt.Sprintf("/api/v1/namespaces/%s/pods?labelSelector=%s", m.workNS, sel))
	if err != nil || obj == nil {
		return "(no pod logs)"
	}
	items, _ := obj["items"].([]interface{})
	if len(items) == 0 {
		return "(no pods)"
	}
	pod, _ := items[len(items)-1].(map[string]interface{})
	meta, _ := pod["metadata"].(map[string]interface{})
	name, _ := meta["name"].(string)
	if name == "" {
		return "(unnamed pod)"
	}
	data, code, err := m.k8s.GetBytes(fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log?tailLines=40", m.workNS, name))
	if err != nil || code >= 300 {
		return fmt.Sprintf("pod %s log unavailable", name)
	}
	s := strings.TrimSpace(string(data))
	s = redactAutounattend(s)
	return truncate(s, 800)
}

func (m *Manager) annotateDV(name, key, val string) {
	path := m.dvPath(m.workNS, name)
	body := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]interface{}{key: val},
		},
	}
	if err := m.k8s.Patch(path, body); err != nil {
		logf("annotate DV %s: %v", name, err)
	}
}

func (m *Manager) deleteInstallVM(name string) error {
	vmPath := fmt.Sprintf("/apis/kubevirt.io/v1/namespaces/%s/virtualmachines/%s", m.workNS, name)
	vmiPath := fmt.Sprintf("/apis/kubevirt.io/v1/namespaces/%s/virtualmachineinstances/%s", m.workNS, name)
	_ = m.k8s.Delete(vmPath)
	_ = m.k8s.Delete(vmiPath)
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		_, vmCode, _ := m.k8s.Get(vmPath)
		_, vmiCode, _ := m.k8s.Get(vmiPath)
		if vmCode == http.StatusNotFound && vmiCode == http.StatusNotFound {
			time.Sleep(5 * time.Second)
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("timeout deleting install VM %s", name)
}

func asInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

func redactAutounattend(s string) string {
	low := strings.ToLower(s)
	if strings.Contains(low, "autounattend") || strings.Contains(low, "<unattend") || strings.Contains(low, "password") {
		return "(redacted builder log)"
	}
	return s
}

func dvHTTPURL(obj map[string]interface{}) string {
	return nestedString(obj, "spec", "source", "http", "url")
}

func dvSucceeded(obj map[string]interface{}) bool {
	return nestedString(obj, "status", "phase") == "Succeeded"
}
