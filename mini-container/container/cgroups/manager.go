package cgroups

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mini-container/container/cgroups/resource"
	v1 "mini-container/container/cgroups/v1"
	v2 "mini-container/container/cgroups/v2"
)

const defaultRoot = "/sys/fs/cgroup"

type ResourceConfig = resource.Config

type Mode string

const (
	ModeV1 Mode = "cgroupfs-v1"
	ModeV2 Mode = "cgroupfs-v2"
)

type Manager interface {
	// Apply creates the cgroup if needed and moves pid into it.
	Apply(pid int) error

	// Set writes resource limits to the cgroup.
	Set(r ResourceConfig) error

	// Destroy removes the cgroup directories managed by this manager.
	Destroy() error
}

// NewManager creates a manager for the default cgroup mount point.
// It keeps the common path simple while NewManagerAt allows tests or callers
// to provide a different cgroup root.
func NewManager(name string) (Manager, error) {
	return NewManagerAt(defaultRoot, name)
}

// NewManagerAt creates a manager rooted at root.
// name must be a relative cgroup path under root, not an absolute filesystem path.
func NewManagerAt(root, name string) (Manager, error) {
	cgroupName, err := cleanName(name)
	if err != nil {
		return nil, err
	}

	mode, err := DetectModeAt(root)
	if err != nil {
		return nil, err
	}

	switch mode {
	case ModeV2:
		return v2.NewManager(root, cgroupName), nil
	case ModeV1:
		return v1.NewManager(root, cgroupName), nil
	default:
		return nil, fmt.Errorf("unsupported cgroup mode: %s", mode)
	}
}

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

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("cgroup name is empty")
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("cgroup name must be relative: %s", name)
	}

	parts := strings.Split(filepath.Clean(name), string(os.PathSeparator))
	for _, part := range parts {
		if part == "." || part == ".." || part == "" {
			return "", fmt.Errorf("invalid cgroup name: %s", name)
		}
	}

	return filepath.Join(parts...), nil
}
