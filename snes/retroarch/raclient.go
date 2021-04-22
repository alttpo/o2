package retroarch

import (
	"net"
	"o2/udpclient"
)

type RAClient struct {
	udpclient.UDPClient

	addr *net.UDPAddr
}
