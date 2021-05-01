package emulator

import (
	"o2/snes/emulator/bus"
	"o2/snes/emulator/cpu65c816"
	"o2/snes/emulator/memory"
)

type System struct {
	// emulated system:
	Bus *bus.Bus
	Cpu *cpu65c816.CPU

	ROM  [0x1000000]byte
	WRAM [0x20000]byte
	SRAM [0x8000]byte
}

func (q *System) CreateEmulator() (err error) {
	// create Bus and Cpu for emulator:
	q.Bus, _ = bus.New()
	q.Cpu, _ = cpu65c816.New(q.Bus)

	// map in ROM to Bus; parts of this mapping will be overwritten:
	for b := uint32(0); b < 0x40; b++ {
		halfBank := b << 15
		bank := b << 16
		err = q.Bus.Attach(
			memory.NewRAM(q.ROM[halfBank:halfBank+0x8000], bank|0x8000),
			"rom",
			bank|0x8000,
			bank|0xFFFF,
		)
		if err != nil {
			return
		}
		// mirror:
		err = q.Bus.Attach(
			memory.NewRAM(q.ROM[halfBank:halfBank+0x8000], (bank+0x80)|0x8000),
			"rom",
			(bank+0x80)|0x8000,
			(bank+0x80)|0xFFFF,
		)
		if err != nil {
			return
		}
	}

	// SRAM:
	{
		err = q.Bus.Attach(
			memory.NewRAM(q.SRAM[0:0x8000], 0x700000),
			"sram",
			0x700000,
			0x707FFF,
		)
		if err != nil {
			return
		}
		// mirror:
		err = q.Bus.Attach(
			memory.NewRAM(q.SRAM[0:0x8000], 0xF00000),
			"sram",
			0xF00000,
			0xF07FFF,
		)
		if err != nil {
			return
		}
	}

	// WRAM:
	{
		err = q.Bus.Attach(
			memory.NewRAM(q.WRAM[0:0x20000], 0x7E0000),
			"wram",
			0x7E0000,
			0x7FFFFF,
		)
		if err != nil {
			return
		}
	}

	return
}
