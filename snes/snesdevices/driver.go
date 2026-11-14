// Package snesdevices adapts the snes-devices drivers (github.com/alttpo/snes-devices) to o2's snes.Driver and
// snes.Queue interfaces so the rest of o2 stays independent of any particular device library.
package snesdevices

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"

	devconfig "github.com/alttpo/snes-devices/config"
	"github.com/alttpo/snes-devices/devices"
	"github.com/alttpo/snes-devices/drivers/emunwa"
	"github.com/alttpo/snes-devices/drivers/fxpakpro"
	"github.com/alttpo/snes-devices/drivers/mock"
	"github.com/alttpo/snes-devices/drivers/qusb2snes"
	"github.com/alttpo/snes-devices/drivers/retroarch"
	"github.com/alttpo/snes-devices/drivers/sniclient"
	"github.com/alttpo/snes-devices/protos/sni"

	"o2/snes"
)

// o2Names maps snes-devices driver names to the names o2 has always used for them (saved configurations refer to
// these).
var o2Names = map[string]string{
	"fxpakpro":  "fxpakpro",
	"ra":        "retroarch",
	"emunwa":    "emunwa",
	"qusb2snes": "qusb2snes",
	"sni":       "sni",
	"mock":      "mock",
}

// Init configures and initializes the device drivers o2 uses and registers them with o2's snes driver registry.
func Init() {
	devconfig.Set(newO2Config())

	fxpakpro.DriverInit()
	retroarch.DriverInit()
	emunwa.DriverInit()
	qusb2snes.DriverInit()
	sniclient.DriverInit()
	mock.DriverInit()

	for _, nd := range devices.Drivers() {
		name, ok := o2Names[nd.Name]
		if !ok {
			continue
		}
		snes.Register(name, &Driver{name: name, drv: nd.Driver})
	}
}

// Driver adapts a devices.Driver to snes.Driver.
type Driver struct {
	name string
	drv  devices.Driver

	// detected maps device ids to the exact URIs the driver detected. Device descriptors round-trip through the web
	// UI as strings, and url.Parse(u.String()) does not always give back u (e.g. the relative "COM4" path of an FX Pak
	// Pro on Windows becomes "/COM4").
	detectedLock sync.Mutex
	detected     map[string]url.URL
}

func (d *Driver) DisplayOrder() int {
	if dd, ok := d.drv.(devices.DriverDescriptor); ok {
		return dd.DisplayOrder()
	}
	return 0
}

func (d *Driver) DisplayName() string {
	if dd, ok := d.drv.(devices.DriverDescriptor); ok {
		return dd.DisplayName()
	}
	return d.name
}

func (d *Driver) DisplayDescription() string {
	if dd, ok := d.drv.(devices.DriverDescriptor); ok {
		return dd.DisplayDescription()
	}
	return d.name + " driver"
}

// DeviceDescriptor identifies a device by its snes-devices URI.
type DeviceDescriptor struct {
	snes.DeviceDescriptorBase
	Uri string `json:"uri"`
}

func (dd *DeviceDescriptor) Base() *snes.DeviceDescriptorBase { return &dd.DeviceDescriptorBase }

func (dd *DeviceDescriptor) GetId() string { return dd.Uri }

func (dd *DeviceDescriptor) GetDisplayName() string {
	if dd.DisplayName != "" {
		return dd.DisplayName
	}
	return dd.Uri
}

// MatchesId matches the ids o2's previous drivers used for the same device: the serial port (fxpakpro), the host
// address (retroarch), the device URI known to SNI (sni), the device name (qusb2snes) and "mock".
func (dd *DeviceDescriptor) MatchesId(id string) bool {
	if id == "" {
		return false
	}
	u, err := url.Parse(dd.Uri)
	if err != nil {
		return false
	}
	q := u.Query()
	for _, legacy := range []string{u.Path, strings.TrimPrefix(u.Path, "/"), u.Host, u.Opaque, q.Get("uri"), q.Get("device")} {
		if legacy != "" && legacy != "." && legacy == id {
			return true
		}
	}
	return false
}

func (d *Driver) Empty() snes.DeviceDescriptor { return &DeviceDescriptor{} }

func (d *Driver) Detect() ([]snes.DeviceDescriptor, error) {
	devs, err := d.drv.Detect()
	if err != nil {
		return nil, err
	}

	d.detectedLock.Lock()
	if d.detected == nil {
		d.detected = make(map[string]url.URL)
	}
	for i := range devs {
		d.detected[devs[i].Uri.String()] = devs[i].Uri
	}
	d.detectedLock.Unlock()

	descs := make([]snes.DeviceDescriptor, 0, len(devs))
	for i := range devs {
		descs = append(descs, &DeviceDescriptor{
			DeviceDescriptorBase: snes.DeviceDescriptorBase{
				Id:          devs[i].Uri.String(),
				DisplayName: devs[i].DisplayName,
			},
			Uri: devs[i].Uri.String(),
		})
	}
	return descs, nil
}

func (d *Driver) Open(desc snes.DeviceDescriptor) (snes.Queue, error) {
	dd, ok := desc.(*DeviceDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s: unexpected device descriptor type %T", d.name, desc)
	}

	var uri *url.URL
	d.detectedLock.Lock()
	if u, ok := d.detected[dd.Uri]; ok {
		uri = &u
	}
	d.detectedLock.Unlock()
	if uri == nil {
		var err error
		if uri, err = url.Parse(dd.Uri); err != nil {
			return nil, fmt.Errorf("%s: invalid device uri %q: %w", d.name, dd.Uri, err)
		}
	}

	q := newQueue(d.name, d.drv.Device(uri))

	// only offer uploading and booting ROMs on devices that can do both:
	if ok, _ := d.drv.HasCapabilities(sni.DeviceCapability_PutFile, sni.DeviceCapability_BootFile); ok {
		rq := &ROMControlQueue{Queue: q}
		rq.BaseInit(d.name, rq)
		log.Printf("%s: opened %s\n", d.name, dd.Uri)
		return rq, nil
	}

	q.BaseInit(d.name, q)
	log.Printf("%s: opened %s\n", d.name, dd.Uri)
	return q, nil
}
