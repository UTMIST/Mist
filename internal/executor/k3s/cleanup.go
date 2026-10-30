package k3s

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var podUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// VerifyCleanup runs on the node, as root, and checks that kubelet has
// finished with a Pod: its pod directory (which holds emptyDir scratch and
// volume mounts) is gone, and no cgroup for it remains, so none of its
// processes can still run or hold devices. It only reads; it never deletes
// anything, and it accepts only a well-formed UID, never a path.
//
// Device-plugin release is implied by the Pod's removal from kubelet; TT
// boards (M4) additionally need a reset receipt.
func VerifyCleanup(kubeletRoot, cgroupRoot, podUID string) (string, error) {
	if !podUIDPattern.MatchString(podUID) {
		return "", fmt.Errorf("cleanup: %q is not a Pod UID", podUID)
	}
	podDir := filepath.Join(kubeletRoot, "pods", podUID)
	if _, err := os.Lstat(podDir); err == nil {
		return "", fmt.Errorf("cleanup: %s still exists", podDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(kubeletRoot, "pods")); err != nil {
		return "", fmt.Errorf("cleanup: kubelet root %s unreadable: %w", kubeletRoot, err)
	}

	// cgroupfs driver: .../kubepods/[burstable/]pod<uid>
	// systemd driver:  .../kubepods-[burstable-]pod<uid_with_underscores>.slice
	names := []string{"pod" + podUID, "pod" + strings.ReplaceAll(podUID, "-", "_")}
	var found string
	err := filepath.WalkDir(cgroupRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // a cgroup vanishing mid-walk is fine
		}
		if !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(cgroupRoot, p)
		if strings.Count(rel, string(filepath.Separator)) > 4 {
			return filepath.SkipDir // pod cgroups sit a few levels down at most
		}
		for _, n := range names {
			if strings.Contains(d.Name(), n) {
				found = p
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found != "" {
		return "", fmt.Errorf("cleanup: cgroup %s still exists", found)
	}
	return fmt.Sprintf("kubelet pod directory and cgroups for Pod %s are gone", podUID), nil
}
