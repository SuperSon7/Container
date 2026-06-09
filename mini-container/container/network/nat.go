package network

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var ipv4ForwardingPath = "/proc/sys/net/ipv4/ip_forward"

type NATConfig struct {
	// SourceCIDR is the container subnet that should be masqueraded.
	SourceCIDR string

	// OutboundInterface is the host interface used to leave the host network.
	OutboundInterface string
}

// SetupNAT configures host-side masquerading for container subnet traffic.
func SetupNAT(config NATConfig) error {
	// The host must forward packets between the bridge and outbound interface.
	// Without IP forwarding, Linux drops transit packets whose destination is not local.
	if err := enableIPv4Forwarding(); err != nil {
		return err
	}

	cmd := exec.Command(
		"iptables",
		"-t", "nat",
		"-A", "POSTROUTING",
		"-s", config.SourceCIDR,
		"-o", config.OutboundInterface,
		"-j", "MASQUERADE",
	)

	return cmd.Run()
}

// DestroyNAT removes host-side NAT state created by SetupNAT.
func DestroyNAT(config NATConfig) error {
	cmd := exec.Command(
		"iptables",
		"-t", "nat",
		"-D", "POSTROUTING",
		"-s", config.SourceCIDR,
		"-o", config.OutboundInterface,
		"-j", "MASQUERADE",
	)

	return cmd.Run()
}

func enableIPv4Forwarding() error {
	current, err := os.ReadFile(ipv4ForwardingPath)
	if err != nil {
		return fmt.Errorf("read IPv4 forwarding setting: %w", err)
	}

	if strings.TrimSpace(string(current)) == "1" {
		return nil
	}

	if err := os.WriteFile(ipv4ForwardingPath, []byte("1\n"), 0644); err != nil {
		return fmt.Errorf("enable IPv4 forwarding: %w", err)
	}

	return nil
}
