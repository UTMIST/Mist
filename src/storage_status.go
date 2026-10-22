package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Usage is sampled on the owning storage host; HTTP handlers never walk NFS
// trees. Numbers are logical file bytes and may lag active writes by 30 seconds.
type teamStorageStatus struct {
	State             string `json:"state"`
	Layout            string `json:"layout"`
	DatasetUsedBytes  int64  `json:"dataset_used_bytes"`
	ModelUsedBytes    int64  `json:"model_used_bytes"`
	UsageObservedUnix int64  `json:"usage_observed_unix"`
}

func (s *TeamService) storageStatus(id string) (teamStorageStatus, error) {
	var status teamStorageStatus
	data, err := os.ReadFile(filepath.Join(s.app.executor.storage.root, "metadata/provision-status", id+".json"))
	if err != nil {
		return status, err
	}
	if err = json.Unmarshal(data, &status); err != nil {
		return status, err
	}
	if status.State != "Ready" || status.Layout != sharedFolderLayout || status.DatasetUsedBytes < 0 || status.ModelUsedBytes < 0 || status.UsageObservedUnix <= 0 {
		return status, errors.New("team storage usage is unavailable")
	}
	return status, nil
}

func (s teamStorageStatus) observedAt() string {
	return time.Unix(s.UsageObservedUnix, 0).UTC().Format(time.RFC3339)
}
