package alttp

import (
	"github.com/alttpo/snes/asm"
	"o2/snes"
)

// Update data
//
// Generated update routines are kept short: the code that applies a list of changes (map16 changes, manipulable
// objects, chests, flying tiles) is in ROM (see patchROM), and the update routine only calls it through the SRAM stub
// with A = the address of a table of the changes, which o2 writes to the update data buffer that goes with the update
// routine buffer: data buffer A with routine buffer A, B with B. Like the routine, the table is written to the buffers
// that the game does not use: o2 writes the data, then the routine, and only then points callUpdate's JSR at it.
//
// The tables are in bank updateDataBank; the ROM routines read them with long addressing, so DB is whatever the update
// routine runs with (the game's).
const (
	updateDataAAddr = uint32(0x707100)
	updateDataBAddr = uint32(0x707280)
	updateDataSize  = 0x180
	updateDataBank  = updateDataAAddr & 0xFF0000

	// scratch words of the ROM routines, which call game routines that use the direct page scratch ($00-$0F) freely:
	updateScratchAddr = uint32(0x7070F0)
	// the address of the table entry being applied:
	updScratchPtr = updateScratchAddr + 0
	// how many entries were applied:
	updScratchCount = updateScratchAddr + 2
	// a temporary value:
	updScratchTmp = updateScratchAddr + 4
	// a second temporary value:
	updScratchTmp2   = updateScratchAddr + 6
	updateScratchEnd = updateScratchAddr + 8

	// updateTableEnd terminates the lists in the tables:
	updateTableEnd = uint16(0xFFFF)
)

// the data buffers must end before the update routine buffers, and the scratch before the data buffers:
const _ = updateBufAAddr - (updateDataBAddr + updateDataSize)
const _ = updateDataBAddr - (updateDataAAddr + updateDataSize)
const _ = updateDataAAddr - updateScratchEnd
const _ = updateScratchAddr - (runTimingAddr + runTimingSize)

// updateDataAddrFor returns the update data buffer that goes with the update routine buffer at target.
func updateDataAddrFor(target uint32) uint32 {
	if target == updateBufBAddr {
		return updateDataBAddr
	}
	return updateDataAAddr
}

// updateData collects the tables of the update routine being generated.
type updateData struct {
	base uint32
	buf  []byte
}

// add appends a table and returns its address, or ok = false if it does not fit; the generator then emits nothing.
func (d *updateData) add(b []byte) (addr uint32, ok bool) {
	if d == nil || len(d.buf)+len(b) > updateDataSize {
		return 0, false
	}
	addr = d.base + uint32(len(d.buf))
	d.buf = append(d.buf, b...)
	return addr, true
}

// mark returns the length of the data so far, for reset to drop the tables of routines that were not emitted.
func (d *updateData) mark() int {
	if d == nil {
		return 0
	}
	return len(d.buf)
}

func (d *updateData) reset(mark int) {
	if d == nil || mark > len(d.buf) {
		return
	}
	d.buf = d.buf[:mark]
}

// emitCallUpdateROM emits the call of the ROM routine behind the SRAM stub's JML at stub with A = table; 16-bit A.
func emitCallUpdateROM(a *asm.Emitter, stub, table uint32) {
	a.LDA_imm16_w(uint16(table))
	a.JSL(stub)
}

// owRevealROM are the routines and objects of the ROM that the map16 update routine reveals secrets with; see
// syncsecrets.go. ok is false if any is missing: the routine then reveals nothing.
type owRevealROM struct {
	ok                       bool
	fnReveal, fnRTL, fnSpawn uint32
	liftables                []owLiftable
}

func owRevealROMFor(rom *snes.ROM, fns map[romFunction]uint32) (r owRevealROM) {
	var ok1, ok2, ok3 bool
	r.fnReveal, ok1 = fns[fnOverworldRevealSecret]
	r.fnRTL, ok2 = fns[fnBank1BRTL]
	r.fnSpawn, ok3 = fns[fnSpawnThrowableTerrainSilently]
	r.liftables = readOwLiftables(rom)
	r.ok = ok1 && ok2 && ok3 && len(r.liftables) > 0
	return
}
