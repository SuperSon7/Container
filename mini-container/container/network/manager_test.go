package network

import (
	"errors"
	"testing"
)

func TestNewManagerStoresConfig(t *testing.T) {
	config := Config{
		Enabled:                true,
		BridgeName:             "mini0",
		HostInterfaceName:      "veth-host",
		ContainerInterfaceName: "eth0",
		ContainerAddress:       "10.0.0.2/24",
		GatewayAddress:         "10.0.0.1/24",
		EnableNAT:              true,
	}

	manager := NewManager(config)
	if manager == nil {
		t.Fatal("expected manager, got nil")
	}

	if manager.config != config {
		t.Fatalf("expected config %+v, got %+v", config, manager.config)
	}
}

func TestManagerSetupReturnsNilAfterBridgeAndContainerSetup(t *testing.T) {
	manager := NewManager(validConfig())
	manager.setupBridge = func(name string, gatewayAddress string) error {
		return nil
	}
	manager.connectContainer = func(bridgeName string, containerPID int, containerAddress string, gatewayAddress string) error {
		return nil
	}

	if err := manager.Setup(1234); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestManagerSetupSkipsDisabledNetwork(t *testing.T) {
	manager := NewManager(Config{})
	manager.setupBridge = func(name string, gatewayAddress string) error {
		t.Fatal("expected setupBridge not to be called")
		return nil
	}
	manager.connectContainer = func(bridgeName string, containerPID int, containerAddress string, gatewayAddress string) error {
		t.Fatal("expected connectContainer not to be called")
		return nil
	}

	if err := manager.Setup(1234); err != nil {
		t.Fatalf("expected nil error for disabled network, got %v", err)
	}
}

func TestManagerSetupCallsBridgeSetupWithConfig(t *testing.T) {
	manager := NewManager(validConfig())
	called := false

	manager.setupBridge = func(name string, gatewayAddress string) error {
		called = true

		if name != "mini0" {
			t.Fatalf("expected bridge name mini0, got %s", name)
		}
		if gatewayAddress != "10.0.0.1/24" {
			t.Fatalf("expected gateway address 10.0.0.1/24, got %s", gatewayAddress)
		}

		return nil
	}
	manager.connectContainer = func(bridgeName string, containerPID int, containerAddress string, gatewayAddress string) error {
		return nil
	}

	if err := manager.Setup(1234); err != nil {
		t.Fatalf("expected nil error after bridge setup, got %v", err)
	}
	if !called {
		t.Fatal("expected setupBridge to be called")
	}
}

func TestManagerSetupCallsConnectContainerWithConfig(t *testing.T) {
	manager := NewManager(validConfig())
	calls := []string{}

	manager.setupBridge = func(name string, gatewayAddress string) error {
		calls = append(calls, "bridge")
		return nil
	}
	manager.connectContainer = func(bridgeName string, containerPID int, containerAddress string, gatewayAddress string) error {
		calls = append(calls, "connect")

		if bridgeName != "mini0" {
			t.Fatalf("expected bridge name mini0, got %s", bridgeName)
		}
		if containerPID != 1234 {
			t.Fatalf("expected container pid 1234, got %d", containerPID)
		}
		if containerAddress != "10.0.0.2/24" {
			t.Fatalf("expected container address 10.0.0.2/24, got %s", containerAddress)
		}
		if gatewayAddress != "10.0.0.1/24" {
			t.Fatalf("expected gateway address 10.0.0.1/24, got %s", gatewayAddress)
		}

		return nil
	}

	if err := manager.Setup(1234); err != nil {
		t.Fatalf("expected nil error after container connect, got %v", err)
	}
	if len(calls) != 2 || calls[0] != "bridge" || calls[1] != "connect" {
		t.Fatalf("expected bridge then connect calls, got %v", calls)
	}
}

func TestManagerSetupReturnsNotImplementedWhenNATEnabled(t *testing.T) {
	config := validConfig()
	config.EnableNAT = true
	manager := NewManager(config)

	manager.setupBridge = func(name string, gatewayAddress string) error {
		return nil
	}
	manager.connectContainer = func(bridgeName string, containerPID int, containerAddress string, gatewayAddress string) error {
		return nil
	}

	err := manager.Setup(1234)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented for NAT setup, got %v", err)
	}
}

