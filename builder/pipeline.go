package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	k8s               *K8sClient
	workNS            string
	defaultGoldenNS   string
	defaultTemplateNS string
	mu                sync.Mutex
	builds            map[string]*BuildRecord
	running           map[string]bool
}

func NewManager(k8s *K8sClient, workNS, goldenNS, templateNS string) *Manager {
	m := &Manager{
		k8s:               k8s,
		workNS:            workNS,
		defaultGoldenNS:   goldenNS,
		defaultTemplateNS: templateNS,
		builds:            map[string]*BuildRecord{},
		running:           map[string]bool{},
	}
	m.loadFromCluster()
	return m
}

func (m *Manager) List() []BuildRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]BuildRecord, 0, len(m.builds))
	for _, b := range m.builds {
		out = append(out, *b)
	}
	return out
}

func (m *Manager) Start(req StartBuildRequest) (*BuildRecord, error) {
	req.DiskName = strings.TrimSpace(req.DiskName)
	req.ISOURL = strings.TrimSpace(req.ISOURL)
	if !validDiskName(req.DiskName) {
		return nil, fmt.Errorf("diskName must be a DNS-1123 name of at most 50 characters")
	}
	if req.ISOURL == "" {
		return nil, fmt.Errorf("isoURL is required")
	}
	if !strings.HasPrefix(req.ISOURL, "http://") && !strings.HasPrefix(req.ISOURL, "https://") {
		return nil, fmt.Errorf("isoURL must be http or https")
	}
	if strings.TrimSpace(req.Autounattend) == "" {
		return nil, fmt.Errorf("autounattend XML is required")
	}
	if req.DiskSize == "" {
		req.DiskSize = "60Gi"
	}
	if req.ISOSize == "" {
		req.ISOSize = "12Gi"
	}
	if req.Memory == "" {
		req.Memory = "4Gi"
	}
	if req.Cores <= 0 {
		req.Cores = 2
	}
	if req.GoldenNamespace == "" {
		req.GoldenNamespace = m.defaultGoldenNS
	}
	if req.TemplateNamespace == "" {
		req.TemplateNamespace = m.defaultTemplateNS
	}

	m.mu.Lock()
	if m.running[req.DiskName] {
		m.mu.Unlock()
		return nil, fmt.Errorf("a build for %s is already running", req.DiskName)
	}
	m.running[req.DiskName] = true
	rec := &BuildRecord{
		DiskName:          req.DiskName,
		Status:            StatusPending,
		Message:           "Queued",
		ISOHostPath:       sanitizeURLForLog(req.ISOURL),
		TemplateName:      req.TemplateName,
		TemplateNamespace: req.TemplateNamespace,
		GoldenNamespace:   req.GoldenNamespace,
		VirtioImage:       req.VirtioImage,
		StartedAt:         nowRFC3339(),
		UpdatedAt:         nowRFC3339(),
	}
	m.builds[req.DiskName] = rec
	m.mu.Unlock()

	logf("start build disk=%s iso=%s goldenNS=%s template=%s/%s", rec.DiskName, rec.ISOHostPath, rec.GoldenNamespace, rec.TemplateNamespace, rec.TemplateName)
	m.persist(rec)
	go m.run(req)
	return rec, nil
}

func (m *Manager) set(disk string, st BuildStatus, msg string) {
	m.mu.Lock()
	rec := m.builds[disk]
	if rec == nil {
		rec = &BuildRecord{DiskName: disk}
		m.builds[disk] = rec
	}
	rec.Status = st
	rec.Message = msg
	rec.UpdatedAt = nowRFC3339()
	cp := *rec
	m.mu.Unlock()
	logf("build %s → %s: %s", disk, st, msg)
	m.persist(&cp)
}

func (m *Manager) done(disk string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.running, disk)
}

