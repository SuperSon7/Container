package cgroups

import (
	"mini-container/container/cgroups/resource"
)

const defaultRoot = "/sys/fs/cgroup"

type ResourceConfig = resource.Config

type Mode string

const (
	// ModeV1 and ModeV2 are cgroupfs version
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
