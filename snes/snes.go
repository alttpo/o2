package snes

import (
	"fmt"
	"sort"
	"sync"
)

type Driver interface {
	Open(name string) (Conn, error)
}

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
	// closes the current connection
	Close() error

	// Submits a batch of read requests to the device.
	SubmitRead(reqs []ReadRequest)

	// Submits a batch of write requests to the device.
	SubmitWrite(reqs []WriteRequest)
}

type ROMControl interface {
	// Loads the given ROM into the system and resets.
	PlayROM(name string, rom []byte)
}

type ReadOrWriteResponse struct {
	IsWrite bool // was the request a read or write?
	Address uint32
	Size    uint8
	Data    []byte // the data that was read or written
}

type ReadOrWriteCompleted func(response ReadOrWriteResponse)

type ReadRequest struct {
	Address   uint32
	Size      uint8
	Completed ReadOrWriteCompleted
}

type WriteRequest struct {
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

func Open(driverName, portName string) (Conn, error) {
	driversMu.RLock()
	driveri, ok := drivers[driverName]
	driversMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("snes: unknown driver %q (forgotten import?)", driverName)
	}

	return driveri.Open(portName)
}
