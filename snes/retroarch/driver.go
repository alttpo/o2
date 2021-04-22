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
	"time"
)

const driverName = "retroarch"

type Driver struct {
	detectors []*RAClient

	devices []snes.DeviceDescriptor
	opened  *Queue
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

func (d *Driver) Open(desc snes.DeviceDescriptor) (q snes.Queue, err error) {
	descriptor, ok := desc.(*DeviceDescriptor)
	if !ok {
		return nil, fmt.Errorf("retroarch: open: descriptor is not of expected type")
	}

	// find detector with same id:
	var c *RAClient
	for _, detector := range d.detectors {
		if descriptor.GetId() == detector.GetId() {
			c = detector
			break
		}
	}

	if c == nil {
		return nil, fmt.Errorf("retroarch: open: could not find socket by device='%s'\n", descriptor.GetId())
	}

	qu := &Queue{c: c}
	qu.BaseInit(driverName, qu)
	qu.Init()

	q = qu

	// record that this device is opened:
	d.opened = qu
	go func() {
		<-q.Closed()
		d.opened = nil
	}()

	return
}

func (d *Driver) Detect() (devices []snes.DeviceDescriptor, err error) {
	// stop auto-detection if connected already:
	if d.opened != nil {
		devices = d.devices
		return
	}

	devices = make([]snes.DeviceDescriptor, 0, len(d.detectors))
	for i, detector := range d.detectors {
		if !detector.IsConnected() {
			// "connect" to this UDP endpoint:
			err = detector.Connect(detector.addr)
			if err != nil {
				log.Printf("retroarch: detect: detector[%d]: connect: %v\n", i, err)
				continue
			}
		}

		// issue a sample read:
		request := []byte("READ_CORE_RAM 40FFC0 32\n")
		log.Printf("%s: < %s", detector.addr, string(request))
		err = detector.WriteTimeout(request, time.Second)
		if err != nil {
			err = nil
			continue
		}

		var rsp []byte
		rsp, err = detector.ReadTimeout(time.Second)
		if err != nil {
			err = nil
			continue
		}
		if rsp == nil {
			continue
		}

		descriptor := &DeviceDescriptor{
			DeviceDescriptorBase: snes.DeviceDescriptorBase{},
			addr:                 detector.addr,
		}

		rsps := string(rsp)
		log.Printf("%s: > %s", detector.addr, rsps)
		if rsps == "READ_CORE_RAM 40ffc0 -1\n" {
			descriptor.IsGameLoaded = false
		} else {
			descriptor.IsGameLoaded = true
		}

		snes.MarshalDeviceDescriptor(descriptor)
		devices = append(devices, descriptor)
	}

	d.devices = devices
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
		const count = 1
		for i := 0; i < count; i++ {
			sb.WriteString(fmt.Sprintf("localhost:%d", 55355+i))
			if i < count-1 {
				sb.WriteByte(',')
			}
		}
		hostsStr = sb.String()
	}

	// split the hostsStr list by commas:
	hosts := strings.Split(hostsStr, ",")

	// resolve the addresses:
	addresses := make([]*net.UDPAddr, 0, len(hosts))
	for _, host := range hosts {
		addr, err := net.ResolveUDPAddr("udp", host)
		if err != nil {
			log.Printf("retroarch: resolve('%s'): %v\n", host, err)
			// drop the address if it doesn't resolve:
			// TODO: consider retrying the resolve later? maybe not worth worrying about.
			continue
		}

		addresses = append(addresses, addr)
	}

	// register the driver:
	snes.Register(driverName, NewDriver(addresses))
}
