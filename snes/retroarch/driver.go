package retroarch

import (
	"fmt"
	"log"
	"net"
	"o2/interfaces"
	"o2/snes"
	"o2/udpclient"
	"os"
	"strings"
)

const driverName = "retroarch"

type Driver struct {
	detectors []*RAClient
	devices   []snes.DeviceDescriptor
}

func NewDriver(addresses []*net.UDPAddr) *Driver {
	d := &Driver{
		detectors: make([]*RAClient, len(addresses)),
	}

	for i, addr := range addresses {
		c := &RAClient{}
		d.detectors[i] = c
		udpclient.MakeUDPClient(fmt.Sprintf("retroarch[%d]", i), &c.UDPClient)
		c.addr = addr
	}

	return d
}

func (d *Driver) DisplayOrder() int {
	return 2
}

func (d *Driver) DisplayName() string {
	return "RetroArch"
}

func (d *Driver) DisplayDescription() string {
	return "Connect to a RetroArch emulator"
}

func (d *Driver) Open(desc snes.DeviceDescriptor) (snes.Queue, error) {
	c := &Queue{}
	c.BaseInit(driverName, c)
	c.Init()
	return c, nil
}

func (d *Driver) Detect() (devices []snes.DeviceDescriptor, err error) {
	if d.devices == nil {
		d.devices = make([]snes.DeviceDescriptor, 0, len(d.detectors))

		for i, detector := range d.detectors {
			// "connect" to this UDP endpoint:
			err = detector.Connect(detector.addr)
			if err != nil {
				log.Printf("retroarch: detect: detector[%d]: connect: %v\n", i, err)
				continue
			}

			descriptor := &DeviceDescriptor{
				DeviceDescriptorBase: snes.DeviceDescriptorBase{},
				addr:                 detector.addr,
			}
			snes.MarshalDeviceDescriptor(descriptor)
			d.devices = append(d.devices, descriptor)
		}
	}

	devices = d.devices
	err = nil
	return
}

func (d *Driver) Empty() snes.DeviceDescriptor {
	return &DeviceDescriptor{}
}

func init() {
	if interfaces.IsTruthy(os.Getenv("O2_RETROARCH_DISABLE")) {
		return
	}

	// comma-delimited list of host:port pairs:
	hostsStr := os.Getenv("O2_RETROARCH_HOSTS")
	if hostsStr == "" {
		// default network_cmd_port for RA is UDP 55355. we want to support connecting to multiple
		// instances so let's auto-detect RA instances listening on UDP ports in the range
		// [55355..55362]. realistically we probably won't be running any more than a few instances on
		// the same machine at one time. i picked 8 since i currently have an 8-core CPU :)
		var sb strings.Builder
		for i := 0; i < 8; i++ {
			sb.WriteString(fmt.Sprintf("localhost:%d", 55355+i))
			if i < 7 {
				sb.WriteByte(',')
			}
		}
		hostsStr = sb.String()
	}

	// split the hostsStr list by commas:
	hosts := strings.Split(hostsStr, ",")

	// resolve the addresses:
	addresses := make([]*net.UDPAddr, 0, 8)
	for _, host := range hosts {
		addr, err := net.ResolveUDPAddr("udp", host)
		if err != nil {
			continue
		}

		addresses = append(addresses, addr)
	}

	// register the driver:
	snes.Register(driverName, NewDriver(addresses))
}
