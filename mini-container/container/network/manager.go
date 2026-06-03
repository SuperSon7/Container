package network

import "errors"

var ErrNotImplemented = errors.New("network: not implemented")

type Manager struct {
	config Config
}

func NewManager(config Config) *Manager {
	return &Manager{config: config}
}

func (m *Manager) Setup(pid int) error {
	// TODO: configure network resources for the container process.
	return ErrNotImplemented
}

func (m *Manager) Destroy() error {
	// TODO: remove network resources created by Setup.
	return ErrNotImplemented
}
