package network

import "testing"

func TestParseGatewayAddressAcceptsIP(t *testing.T) {
	ip, err := parseGatewayAddress("10.0.0.1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if got := ip.String(); got != "10.0.0.1" {
		t.Fatalf("expected gateway 10.0.0.1, got %s", got)
	}
}

func TestParseGatewayAddressAcceptsCIDR(t *testing.T) {
	ip, err := parseGatewayAddress("10.0.0.1/24")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if got := ip.String(); got != "10.0.0.1" {
		t.Fatalf("expected gateway 10.0.0.1, got %s", got)
	}
}

func TestParseGatewayAddressRejectsInvalidValue(t *testing.T) {
	for _, address := range []string{"", "not-an-ip", "10.0.0.1/invalid"} {
		t.Run(address, func(t *testing.T) {
			if _, err := parseGatewayAddress(address); err == nil {
				t.Fatal("expected error for invalid gateway address")
			}
		})
	}
}
