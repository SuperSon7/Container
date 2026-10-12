package ipam

import (
	"fmt"
	"net/netip"
	"sync"
)

// Allocator belongs to the daemon. Callers only allocate and release addresses.
type Allocator struct {
	mu      sync.Mutex
	subnet  netip.Prefix
	gateway netip.Addr
	leases  map[string]netip.Addr
}

func New(gatewayCIDR string) (*Allocator, error) {
	gateway, err := netip.ParsePrefix(gatewayCIDR)
	if err != nil || !gateway.Addr().Is4() || gateway.Bits() > 30 {
		return nil, fmt.Errorf("ipam: expected IPv4 gateway with a /0 through /30 prefix: %q", gatewayCIDR)
	}
	subnet := gateway.Masked()
	if gateway.Addr() == subnet.Addr() || !subnet.Contains(gateway.Addr().Next()) {
		return nil, fmt.Errorf("ipam: gateway is a network or broadcast address: %q", gatewayCIDR)
	}
	return &Allocator{subnet: subnet, gateway: gateway.Addr(), leases: make(map[string]netip.Addr)}, nil
}

func (a *Allocator) Allocate(id string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if id == "" {
		return "", fmt.Errorf("ipam: connection ID is required")
	}
	if ip, ok := a.leases[id]; ok {
		return netip.PrefixFrom(ip, a.subnet.Bits()).String(), nil
	}
	used := make(map[netip.Addr]bool, len(a.leases))
	for _, ip := range a.leases {
		used[ip] = true
	}
	for ip := a.subnet.Addr().Next(); a.subnet.Contains(ip) && a.subnet.Contains(ip.Next()); ip = ip.Next() {
		if ip != a.gateway && !used[ip] {
			a.leases[id] = ip
			return netip.PrefixFrom(ip, a.subnet.Bits()).String(), nil
		}
	}
	return "", fmt.Errorf("ipam: address pool %s is exhausted", a.subnet)
}

func (a *Allocator) Release(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.leases, id)
	return nil
}
