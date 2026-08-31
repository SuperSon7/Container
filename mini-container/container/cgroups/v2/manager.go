package v2

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mini-container/container/cgroups/resource"
)

const defaultCpuPeriod = 100000

type Manager struct {
	root string
	path string
}

// NewManager builds a v2 manager for root/name.
// The top-level cgroups package passes a cleaned, non-empty relative name here.
func NewManager(root, name string) *Manager {
	return &Manager{
		root: root,
		path: filepath.Join(root, name),
	}
}

func (m *Manager) Set(r resource.Config) error {
	if err := m.ensurePath(r); err != nil {
		return err
	}

	if r.MemoryLimit != 0 {
		if err := writeFile(m.path, "memory.max", maxOrInt(r.MemoryLimit)); err != nil {
			return err
		}
	}

	if r.CpuPeriod != 0 || r.CpuQuota != 0 {
		if err := writeFile(m.path, "cpu.max", cpuMax(r)); err != nil {
			return err
		}
	}

	if r.PidsLimit != nil {
		if err := writeFile(m.path, "pids.max", maxOrInt(*r.PidsLimit)); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) Apply(pid int) error {
	if err := os.MkdirAll(m.path, 0o755); err != nil {
		return err
	}
	return writeFile(m.path, "cgroup.procs", strconv.Itoa(pid))
}

func (m *Manager) Destroy() error {
	return os.RemoveAll(m.path)
}

// ensurePath walks from root to m.path so each parent can delegate the
// requested controllers before the child cgroup is created.
func (m *Manager) ensurePath(r resource.Config) error {
	controllers := neededControllers(r)
	rel, err := filepath.Rel(m.root, m.path)
	if err != nil {
		return err
	}

	current := m.root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if len(controllers) > 0 {
			if err := enableControllers(current, controllers); err != nil {
				return err
			}
		}

		current = filepath.Join(current, part)
		if err := os.MkdirAll(current, 0o755); err != nil {
			return err
		}
	}

	return nil
}

// neededControllers returns only the controllers required by configured limits.
func neededControllers(r resource.Config) []string {
	var controllers []string
	if r.MemoryLimit != 0 {
		controllers = append(controllers, "memory")
	}
	if r.CpuPeriod != 0 || r.CpuQuota != 0 {
		controllers = append(controllers, "cpu")
	}
	if r.PidsLimit != nil {
		controllers = append(controllers, "pids")
	}
	return controllers
}

func enableControllers(dir string, controllers []string) error {
	// A child cgroup can use only controllers that are both available in this
	// cgroup and enabled through cgroup.subtree_control.
	available, err := readControllerSet(filepath.Join(dir, "cgroup.controllers"))
	if err != nil {
		return err
	}

	enabled, err := readControllerSet(filepath.Join(dir, "cgroup.subtree_control"))
	if err != nil {
		return err
	}

	for _, controller := range controllers {
		if !available[controller] {
			return fmt.Errorf("cgroup v2 controller is not available: %s", controller)
		}
		if enabled[controller] {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "cgroup.subtree_control"), []byte("+"+controller), 0o644); err != nil {
			return err
		}
	}

	return nil
}

// readControllerSet reads a whitespace-separated cgroup controller list as a set.
func readControllerSet(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	controllers := make(map[string]bool)
	for _, controller := range strings.Fields(string(data)) {
		controllers[controller] = true
	}
	return controllers, nil
}

func writeFile(dir, file, value string) error {
	return os.WriteFile(filepath.Join(dir, file), []byte(value), 0o644)
}

func cpuMax(r resource.Config) string {
	period := r.CpuPeriod
	if period == 0 {
		period = defaultCpuPeriod
	}

	quota := "max"
	if r.CpuQuota > 0 {
		quota = strconv.FormatInt(r.CpuQuota, 10)
	}

	return fmt.Sprintf("%s %d", quota, period)
}

func maxOrInt(value int64) string {
	if value < 0 {
		return "max"
	}
	return strconv.FormatInt(value, 10)
}
