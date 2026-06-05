package network

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

var ErrNotImplemented = errors.New("network: not implemented")

type setupBridgeFunc func(name string, gatewayAddress string) error

type Manager struct {
	config      Config
	setupBridge setupBridgeFunc
}

func NewManager(config Config) *Manager {
	return &Manager{
		config:      config,
		setupBridge: SetupBridge,
	}
}

func (m *Manager) Setup(pid int) error {
	if err := m.validateSetupConfig(); err != nil {
		return err
	}

	if !m.config.Enabled {
		return nil
	}

	if err := m.setupBridge(m.config.BridgeName, m.config.GatewayAddress); err != nil {
		return err
	}

	// TODO: configure network resources for the container process.
	return ErrNotImplemented
}

func (m *Manager) Destroy() error {
	// TODO: remove network resources created by Setup.
	return ErrNotImplemented
}

func (m *Manager) validateSetupConfig() error {
	if !m.config.Enabled {
		return nil
	}

	if strings.TrimSpace(m.config.BridgeName) == "" {
		return fmt.Errorf("network: bridge name is required")
	}

	if _, _, err := net.ParseCIDR(m.config.GatewayAddress); err != nil {
		return fmt.Errorf("network: invalid gateway address %q: %w", m.config.GatewayAddress, err)
	}

	return nil
}
