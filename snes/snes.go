package snes

// Represents an asynchronous communication interface to either a physical or emulated SNES system.
// Communication with a physical SNES console is done via a flash cart with a USB connection.
// Both read and write requests are both enqueued into the same request queue and are processed in the order received.
// For reads, the read data is sent to the ReplyTo channel specified in the ReadRequest struct.
// Depending on the implementation, reads and writes may be broken up into fixed-size batches.
// Read requests can read from ROM, SRAM, and WRAM. Flash carts can listen to the SNES address and data buses in order
// to shadow WRAM for reading.
// Write requests can only write to ROM and SRAM. WRAM cannot be written to from flash carts on real hardware; this is a
// hard limitation due to the design of the SNES and is not specific to any flash cart.
type SNES interface {
	// Submits a batch of read requests to the device. Data comes back on the ReplyTo channel specified.
	SubmitRead(reqs []ReadRequest)
	// Submits a batch of write requests to the device.
	SubmitWrite(reqs []WriteRequest)
}

type ReadRequest struct {
	Address uint32
	Size    uint8
	ReplyTo chan<- []byte
}

type WriteRequest struct {
	Address uint32
	Size    uint8
	Data    []byte
}
