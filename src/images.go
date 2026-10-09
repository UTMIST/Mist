package main

import (
	"net/http"
	"sort"
)

type ImageOption struct {
	Reference    string   `json:"reference"`
	Accelerators []string `json:"accelerators"`
}

type ComputeProfile struct {
	Accelerator  string `json:"accelerator"`
	DefaultImage string `json:"default_image"`
	DeviceUnit   string `json:"device_unit"`
	MaxDevices   int    `json:"max_devices"`
	Note         string `json:"note,omitempty"`
}

type ImageCatalog struct {
	Images       []ImageOption    `json:"images"`
	Profiles     []ComputeProfile `json:"profiles"`
	Registries   []string         `json:"registries,omitempty"`
	SelfService  bool             `json:"self_service"`
	TeamsEnabled bool             `json:"teams_enabled"`
}

// Accelerator lists describe server policy, not a guarantee that an image's
// code supports that hardware. Team TT jobs can select a compatible container
// runtime or the tested read-only host runtime profile.
func (e *KubernetesExecutor) imageCatalog() ImageCatalog {
	catalog := ImageCatalog{Images: []ImageOption{}, Profiles: []ComputeProfile{
		{Accelerator: "cpu", DefaultImage: cpuImage, DeviceUnit: "none", MaxDevices: 0},
		{Accelerator: "nvidia", DefaultImage: nvidiaImage, DeviceUnit: "GPU", MaxDevices: 2},
		{Accelerator: "tenstorrent", DefaultImage: ttImage, DeviceUnit: "board", MaxDevices: 4,
			Note: "One n300 board has two chips. This pilot uses the tested image and installed host TT runtime."},
	}}
	for ref := range e.allowedImages {
		accelerators := []string{"cpu", "nvidia"}
		if ref == ttImage {
			accelerators = append(accelerators, "tenstorrent")
		}
		catalog.Images = append(catalog.Images, ImageOption{ref, accelerators})
	}
	sort.Slice(catalog.Images, func(i, j int) bool { return catalog.Images[i].Reference < catalog.Images[j].Reference })
	return catalog
}

func (a *App) images(w http.ResponseWriter, r *http.Request) {
	e := a.requestExecutor(r)
	catalog := e.imageCatalog()
	catalog.TeamsEnabled = a.teams != nil
	if e.team != nil {
		catalog.SelfService = true
		catalog.Registries = e.team.Team.Policy.Registries
	}
	writeJSON(w, http.StatusOK, catalog)
}
