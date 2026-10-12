package network

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func testManager(t *testing.T) (*Manager, *[]string) {
	t.Helper()
	m, err := NewManager(Config{Enabled: true, BridgeName: "mini0", GatewayAddress: "10.0.0.1/24", EnableNAT: true, OutboundInterface: "eth0"})
	if err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	m.setupBridge = func(name, gateway string) error {
		if name != "mini0" || gateway != "10.0.0.1/24" {
			t.Fatalf("unexpected bridge %s %s", name, gateway)
		}
		calls = append(calls, "bridge")
		return nil
	}
	m.setupNAT = func(c NATConfig) error {
		if c.SourceCIDR != "10.0.0.0/24" || c.OutboundInterface != "eth0" {
			t.Fatalf("unexpected NAT %+v", c)
		}
		calls = append(calls, "nat")
		return nil
	}
	m.destroyBridge = func(string) error { calls = append(calls, "delete-bridge"); return nil }
	m.destroyNAT = func(NATConfig) error { calls = append(calls, "delete-nat"); return nil }
	m.connectContainer = func(_ string, pid int, address, _ string) (func() error, error) {
		calls = append(calls, fmt.Sprintf("attach-%d", pid))
		return func() error { calls = append(calls, fmt.Sprintf("detach-%d", pid)); return nil }, nil
	}
	return m, &calls
}

func TestConnectionsShareNetworkButHaveSeparateAddresses(t *testing.T) {
	m, calls := testManager(t)
	a, err := m.Attach("a", 101)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Attach("b", 102)
	if err != nil {
		t.Fatal(err)
	}
	if a != "10.0.0.2/24" || b != "10.0.0.3/24" {
		t.Fatalf("addresses: %s %s", a, b)
	}
	if err := m.Destroy(); err == nil {
		t.Fatal("deleted an active network")
	}
	if err := m.Detach("a"); err != nil {
		t.Fatal(err)
	}
	if err := m.Detach("a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.connections["b"]; !ok {
		t.Fatal("other connection lost")
	}
	if err := m.Detach("b"); err != nil {
		t.Fatal(err)
	}
	want := []string{"bridge", "nat", "attach-101", "attach-102", "detach-101", "detach-102"}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("unexpected lifecycle: %v", *calls)
	}
	if err := m.Destroy(); err != nil {
		t.Fatal(err)
	}
	want = append(want, "delete-nat", "delete-bridge")
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("explicit deletion: %v", *calls)
	}
	if err := m.Setup(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual((*calls)[len(want):], []string{"bridge", "nat"}) {
		t.Fatal("network not recreated")
	}
}

func TestAttachFailureUsesConnectionCleanupOnly(t *testing.T) {
	for _, created := range []bool{false, true} {
		t.Run(fmt.Sprint(created), func(t *testing.T) {
			m, calls := testManager(t)
			if _, err := m.Attach("existing", 101); err != nil {
				t.Fatal(err)
			}
			setupErr := errors.New("setup failed")
			m.connectContainer = func(string, int, string, string) (func() error, error) {
				if !created {
					return nil, setupErr
				}
				return func() error { *calls = append(*calls, "partial-cleanup"); return nil }, setupErr
			}
			if _, err := m.Attach("failed", 102); !errors.Is(err, setupErr) {
				t.Fatalf("got %v", err)
			}
			if len(m.connections) != 1 {
				t.Fatalf("connections: %v", m.connections)
			}
			ip, err := m.allocator.Allocate("next")
			if err != nil || ip != "10.0.0.3/24" {
				t.Fatalf("lease not released: %s %v", ip, err)
			}
			want := []string{"bridge", "nat", "attach-101"}
			if created {
				want = append(want, "partial-cleanup")
			}
			if !reflect.DeepEqual(*calls, want) {
				t.Fatalf("unexpected cleanup: %v", *calls)
			}
		})
	}
}

func TestCleanupFailureKeepsLeaseUntilRetry(t *testing.T) {
	m, _ := testManager(t)
	setupErr, cleanupErr := errors.New("setup"), errors.New("cleanup")
	fail := true
	m.connectContainer = func(string, int, string, string) (func() error, error) {
		return func() error {
			if fail {
				return cleanupErr
			}
			return nil
		}, setupErr
	}
	_, err := m.Attach("a", 101)
	if !errors.Is(err, setupErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("lost errors: %v", err)
	}
	if err := m.Destroy(); err == nil {
		t.Fatal("deleted network with incomplete cleanup")
	}
	ip, err := m.allocator.Allocate("b")
	if err != nil || ip != "10.0.0.3/24" {
		t.Fatalf("reused active lease: %s %v", ip, err)
	}
	fail = false
	if err := m.Detach("a"); err != nil {
		t.Fatal(err)
	}
	ip, err = m.allocator.Allocate("c")
	if err != nil || ip != "10.0.0.2/24" {
		t.Fatalf("lease not released: %s %v", ip, err)
	}
}

func TestSetupFailurePreservesSharedResources(t *testing.T) {
	m, calls := testManager(t)
	setupErr := errors.New("nat")
	m.setupNAT = func(NATConfig) error { return setupErr }
	if _, err := m.Attach("a", 101); !errors.Is(err, setupErr) {
		t.Fatalf("got %v", err)
	}
	if !reflect.DeepEqual(*calls, []string{"bridge"}) {
		t.Fatalf("unexpected teardown: %v", *calls)
	}
}

func TestManagerValidationAndDisabledNetwork(t *testing.T) {
	for _, config := range []Config{
		{Enabled: true, GatewayAddress: "10.0.0.1/24"},
		{Enabled: true, BridgeName: "mini0", GatewayAddress: "bad"},
		{Enabled: true, BridgeName: "mini0", GatewayAddress: "10.0.0.1/24", EnableNAT: true},
	} {
		if _, err := NewManager(config); err == nil {
			t.Fatalf("accepted %+v", config)
		}
	}
	m, err := NewManager(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach("a", 101); err != nil {
		t.Fatal(err)
	}
	if err := m.Detach("a"); err != nil {
		t.Fatal(err)
	}
	if err := m.Destroy(); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateAttachmentDoesNotChangeExistingConnection(t *testing.T) {
	m, calls := testManager(t)
	if _, err := m.Attach("a", 101); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach("a", 102); err == nil {
		t.Fatal("duplicate attachment accepted")
	}
	if len(*calls) != 3 {
		t.Fatalf("duplicate setup: %v", *calls)
	}
}
