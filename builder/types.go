package main

import "time"

type BuildStatus string

const (
	StatusPending    BuildStatus = "Pending"
	StatusInstalling BuildStatus = "Installing"
	StatusSysprep    BuildStatus = "Sysprep"
	StatusReady      BuildStatus = "Ready"
	StatusError      BuildStatus = "Error"
	StatusCancelled  BuildStatus = "Cancelled"
)

type StartBuildRequest struct {
	DiskName          string `json:"diskName"`
	ISOURL            string `json:"isoURL"`
	Autounattend      string `json:"autounattend"`
	StorageClassName  string `json:"storageClassName,omitempty"`
	DiskSize          string `json:"diskSize,omitempty"`
	ISOSize           string `json:"isoSize,omitempty"`
	GoldenNamespace   string `json:"goldenNamespace,omitempty"`
	TemplateName      string `json:"templateName,omitempty"`
	TemplateNamespace string `json:"templateNamespace,omitempty"`
	CustomTemplate    bool   `json:"customTemplate"`
	VirtioImage       string `json:"virtioImage,omitempty"`
	Memory            string `json:"memory,omitempty"`
	Cores             int    `json:"cores,omitempty"`
}

type BuildRecord struct {
	DiskName          string      `json:"diskName"`
	Status            BuildStatus `json:"status"`
	Message           string      `json:"message"`
	ISOHostPath       string      `json:"isoHostPath,omitempty"`
	TemplateName      string      `json:"templateName,omitempty"`
	TemplateNamespace string      `json:"templateNamespace,omitempty"`
	GoldenNamespace   string      `json:"goldenNamespace,omitempty"`
	VirtioImage       string      `json:"virtioImage,omitempty"`
	// Fields a restarted builder needs to resume a build.
	DiskSize         string `json:"diskSize,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	CustomTemplate   bool   `json:"customTemplate,omitempty"`
	VMCreatedAt      string `json:"vmCreatedAt,omitempty"`
	StartedAt        string `json:"startedAt,omitempty"`
	UpdatedAt        string `json:"updatedAt,omitempty"`
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
