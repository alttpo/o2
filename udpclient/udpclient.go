package udpclient

import (
	"errors"
	"fmt"
	"log"
	"net"
	"time"
)

type UDPClient struct {
	name string

	c *net.UDPConn

	isConnected bool
	read        chan []byte
	write       chan []byte

	hostname string
	port     uint16
}

func NewUDPClient(name string) *UDPClient {
	return &UDPClient{
		name:  name,
		read:  make(chan []byte, 64),
		write: make(chan []byte, 64),
	}
}

func MakeUDPClient(name string, c *UDPClient) *UDPClient {
	c.name = name
	c.read = make(chan []byte, 64)
	c.write = make(chan []byte, 64)
	return c
}

func (c *UDPClient) Hostname() string { return c.hostname }
func (c *UDPClient) Port() uint16     { return c.port }

func (c *UDPClient) Write() chan<- []byte { return c.write }
func (c *UDPClient) Read() <-chan []byte  { return c.read }

func (c *UDPClient) IsConnected() bool { return c.isConnected }

func (c *UDPClient) Connect(hostname string, port uint16) (err error) {
	log.Printf("%s: connect to server '%s'\n", c.name, hostname)

	if c.isConnected {
		return fmt.Errorf("%s: already connected", c.name)
	}

	c.hostname = hostname
	c.port = port

	hostport := fmt.Sprintf("%s:%d", hostname, port)
	raddr, err := net.ResolveUDPAddr("udp", hostport)
	if err != nil {
		return
	}

	c.c, err = net.DialUDP("udp", nil, raddr)
	if err != nil {
		return
	}

	c.isConnected = true
	log.Printf("%s: connected to server '%s'\n", c.name, hostname)

	go c.readLoop()
	go c.writeLoop()

	return
}

func (c *UDPClient) Disconnect() {
	log.Printf("%s: disconnect from server '%s'\n", c.name, c.hostname)

	if !c.isConnected {
		return
	}

	c.isConnected = false
	err := c.c.SetReadDeadline(time.Now())
	if err != nil {
		log.Printf("%s: setreaddeadline: %v\n", c.name, err)
	}

	err = c.c.SetWriteDeadline(time.Now())
	if err != nil {
		log.Printf("%s: setwritedeadline: %v\n", c.name, err)
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
		log.Printf("%s: close: %v\n", c.name, err)
	}

	log.Printf("%s: disconnected from server '%s'\n", c.name, c.hostname)

	c.c = nil
}

func (c *UDPClient) Close() {
	close(c.read)
	close(c.write)
	c.read = nil
	c.write = nil
}

// must run in a goroutine
func (c *UDPClient) readLoop() {
	log.Printf("%s: readLoop started\n", c.name)

	defer func() {
		c.Disconnect()
		log.Printf("%s: disconnected; readLoop exited\n", c.name)
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
func (c *UDPClient) writeLoop() {
	log.Printf("%s: writeLoop started\n", c.name)

	defer func() {
		c.Disconnect()
		log.Printf("%s: disconnected; writeLoop exited\n", c.name)
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
