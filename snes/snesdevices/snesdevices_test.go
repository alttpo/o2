package snesdevices

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/alttpo/snes-devices/devices"
	"github.com/alttpo/snes-devices/protos/sni"

	"o2/snes"
)

// fakeDevice records requests and serves memory reads and writes.
type fakeDevice struct {
	lock     sync.Mutex
	memory   [0x1000000]byte
	reads    []devices.MemoryReadRequest
	readErr  error
	closed   bool
	mkdirErr error
	ops      []string
	files    map[string][]byte
}

func (d *fakeDevice) Close() error      { d.lock.Lock(); d.closed = true; d.lock.Unlock(); return nil }
func (d *fakeDevice) URI() *url.URL     { return &url.URL{Scheme: "fake", Opaque: "dev"} }
func (d *fakeDevice) DeviceKey() string { return "dev" }

func (d *fakeDevice) RequiresMemoryMappingForAddressSpace(context.Context, sni.AddressSpace) (bool, error) {
	return false, nil
}
func (d *fakeDevice) RequiresMemoryMappingForAddress(context.Context, devices.AddressTuple) (bool, error) {
	return false, nil
}

func (d *fakeDevice) MultiReadMemory(_ context.Context, reads ...devices.MemoryReadRequest) ([]devices.MemoryReadResponse, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	if d.readErr != nil {
		return nil, d.readErr
	}
	d.reads = append(d.reads, reads...)
	rsps := make([]devices.MemoryReadResponse, len(reads))
	for i, r := range reads {
		a := r.RequestAddress.Address
		rsps[i] = devices.MemoryReadResponse{RequestAddress: r.RequestAddress, DeviceAddress: r.RequestAddress, Data: append([]byte(nil), d.memory[a:a+uint32(r.Size)]...)}
	}
	return rsps, nil
}

func (d *fakeDevice) MultiWriteMemory(_ context.Context, writes ...devices.MemoryWriteRequest) ([]devices.MemoryWriteResponse, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	rsps := make([]devices.MemoryWriteResponse, len(writes))
	for i, w := range writes {
		copy(d.memory[w.RequestAddress.Address:], w.Data)
		rsps[i] = devices.MemoryWriteResponse{RequestAddress: w.RequestAddress, DeviceAddress: w.RequestAddress, Size: len(w.Data)}
	}
	return rsps, nil
}

func (d *fakeDevice) ResetSystem(context.Context) error                { return nil }
func (d *fakeDevice) ResetToMenu(context.Context) error                { return nil }
func (d *fakeDevice) PauseUnpause(context.Context, bool) (bool, error) { return false, nil }
func (d *fakeDevice) PauseToggle(context.Context) error                { return nil }

func (d *fakeDevice) ReadDirectory(context.Context, string) ([]devices.DirEntry, error) {
	return nil, nil
}
func (d *fakeDevice) MakeDirectory(_ context.Context, path string) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.ops = append(d.ops, "mkdir "+path)
	return d.mkdirErr
}
func (d *fakeDevice) RemoveFile(context.Context, string) error         { return nil }
func (d *fakeDevice) RenameFile(context.Context, string, string) error { return nil }
func (d *fakeDevice) PutFile(_ context.Context, path string, size uint32, r io.Reader, progress devices.ProgressReportFunc) (uint32, error) {
	data, _ := io.ReadAll(r)
	d.lock.Lock()
	defer d.lock.Unlock()
	d.ops = append(d.ops, "put "+path)
	if d.files == nil {
		d.files = map[string][]byte{}
	}
	d.files[path] = data
	progress(size, size)
	return size, nil
}
func (d *fakeDevice) GetFile(context.Context, string, io.Writer, devices.SizeReceivedFunc, devices.ProgressReportFunc) (uint32, error) {
	return 0, nil
}
func (d *fakeDevice) BootFile(_ context.Context, path string) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.ops = append(d.ops, "boot "+path)
	return nil
}
func (d *fakeDevice) FetchFields(context.Context, ...sni.Field) ([]string, error) { return nil, nil }
func (d *fakeDevice) NWACommand(context.Context, string, string, []byte) ([]map[string]string, []byte, error) {
	return nil, nil, nil
}

type fakeDriver struct {
	dev  *fakeDevice
	caps []sni.DeviceCapability
}

func (f *fakeDriver) Kind() string { return "fake" }
func (f *fakeDriver) Detect() ([]devices.DeviceDescriptor, error) {
	return []devices.DeviceDescriptor{{Uri: url.URL{Scheme: "fake", Host: ".", Path: "/dev/x"}, DisplayName: "Fake Device"}}, nil
}
func (f *fakeDriver) Device(*url.URL) devices.AutoCloseableDevice { return f.dev }
func (f *fakeDriver) DeviceKey(*url.URL) string                   { return "dev" }
func (f *fakeDriver) DisconnectAll()                              {}
func (f *fakeDriver) HasCapabilities(capabilities ...sni.DeviceCapability) (bool, error) {
	return devices.CheckCapabilities(capabilities, f.caps)
}

