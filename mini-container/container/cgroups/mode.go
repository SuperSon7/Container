package cgroups

import (
	"fmt"
	"os"
	"path/filepath"
)

// DetectMode detects whether the default cgroup root is v1 or v2.
func DetectMode() (Mode, error) {
	return DetectModeAt(defaultRoot)
}

// DetectModeAt detects the cgroup mode under root instead of binding the logic
// to the host's /sys/fs/cgroup.
func DetectModeAt(root string) (Mode, error) {
	// cgroup v2 exposes cgroup.controllers at the hierarchy root.
	if fileExists(filepath.Join(root, "cgroup.controllers")) {
		return ModeV2, nil
	}

	if v1Mounted(root) {
		return ModeV1, nil
	}

	return "", fmt.Errorf("no supported cgroup hierarchy found under %s", root)
}

func v1Mounted(root string) bool {
	for _, controller := range []string{"memory", "cpu", "pids"} {
		if fileExists(filepath.Join(root, controller)) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
