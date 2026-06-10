package network

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

var ErrNotImplemented = errors.New("network: not implemented")

type setupBridgeFunc func(name string, gatewayAddress string) error
type connectContainerFunc func(bridgeName string, containerPID int, containerAddress string, gatewayAddress string) error

type Manager struct {
	config           Config
	setupBridge      setupBridgeFunc
	connectContainer connectContainerFunc
}

// NewManager stores the desired network config and wires the real setup helpers.
func NewManager(config Config) *Manager {
	return &Manager{
		config:           config,
		setupBridge:      SetupBridge,
		connectContainer: ConnectContainer,
	}
}

// Setup creates host-side network resources and connects the target process to them.
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

	if err := m.connectContainer(m.config.BridgeName, pid, m.config.ContainerAddress, m.config.GatewayAddress); err != nil {
		return err
	}

	if m.config.EnableNAT {
		// TODO: Wire SetupNAT once Config knows the host outbound interface.
		return ErrNotImplemented
	}

	return nil
}

// Destroy will remove network resources created by Setup.
func (m *Manager) Destroy() error {
	// TODO: remove network resources created by Setup.
	return ErrNotImplemented
}

// validateSetupConfig checks only the config required before touching netlink state.
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

	if _, _, err := net.ParseCIDR(m.config.ContainerAddress); err != nil {
		return fmt.Errorf("network: invalid container address %q: %w", m.config.ContainerAddress, err)
	}

	return nil
}