func (m *Manager) persist(rec *BuildRecord) {
	if m.k8s == nil {
		return
	}
	body, _ := json.Marshal(rec)
	cm := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "wb-build-" + rec.DiskName,
			"namespace": m.workNS,
			"labels": map[string]interface{}{
				"app.kubernetes.io/part-of":  "oct-windows-builder",
				"oct-windows-builder/disk":   rec.DiskName,
				"oct-windows-builder/status": string(rec.Status),
			},
		},
		"data": map[string]interface{}{
			"record": string(body),
		},
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/configmaps/%s", m.workNS, "wb-build-"+rec.DiskName)
	existing, code, err := m.k8s.Get(path)
	if err != nil {
		logf("persist get: %v", err)
		return
	}
	if code == http.StatusNotFound {
		if _, err := m.k8s.Create(fmt.Sprintf("/api/v1/namespaces/%s/configmaps", m.workNS), cm); err != nil {
			logf("persist create: %v", err)
		}
		return
	}
	if existing != nil {
		cm["metadata"] = existing["metadata"]
		if meta, ok := cm["metadata"].(map[string]interface{}); ok {
			labels, _ := meta["labels"].(map[string]interface{})
			if labels == nil {
				labels = map[string]interface{}{}
			}
			labels["app.kubernetes.io/part-of"] = "oct-windows-builder"
			labels["oct-windows-builder/disk"] = rec.DiskName
			labels["oct-windows-builder/status"] = string(rec.Status)
			meta["labels"] = labels
		}
		cm["data"] = map[string]interface{}{"record": string(body)}
		if _, err := m.k8s.Put(path, cm); err != nil {
			logf("persist put: %v", err)
		}
	}
}

func (m *Manager) loadFromCluster() {
	if m.k8s == nil {
		return
	}
	obj, code, err := m.k8s.Get(fmt.Sprintf("/api/v1/namespaces/%s/configmaps?labelSelector=app.kubernetes.io/part-of=oct-windows-builder", m.workNS))
	if err != nil || code >= 300 || obj == nil {
		return
	}
	items, _ := obj["items"].([]interface{})
	for _, raw := range items {
		cm, _ := raw.(map[string]interface{})
		data := nestedMap(cm, "data")
		if data == nil {
			continue
		}
		s, _ := data["record"].(string)
		if s == "" {
			continue
		}
		var rec BuildRecord
		if err := json.Unmarshal([]byte(s), &rec); err != nil {
			continue
		}
		if rec.Status == StatusInstalling || rec.Status == StatusSysprep || rec.Status == StatusPending {
			rec.Status = StatusError
			rec.Message = "Builder restarted while the install was in progress. Re-run Start build."
			rec.UpdatedAt = nowRFC3339()
		}
		m.builds[rec.DiskName] = &rec
		m.persist(&rec)
	}
}

func (m *Manager) run(req StartBuildRequest) {
	defer m.done(req.DiskName)
	if m.k8s == nil {
		m.set(req.DiskName, StatusError, "Builder has no in-cluster Kubernetes client")
		return
	}

	disk := req.DiskName
	isoName := "wb-iso-" + disk
	installName := "wb-install-" + disk
	sysprepName := "wb-sysprep-" + disk

	m.set(disk, StatusPending, "Creating Autounattend ConfigMap")
	if err := m.ensureSysprepCM(sysprepName, req.Autounattend); err != nil {
		m.set(disk, StatusError, err.Error())
		return
	}

	m.set(disk, StatusPending, "Importing Windows ISO")
	if err := m.ensureHTTPDataVolume(isoName, req.ISOURL, req.ISOSize, req.StorageClassName); err != nil {
		m.set(disk, StatusError, err.Error())
		return
	}
	if _, err := waitPhase(m.k8s, m.dvPath(m.workNS, isoName), "Succeeded", 2*time.Hour, []string{"Failed"}); err != nil {
		m.set(disk, StatusError, "ISO DataVolume: "+err.Error())
		return
	}

	m.set(disk, StatusPending, "Releasing install VM so the ISO can be patched")
	if err := m.deleteInstallVM(installName); err != nil {
		m.set(disk, StatusError, err.Error())
		return
	}

	m.set(disk, StatusPending, "Patching ISO for unattended EFI boot (no Press any key)")
	if err := m.patchISOForUnattendedEFI(isoName); err != nil {
		m.set(disk, StatusError, "ISO El Torito patch: "+err.Error())
		return
	}

	m.set(disk, StatusPending, "Creating blank install disk")
	if err := m.ensureBlankDataVolume(installName, req.DiskSize, req.StorageClassName); err != nil {
		m.set(disk, StatusError, err.Error())
		return
	}
	if _, err := waitPhase(m.k8s, m.dvPath(m.workNS, installName), "Succeeded", 30*time.Minute, []string{"Failed"}); err != nil {
		m.set(disk, StatusError, "Install DataVolume: "+err.Error())
		return
	}

	m.set(disk, StatusPending, "Creating install VM")
	if err := m.ensureInstallVM(installName, isoName, sysprepName, req); err != nil {
		m.set(disk, StatusError, err.Error())
		return
	}

	m.set(disk, StatusInstalling, "Windows Setup running (unattended)")
	if err := m.waitVMI(installName); err != nil {
		m.set(disk, StatusError, err.Error())
		return
	}

	m.set(disk, StatusPending, "Install VM shut down after sysprep; cloning golden disk")
	_ = m.k8s.Delete(fmt.Sprintf("/apis/kubevirt.io/v1/namespaces/%s/virtualmachines/%s", m.workNS, installName))

	goldenPath := m.dvPath(req.GoldenNamespace, disk)
	existing, code, err := m.k8s.Get(goldenPath)
	if err != nil {
		m.set(disk, StatusError, err.Error())
		return
	}
	if existing != nil && code != http.StatusNotFound {
		logf("replacing existing golden DV %s/%s", req.GoldenNamespace, disk)
		if err := m.k8s.Delete(goldenPath); err != nil {
			m.set(disk, StatusError, "delete old golden DV: "+err.Error())
			return
		}
		for i := 0; i < 60; i++ {
			_, c, _ := m.k8s.Get(goldenPath)
			if c == http.StatusNotFound {
				break
			}
			time.Sleep(2 * time.Second)
		}
	}

	if err := m.ensureCloneDV(req.GoldenNamespace, disk, m.workNS, installName, req.DiskSize, req.StorageClassName); err != nil {
		m.set(disk, StatusError, err.Error())
		return
	}
	if _, err := waitPhase(m.k8s, goldenPath, "Succeeded", 2*time.Hour, []string{"Failed"}); err != nil {
		m.set(disk, StatusError, "golden DataVolume: "+err.Error())
		return
	}

	if err := m.ensureDataSource(req.GoldenNamespace, disk); err != nil {
		m.set(disk, StatusError, "DataSource: "+err.Error())
		return
	}

	if req.TemplateName != "" {
		m.set(disk, StatusPending, "Updating Template")
		if err := m.ensureTemplate(req); err != nil {
			m.set(disk, StatusError, "Template: "+err.Error())
			return
		}
	}

	m.set(disk, StatusReady, "DataVolume "+disk+" Succeeded")
}

