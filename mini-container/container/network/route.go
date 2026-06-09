package network

type RouteConfig struct {
	// InterfaceName is the interface used inside the container network namespace.
	InterfaceName string

	// GatewayAddress is the gateway IP reachable through InterfaceName.
	GatewayAddress string
}

// SetupDefaultRoute configures the container's default route in the current netns.
func SetupDefaultRoute(config RouteConfig) error {
	// TODO: Add "default via GatewayAddress dev InterfaceName" inside the container netns.
	return ErrNotImplemented
}

// DestroyDefaultRoute removes the default route created by SetupDefaultRoute.
func DestroyDefaultRoute(config RouteConfig) error {
	// TODO: Remove the container default route from the current netns.
	return ErrNotImplemented
}
