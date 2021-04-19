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
	return &Client{
		read:  make(chan []byte, 64),
		write: make(chan []byte, 64),
	}
}

func (c *Client) Group() []byte    { return c.group[:] }
func (c *Client) Hostname() string { return c.hostname }

func (c *Client) SetGroup(group string) {
	n := copy(c.group[:], group)
	for ; n < 20; n++ {
		c.group[n] = ' '
	}
	log.Printf("client: actual group name '%s'\n", c.group[:])
}

func (c *Client) Write() chan<- []byte { return c.write }
func (c *Client) Read() <-chan []byte  { return c.read }

func (c *Client) IsConnected() bool { return c.isConnected }

func (c *Client) Connect(hostname string) (err error) {
	log.Printf("client: connect to server '%s'\n", hostname)

	if c.isConnected {
		return fmt.Errorf("already connected")
	}

	c.hostname = hostname

	raddr, err := net.ResolveUDPAddr("udp", hostname+":4590")
	if err != nil {
		return
	}

	c.c, err = net.DialUDP("udp", nil, raddr)
	if err != nil {
		return
	}

	c.isConnected = true
	log.Printf("client: connected to server '%s'\n", hostname)

	go c.readLoop()
	go c.writeLoop()

	return
}

func (c *Client) Disconnect() {
	log.Printf("client: disconnect from server '%s'\n", c.hostname)

	if !c.isConnected {
		return
	}

	c.isConnected = false
	err := c.c.SetReadDeadline(time.Now())
	if err != nil {
		log.Printf("client: setreaddeadline: %v", err)
	}

	err = c.c.SetWriteDeadline(time.Now())
	if err != nil {
		log.Printf("client: setwritedeadline: %v", err)
	}

	// signal a disconnect took place:
	c.read <- nil
	c.write <- nil

	// empty the write channel:
	for more := true; more; {
		select {
		case <-c.write:
		default:
			more = false
		}
	}

	// close the underlying connection:
	err = c.c.Close()
	if err != nil {
		log.Printf("client: close: %v", err)
	}

	log.Printf("client: disconnected from server '%s'\n", c.hostname)

	c.c = nil
}

func (c *Client) Close() {
	close(c.read)
	close(c.write)
	c.read = nil
	c.write = nil
}

// must run in a goroutine
func (c *Client) readLoop() {
	log.Printf("client: readLoop started\n")

	defer func() {
		c.Disconnect()
		log.Println("client disconnected; readLoop exited")
	}()

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
	log.Printf("client: writeLoop started\n")

	defer func() {
		c.Disconnect()
		log.Println("client disconnected; writeLoop exited")
	}()

	for w := range c.write {
		if w == nil {
			return
		}

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
