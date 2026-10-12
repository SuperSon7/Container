package network

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const defaultContainerInterfaceName = "eth0"

// ConnectContainer creates a veth pair, attaches the host side to the bridge,
// moves the peer into the target netns, and configures it as eth0.
// The caller must run a non-nil cleanup action even when setup returns an error.
func ConnectContainer(bridgeName string, containerPID int, containerAddress string, gatewayAddress string) (cleanup func() error, err error) {
	// TODO: Allocate interface names from runtime network state when multiple
	// containers and configurable network attachments are supported.
	hostVethName := fmt.Sprintf("veth-%d", containerPID)
	containerVethName := fmt.Sprintf("vethc-%d", containerPID)

	// Hold a target netns fd before moving the peer so this setup has a stable handle.
	targetNS, err := netns.GetFromPid(containerPID)
	if err != nil {
		return cleanup, fmt.Errorf("get netns for pid %d: %w", containerPID, err)
	}
	defer targetNS.Close()

	// Refuse to configure the current host netns; the child must be created with CLONE_NEWNET.
	currentNS, err := netns.Get()
	if err != nil {
		return cleanup, fmt.Errorf("get current netns: %w", err)
	}
	defer currentNS.Close()
	if currentNS.Equal(targetNS) {
		return cleanup, fmt.Errorf("network: pid %d is in the current netns; CLONE_NEWNET is required", containerPID)
	}

	// Create the veth pair in the host netns first; one side will be moved later.
	la := netlink.NewLinkAttrs()
	la.Name = hostVethName
	veth := &netlink.Veth{LinkAttrs: la, PeerName: containerVethName}
	if err := netlink.LinkAdd(veth); err != nil {
		return cleanup, fmt.Errorf("create veth pair %s/%s: %w", hostVethName, containerVethName, err)
	}
	// Return the same cleanup action for partial failure and normal teardown.
	// Only a successful LinkAdd grants ownership of this pair.
	cleanup = func() error {
		if err := netlink.LinkDel(veth); err != nil && !errors.Is(err, syscall.ENODEV) {
			return fmt.Errorf("delete veth pair %s/%s: %w", hostVethName, containerVethName, err)
		}
		return nil
	}

	// Attach the host-side veth to the host bridge so it can reach the gateway.
	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return cleanup, fmt.Errorf("find bridge %s: %w", bridgeName, err)
	}

	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil {
		return cleanup, fmt.Errorf("find host veth %s: %w", hostVethName, err)
	}

	if err := netlink.LinkSetMaster(hostVeth, br); err != nil {
		return cleanup, fmt.Errorf("attach host veth %s to bridge %s: %w", hostVethName, bridgeName, err)
	}

	if err := netlink.LinkSetUp(hostVeth); err != nil {
		return cleanup, fmt.Errorf("set host veth %s up: %w", hostVethName, err)
	}

	// Move the container-side veth into the target network namespace.
	containerVeth, err := netlink.LinkByName(containerVethName)
	if err != nil {
		return cleanup, fmt.Errorf("find container veth %s: %w", containerVethName, err)
	}

	if err := netlink.LinkSetNsPid(containerVeth, containerPID); err != nil {
		return cleanup, fmt.Errorf("move container veth %s to netns of pid %d: %w", containerVethName, containerPID, err)
	}

	return cleanup, configureContainerInterface(
		targetNS,
		containerVethName,
		defaultContainerInterfaceName,
		containerAddress,
		gatewayAddress,
	)
}

// configureContainerInterface runs inside targetNS and makes the moved veth
// usable as the container's eth0 interface.
func configureContainerInterface(
	targetNS netns.NsHandle,
	currentName string,
	targetName string,
	containerAddress string,
	gatewayAddress string,
) error {
	return runInNetworkNamespace(targetNS, func() error {
		// The moved peer is visible by its temporary name only inside targetNS now.
		link, err := netlink.LinkByName(currentName)
		if err != nil {
			return fmt.Errorf("find container interface %s: %w", currentName, err)
		}

		// Give the peer the conventional eth0 name that container processes expect.
		if err := netlink.LinkSetName(link, targetName); err != nil {
			return fmt.Errorf("rename container interface %s to %s: %w", currentName, targetName, err)
		}

		// Look it up again by the final name so the following operations read clearly.
		link, err = netlink.LinkByName(targetName)
		if err != nil {
			return fmt.Errorf("find container interface %s: %w", targetName, err)
		}

		// Assign the address supplied by IPAM.
		addr, err := netlink.ParseAddr(containerAddress)
		if err != nil {
			return fmt.Errorf("parse container address %s: %w", containerAddress, err)
		}
		if err := netlink.AddrAdd(link, addr); err != nil {
			return fmt.Errorf("assign container address %s to %s: %w", containerAddress, targetName, err)
		}

		// Bring the interface up so the kernel can transmit and receive through it.
		if err := netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("set container interface %s up: %w", targetName, err)
		}

		// Bring loopback up for programs that expect 127.0.0.1 inside the container.
		if err := setLoopbackUp(); err != nil {
			return err
		}

		// Install default routing while this thread still observes the container netns.
		if err := SetupDefaultRoute(RouteConfig{
			InterfaceName:  targetName,
			GatewayAddress: gatewayAddress,
		}); err != nil {
			return err
		}

		return nil
	})
}

// setLoopbackUp enables lo inside the current network namespace.
func setLoopbackUp() error {
	link, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("find loopback interface: %w", err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("set loopback interface up: %w", err)
	}

	return nil
}

// runInNetworkNamespace confines setns to a dedicated goroutine. If restoring
// the host namespace fails, Go retires the locked thread instead of reusing it
// for another daemon request in the wrong namespace.
func runInNetworkNamespace(targetNS netns.NsHandle, fn func() error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		currentNS, err := netns.Get()
		if err != nil {
			runtime.UnlockOSThread()
			done <- fmt.Errorf("get current netns: %w", err)
			return
		}
		defer currentNS.Close()
		if err := netns.Set(targetNS); err != nil {
			runtime.UnlockOSThread()
			done <- fmt.Errorf("enter target netns: %w", err)
			return
		}
		err = fn()
		if restoreErr := netns.Set(currentNS); restoreErr != nil {
			done <- errors.Join(err, fmt.Errorf("restore original netns: %w", restoreErr))
			return
		}
		runtime.UnlockOSThread()
		done <- err
	}()
	return <-done
}