func (m *Manager) waitVMI(name string) error {
	path := fmt.Sprintf("/apis/kubevirt.io/v1/namespaces/%s/virtualmachineinstances/%s", m.workNS, name)
	deadline := time.Now().Add(4 * time.Hour)
	var last string
	for time.Now().Before(deadline) {
		obj, code, err := m.k8s.Get(path)
		if err != nil {
			return err
		}
		if code == http.StatusNotFound {
			last = "NotFound"
			time.Sleep(8 * time.Second)
			continue
		}
		phase := nestedString(obj, "status", "phase")
		last = phase
		switch phase {
		case "Succeeded":
			return nil
		case "Failed", "Unknown":
			return fmt.Errorf("VMI %s: %s", phase, conditionMessage(obj))
		case "Running":
			if guestAgentConnected(obj) {
				disk := strings.TrimPrefix(name, "wb-install-")
				m.mu.Lock()
				already := m.builds[disk] != nil && m.builds[disk].Status == StatusSysprep
				m.mu.Unlock()
				if !already {
					m.set(disk, StatusSysprep, "Guest agent connected; waiting for sysprep shutdown")
				}
			}
		}
		time.Sleep(15 * time.Second)
	}
	return fmt.Errorf("timeout waiting for VMI Succeeded (last %s)", last)
}

func (m *Manager) dvPath(ns, name string) string {
	return fmt.Sprintf("/apis/cdi.kubevirt.io/v1beta1/namespaces/%s/datavolumes/%s", ns, name)
}

func (m *Manager) ensureSysprepCM(name, xml string) error {
	path := fmt.Sprintf("/api/v1/namespaces/%s/configmaps/%s", m.workNS, name)
	_, code, err := m.k8s.Get(path)
	if err != nil {
		return err
	}
	if code != http.StatusNotFound {
		_ = m.k8s.Delete(path)
	}
	cm := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": m.workNS,
			"labels": map[string]interface{}{
				"app.kubernetes.io/part-of": "oct-windows-builder",
			},
		},
		"data": sysprepAnswerFiles(xml),
	}
	_, err = m.k8s.Create(fmt.Sprintf("/api/v1/namespaces/%s/configmaps", m.workNS), cm)
	return err
}

// GitOps win2k19 uses lowercase autounattend.xml on a ConfigMap CD.
func sysprepAnswerFiles(xml string) map[string]interface{} {
	return map[string]interface{}{
		"autounattend.xml": xml,
		"Autounattend.xml": xml,
		"unattend.xml":     xml,
	}
}

