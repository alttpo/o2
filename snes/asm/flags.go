package asm

// Flags nvmxdizc
type Flags uint8

const (
	Carry Flags = 1 << iota
	Zero
	IRQDisable
	DecimalMode
	IndexRegister8bit
	Accumulator8bit
	Overflow
	Negative
)

type FlagsTracker interface {
	Flags() Flags
	IsX16bit() bool
	IsM16bit() bool
	AssumeREP(c uint8)
	AssumeSEP(c uint8)
}

// flagsTracker implements FlagsTracker
type flagsTracker Flags

func (t flagsTracker) Flags() Flags {
	return Flags(t)
}

func (t flagsTracker) IsX16bit() bool {
	return Flags(t)&IndexRegister8bit == 0
}

func (t flagsTracker) IsM16bit() bool {
	return Flags(t)&Accumulator8bit == 0
}

func (t *flagsTracker) AssumeREP(c uint8) {
	*t &= ^flagsTracker(c)
}

func (t *flagsTracker) AssumeSEP(c uint8) {
	*t |= flagsTracker(c)
}
