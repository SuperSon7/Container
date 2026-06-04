package network

type Config struct {
	// Enabled controls whether container networking should be configured.
	Enabled bool

	// BridgeName is the bridge device created in the host network namespace.
	BridgeName string

	// HostInterfaceName is the veth endpoint kept in the host network namespace.
	HostInterfaceName string

	// ContainerInterfaceName is the veth endpoint moved into the container network namespace.
	ContainerInterfaceName string

	// ContainerAddress is the IP/CIDR assigned to the container-side interface.
	ContainerAddress string

	// GatewayAddress is the IP/CIDR assigned to the bridge.
	// Containers use this address as their default gateway.
	GatewayAddress string

	// EnableNAT controls whether container subnet traffic is masqueraded on the host.
	EnableNAT bool
}
