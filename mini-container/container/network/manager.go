package network

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"mini-container/container/network/ipam"
)

// AddressAllocator keeps address assignment separate from link configuration.
type AddressAllocator interface {
	Allocate(id string) (string, error)
	Release(id string) error
}

type connection struct {
	cleanup func() error
}

// Manager is shared by all containers in one daemon. The lock also prevents
// network deletion from racing with a new attachment.
type Manager struct {
	mu               sync.Mutex
	config           Config
	allocator        AddressAllocator
	connections      map[string]*connection
	ready            bool
	setupBridge      func(string, string) error
	setupNAT         func(NATConfig) error
	destroyBridge    func(string) error
	destroyNAT       func(NATConfig) error
	connectContainer func(string, int, string, string) (func() error, error)
}

func NewManager(config Config) (*Manager, error) {
	m := &Manager{
		config:      config,
		connections: make(map[string]*connection),
		setupBridge: SetupBridge, setupNAT: SetupNAT,
		destroyBridge: DestroyBridge, destroyNAT: DestroyNAT,
		connectContainer: ConnectContainer,
	}
	if !config.Enabled {
		return m, nil
	}
	if strings.TrimSpace(config.BridgeName) == "" {
		return nil, fmt.Errorf("network: bridge name is required")
	}
	if config.EnableNAT && strings.TrimSpace(config.OutboundInterface) == "" {
		return nil, fmt.Errorf("network: outbound interface is required when NAT is enabled")
	}
	allocator, err := ipam.New(config.GatewayAddress)
	if err != nil {
		return nil, err
	}
	m.allocator = allocator
	return m, nil
}

// Setup prepares shared resources. Container failure/exit never removes them.
func (m *Manager) Setup() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.setup()
}

func (m *Manager) setup() error {
	if !m.config.Enabled || m.ready {
		return nil
	}
	if err := m.setupBridge(m.config.BridgeName, m.config.GatewayAddress); err != nil {
		return err
	}
	if m.config.EnableNAT {
		if err := m.setupNAT(m.natConfig()); err != nil {
			return err
		}
	}
	m.ready = true
	return nil
}

// Attach owns an IP lease and a veth pair. ConnectContainer returns cleanup
// even on partial failure; both that failure and normal exit use detach.
func (m *Manager) Attach(id string, pid int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.config.Enabled {
		return "", nil
	}
	if _, exists := m.connections[id]; exists {
		return "", fmt.Errorf("network: connection %s already exists", id)
	}
	if err := m.setup(); err != nil {
		return "", err
	}
	address, err := m.allocator.Allocate(id)
	if err != nil {
		return "", err
	}
	c := &connection{}
	m.connections[id] = c
	c.cleanup, err = m.connectContainer(m.config.BridgeName, pid, address, m.config.GatewayAddress)
	if err != nil {
		return "", errors.Join(err, m.detach(id))
	}
	return address, nil
}

// Detach removes only this connection. A failed cleanup keeps its lease so
// another container cannot receive an address still in use; callers may retry.
func (m *Manager) Detach(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.detach(id)
}

func (m *Manager) detach(id string) error {
	c, exists := m.connections[id]
	if !exists {
		return nil
	}
	if c.cleanup != nil {
		if err := c.cleanup(); err != nil {
			return err
		}
		c.cleanup = nil
	}
	if err := m.allocator.Release(id); err != nil {
		return err
	}
	delete(m.connections, id)
	return nil
}

// Destroy is an explicit network operation, never container-exit cleanup.
func (m *Manager) Destroy() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.connections) != 0 {
		return fmt.Errorf("network: %s still has %d connections", m.config.BridgeName, len(m.connections))
	}
	if !m.config.Enabled {
		return nil
	}
	m.ready = false
	var natErr error
	if m.config.EnableNAT {
		natErr = m.destroyNAT(m.natConfig())
	}
	return errors.Join(natErr, m.destroyBridge(m.config.BridgeName))
}

func (m *Manager) natConfig() NATConfig {
	_, subnet, _ := net.ParseCIDR(m.config.GatewayAddress) // validated by NewManager
	return NATConfig{SourceCIDR: subnet.String(), OutboundInterface: m.config.OutboundInterface}
}
