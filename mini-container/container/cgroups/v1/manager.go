package v1

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"mini-container/container/cgroups/resource"
)

var defaultControllers = []string{"memory", "cpu", "pids"}

type Manager struct {
	paths map[string]string
}

func NewManager(root, name string) *Manager {
	paths := make(map[string]string, len(defaultControllers))
	for _, controller := range defaultControllers {
		controllerRoot := filepath.Join(root, controller)
		if _, err := os.Stat(controllerRoot); err == nil {
			paths[controller] = filepath.Join(controllerRoot, name)
		}
	}

	return &Manager{paths: paths}
}

func (m *Manager) Apply(pid int) error {
	var errs []error
	for _, path := range m.paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := writeFile(path, "cgroup.procs", strconv.Itoa(pid)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) Set(r resource.Config) error {
	if r.MemoryLimit != 0 {
		path, err := m.ensure("memory")
		if err != nil {
			return err
		}
		if err := writeFile(path, "memory.limit_in_bytes", maxOrInt(r.MemoryLimit)); err != nil {
			return err
		}
	}

	if r.CpuPeriod != 0 || r.CpuQuota != 0 {
		path, err := m.ensure("cpu")
		if err != nil {
			return err
		}
		if r.CpuPeriod != 0 {
			if err := writeFile(path, "cpu.cfs_period_us", strconv.FormatUint(r.CpuPeriod, 10)); err != nil {
				return err
			}
		}
		if r.CpuQuota != 0 {
			if err := writeFile(path, "cpu.cfs_quota_us", strconv.FormatInt(r.CpuQuota, 10)); err != nil {
				return err
			}
		}
	}

	if r.PidsLimit != nil {
		path, err := m.ensure("pids")
		if err != nil {
			return err
		}
		if err := writeFile(path, "pids.max", maxOrInt(*r.PidsLimit)); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) Destroy() error {
	var errs []error
	for _, path := range m.paths {
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) ensure(controller string) (string, error) {
	path, ok := m.paths[controller]
	if !ok {
		return "", fmt.Errorf("cgroup v1 controller is not mounted: %s", controller)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func writeFile(dir, file, value string) error {
	return os.WriteFile(filepath.Join(dir, file), []byte(value), 0o644)
}

func maxOrInt(value int64) string {
	if value < 0 {
		return "max"
	}
	return strconv.FormatInt(value, 10)
}
