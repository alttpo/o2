package qusb2snes

import (
	"errors"
	"fmt"
	"o2/snes"
	"sync"
	"syscall"
)

const driverName = "qusb2snes"

type Driver struct {
	wsLock sync.Mutex

	opened   *Queue
	detected []snes.DeviceDescriptor
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
	dev, ok := desc.(DeviceDescriptor)
	if !ok {
		err = fmt.Errorf("desc is not of expected type")
		return
	}

	qu := &Queue{
		d:          d,
		deviceName: dev.name,
	}

	err = NewWebSocketClient(&qu.ws, "ws://localhost:8080/", "o2")
	if err != nil {
		return
	}

	q = qu

	qu.BaseInit(driverName, q)
	err = qu.Init()
	if err != nil {
		return
	}

	// record that this device is opened:
	d.opened = qu
	go func() {
		<-q.Closed()
		d.opened = nil
	}()

	return
}

func (d *Driver) Detect() (devices []snes.DeviceDescriptor, err error) {
	// Prevent auto-detection when opened because DeviceList opcode breaks other websockets:
	if d.opened != nil {
		devices = d.detected
		if devices == nil {
			devices = []snes.DeviceDescriptor{DeviceDescriptor{name: "Auto-detection disabled when connected"}}
		}
		return
	}

	// attempt to create a websocket connection to qusb2snes:
	var ws WebSocketClient
	err = NewWebSocketClient(&ws, "ws://localhost:8080/", "o2discover")
	defer func() { ws.Close() }()
	if err != nil {
		// intercept "connection refused" errors to silence them:
		var serr syscall.Errno
		if errors.As(err, &serr) {
			if serr == syscall.ECONNREFUSED {
				err = nil
				return
			}
		}
		// otherwise return the error:
		return
	}

	// request a device list:
	defer func() {
		d.wsLock.Unlock()
		//log.Println("qusb2snes: DeviceList request end")
	}()
	d.wsLock.Lock()
	//log.Println("qusb2snes: DeviceList request start")
	err = ws.SendCommand(qusbCommand{
		Opcode:   "DeviceList",
		Space:    "SNES",
		Operands: []string{},
	})
	if err != nil {
		err = fmt.Errorf("qusb2snes: DeviceList request: %w", err)
		return
	}

	// handle response:
	var list qusbResult
	err = ws.ReadCommandResponse("DeviceList", &list)
	if err != nil {
		return
	}

	// make the device list:
	devices = make([]snes.DeviceDescriptor, 0, len(list.Results))
	for _, name := range list.Results {
		devices = append(devices, DeviceDescriptor{name})
	}

	d.detected = devices

	return
}

func (d *Driver) Empty() snes.DeviceDescriptor {
	return DeviceDescriptor{}
}

func init() {
	snes.Register(driverName, &Driver{})
}
