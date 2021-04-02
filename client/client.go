package client

import (
	"errors"
	"fmt"
	"log"
	"net"
	"time"
)

type Client struct {
	c *net.UDPConn

	isConnected bool
	read        chan []byte
	write       chan []byte

	// model state:
	hostname string
	group    [20]byte
}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Group() []byte    { return c.group[:] }
func (c *Client) Hostname() string { return c.hostname }

func (c *Client) Write() chan<- []byte { return c.write }
func (c *Client) Read() <-chan []byte  { return c.read }

func (c *Client) IsConnected() bool { return c.isConnected }

func (c *Client) Connect(hostname string, group string) (err error) {
	if c.isConnected {
		return fmt.Errorf("already connected")
	}

	c.hostname = hostname
	copy(c.group[:], group)

	raddr, err := net.ResolveUDPAddr("udp", hostname+":4590")
	if err != nil {
		return
	}

	c.c, err = net.DialUDP("udp", nil, raddr)
	if err != nil {
		return
	}

	c.read = make(chan []byte, 64)
	c.write = make(chan []byte, 64)

	c.isConnected = true
	go c.readLoop()
	go c.writeLoop()

	return
}

func (c *Client) Disconnect() {
	if !c.isConnected {
		return
	}

	c.isConnected = false
	err := c.c.SetReadDeadline(time.Now())
	if err != nil {
		log.Print(err)
	}

	err = c.c.SetWriteDeadline(time.Now())
	if err != nil {
		log.Print(err)
	}

	close(c.read)
	close(c.write)

	err = c.c.Close()
	if err != nil {
		log.Print(err)
	}

	c.c = nil
}

// must run in a goroutine
func (c *Client) readLoop() {
	defer c.Disconnect()

	// we only need a single receive buffer:
	b := make([]byte, 1500)

	for c.isConnected {
		// wait for a packet from UDP socket:
		var n, _, err = c.c.ReadFromUDP(b)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Print(err)
			}
			return
		}

		// copy the envelope:
		envelope := make([]byte, n)
		copy(envelope, b[:n])

		c.read <- envelope
	}
}

// must run in a goroutine
func (c *Client) writeLoop() {
	defer c.Disconnect()

	for w := range c.write {
		// wait for a packet from UDP socket:
		var _, err = c.c.Write(w)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Print(err)
			}
			return
		}
	}
}
