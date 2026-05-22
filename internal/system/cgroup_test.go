package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureManagedCgroupEnablesCPUOnIntermediateParents(t *testing.T) {
	root := t.TempDir()
	writeFakeCgroupFiles(t, filepath.Join(root, "cpuguard"))
	writeFakeCgroupFiles(t, filepath.Join(root, "cpuguard", "rule"))

	manager := &CgroupManager{Root: root}
	fullPath, err := manager.ensureManagedCgroup("/cpuguard/rule/123")
	if err != nil {
		t.Fatalf("ensureManagedCgroup returned error: %v", err)
	}
	if fullPath != filepath.Join(root, "cpuguard", "rule", "123") {
		t.Fatalf("managed path = %q", fullPath)
	}
	assertFileContains(t, filepath.Join(root, "cpuguard", "cgroup.subtree_control"), "+cpu\n")
	assertFileContains(t, filepath.Join(root, "cpuguard", "rule", "cgroup.subtree_control"), "+cpu\n")
	if _, err := os.Stat(filepath.Join(root, "cpuguard", "rule", "123")); err != nil {
		t.Fatalf("leaf cgroup was not created: %v", err)
	}
}

func TestEnableCPUControllerSkipsAlreadyEnabledController(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cgroup.controllers"), []byte("cpu memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup.subtree_control"), []byte("cpu\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := enableCPUController(dir); err != nil {
		t.Fatalf("enableCPUController returned error: %v", err)
	}
	assertFileContains(t, filepath.Join(dir, "cgroup.subtree_control"), "cpu\n")
}

func writeFakeCgroupFiles(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup.controllers"), []byte("cpuset cpu io memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup.subtree_control"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}
