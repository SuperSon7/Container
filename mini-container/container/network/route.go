package network

import (
	"fmt"
	"net"
	"strings"

	"github.com/vishvananda/netlink"
)

type RouteConfig struct {
	// InterfaceName is the interface used inside the container network namespace.
	InterfaceName string

	// GatewayAddress is the gateway IP reachable through InterfaceName.
	// It may be a bare IP or an IP/CIDR value from Config.GatewayAddress.
	GatewayAddress string
}

// SetupDefaultRoute configures the container's default route in the current netns.
func SetupDefaultRoute(config RouteConfig) error {
	link, err := netlink.LinkByName(config.InterfaceName)
	if err != nil {
		return fmt.Errorf("find route interface %s: %w", config.InterfaceName, err)
	}

	gateway, err := parseGatewayAddress(config.GatewayAddress)
	if err != nil {
		return err
	}

	route := netlink.Route{
		LinkIndex: link.Attrs().Index,
		Gw:        gateway,
	}
	if err := netlink.RouteAdd(&route); err != nil {
		return fmt.Errorf("add default route via %s dev %s: %w", config.GatewayAddress, config.InterfaceName, err)
	}

	return nil
}

// DestroyDefaultRoute removes the default route created by SetupDefaultRoute.
func DestroyDefaultRoute(config RouteConfig) error {
	link, err := netlink.LinkByName(config.InterfaceName)
	if err != nil {
		return fmt.Errorf("find route interface %s: %w", config.InterfaceName, err)
	}

	gateway, err := parseGatewayAddress(config.GatewayAddress)
	if err != nil {
		return err
	}

	route := netlink.Route{
		LinkIndex: link.Attrs().Index,
		Gw:        gateway,
	}
	if err := netlink.RouteDel(&route); err != nil {
		return fmt.Errorf("delete default route via %s dev %s: %w", config.GatewayAddress, config.InterfaceName, err)
	}

	return nil
}

func parseGatewayAddress(address string) (net.IP, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("network: gateway address is required")
	}

	if ip := net.ParseIP(address); ip != nil {
		return ip, nil
	}

	ip, _, err := net.ParseCIDR(address)
	if err == nil {
		return ip, nil
	}

	return nil, fmt.Errorf("network: invalid gateway address %q", address)
}
