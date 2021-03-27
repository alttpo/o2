package snes

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

	// Enqueues a command with a channel to be sent to when completed [err==nil] or errored [err!=nil]
	EnqueueWithCompletion(cmd Command, complete chan<- error)

	// Enqueues a sequence of commands to be executed in order
	EnqueueMulti(cmds CommandSequence)

	// Enqueues a sequence of commands to be executed in order with only the last command receiving the completion channel
	EnqueueMultiWithCompletion(cmds CommandSequence, complete chan<- error)

	// Creates a set of Commands that submits a batch of read requests to the device
	MakeReadCommands(reqs []ReadRequest) CommandSequence

	// Creates a set of Commands that submits a batch of write requests to the device
	MakeWriteCommands(reqs []WriteRequest) CommandSequence
}
