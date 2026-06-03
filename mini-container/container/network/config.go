package network

type Config struct {
	Enabled bool

	BridgeName             string
	HostInterfaceName      string
	ContainerInterfaceName string

	ContainerAddress string
	GatewayAddress   string

	EnableNAT bool
}
