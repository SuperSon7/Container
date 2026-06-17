package network

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var ipv4ForwardingPath = "/proc/sys/net/ipv4/ip_forward"
var (
	// runIPTables is replaceable in tests so unit tests never mutate host iptables.
	runIPTables = runIPTablesCommand

	// checkNATRule is replaceable in tests to drive existing/missing rule paths.
	checkNATRule = natRuleExists
)

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

	exists, err := checkNATRule(config)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	return runIPTables(natRuleArgs("-A", config)...)
}

// DestroyNAT removes host-side NAT state created by SetupNAT.
func DestroyNAT(config NATConfig) error {
	exists, err := checkNATRule(config)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	return runIPTables(natRuleArgs("-D", config)...)
}

func natRuleExists(config NATConfig) (bool, error) {
	err := runIPTables(natRuleArgs("-C", config)...)
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}

	return false, err
}

func natRuleArgs(action string, config NATConfig) []string {
	return []string{
		"-t", "nat",
		action, "POSTROUTING",
		"-s", config.SourceCIDR,
		"-o", config.OutboundInterface,
		"-j", "MASQUERADE",
	}
}

// runIPTablesCommand keeps iptables stderr/stdout attached to the returned error.
func runIPTablesCommand(args ...string) error {
	cmd := exec.Command("iptables", args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	details := strings.TrimSpace(string(output))
	if details == "" {
		return fmt.Errorf("iptables %s: %w", strings.Join(args, " "), err)
	}

	return fmt.Errorf("iptables %s: %w: %s", strings.Join(args, " "), err, details)
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
