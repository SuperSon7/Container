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

func TestManagerSetupReturnsNotImplemented(t *testing.T) {
	manager := NewManager(validConfig())
	manager.setupBridge = func(name string, gatewayAddress string) error {
		return nil
	}

	err := manager.Setup(1234)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}

func TestManagerSetupSkipsDisabledNetwork(t *testing.T) {
	manager := NewManager(Config{})
	manager.setupBridge = func(name string, gatewayAddress string) error {
		t.Fatal("expected setupBridge not to be called")
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

	err := manager.Setup(1234)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented after bridge setup, got %v", err)
	}
	if !called {
		t.Fatal("expected setupBridge to be called")
	}
}

func TestManagerSetupReturnsBridgeSetupError(t *testing.T) {
	bridgeErr := errors.New("bridge setup failed")
	manager := NewManager(validConfig())
	manager.setupBridge = func(name string, gatewayAddress string) error {
		return bridgeErr
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
		Enabled:        true,
		BridgeName:     "",
		GatewayAddress: "10.0.0.1/24",
	})

	if err := manager.validateSetupConfig(); err == nil {
		t.Fatal("expected error for empty bridge name")
	}
}

func TestManagerValidateSetupConfigRejectsInvalidGatewayAddress(t *testing.T) {
	for _, gatewayAddress := range []string{"", "10.0.0.1", "not-a-cidr"} {
		t.Run(gatewayAddress, func(t *testing.T) {
			manager := NewManager(Config{
				Enabled:        true,
				BridgeName:     "mini0",
				GatewayAddress: gatewayAddress,
			})

			if err := manager.validateSetupConfig(); err == nil {
				t.Fatal("expected error for invalid gateway address")
			}
		})
	}
}

func TestManagerValidateSetupConfigAcceptsBridgeNameAndGatewayCIDR(t *testing.T) {
	manager := NewManager(Config{
		Enabled:        true,
		BridgeName:     "mini0",
		GatewayAddress: "10.0.0.1/24",
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
		Enabled:        true,
		BridgeName:     "mini0",
		GatewayAddress: "10.0.0.1/24",
	}
}
