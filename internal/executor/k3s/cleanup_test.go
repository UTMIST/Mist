package k3s

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCleanup(t *testing.T) {
	uid := "0b0e6a5c-1f2e-4d3c-9b8a-7f6e5d4c3b2a"
	kubelet, cgroups := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(kubelet, "pods", uid, "volumes"), 0o755)
	os.MkdirAll(filepath.Join(cgroups, "kubepods", "pod"+uid), 0o755)

	if _, err := VerifyCleanup(kubelet, cgroups, uid); err == nil {
		t.Fatal("pod directory present, cleanup confirmed")
	}
	os.RemoveAll(filepath.Join(kubelet, "pods", uid))
	if _, err := VerifyCleanup(kubelet, cgroups, uid); err == nil || !strings.Contains(err.Error(), "cgroup") {
		t.Fatalf("cgroup present: %v", err)
	}
	os.RemoveAll(filepath.Join(cgroups, "kubepods"))
	os.MkdirAll(filepath.Join(cgroups, "kubepods.slice", "kubepods-pod"+strings.ReplaceAll(uid, "-", "_")+".slice"), 0o755)
	if _, err := VerifyCleanup(kubelet, cgroups, uid); err == nil {
		t.Fatal("systemd-style cgroup missed")
	}
	os.RemoveAll(filepath.Join(cgroups, "kubepods.slice"))
	if ev, err := VerifyCleanup(kubelet, cgroups, uid); err != nil || ev == "" {
		t.Fatalf("clean node: %q %v", ev, err)
	}
	for _, bad := range []string{"../../etc", "", uid + "/x"} {
		if _, err := VerifyCleanup(kubelet, cgroups, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := VerifyCleanup(filepath.Join(kubelet, "missing"), cgroups, uid); err == nil {
		t.Error("unreadable kubelet root treated as proof")
	}
}
