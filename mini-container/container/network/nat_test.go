package network

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnableIPv4ForwardingLeavesEnabledValue(t *testing.T) {
	path := writeForwardingSetting(t, "1\n")
	useIPv4ForwardingPath(t, path)

	if err := enableIPv4Forwarding(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	assertForwardingSetting(t, path, "1\n")
}

func TestEnableIPv4ForwardingEnablesDisabledValue(t *testing.T) {
	path := writeForwardingSetting(t, "0\n")
	useIPv4ForwardingPath(t, path)

	if err := enableIPv4Forwarding(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	assertForwardingSetting(t, path, "1\n")
}

func TestEnableIPv4ForwardingReturnsReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	useIPv4ForwardingPath(t, path)

	if err := enableIPv4Forwarding(); err == nil {
		t.Fatal("expected error for missing forwarding setting")
	}
}

func TestSetupNATSkipsExistingRule(t *testing.T) {
	path := writeForwardingSetting(t, "1\n")
	useIPv4ForwardingPath(t, path)
	useNATRuleCheck(t, true, nil)
	useIPTablesRunner(t, func(args ...string) error {
		t.Fatalf("expected iptables add not to run, got %v", args)
		return nil
	})

	if err := SetupNAT(validNATConfig()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestSetupNATAddsMissingRule(t *testing.T) {
	path := writeForwardingSetting(t, "1\n")
	useIPv4ForwardingPath(t, path)
	useNATRuleCheck(t, false, nil)
	var calls [][]string
	useIPTablesRunner(t, func(args ...string) error {
		calls = append(calls, args)
		return nil
	})

	if err := SetupNAT(validNATConfig()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	want := [][]string{{
		"-t", "nat",
		"-A", "POSTROUTING",
		"-s", "10.0.0.0/24",
		"-o", "eth0",
		"-j", "MASQUERADE",
	}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("expected iptables calls %v, got %v", want, calls)
	}
}

func TestSetupNATReturnsRuleCheckError(t *testing.T) {
	path := writeForwardingSetting(t, "1\n")
	useIPv4ForwardingPath(t, path)
	checkErr := errors.New("check failed")
	useNATRuleCheck(t, false, checkErr)

	err := SetupNAT(validNATConfig())
	if !errors.Is(err, checkErr) {
		t.Fatalf("expected rule check error, got %v", err)
	}
}

func TestDestroyNATSkipsMissingRule(t *testing.T) {
	useNATRuleCheck(t, false, nil)
	useIPTablesRunner(t, func(args ...string) error {
		t.Fatalf("expected iptables delete not to run, got %v", args)
		return nil
	})

	if err := DestroyNAT(validNATConfig()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestDestroyNATDeletesExistingRule(t *testing.T) {
	useNATRuleCheck(t, true, nil)
	var calls [][]string
	useIPTablesRunner(t, func(args ...string) error {
		calls = append(calls, args)
		return nil
	})

	if err := DestroyNAT(validNATConfig()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	want := [][]string{{
		"-t", "nat",
		"-D", "POSTROUTING",
		"-s", "10.0.0.0/24",
		"-o", "eth0",
		"-j", "MASQUERADE",
	}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("expected iptables calls %v, got %v", want, calls)
	}
}

func TestRunIPTablesCommandIncludesOutputInError(t *testing.T) {
	dir := t.TempDir()
	iptablesPath := filepath.Join(dir, "iptables")
	if err := os.WriteFile(iptablesPath, []byte("#!/bin/sh\necho iptables failed >&2\nexit 2\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runIPTablesCommand("-t", "nat")
	if err == nil {
		t.Fatal("expected iptables error")
	}
	if !strings.Contains(err.Error(), "iptables -t nat") {
		t.Fatalf("expected command args in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "iptables failed") {
		t.Fatalf("expected command output in error, got %v", err)
	}
}

func useIPv4ForwardingPath(t *testing.T, path string) {
	t.Helper()

	old := ipv4ForwardingPath
	ipv4ForwardingPath = path
	t.Cleanup(func() {
		ipv4ForwardingPath = old
	})
}

func useIPTablesRunner(t *testing.T, run func(args ...string) error) {
	t.Helper()

	old := runIPTables
	runIPTables = run
	t.Cleanup(func() {
		runIPTables = old
	})
}

func useNATRuleCheck(t *testing.T, exists bool, err error) {
	t.Helper()

	old := checkNATRule
	checkNATRule = func(config NATConfig) (bool, error) {
		return exists, err
	}
	t.Cleanup(func() {
		checkNATRule = old
	})
}

func validNATConfig() NATConfig {
	return NATConfig{
		SourceCIDR:        "10.0.0.0/24",
		OutboundInterface: "eth0",
	}
}

func writeForwardingSetting(t *testing.T, value string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ip_forward")
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		t.Fatalf("write forwarding setting: %v", err)
	}

	return path
}

func assertForwardingSetting(t *testing.T, path string, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read forwarding setting: %v", err)
	}

	if string(got) != want {
		t.Fatalf("expected forwarding setting %q, got %q", want, string(got))
	}
}
