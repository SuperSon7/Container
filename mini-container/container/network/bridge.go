package network

import "github.com/vishvananda/netlink"

func CreateBridge(name string, gatewayAddress string) error {
	// Create a host-side bridge device that will act as the container gateway.
	la := netlink.NewLinkAttrs()
	la.Name = name
	br := &netlink.Bridge{LinkAttrs: la}
	netlink.LinkAdd(br)

	// Assign the gateway IP/CIDR to the bridge.
	addr, _ := netlink.ParseAddr(gatewayAddress) // "10.0.0.1/24"
	// TODO: For multiple containers, keep bridge/gateway setup network-scoped
	// and allocate container IPs separately.
	netlink.AddrAdd(br, addr)

	// Bring the bridge interface up so it can forward traffic.
	netlink.LinkSetUp(br)
	return nil
}
