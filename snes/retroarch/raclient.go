package retroarch

import (
	"net"
	"o2/udpclient"
)

type RAClient struct {
	udpclient.UDPClient

	addr *net.UDPAddr
}

func (c *RAClient) GetId() string {
	return c.addr.String()
}
