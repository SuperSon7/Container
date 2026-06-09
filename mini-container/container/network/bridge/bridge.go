package bridge

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

func SetupBridge(name string, gatewayAddress string) error {
	// Assign the gateway IP/CIDR to the bridge.
	addr, err := netlink.ParseAddr(gatewayAddress) // "10.0.0.1/24"
	if err != nil {
		return fmt.Errorf("parse bridge gateway address %s: %w", gatewayAddress, err)
	}

	// Create a host-side bridge device that will act as the container gateway.
	la := netlink.NewLinkAttrs()
	la.Name = name
	br := &netlink.Bridge{LinkAttrs: la}
	if err := netlink.LinkAdd(br); err != nil {
		return fmt.Errorf("create bridge %s: %w", name, err)
	}

	// TODO: For multiple containers, keep bridge/gateway setup network-scoped
	// and allocate container IPs separately.
	if err := netlink.AddrAdd(br, addr); err != nil {
		return fmt.Errorf("assign gateway address %s to bridge %s: %w", gatewayAddress, name, err)
	}

	// Bring the bridge interface up so it can forward traffic.
	if err := netlink.LinkSetUp(br); err != nil {
		return fmt.Errorf("set bridge %s up: %w", name, err)
	}

	return nil
}