func storageSpec(size, storageClass string) map[string]interface{} {
	st := map[string]interface{}{
		"accessModes": []interface{}{"ReadWriteOnce"},
		"resources": map[string]interface{}{
			"requests": map[string]interface{}{"storage": size},
		},
	}
	if strings.TrimSpace(storageClass) != "" {
		st["storageClassName"] = storageClass
	}
	return st
}

func (m *Manager) recreateDV(ns, name string, spec map[string]interface{}) error {
	path := m.dvPath(ns, name)
	_, code, err := m.k8s.Get(path)
	if err != nil {
		return err
	}
	if code != http.StatusNotFound {
		if err := m.k8s.Delete(path); err != nil {
			return err
		}
		for i := 0; i < 60; i++ {
			_, c, _ := m.k8s.Get(path)
			if c == http.StatusNotFound {
				break
			}
			time.Sleep(2 * time.Second)
		}
	}
	dv := map[string]interface{}{
		"apiVersion": "cdi.kubevirt.io/v1beta1",
		"kind":       "DataVolume",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": ns,
			"labels": map[string]interface{}{
				"app.kubernetes.io/part-of": "oct-windows-builder",
			},
			"annotations": map[string]interface{}{
				"cdi.kubevirt.io/storage.bind.immediate.requested": "true",
			},
		},
		"spec": spec,
	}
	_, err = m.k8s.Create(fmt.Sprintf("/apis/cdi.kubevirt.io/v1beta1/namespaces/%s/datavolumes", ns), dv)
	return err
}

func (m *Manager) ensureHTTPDataVolume(name, isoURL, size, sc string) error {
	path := m.dvPath(m.workNS, name)
	existing, code, err := m.k8s.Get(path)
	if err != nil {
		return err
	}
	if existing != nil && code != http.StatusNotFound && dvSucceeded(existing) && dvHTTPURL(existing) == isoURL {
		logf("reusing ISO DataVolume %s", name)
		return nil
	}
	return m.recreateDV(m.workNS, name, map[string]interface{}{
		"source": map[string]interface{}{
			"http": map[string]interface{}{"url": isoURL},
		},
		"storage": storageSpec(size, sc),
	})
}

func (m *Manager) ensureBlankDataVolume(name, size, sc string) error {
	path := m.dvPath(m.workNS, name)
	existing, code, err := m.k8s.Get(path)
	if err != nil {
		return err
	}
	if existing != nil && code != http.StatusNotFound && dvSucceeded(existing) {
		logf("reusing blank install DataVolume %s", name)
		return nil
	}
	return m.recreateDV(m.workNS, name, map[string]interface{}{
		"source":  map[string]interface{}{"blank": map[string]interface{}{}},
		"storage": storageSpec(size, sc),
	})
}

func (m *Manager) ensureCloneDV(ns, name, srcNS, srcName, size, sc string) error {
	return m.recreateDV(ns, name, map[string]interface{}{
		"source": map[string]interface{}{
			"pvc": map[string]interface{}{
				"namespace": srcNS,
				"name":      srcName,
			},
		},
		"storage": storageSpec(size, sc),
	})
}

