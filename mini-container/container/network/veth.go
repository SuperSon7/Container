package network

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

func ConnectContainer(bridgeName string, containerPID int, containerIP string) error {
	hostVethName := fmt.Sprintf("veth-%d", containerPID)
	containerVethName := fmt.Sprintf("vethc-%d", containerPID)

	// Create the veth pair in the host netns first; one side will be moved later.
	la := netlink.NewLinkAttrs()
	la.Name = hostVethName
	veth := &netlink.Veth{LinkAttrs: la, PeerName: containerVethName}
	if err := netlink.LinkAdd(veth); err != nil {
		return fmt.Errorf("create veth pair %s/%s: %w", hostVethName, containerVethName, err)
	}

	// Attach the host-side veth to the host bridge so it can reach the gateway.
	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("find bridge %s: %w", bridgeName, err)
	}

	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil {
		return fmt.Errorf("find host veth %s: %w", hostVethName, err)
	}

	if err := netlink.LinkSetMaster(hostVeth, br); err != nil {
		return fmt.Errorf("attach host veth %s to bridge %s: %w", hostVethName, bridgeName, err)
	}

	if err := netlink.LinkSetUp(hostVeth); err != nil {
		return fmt.Errorf("set host veth %s up: %w", hostVethName, err)
	}

	// Move the container-side veth into the target network namespace.
	containerVeth, err := netlink.LinkByName(containerVethName)
	if err != nil {
		return fmt.Errorf("find container veth %s: %w", containerVethName, err)
	}

	if err := netlink.LinkSetNsPid(containerVeth, containerPID); err != nil {
		return fmt.Errorf("move container veth %s to netns of pid %d: %w", containerVethName, containerPID, err)
	}

	// TODO: Enter the container netns, rename this peer to eth0, assign
	// containerIP, bring it up, and install the default route.

	return nil
}
