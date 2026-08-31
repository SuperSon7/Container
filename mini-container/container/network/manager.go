package network

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

type setupBridgeFunc func(name string, gatewayAddress string) error
type connectContainerFunc func(bridgeName string, containerPID int, containerAddress string, gatewayAddress string) error
type setupNATFunc func(config NATConfig) error
type destroyBridgeFunc func(name string) error
type destroyNATFunc func(config NATConfig) error

type Manager struct {
	config           Config
	setupBridge      setupBridgeFunc
	connectContainer connectContainerFunc
	setupNAT         setupNATFunc
	destroyBridge    destroyBridgeFunc
	destroyNAT       destroyNATFunc
}

// NewManager stores the desired network config and wires the real setup helpers.
func NewManager(config Config) *Manager {
	return &Manager{
		config:           config,
		setupBridge:      SetupBridge,
		connectContainer: ConnectContainer,
		setupNAT:         SetupNAT,
		destroyBridge:    DestroyBridge,
		destroyNAT:       DestroyNAT,
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
		return m.rollbackSetup(err)
	}

	if m.config.EnableNAT {
		sourceCIDR, err := containerSubnetCIDR(m.config.ContainerAddress)
		if err != nil {
			return m.rollbackSetup(err)
		}

		if err := m.setupNAT(NATConfig{
			SourceCIDR:        sourceCIDR,
			OutboundInterface: m.config.OutboundInterface,
		}); err != nil {
			return m.rollbackSetup(err)
		}
	}

	return nil
}

// Destroy removes host-side network resources created by Setup.
func (m *Manager) Destroy() error {
	if err := m.validateSetupConfig(); err != nil {
		return err
	}

	if !m.config.Enabled {
		return nil
	}

	var errs []error
	if m.config.EnableNAT {
		sourceCIDR, err := containerSubnetCIDR(m.config.ContainerAddress)
		if err != nil {
			errs = append(errs, err)
		} else if err := m.destroyNAT(NATConfig{
			SourceCIDR:        sourceCIDR,
			OutboundInterface: m.config.OutboundInterface,
		}); err != nil {
			errs = append(errs, err)
		}
	}

	if err := m.destroyBridge(m.config.BridgeName); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// rollbackSetup cleans up resources from a partially completed Setup call.
func (m *Manager) rollbackSetup(setupErr error) error {
	if cleanupErr := m.Destroy(); cleanupErr != nil {
		return errors.Join(setupErr, cleanupErr)
	}

	return setupErr
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

	if m.config.EnableNAT && strings.TrimSpace(m.config.OutboundInterface) == "" {
		return fmt.Errorf("network: outbound interface is required when NAT is enabled")
	}

	return nil
}

// containerSubnetCIDR converts a static container IP/CIDR into the subnet NAT should match.
func containerSubnetCIDR(containerAddress string) (string, error) {
	_, ipNet, err := net.ParseCIDR(containerAddress)
	if err != nil {
		return "", fmt.Errorf("network: invalid container address %q: %w", containerAddress, err)
	}

	return ipNet.String(), nil
}
