package fxpakpro

import (
	"errors"
	"fmt"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
	"o2/snes"
	"strconv"
	"strings"
)

type Driver struct{}

type rwop struct {
	isRead bool
	read   []snes.ReadRequest
	write  []snes.WriteRequest
}

type Conn struct {
	f serial.Port
	q chan rwop
}

var (
	ErrNoFXPakProFound = errors.New("fxpakpro: no device found among serial ports")
	baudRates          = []int{
		921600, // first rate that works on Windows
		460800,
		256000,
		230400, // first rate that works on MacOS
		153600,
		128000,
		115200,
		76800,
		57600,
		38400,
		28800,
		19200,
		14400,
		9600,
	}
)

func DetectDevice() (portName string, err error) {
	var ports []*enumerator.PortDetails

	portName = ""

	ports, err = enumerator.GetDetailedPortsList()
	if err != nil {
		return
	}

	for _, port := range ports {
		if !port.IsUSB {
			continue
		}

		//log.Printf("%s: Found USB port\n", port.Name)
		//if port.IsUSB {
		//	log.Printf("   USB ID     %s:%s\n", port.VID, port.PID)
		//	log.Printf("   USB serial %s\n", port.SerialNumber)
		//}

		if port.SerialNumber == "DEMO00000000" {
			portName = port.Name
			err = nil
			return
		}
	}

	return
}

func (d *Driver) Open(name string) (snes.Conn, error) {
	var err error

	parts := strings.Split(name, ";")

	portName := parts[0]
	if portName == "" {
		portName, err = DetectDevice()
		if err != nil {
			return nil, err
		}
	}
	if portName == "" {
		return nil, ErrNoFXPakProFound
	}

	baudRequest := baudRates[0]
	if len(parts) > 1 {
		if n, e := strconv.Atoi(parts[1]); e == nil {
			baudRequest = n
		}
	}

	// Try all the common baud rates in descending order:
	f := serial.Port(nil)
	for _, baud := range baudRates {
		if baud > baudRequest {
			continue
		}

		//log.Printf("%s: open(%d)\n", portName, baud)
		f, err = serial.Open(portName, &serial.Mode{
			BaudRate: baud,
			DataBits: 8,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
		})
		if err == nil {
			break
		}
		//log.Printf("%s: %v\n", portName, err)
	}
	if err != nil {
		return nil, fmt.Errorf("fxpakpro: failed to open serial port at any baud rate: %w", err)
	}

	// set DTR:
	//log.Printf("serial: Set DTR on\n")
	if err = f.SetDTR(true); err != nil {
		//log.Printf("serial: %v\n", err)
		f.Close()
		return nil, fmt.Errorf("fxpakpro: failed to set DTR: %w", err)
	}

	c := &Conn{f: f, q: make(chan rwop, 64)}
	go c.handleQueue()

	return c, err
}

func init() {
	snes.Register("fxpakpro", &Driver{})
}
