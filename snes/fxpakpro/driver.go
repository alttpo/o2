package fxpakpro

import (
	"errors"
	"fmt"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
	"log"
	"o2/snes"
)

var (
	ErrNoFXPakProFound = errors.New("fxpakpro: no device found among serial ports")

	baudRates = []int{
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

type Driver struct{}

func (d *Driver) DisplayName() string {
	return "FX Pak Pro"
}

func (d *Driver) DisplayDescription() string {
	return "Connect to an FX Pak Pro or SD2SNES via USB"
}

type DeviceDescriptor struct {
	Port string
	Baud *int
	VID  string
	PID  string
}

func (d DeviceDescriptor) DisplayName() string {
	return fmt.Sprintf("%s (%s:%s)", d.Port, d.VID, d.PID)
}

type Conn struct {
	// must be only accessed via Command.Execute
	f serial.Port

	// command execution queue:
	cq chan CommandWithCallback
}

func (d *Driver) Empty() snes.DeviceDescriptor {
	return DeviceDescriptor{
		Port: "",
		Baud: &(baudRates[0]),
	}
}

func (d *Driver) Detect() (devices []snes.DeviceDescriptor, err error) {
	var ports []*enumerator.PortDetails

	// It would be surprising to see more than one FX Pak Pro connected to a PC.
	devices = make([]snes.DeviceDescriptor, 0, 1)

	ports, err = enumerator.GetDetailedPortsList()
	if err != nil {
		return
	}

	for _, port := range ports {
		if !port.IsUSB {
			continue
		}

		//log.Printf("   USB ID     %s:%s\n", port.VID, port.PID)
		//log.Printf("   USB serial %s\n", port.SerialNumber)

		if port.SerialNumber == "DEMO00000000" {
			devices = append(devices, DeviceDescriptor{
				port.Name,
				nil,
				port.VID,
				port.PID,
			})
		}
	}

	err = nil
	return
}

func (d *Driver) Open(ddg snes.DeviceDescriptor) (snes.Conn, error) {
	var err error

	dd := ddg.(DeviceDescriptor)
	portName := dd.Port
	if portName == "" {
		ddgs, err := d.Detect()
		if err != nil {
			return nil, err
		}

		// pick first device found, if any:
		if len(ddgs) > 0 {
			portName = ddgs[0].(DeviceDescriptor).Port
		}
	}
	if portName == "" {
		return nil, ErrNoFXPakProFound
	}

	baudRequest := baudRates[0]
	if dd.Baud != nil {
		b := *dd.Baud
		if b > 0 {
			baudRequest = b
		}
	}

	// Try all the common baud rates in descending order:
	f := serial.Port(nil)
	var baud int
	for _, baud = range baudRates {
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

	// set baud rate on descriptor:
	pBaud := new(int)
	*pBaud = baud
	dd.Baud = pBaud

	// set DTR:
	//log.Printf("serial: Set DTR on\n")
	if err = f.SetDTR(true); err != nil {
		//log.Printf("serial: %v\n", err)
		f.Close()
		return nil, fmt.Errorf("fxpakpro: failed to set DTR: %w", err)
	}

	c := &Conn{
		f:  f,
		cq: make(chan CommandWithCallback, 64),
	}
	go c.handleQueue()

	return c, err
}

func (c *Conn) handleQueue() {
	var err error
	defer func() {
		log.Printf("fxpakpro: %v\n", err)
	}()

	for {
		pair := <-c.cq
		cmd := pair.Command
		if cmd == nil {
			break
		}

		err = cmd.Execute(c.f)
		if pair.OnComplete != nil {
			pair.OnComplete(err)
		} else if err != nil {
			log.Println(err)
		}
	}
}

func (c *Conn) submitCommand(cmd Command) {
	c.cq <- CommandWithCallback{
		Command:    cmd,
		OnComplete: nil,
	}
}

func (c *Conn) submitCommandWithCallback(cmd Command, onComplete func(error)) {
	c.cq <- CommandWithCallback{
		Command:    cmd,
		OnComplete: onComplete,
	}
}

func init() {
	snes.Register("fxpakpro", &Driver{})
}
