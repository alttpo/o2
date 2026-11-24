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
	password string
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

// Password is the group's password, empty for a group without one. It never leaves the client; see protocol05's auth.go.
func (c *Client) Password() string { return c.password }

// SetPassword sets the password of the group to join: it's set on the group if this player is the first to join it,
// and proven otherwise.
func (c *Client) SetPassword(password string) { c.password = password }

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
