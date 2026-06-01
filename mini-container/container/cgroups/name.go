package cgroups

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
