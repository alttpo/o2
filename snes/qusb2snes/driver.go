package qusb2snes

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"log"
	"net"
	"o2/snes"
)

const driverName = "qusb2snes"

type Driver struct {
	ws      net.Conn
	r       *wsutil.Reader
	w       *wsutil.Writer
	encoder *json.Encoder
	decoder *json.Decoder
}

func (d *Driver) DisplayOrder() int {
	return 1
}

func (d *Driver) DisplayName() string {
	return "QUsb2Snes"
}

func (d *Driver) DisplayDescription() string {
	return "Connect to the QUsb2Snes service"
}

func (d *Driver) Open(_ snes.DeviceDescriptor) (q snes.Queue, err error) {
	qu := &Queue{
		d:       d,
		encoder: d.encoder,
		decoder: d.decoder,
	}
	q = qu

	qu.BaseInit(driverName, q)
	qu.Init()

	return
}

func (d *Driver) SendCommand(name string, cmd interface{}) (err error) {
	//log.Printf("qusb2snes: Encode(%s)\n", name)
	err = d.encoder.Encode(cmd)
	if err != nil {
		err = fmt.Errorf("qusb2snes: %s command encode: %w", name, err)
		return
	}

	//log.Println("qusb2snes: Flush()")
	err = d.w.Flush()
	if err != nil {
		err = fmt.Errorf("qusb2snes: %s command flush: %w", name, err)
		return
	}
	return
}

func (d *Driver) CommandResponse(name string, rsp interface{}) (err error) {
	//log.Println("qusb2snes: NextFrame()")
	hdr, err := d.r.NextFrame()
	if err != nil {
		err = fmt.Errorf("qusb2snes: %s command response: error reading next websocket frame: %w", name, err)
		return
	}
	if hdr.OpCode == ws.OpClose {
		err = fmt.Errorf("qusb2snes: %s command response: websocket closed: %w", name, err)
		return
	}

	//log.Println("qusb2snes: Decode()")
	err = d.decoder.Decode(rsp)
	if err != nil {
		err = fmt.Errorf("qusb2snes: %s command response: decode response: %w", name, err)
		return
	}

	log.Println("qusb2snes: response received")
	return
}

func (d *Driver) Detect() (devices []snes.DeviceDescriptor, err error) {
	if d.ws == nil {
		d.ws, _, _, err = ws.Dial(context.Background(), "ws://localhost:8080/")
		if err != nil {
			return
		}

		d.r = wsutil.NewClientSideReader(d.ws)
		d.w = wsutil.NewWriter(d.ws, ws.StateClientSide, ws.OpText)
		d.encoder = json.NewEncoder(d.w)
		d.decoder = json.NewDecoder(d.r)

		err = d.SendCommand("Name", &map[string]interface{}{
			"Opcode":   "Name",
			"Space":    "SNES",
			"Operands": []string{"o2discover"},
		})
		if err != nil {
			return
		}
	}

	// request a device list:
	err = d.SendCommand("DeviceList", &map[string]interface{}{
		"Opcode":   "DeviceList",
		"Space":    "SNES",
		"Operands": []string{},
	})
	if err != nil {
		err = fmt.Errorf("qusb2snes: DeviceList request: %w", err)
		return
	}

	// handle response:
	var list struct {
		Results []string `json:"Results"`
	}
	err = d.CommandResponse("DeviceList", &list)
	if err != nil {
		return
	}

	// make the device list:
	devices = make([]snes.DeviceDescriptor, 0, len(list.Results))
	for _, name := range list.Results {
		devices = append(devices, DeviceDescriptor{name})
	}

	return
}

func (d *Driver) Empty() snes.DeviceDescriptor {
	return DeviceDescriptor{}
}

func init() {
	snes.Register(driverName, &Driver{})
}
