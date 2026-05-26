package cgroups

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mini-container/container/cgroups/resource"
	"mini-container/container/cgroups/v1"
	"mini-container/container/cgroups/v2"
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

func NewManager(name string) (Manager, error) {
	return NewManagerAt(defaultRoot, name)
}

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

func DetectMode() (Mode, error) {
	return DetectModeAt(defaultRoot)
}

func DetectModeAt(root string) (Mode, error) {
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