func (m *Manager) ensureInstallVM(vmName, isoName, sysprepName string, req StartBuildRequest) error {
	path := fmt.Sprintf("/apis/kubevirt.io/v1/namespaces/%s/virtualmachines/%s", m.workNS, vmName)
	_, code, err := m.k8s.Get(path)
	if err != nil {
		return err
	}
	if code != http.StatusNotFound {
		_ = m.k8s.Delete(path)
		time.Sleep(3 * time.Second)
	}

	disks := installVMDisks(req.VirtioImage)
	volumes := []interface{}{
		map[string]interface{}{
			"name":       "rootdisk",
			"dataVolume": map[string]interface{}{"name": vmName},
		},
		map[string]interface{}{
			"name":       "installiso",
			"dataVolume": map[string]interface{}{"name": isoName},
		},
	}
	if strings.TrimSpace(req.VirtioImage) != "" {
		volumes = append(volumes, map[string]interface{}{
			"name":          "virtio",
			"containerDisk": map[string]interface{}{"image": req.VirtioImage},
		})
	}
	// GitOps win2k19 attaches the answer file as a ConfigMap CD-ROM (not
	// volumes[].sysprep). Same ConfigMap; Setup reads autounattend.xml on that CD.
	volumes = append(volumes, map[string]interface{}{
		"name":      "sysprep",
		"configMap": map[string]interface{}{"name": sysprepName},
	})

	vm := map[string]interface{}{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachine",
		"metadata": map[string]interface{}{
			"name":      vmName,
			"namespace": m.workNS,
			"labels": map[string]interface{}{
				"app.kubernetes.io/part-of": "oct-windows-builder",
				"app":                       "windows-install",
			},
		},
		"spec": map[string]interface{}{
			"runStrategy": "RerunOnFailure",
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"kubevirt.io/domain": vmName,
					},
				},
				"spec": map[string]interface{}{
					"domain": map[string]interface{}{
						"cpu": map[string]interface{}{"cores": req.Cores},
						"firmware": map[string]interface{}{
							"bootloader": map[string]interface{}{
								"efi": map[string]interface{}{"secureBoot": false},
							},
						},
						"features": map[string]interface{}{
							"acpi": map[string]interface{}{},
							"smm":  map[string]interface{}{"enabled": true},
						},
						"devices": map[string]interface{}{
							"disks": disks,
							"interfaces": []interface{}{
								map[string]interface{}{"name": "default", "masquerade": map[string]interface{}{}},
							},
							"inputs": []interface{}{
								map[string]interface{}{"name": "tablet", "type": "tablet", "bus": "usb"},
							},
							"tpm": map[string]interface{}{"persistent": false},
						},
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{"memory": req.Memory},
						},
					},
					"networks": []interface{}{
						map[string]interface{}{"name": "default", "pod": map[string]interface{}{}},
					},
					"terminationGracePeriodSeconds": 60,
					"volumes":                       volumes,
				},
			},
		},
	}
	_, err = m.k8s.Create(fmt.Sprintf("/apis/kubevirt.io/v1/namespaces/%s/virtualmachines", m.workNS), vm)
	return err
}

// installVMDisks: empty SATA disk bootOrder 1 (firmware skips until Setup
// writes Boot Manager — required with EFI noprompt; GitOps BIOS used ISO
// bootOrder 1 instead). ISO CD bootOrder 2. virtio/answer-file CDs are not bootable.
// Answer file is a ConfigMap CD like GitOps win2k19 (not a floppy).
func installVMDisks(virtioImage string) []interface{} {
	disks := []interface{}{
		map[string]interface{}{
			"name":      "rootdisk",
			"disk":      map[string]interface{}{"bus": "sata"},
			"bootOrder": 1,
		},
		map[string]interface{}{
			"name":      "installiso",
			"cdrom":     map[string]interface{}{"bus": "sata"},
			"bootOrder": 2,
		},
	}
	if strings.TrimSpace(virtioImage) != "" {
		disks = append(disks, map[string]interface{}{
			"name":  "virtio",
			"cdrom": map[string]interface{}{"bus": "sata"},
		})
	}
	return append(disks, map[string]interface{}{
		"name":  "sysprep",
		"cdrom": map[string]interface{}{"bus": "sata"},
	})
}

func (m *Manager) ensureDataSource(ns, name string) error {
	path := fmt.Sprintf("/apis/cdi.kubevirt.io/v1beta1/namespaces/%s/datasources/%s", ns, name)
	ds := map[string]interface{}{
		"apiVersion": "cdi.kubevirt.io/v1beta1",
		"kind":       "DataSource",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": ns,
			"labels": map[string]interface{}{
				"app.kubernetes.io/part-of": "oct-windows-builder",
			},
		},
		"spec": map[string]interface{}{
			"source": map[string]interface{}{
				"pvc": map[string]interface{}{
					"name":      name,
					"namespace": ns,
				},
			},
		},
	}
	existing, code, err := m.k8s.Get(path)
	if err != nil {
		return err
	}
	if code == http.StatusNotFound {
		_, err = m.k8s.Create(fmt.Sprintf("/apis/cdi.kubevirt.io/v1beta1/namespaces/%s/datasources", ns), ds)
		return err
	}
	if existing != nil {
		ds["metadata"] = existing["metadata"]
		ds["spec"] = map[string]interface{}{
			"source": map[string]interface{}{
				"pvc": map[string]interface{}{"name": name, "namespace": ns},
			},
		}
		_, err = m.k8s.Put(path, ds)
		return err
	}
	return nil
}