func openFake(t *testing.T, caps ...sni.DeviceCapability) (*fakeDevice, snes.Queue) {
	t.Helper()
	dev := &fakeDevice{}
	d := &Driver{name: "fake", drv: &fakeDriver{dev: dev, caps: caps}}

	descs, err := d.Detect()
	if err != nil || len(descs) != 1 {
		t.Fatalf("detect: %v %v", descs, err)
	}
	desc := snes.MarshalDeviceDescriptor(descs[0])
	if desc.GetId() != "fake://./dev/x" || desc.Base().DisplayName != "Fake Device" {
		t.Errorf("unexpected descriptor %+v", desc)
	}

	q, err := d.Open(desc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return dev, q
}

func runSequence(t *testing.T, q snes.Queue, seq snes.CommandSequence) error {
	t.Helper()
	done := make(chan error, len(seq))
	for i := range seq {
		seq[i].Completion = func(_ snes.Command, err error) { done <- err }
	}
	if err := seq.EnqueueTo(q); err != nil {
		t.Fatal(err)
	}
	var firstErr error
	for range seq {
		select {
		case err := <-done:
			if firstErr == nil {
				firstErr = err
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out")
		}
	}
	return firstErr
}

// uriDriver detects one device with the given URI and records the URI it is asked to open.
type uriDriver struct {
	fakeDriver
	uri    url.URL
	opened *url.URL
}

func (f *uriDriver) Detect() ([]devices.DeviceDescriptor, error) {
	return []devices.DeviceDescriptor{{Uri: f.uri, DisplayName: "COM4"}}, nil
}
func (f *uriDriver) Device(uri *url.URL) devices.AutoCloseableDevice {
	f.opened = uri
	return f.dev
}

func TestDriver_OpenUsesDetectedURI(t *testing.T) {
	f := &uriDriver{fakeDriver: fakeDriver{dev: &fakeDevice{}}, uri: url.URL{Scheme: "fxpakpro", Host: ".", Path: "COM4"}}
	d := &Driver{name: "fxpakpro", drv: f}

	descs, err := d.Detect()
	if err != nil {
		t.Fatal(err)
	}
	// round-trip through JSON as the web UI does:
	b, _ := json.Marshal(snes.MarshalDeviceDescriptor(descs[0]))
	desc := d.Empty()
	if err = json.Unmarshal(b, desc); err != nil {
		t.Fatal(err)
	}

	q, err := d.Open(desc)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if f.opened == nil || f.opened.Path != "COM4" {
		t.Errorf("expected to open the detected URI with path COM4; got %+v", f.opened)
	}
}

func TestQueue_ReadWrite(t *testing.T) {
	dev, q := openFake(t)

	if err := runSequence(t, q, q.MakeWriteCommands([]snes.Write{{Address: 0xE00010, Size: 3, Data: []byte{7, 8, 9}}}, nil)); err != nil {
		t.Fatal(err)
	}

	var rsps []snes.Response
	reads := []snes.Read{
		{Address: 0xE00010, Size: 3, Extra: "a", Completion: func(r snes.Response) { rsps = append(rsps, r) }},
		{Address: 0xF50010, Size: 1, Extra: "b", Completion: func(r snes.Response) { rsps = append(rsps, r) }},
	}
	if err := runSequence(t, q, q.MakeReadCommands(reads, nil)); err != nil {
		t.Fatal(err)
	}

	if len(rsps) != 2 || string(rsps[0].Data) != "\x07\x08\x09" || rsps[0].Extra != "a" || rsps[1].Extra != "b" {
		t.Errorf("unexpected responses %+v", rsps)
	}
	for _, r := range dev.reads {
		if r.RequestAddress.AddressSpace != sni.AddressSpace_FxPakPro || r.RequestAddress.MemoryMapping != sni.MemoryMapping_LoROM {
			t.Errorf("unexpected request address %+v", r.RequestAddress)
		}
	}
	if _, ok := q.(snes.ROMControl); ok {
		t.Errorf("queue should not offer ROM control without PutFile and BootFile capabilities")
	}
}

func TestQueue_FatalErrorClosesQueue(t *testing.T) {
	dev, q := openFake(t)

	// a non-fatal error is reported but keeps the queue open:
	dev.readErr = devices.DeviceNonFatal("hiccup", nil)
	if err := runSequence(t, q, q.MakeReadCommands([]snes.Read{{Address: 0xF50000, Size: 1}}, nil)); err == nil {
		t.Fatal("expected an error")
	}
	select {
	case <-q.Closed():
		t.Fatal("queue closed on a non-fatal error")
	default:
	}

	// a fatal error is terminal:
	dev.readErr = devices.DeviceFatal("unplugged", errors.New("eof"))
	err := runSequence(t, q, q.MakeReadCommands([]snes.Read{{Address: 0xF50000, Size: 1}}, nil))
	var term *snes.TerminalError
	if !errors.As(err, &term) {
		t.Fatalf("expected a terminal error; got %v", err)
	}
	select {
	case <-q.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("queue not closed after a fatal error")
	}
	if !dev.closed {
		t.Error("device not closed")
	}
}

func TestQueue_ROMControl(t *testing.T) {
	dev, q := openFake(t, sni.DeviceCapability_PutFile, sni.DeviceCapability_BootFile)
	rc, ok := q.(snes.ROMControl)
	if !ok {
		t.Fatal("expected ROM control")
	}

	// the folder already existing is not an error:
	dev.mkdirErr = devices.DeviceNonFatal("exists", nil)
	path, cmds := rc.MakeUploadROMCommands("o2/", "/Game.SFC", []byte{1, 2, 3})
	if path != "o2/game.sfc" {
		t.Errorf("unexpected path %q", path)
	}
	if err := runSequence(t, q, cmds); err != nil {
		t.Fatal(err)
	}
	if err := runSequence(t, q, rc.MakeBootROMCommands(path)); err != nil {
		t.Fatal(err)
	}

	expected := []string{"mkdir o2", "put o2/game.sfc", "boot o2/game.sfc"}
	if len(dev.ops) != len(expected) {
		t.Fatalf("expected %v; got %v", expected, dev.ops)
	}
	for i := range expected {
		if dev.ops[i] != expected[i] {
			t.Errorf("expected %v; got %v", expected, dev.ops)
			break
		}
	}
	if string(dev.files["o2/game.sfc"]) != "\x01\x02\x03" {
		t.Errorf("ROM not uploaded")
	}
}

func TestConfig(t *testing.T) {
	t.Setenv("O2_RETROARCH_HOSTS", "otherhost:1234")
	t.Setenv("O2_FXPAKPRO_READ_TIMEOUT", "3s")
	c := newO2Config()

	if c.GetString("retroarch_hosts") != "otherhost:1234" {
		t.Errorf("environment should override default; got %q", c.GetString("retroarch_hosts"))
	}
	if c.GetString("nwa_port_range") != "0xbeef" || !c.IsSet("nwa_port_range") {
		t.Errorf("expected default NWA port range")
	}
	if !c.GetBool("qusb2snes_disable") {
		t.Errorf("expected QUsb2Snes disabled by default")
	}
	if c.GetDuration("fxpakpro_read_timeout") != 3*time.Second {
		t.Errorf("unexpected duration %v", c.GetDuration("fxpakpro_read_timeout"))
	}
	if c.IsSet("fxpakpro_write_timeout") {
		t.Errorf("unset keys must not be reported as set so drivers keep their built-in defaults")
	}
	if c.Name("sni_grpc_addr") != "O2_SNI_GRPC_ADDR" {
		t.Errorf("unexpected name %q", c.Name("sni_grpc_addr"))
	}
}

func TestInit_RegistersO2Drivers(t *testing.T) {
	t.Setenv("O2_FXPAKPRO_DISABLE", "1")
	Init()

	for _, name := range []string{"retroarch", "emunwa", "sni"} {
		if _, ok := snes.DriverByName(name); !ok {
			t.Errorf("expected driver %q to be registered", name)
		}
	}
	for _, name := range []string{"fxpakpro", "qusb2snes", "mock", "luabridge", "proxy", "ra"} {
		if _, ok := snes.DriverByName(name); ok {
			t.Errorf("driver %q should not be registered", name)
		}
	}
}

func TestDeviceDescriptor_MatchesLegacyIds(t *testing.T) {
	for _, tc := range []struct {
		uri, legacy string
	}{
		{"fxpakpro://./dev/cu.usbmodemDEMO000000001", "/dev/cu.usbmodemDEMO000000001"},
		{(&url.URL{Scheme: "fxpakpro", Host: ".", Path: "COM4"}).String(), "COM4"},
		{"ra://127.0.0.1:55355", "127.0.0.1:55355"},
		{"sni://localhost:8191?uri=fxpakpro%3A%2F%2F.%2Fdev%2Fcu.usbmodem1", "fxpakpro://./dev/cu.usbmodem1"},
		{"qusb2snes://localhost:8080?device=SD2SNES+COM3", "SD2SNES COM3"},
		{"mock:mock", "mock"},
	} {
		dd := &DeviceDescriptor{Uri: tc.uri}
		if tc.legacy != "" && !dd.MatchesId(tc.legacy) {
			t.Errorf("%s: expected to match legacy id %q", tc.uri, tc.legacy)
		}
		if dd.MatchesId("something-else") || dd.MatchesId("") || dd.MatchesId(".") {
			t.Errorf("%s: matched an unrelated id", tc.uri)
		}
	}
}