func TestManagerSetupReturnsBridgeSetupError(t *testing.T) {
	bridgeErr := errors.New("bridge setup failed")
	manager := NewManager(validConfig())
	manager.setupBridge = func(name string, gatewayAddress string) error {
		return bridgeErr
	}
	manager.connectContainer = func(bridgeName string, containerPID int, containerAddress string, gatewayAddress string) error {
		t.Fatal("expected connectContainer not to be called")
		return nil
	}

	err := manager.Setup(1234)
	if !errors.Is(err, bridgeErr) {
		t.Fatalf("expected bridge setup error, got %v", err)
	}
}

func TestManagerSetupRejectsInvalidConfig(t *testing.T) {
	manager := NewManager(Config{
		Enabled:        true,
		GatewayAddress: "10.0.0.1/24",
	})
	manager.setupBridge = func(name string, gatewayAddress string) error {
		t.Fatal("expected setupBridge not to be called")
		return nil
	}
	manager.connectContainer = func(bridgeName string, containerPID int, containerAddress string, gatewayAddress string) error {
		t.Fatal("expected connectContainer not to be called")
		return nil
	}

	err := manager.Setup(1234)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
	if errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected config validation error, got %v", err)
	}
}

func TestManagerValidateSetupConfigSkipsDisabledNetwork(t *testing.T) {
	manager := NewManager(Config{})

	if err := manager.validateSetupConfig(); err != nil {
		t.Fatalf("expected nil error for disabled network, got %v", err)
	}
}

func TestManagerValidateSetupConfigRejectsEmptyBridgeName(t *testing.T) {
	manager := NewManager(Config{
		Enabled:          true,
		BridgeName:       "",
		ContainerAddress: "10.0.0.2/24",
		GatewayAddress:   "10.0.0.1/24",
	})

	if err := manager.validateSetupConfig(); err == nil {
		t.Fatal("expected error for empty bridge name")
	}
}

func TestManagerValidateSetupConfigRejectsInvalidGatewayAddress(t *testing.T) {
	for _, gatewayAddress := range []string{"", "10.0.0.1", "not-a-cidr"} {
		t.Run(gatewayAddress, func(t *testing.T) {
			manager := NewManager(Config{
				Enabled:          true,
				BridgeName:       "mini0",
				ContainerAddress: "10.0.0.2/24",
				GatewayAddress:   gatewayAddress,
			})

			if err := manager.validateSetupConfig(); err == nil {
				t.Fatal("expected error for invalid gateway address")
			}
		})
	}
}

func TestManagerValidateSetupConfigRejectsInvalidContainerAddress(t *testing.T) {
	for _, containerAddress := range []string{"", "10.0.0.2", "not-a-cidr"} {
		t.Run(containerAddress, func(t *testing.T) {
			manager := NewManager(Config{
				Enabled:          true,
				BridgeName:       "mini0",
				ContainerAddress: containerAddress,
				GatewayAddress:   "10.0.0.1/24",
			})

			if err := manager.validateSetupConfig(); err == nil {
				t.Fatal("expected error for invalid container address")
			}
		})
	}
}

func TestManagerValidateSetupConfigAcceptsRequiredCIDRs(t *testing.T) {
	manager := NewManager(Config{
		Enabled:          true,
		BridgeName:       "mini0",
		ContainerAddress: "10.0.0.2/24",
		GatewayAddress:   "10.0.0.1/24",
	})

	if err := manager.validateSetupConfig(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestManagerDestroyReturnsNotImplemented(t *testing.T) {
	manager := NewManager(Config{Enabled: true})

	err := manager.Destroy()
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}

func validConfig() Config {
	return Config{
		Enabled:          true,
		BridgeName:       "mini0",
		ContainerAddress: "10.0.0.2/24",
		GatewayAddress:   "10.0.0.1/24",
	}
}
