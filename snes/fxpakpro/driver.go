package fxpakpro

import (
	"errors"
	"fmt"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
	"log"
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

	c := &Conn{f: f, q: make(chan rwop)}
	go c.handleQueue()

	return c, err
}

func (c *Conn) Close() (err error) {
	// Clear DTR (ignore any errors since we're closing):
	c.f.SetDTR(false)

	// Close the port:
	err = c.f.Close()
	if err != nil {
		return fmt.Errorf("fxpakpro: could not close serial port: %w", err)
	}

	return
}

func (c *Conn) SubmitRead(reqs []snes.ReadRequest) {
	c.q <- rwop{isRead: true, read: reqs}
}

func (c *Conn) SubmitWrite(reqs []snes.WriteRequest) {
	c.q <- rwop{isRead: false, write: reqs}
}

func sendSerial(f serial.Port, buf []byte) error {
	sent := 0
	for sent < len(buf) {
		n, e := f.Write(buf[sent:])
		if e != nil {
			return e
		}
		sent += n
	}
	return nil
}

func recvSerial(f serial.Port, rsp []byte, expected int) error {
	o := 0
	for o < expected {
		n, err := f.Read(rsp[o:expected])
		if err != nil {
			return err
		}
		if n <= 0 {
			return fmt.Errorf("recvSerial: Read returned %d", n)
		}
		o += n
	}
	return nil
}

func (c *Conn) sendVGET(reqs []snes.ReadRequest) (int, error) {
	// VGET:
	sb := make([]byte, 64)
	sb[0] = byte('U')
	sb[1] = byte('S')
	sb[2] = byte('B')
	sb[3] = byte('A')
	sb[4] = byte(OpVGET)
	sb[5] = byte(SpaceSNES)
	sb[6] = byte(FlagDATA64B | FlagNORESP)

	expected := 0
	for i := 0; i < len(reqs); i++ {
		// 4-byte struct: 1 byte size, 3 byte address
		sb[32+(i*4)] = reqs[i].Size
		sb[33+(i*4)] = byte((reqs[i].Address >> 16) & 0xFF)
		sb[34+(i*4)] = byte((reqs[i].Address >> 8) & 0xFF)
		sb[35+(i*4)] = byte((reqs[i].Address >> 0) & 0xFF)
		expected += int(reqs[i].Size)
	}

	return expected, sendSerial(c.f, sb)
}

func (c *Conn) sendVGETBatch(batch []snes.ReadRequest) error {
	log.Printf("fxpakpro queue: VGET %d requests\n", len(batch))
	total, err := c.sendVGET(batch)
	if err != nil {
		return err
	}

	// wait for response:
	for i := 0; i < len(batch); i++ {
		size := int(batch[i].Size)
		log.Printf("fxpakpro queue: %d VGET wait for %d bytes\n", i, size)
		rsp := make([]byte, size)

		err = recvSerial(c.f, rsp, size)
		if err != nil {
			return err
		}

		// make response callback:
		cb := batch[i].Completed
		if cb != nil {
			cb(snes.ReadOrWriteResponse{
				IsWrite: false,
				Address: batch[i].Address,
				Size:    batch[i].Size,
				Data:    rsp,
			})
		}
	}

	remainder := total & 63
	if remainder > 0 {
		log.Printf("fxpakpro queue: VGET wait for %d remainder\n", remainder)
		buf := make([]byte, 64)
		err = recvSerial(c.f, buf, remainder)
		buf = nil
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Conn) sendVPUT(reqs []snes.WriteRequest) (int, error) {
	// VGET:
	sb := make([]byte, 64)
	sb[0] = byte('U')
	sb[1] = byte('S')
	sb[2] = byte('B')
	sb[3] = byte('A')
	sb[4] = byte(OpVPUT)
	sb[5] = byte(SpaceSNES)
	sb[6] = byte(FlagDATA64B | FlagNORESP)

	expected := 0
	for i := 0; i < len(reqs); i++ {
		// 4-byte struct: 1 byte size, 3 byte address
		sb[32+(i*4)] = reqs[i].Size
		sb[33+(i*4)] = byte((reqs[i].Address >> 16) & 0xFF)
		sb[34+(i*4)] = byte((reqs[i].Address >> 8) & 0xFF)
		sb[35+(i*4)] = byte((reqs[i].Address >> 0) & 0xFF)
		expected += int(reqs[i].Size)
	}

	return expected, sendSerial(c.f, sb)
}

func (c *Conn) sendVPUTBatch(batch []snes.WriteRequest) error {
	total, err := c.sendVPUT(batch)
	if err != nil {
		return err
	}

	// determine total size of accompanying data:
	packets := total / 64
	remainder := total & 63
	// round up to accommodate any remainder:
	if remainder > 0 {
		packets++
	}

	// concatenate all accompanying data together in one large slice:
	whole := make([]byte, packets*64)
	o := 0
	for i := 0; i < len(batch); i++ {
		copy(whole[o:], batch[i].Data)
		o += len(batch[i].Data)
	}

	// send whole slice over serial port:
	err = sendSerial(c.f, whole)
	if err != nil {
		return err
	}

	// reply to each write request:
	for i := 0; i < len(batch); i++ {
		cb := batch[i].Completed
		if cb != nil {
			cb(snes.ReadOrWriteResponse{
				IsWrite: true,
				Address: batch[i].Address,
				Size:    batch[i].Size,
				Data:    batch[i].Data,
			})
		}
	}

	return nil
}

func (c *Conn) handleQueue() {
	var err error
	defer func() {
		log.Printf("fxpakpro queue: %v\n", err)
	}()

	for {
		op := <-c.q

		if op.isRead {
			reqs := op.read

			for len(reqs) >= 8 {
				// send VGET command:
				err = c.sendVGETBatch(reqs[:8])
				if err != nil {
					return
				}

				// move to next batch:
				reqs = reqs[8:]
			}

			if len(reqs) > 0 && len(reqs) <= 8 {
				err = c.sendVGETBatch(reqs)
				if err != nil {
					return
				}
			}
		} else {
			reqs := op.write

			for len(reqs) >= 8 {
				// send VPUT command:
				err = c.sendVPUTBatch(reqs[:8])
				if err != nil {
					return
				}

				// move to next batch:
				reqs = reqs[8:]
			}

			if len(reqs) > 0 && len(reqs) <= 8 {
				err = c.sendVPUTBatch(reqs)
				if err != nil {
					return
				}
			}
		}
	}
}

func init() {
	snes.Register("fxpakpro", &Driver{})
}
