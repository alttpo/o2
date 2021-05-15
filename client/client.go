package client

import (
	"log"
	"o2/udpclient"
)

type Client struct {
	udpclient.UDPClient

	// model state:
	group    [20]byte
	hostName string
}

func NewClient() *Client {
	c := &Client{
		group: [20]byte{},
	}
	udpclient.MakeUDPClient("client", &c.UDPClient)
	return c
}

func (c *Client) Group() []byte { return c.group[:] }
func (c *Client) SetGroup(group string) {
	n := copy(c.group[:], group)
	for ; n < 20; n++ {
		c.group[n] = ' '
	}
	log.Printf("client: actual group name '%s'\n", c.group[:])
}

func (c *Client) SetHostName(hostName string) { c.hostName = hostName }
func (c *Client) HostName() string            { return c.hostName }
