package cgroups_test

import (
	"os"
	"path/filepath"
	"testing"

	"mini-container/container/cgroups"
	v1 "mini-container/container/cgroups/v1"
	v2 "mini-container/container/cgroups/v2"
)

func TestNewManagerAtReturnsV2ManagerWhenRootHasCgroupControllers(t *testing.T) {
	root := t.TempDir()
	// cgroup v2 exposes cgroup.controllers at the hierarchy root.
	mustWriteFile(t, filepath.Join(root, "cgroup.controllers"), "memory cpu pids")

	manager, err := cgroups.NewManagerAt(root, "mini-container/test")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := manager.(*v2.Manager); !ok {
		t.Fatalf("expected *v2.Manager, got %T", manager)
	}
}

func TestNewManagerAtReturnsV1ManagerWhenRootHasControllerDirs(t *testing.T) {
	root := t.TempDir()
	// cgroup v1 uses controller-specific hierarchies.
	for _, controller := range []string{"memory", "cpu", "pids"} {
		mustMkdir(t, filepath.Join(root, controller))
	}

	manager, err := cgroups.NewManagerAt(root, "mini-container/test")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := manager.(*v1.Manager); !ok {
		t.Fatalf("expected *v1.Manager, got %T", manager)
	}
}

func TestNewManagerAtRejectsUnsupportedRoot(t *testing.T) {
	root := t.TempDir()

	_, err := cgroups.NewManagerAt(root, "mini-container/test")
	if err == nil {
		t.Fatal("expected error for unsupported cgroup root")
	}
}

func TestNewManagerAtRejectsInvalidName(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "cgroup.controllers"), "memory cpu pids")

	for _, name := range []string{"", " ", "/mini-container/test", "..", "../test", "test/.."} {
		t.Run(name, func(t *testing.T) {
			_, err := cgroups.NewManagerAt(root, name)
			if err == nil {
				t.Fatal("expected error for invalid cgroup name")
			}
		})
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
