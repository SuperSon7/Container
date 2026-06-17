package network

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// SetupBridge creates the host-side bridge and assigns its gateway address.
func SetupBridge(name string, gatewayAddress string) error {
	// Assign the gateway IP/CIDR to the bridge.
	addr, err := netlink.ParseAddr(gatewayAddress) // "10.0.0.1/24"
	if err != nil {
		return fmt.Errorf("parse bridge gateway address %s: %w", gatewayAddress, err)
	}

	br, err := getOrCreateBridge(name)
	if err != nil {
		return err
	}

	// TODO: For multiple containers, keep bridge/gateway setup network-scoped
	// and allocate container IPs separately.
	if err := ensureBridgeAddress(br, addr); err != nil {
		return fmt.Errorf("assign gateway address %s to bridge %s: %w", gatewayAddress, name, err)
	}

	// Bring the bridge interface up so it can forward traffic.
	if err := netlink.LinkSetUp(br); err != nil {
		return fmt.Errorf("set bridge %s up: %w", name, err)
	}

	return nil
}

// DestroyBridge deletes the host-side bridge when it exists.
func DestroyBridge(name string) error {
	// TODO: When multiple containers share a bridge, track network ownership
	// instead of deleting the shared bridge after each container exits.
	br, err := netlink.LinkByName(name)
	if isLinkNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find bridge %s: %w", name, err)
	}
	if _, ok := br.(*netlink.Bridge); !ok {
		return fmt.Errorf("network: link %s exists but is %T, not bridge", name, br)
	}

	if err := netlink.LinkDel(br); err != nil {
		return fmt.Errorf("delete bridge %s: %w", name, err)
	}

	return nil
}

func getOrCreateBridge(name string) (netlink.Link, error) {
	br, err := netlink.LinkByName(name)
	if err == nil {
		if _, ok := br.(*netlink.Bridge); !ok {
			return nil, fmt.Errorf("network: link %s exists but is %T, not bridge", name, br)
		}
		return br, nil
	}
	if !isLinkNotFound(err) {
		return nil, fmt.Errorf("find bridge %s: %w", name, err)
	}

	// Create a host-side bridge device that will act as the container gateway.
	la := netlink.NewLinkAttrs()
	la.Name = name
	br = &netlink.Bridge{LinkAttrs: la}
	if err := netlink.LinkAdd(br); err != nil {
		return nil, fmt.Errorf("create bridge %s: %w", name, err)
	}

	return br, nil
}

func ensureBridgeAddress(br netlink.Link, addr *netlink.Addr) error {
	addrs, err := netlink.AddrList(br, netlink.FAMILY_ALL)
	if err != nil {
		return err
	}

	for _, existing := range addrs {
		if existing.IPNet != nil && existing.IPNet.String() == addr.IPNet.String() {
			return nil
		}
	}

	return netlink.AddrAdd(br, addr)
}

func isLinkNotFound(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(netlink.LinkNotFoundError)
	return ok
}
