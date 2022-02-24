package sni

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"o2/snes"
	"o2/util"
	"o2/util/env"
	"sync"
)

const (
	driverName = "sni"
)

type Driver struct {
	cc   *grpc.ClientConn
	lock sync.Mutex
}

func (d *Driver) DisplayOrder() int {
	return 2
}

func (d *Driver) DisplayName() string {
	return "SNI"
}

func (d *Driver) DisplayDescription() string {
	return "Connect to SNI service"
}

func (d *Driver) Empty() snes.DeviceDescriptor {
	return &DeviceDescriptor{}
}

func (d *Driver) Detect() (devices []snes.DeviceDescriptor, err error) {
	// Dial the SNI service:
	d.lock.Lock()
	if d.cc == nil {
		d.cc, err = grpc.Dial("localhost:8191", grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			d.lock.Unlock()
			err = nil
			return
		}
	}
	d.lock.Unlock()

	// List devices:
	client := NewDevicesClient(d.cc)
	var rsp *DevicesResponse
	rsp, err = client.ListDevices(context.Background(), &DevicesRequest{})
	if err != nil {
		err = nil
		return
	}

	devices = make([]snes.DeviceDescriptor, 0, 10)
	for _, dev := range rsp.Devices {
		devices = append(devices, &DeviceDescriptor{
			Uri:         dev.Uri,
			DisplayName: dev.DisplayName,
		})
	}

	return
}

func (d *Driver) Open(ddg snes.DeviceDescriptor) (snes.Queue, error) {
	var err error

	dd := ddg.(*DeviceDescriptor)

	c := &Queue{
		memoryClient:     NewDeviceMemoryClient(d.cc),
		filesystemClient: NewDeviceFilesystemClient(d.cc),
		uri:              dd.Uri,
		closed:           make(chan struct{}),
	}
	c.BaseInit(driverName, c)

	return c, err
}

func init() {
	if util.IsTruthy(env.GetOrDefault("O2_SNI_DISABLE", "0")) {
		log.Printf("disabling sni snes driver\n")
		return
	}
	snes.Register(driverName, &Driver{})
}
