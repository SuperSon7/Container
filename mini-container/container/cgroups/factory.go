package cgroups

import (
	"fmt"

	v1 "mini-container/container/cgroups/v1"
	v2 "mini-container/container/cgroups/v2"
)

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
