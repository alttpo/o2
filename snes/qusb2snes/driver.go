package qusb2snes

import (
	"fmt"
	"o2/snes"
)

const driverName = "qusb2snes"

type Driver struct {
	ws WebSocketClient
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

func (d *Driver) Open(desc snes.DeviceDescriptor) (q snes.Queue, err error) {
	dev, ok := desc.(*DeviceDescriptor)
	if !ok {
		err = fmt.Errorf("desc is not of expected type")
		return
	}

	qu := &Queue{name: dev.name}

	err = NewWebSocketClient(&qu.ws, "ws://localhost:8080/", "o2discover")
	if err != nil {
		return
	}

	q = qu

	qu.BaseInit(driverName, q)
	qu.Init()

	return
}

func (d *Driver) Detect() (devices []snes.DeviceDescriptor, err error) {
	if d.ws.ws == nil {
		err = NewWebSocketClient(&d.ws, "ws://localhost:8080/", "o2discover")
		if err != nil {
			return
		}
	}

	// request a device list:
	err = d.ws.SendCommand("DeviceList", &map[string]interface{}{
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
	err = d.ws.ReadCommandResponse("DeviceList", &list)
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