func (m *Manager) ensureTemplate(req StartBuildRequest) error {
	ns := req.TemplateNamespace
	name := req.TemplateName
	path := fmt.Sprintf("/apis/template.openshift.io/v1/namespaces/%s/templates/%s", ns, name)
	existing, code, err := m.k8s.Get(path)
	if err != nil {
		return err
	}
	if existing != nil && code != http.StatusNotFound && !req.CustomTemplate {
		applyTemplateDataSource(existing, req.DiskName, req.GoldenNamespace)
		_, err = m.k8s.Put(path, existing)
		if err != nil {
			return err
		}
		logf("updated Template %s/%s DATA_SOURCE_NAME=%s", ns, name, req.DiskName)
		return nil
	}
	tpl := windowsVMTemplate(ns, name, req.GoldenNamespace, req.DiskName, req.DiskSize)
	if existing != nil && code != http.StatusNotFound {
		tpl["metadata"] = existing["metadata"]
		_, err = m.k8s.Put(path, tpl)
		return err
	}
	_, err = m.k8s.Create(fmt.Sprintf("/apis/template.openshift.io/v1/namespaces/%s/templates", ns), tpl)
	return err
}

func applyTemplateDataSource(tpl map[string]interface{}, disk, goldenNS string) {
	params, _ := tpl["parameters"].([]interface{})
	if params == nil {
		params = []interface{}{}
	}
	foundName, foundNS := false, false
	for _, raw := range params {
		p, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := p["name"].(string)
		switch name {
		case "DATA_SOURCE_NAME":
			p["value"] = disk
			foundName = true
		case "DATA_SOURCE_NAMESPACE":
			p["value"] = goldenNS
			foundNS = true
		}
	}
	if !foundName {
		params = append(params, map[string]interface{}{
			"name":        "DATA_SOURCE_NAME",
			"description": "Name of the DataSource to clone",
			"value":       disk,
		})
	}
	if !foundNS {
		params = append(params, map[string]interface{}{
			"name":        "DATA_SOURCE_NAMESPACE",
			"description": "Namespace of the DataSource",
			"value":       goldenNS,
		})
	}
	tpl["parameters"] = params
}

func windowsVMTemplate(ns, name, goldenNS, disk, size string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "template.openshift.io/v1",
		"kind":       "Template",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": ns,
			"labels": map[string]interface{}{
				"template.kubevirt.io/type":       "vm",
				"app.kubernetes.io/managed-by":    "oct-windows-builder",
				"os.template.kubevirt.io/" + disk: "true",
			},
			"annotations": map[string]interface{}{
				"openshift.io/display-name": "Windows (" + disk + ") — OCT Windows Builder",
				"description":               "Community golden image. Not officially supported by Red Hat.",
				"tags":                      "kubevirt,windows",
				"iconClass":                 "icon-windows",
			},
		},
		"objects": []interface{}{
			map[string]interface{}{
				"apiVersion": "kubevirt.io/v1",
				"kind":       "VirtualMachine",
				"metadata": map[string]interface{}{
					"name": "${NAME}",
				},
				"spec": map[string]interface{}{
					"running": false,
					"dataVolumeTemplates": []interface{}{
						map[string]interface{}{
							"metadata": map[string]interface{}{"name": "${NAME}"},
							"spec": map[string]interface{}{
								"source": map[string]interface{}{
									"pvc": map[string]interface{}{
										"namespace": goldenNS,
										"name":      disk,
									},
								},
								"storage": map[string]interface{}{
									"resources": map[string]interface{}{
										"requests": map[string]interface{}{"storage": size},
									},
								},
							},
						},
					},
					"template": map[string]interface{}{
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{"kubevirt.io/vm": "${NAME}"},
						},
						"spec": map[string]interface{}{
							"domain": map[string]interface{}{
								"cpu": map[string]interface{}{"cores": 2},
								"devices": map[string]interface{}{
									"disks": []interface{}{
										map[string]interface{}{"name": "rootdisk", "disk": map[string]interface{}{"bus": "sata"}},
									},
									"interfaces": []interface{}{
										map[string]interface{}{"name": "default", "masquerade": map[string]interface{}{}},
									},
								},
								"resources": map[string]interface{}{
									"requests": map[string]interface{}{"memory": "4Gi"},
								},
							},
							"networks": []interface{}{
								map[string]interface{}{"name": "default", "pod": map[string]interface{}{}},
							},
							"volumes": []interface{}{
								map[string]interface{}{"name": "rootdisk", "dataVolume": map[string]interface{}{"name": "${NAME}"}},
							},
						},
					},
				},
			},
		},
		"parameters": []interface{}{
			map[string]interface{}{"name": "NAME", "required": true},
		},
	}
}
