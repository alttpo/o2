package client

import (
	"log"
	"o2/client/protocol05"
	"o2/udpclient"
	"strings"
	"time"
)

type Client struct {
	udpclient.UDPClient

	// model state:
	group    string
	hostName string
}

func NewClient() *Client {
	c := &Client{}
	udpclient.MakeUDPClient("client", &c.UDPClient)
	return c
}

func (c *Client) Group() []byte { return []byte(c.group) }

// SetGroup sets the group to join; the server ignores surrounding whitespace and case.
func (c *Client) SetGroup(group string) {
	c.group = strings.TrimSpace(group)
	log.Printf("client: actual group name '%s'\n", c.group)
}

func (c *Client) SetHostName(hostName string) { c.hostName = hostName }
func (c *Client) HostName() string            { return c.hostName }

// Disconnect tells the server we left the group (best effort, so twice) and disconnects.
func (c *Client) Disconnect() {
	if c.IsConnected() {
		for i := 0; i < 2; i++ {
			if err := c.WriteTimeout(protocol05.MakeLeave(), 50*time.Millisecond); err != nil {
				break
			}
		}
		// let the write loop send them before the connection closes:
		time.Sleep(20 * time.Millisecond)
	}
	c.UDPClient.Disconnect()
}
