package snes

import (
	"fmt"
	"sort"
	"sync"
)

// A struct that contains fields used to uniquely identify a device
type DeviceDescriptor interface {
	DisplayName() string
}

type Driver interface {
	// Open a connection to a specific device
	Open(desc DeviceDescriptor) (Conn, error)

	// Detect any present devices
	Detect() ([]DeviceDescriptor, error)

	// Returns a descriptor with all fields empty or defaulted
	Empty() DeviceDescriptor
}

type DriverDescriptor interface {
	DisplayName() string

	DisplayDescription() string
}

type DriverDevicePair struct {
	Driver Driver
	Device DeviceDescriptor
}

type CommandSequence []Command

// Represents an asynchronous communication interface to either a physical or emulated SNES system.
// Communication with a physical SNES console is done via a flash cart with a USB connection.
// Both read and write requests are both enqueued into the same request queue and are processed in the order received.
// For reads, the read data is sent via the Completed callback specified in the ReadRequest struct.
// Depending on the implementation, reads and writes may be broken up into fixed-size batches.
// Read requests can read from ROM, SRAM, and WRAM. Flash carts can listen to the SNES address and data buses in order
// to shadow WRAM for reading.
// Write requests can only write to ROM and SRAM. WRAM cannot be written to from flash carts on real hardware; this is a
// hard limitation due to the design of the SNES and is not specific to any flash cart.
type Conn interface {
	// Enqueues a command to be executed
	Enqueue(cmd Command)

	// Enqueues a command with a callback to be executed when completed [err==nil] or errored [err!=nil]
	EnqueueWithCallback(cmd Command, onComplete func(err error))

	// Enqueues a sequence of commands to be executed in order
	EnqueueMulti(cmds CommandSequence)

	// Enqueues a sequence of commands to be executed in order with only the last command receiving the callback
	EnqueueMultiWithCallback(cmds CommandSequence, onComplete func(err error))

	// Creates a set of Commands that submits a batch of read requests to the device
	MakeReadCommands(reqs []ReadRequest) CommandSequence

	// Creates a set of Commands that submits a batch of write requests to the device
	MakeWriteCommands(reqs []WriteRequest) CommandSequence
}

type ReadOrWriteResponse struct {
	IsWrite bool // was the request a read or write?
	Address uint32
	Size    uint8
	Data    []byte // the data that was read or written
}

type ReadOrWriteCompleted func(response ReadOrWriteResponse)

type ReadRequest struct {
	// E00000-EFFFFF = SRAM
	// F50000-F6FFFF = WRAM
	// F70000-F8FFFF = VRAM
	// F90000-F901FF = CGRAM
	// F90200-F904FF = OAM
	Address   uint32
	Size      uint8
	Completed ReadOrWriteCompleted
}

type WriteRequest struct {
	// E00000-EFFFFF = SRAM
	// F50000-F6FFFF = WRAM
	// F70000-F8FFFF = VRAM
	// F90000-F901FF = CGRAM
	// F90200-F904FF = OAM
	Address   uint32
	Size      uint8
	Data      []byte
	Completed ReadOrWriteCompleted
}

var (
	driversMu sync.RWMutex
	drivers   = make(map[string]Driver)
)

// Register makes a SNES driver available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func Register(name string, driver Driver) {
	driversMu.Lock()
	defer driversMu.Unlock()
	if driver == nil {
		panic("snes: Register driver is nil")
	}
	if _, dup := drivers[name]; dup {
		panic("snes: Register called twice for driver " + name)
	}
	drivers[name] = driver
}

func unregisterAllDrivers() {
	driversMu.Lock()
	defer driversMu.Unlock()
	// For tests.
	drivers = make(map[string]Driver)
}

// Drivers returns a sorted list of the names of the registered drivers.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	list := make([]string, 0, len(drivers))
	for name := range drivers {
		list = append(list, name)
	}
	sort.Strings(list)
	return list
}

func DriverByName(name string) (Driver, bool) {
	d, ok := drivers[name]
	return d, ok
}

func Open(driverName string, desc DeviceDescriptor) (Conn, error) {
	driversMu.RLock()
	driveri, ok := drivers[driverName]
	driversMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("snes: unknown driver %q (forgotten import?)", driverName)
	}

	return driveri.Open(desc)
}
