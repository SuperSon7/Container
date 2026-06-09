package network

import (
	"os"
	"path/filepath"
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

func useIPv4ForwardingPath(t *testing.T, path string) {
	t.Helper()

	old := ipv4ForwardingPath
	ipv4ForwardingPath = path
	t.Cleanup(func() {
		ipv4ForwardingPath = old
	})
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
