package underworldmap

import (
	"encoding/binary"
	"fmt"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/emulator"
	"github.com/alttpo/snes/emulator/memory"
)

const (
	// the scratch RAM the generator's routines run from, at $5000..$5FFF of banks $00 and $02 where nothing is mapped
	// on the SNES; bank $00's calls the game's JSR routines in bank $00, and bank $02's those in bank $02:
	scratchBase = 0x5000
	scratchSize = 0x1000

	// the most CPU cycles a routine may run for before it is considered stuck:
	maxCycles = 50_000_000
)

// emu runs the game's own routines to load and draw underworld rooms: an SNES with a CPU, WRAM, and a PPU for VRAM
// writes and DMA. NMI and IRQ never come, as the routines are called directly rather than from the game's main loop.
type emu struct {
	a   *romAddresses
	sys *emulator.System
	ppu *emulator.PPU

	scratch00 [scratchSize]byte
	scratch02 [scratchSize]byte

	// entry points of the routines in the scratch RAM, all ending with STP:
	pcInit         uint32
	pcLoadEntrance uint32
	pcLoadRoom     uint32
	// pcPeekEntrance runs only LoadUnderworldEntrance, to find the room an entrance starts in; 0 if the ROM doesn't
	// call it where the vanilla game does:
	pcPeekEntrance uint32
	// address of the entrance ID operand of loadEntrance's LDA #imm:
	entranceIDOperand uint32

	// the results of Decompress by the source pointer, the call in progress if it isn't cached, and the routine
	// that returns from a cached call, with the operands it sets the registers from:
	decompressed    map[uint32]*decompressed
	decompressing   *decompressCall
	pcDecompressRet uint32
	decompressRetY  uint32
	decompressRetX  uint32

	// the effects of InitializeTilesets by its inputs, the call being recorded, and the end of the routine, PLB :
	// RTL:
	tilesets  map[string]*routineEffects
	recording *routineRecording
	pcPLBRTL  uint32

	// the state after initialization that each entrance starts from:
	snapWRAM [0x20000]byte
	snapSRAM [0x10000]byte
	snapPPU  emulator.PPU
}

func newEmu(rom []byte, a *romAddresses) (e *emu, err error) {
	e = &emu{
		a:   a,
		sys: &emulator.System{},
		ppu: &emulator.PPU{},
	}
	s := e.sys
	s.Devices = []emulator.Device{e.ppu}
	if err = s.CreateEmulator(); err != nil {
		return nil, err
	}
	// NMI and IRQ stay off; the routines that would enable them run outside the main loop:
	s.MapIO(0x4200, 0x4200, nil, func(uint16, byte) {})

	// WRAM that keeps track of what is written while a routine's effects are recorded:
	if err = e.attachWRAM(); err != nil {
		return nil, err
	}

	// the scratch RAM for our routines:
	if err = s.Bus.Attach(memory.NewRAM(e.scratch00[:], 0x00_0000|scratchBase), "scratch", 0x00_0000|scratchBase, 0x00_0000|scratchBase+scratchSize-1); err != nil {
		return nil, err
	}
	if err = s.Bus.Attach(memory.NewRAM(e.scratch02[:], 0x02_0000|scratchBase), "scratch", 0x02_0000|scratchBase, 0x02_0000|scratchBase+scratchSize-1); err != nil {
		return nil, err
	}

	copy(s.ROM[:], rom)
	if err = e.patchROM(); err != nil {
		return nil, err
	}
	if err = e.emitRoutines(); err != nil {
		return nil, err
	}
	e.hookRoutines()

	// run the game's startup code up to its first JSR, then our initialization. Randomizers hook the startup code
	// and come back to it in the FastROM banks $80 and up, so the bank's mirror bit is ignored:
	s.Reset()
	if err = e.runUntil(a.resetDone, 1_000_000); err != nil {
		return nil, fmt.Errorf("underworldmap: startup code: %w", err)
	}
	if err = e.call(e.pcInit); err != nil {
		return nil, fmt.Errorf("underworldmap: initialization: %w", err)
	}

	e.snapWRAM = s.WRAM
	e.snapSRAM = s.SRAM
	e.snapPPU = *e.ppu
	return e, nil
}

// romOffset converts a LoROM bus address to its offset in the ROM.
func romOffset(addr uint32) uint32 {
	return (addr>>16&0x7F)<<15 | addr&0x7FFF
}

func (e *emu) romEmitter(addr uint32) *asm.Emitter {
	a := asm.NewEmitter(e.sys.ROM[romOffset(addr):], false)
	a.SetBase(addr)
	return a
}

// patchROM patches the emulated ROM to skip what talks to the APU, which isn't emulated, the HUD, the lamp cones of
// dark rooms, loading sprite graphics and uploading the room's tilemaps to VRAM, as the map draws them from WRAM.
func (e *emu) patchROM() error {
	a := e.romEmitter(e.a.loadSongBank)
	a.RTS()

	a = e.romEmitter(e.a.underworldSongBankCall)
	a.JMP_abs_imm16_w(uint16(e.a.underworldSongBankExit))

	a = e.romEmitter(e.a.rebuildHUDKeys)
	a.RTL()

	a = e.romEmitter(e.a.roomDrawLampCones)
	a.RTS()

	a = e.romEmitter(e.a.initializeTilesetsSprites)
	a.WDM(wdmTilesetsEnter)
	a.JMP_abs_imm16_w(uint16(e.a.initializeTilesetsBG))

	a = e.romEmitter(e.a.uploadRoomTilemaps)
	a.JMP_abs_imm16_w(uint16(e.a.uploadRoomTilemapsDone))

	// the hooks of hookRoutines, which take the place of the 2-byte instructions they run:
	a = e.romEmitter(e.a.initializeTilesetsPLB) // PLB : RTL
	a.WDM(wdmTilesetsExit)
	a = e.romEmitter(e.a.decompress) // REP #$10
	a.WDM(wdmDecompressEnter)
	a = e.romEmitter(e.a.decompressExit) // SEP #$10
	a.WDM(wdmDecompressExit)
	return nil
}

func (e *emu) emitRoutines() (err error) {
	r := e.a
	pcLoadRoomBank02 := uint32(0x02_0000 | scratchBase)
	pcPeekBank02 := uint32(0)

	// bank $02: load and draw the room in $A0 as if Link had just entered the dungeon in it:
	{
		a := asm.NewEmitter(e.scratch02[:], false)
		a.SetBase(pcLoadRoomBank02)
		a.SEP(0x30)
		// PullBlocksTorchesFromROM ends with PLB; RTS: have it restore data bank $00:
		a.LDA_imm8_b(0x00)
		a.PHA()
		a.PLB()
		a.JSR_abs(uint16(r.pullBlocksTorchesPHB))
		// Module06_UnderworldLoad's RTL returns to our JSL:
		a.JMP_abs_imm16_w(uint16(r.module06AfterLoadEntrance))

		// Module06_UnderworldLoad's JSR LoadUnderworldEntrance, alone:
		if o := romOffset(r.module06AfterLoadEntrance - 3); e.sys.ROM[o] == 0x20 {
			pcPeekBank02 = a.Label("peekEntrance")
			a.SEP(0x30)
			a.JSR_abs(uint16(e.sys.ROM[o+1]) | uint16(e.sys.ROM[o+2])<<8)
			a.RTL()
		}
		if err = a.Finalize(); err != nil {
			return
		}
	}

	// bank $00:
	a := asm.NewEmitter(e.scratch00[:], false)
	a.SetBase(0x00_0000 | scratchBase)

	e.pcInit = a.Label("init")
	a.SEP(0x30)
	a.JSL(r.loadDefaultTileTypes)
	a.JSL(r.introInitializeDefaultGFX)
	// a new save file, which also sets the DMA source addresses the NMI updates use:
	a.REP(0x10)
	a.LDX_imm16_w(0)
	a.SEP(0x10)
	a.JSL(r.initializeSaveFile)
	a.JSL(r.copySaveToWRAM)
	// the world state after Zelda's rescue: no rain, and no bed cutscene in Link's house:
	a.LDA_imm8_b(0x02)
	a.STA_long(0x7EF3C5)
	a.LDA_imm8_b(0x10)
	a.STA_long(0x7EF3C6)
	// what the skipped Startup_InitializeMemory sets for the NMI updates: the WRAM source of the animated BG tiles
	// (water, lit torches, ...), which DoNMIUpdates uploads to VRAM, and two of Link's graphics sources:
	a.REP(0x20)
	a.LDA_imm16_w(0xA680)
	a.STA_abs(0x0ADC)
	a.LDA_imm16_w(0xB280)
	a.STA_abs(0x0AE0)
	a.LDA_imm16_w(0xB280 + 0x60)
	a.STA_abs(0x0AE2)
	a.SEP(0x20)
	a.STP()

	// enter the dungeon through the entrance in the operand at entranceIDOperand: module $06, submodule $00:
	e.pcLoadEntrance = a.Label("loadEntrance")
	a.SEP(0x30)
	a.LDA_imm8_b(0x06)
	a.STA_dp(0x10)
	a.STZ_dp(0x11)
	a.STZ_dp(0xB0)
	e.entranceIDOperand = a.Label("entranceID") + 1
	a.LDA_imm8_b(0x00)
	a.STA_abs(0x010E)
	a.JSL(r.runModule)
	a.BRA("updateVRAM")

	// find the room the entrance in the same operand starts in:
	if pcPeekBank02 != 0 {
		e.pcPeekEntrance = a.Label("peekEntrance")
		a.SEP(0x30)
		a.LDA_imm8_b(0x06)
		a.STA_dp(0x10)
		a.STZ_dp(0x11)
		a.STZ_dp(0xB0)
		a.LDA_abs(uint16(e.entranceIDOperand))
		a.STA_abs(0x010E)
		a.JSL(pcPeekBank02)
		a.STP()
	}

	// load and draw the room in $A0 with the graphics state of the entrance loaded before:
	e.pcLoadRoom = a.Label("loadRoom")
	a.SEP(0x30)
	a.JSL(r.introInitializeDefaultGFXTail)
	a.STZ_dp(0x11)
	a.JSL(pcLoadRoomBank02)

	a.Label("updateVRAM")
	// have DoNMIUpdates upload the animated tiles:
	a.STZ_abs(0x0710)
	a.JSR_abs(uint16(r.prepareOAMForTransfer))
	a.JSR_abs(uint16(r.doNMIUpdates))
	a.STP()

	// return from a cached Decompress as it does: with 8-bit X and Y, Y the low byte of the size of the output, X
	// that of the source pointer's final address, and the $FF that ends the data compared in A:
	e.pcDecompressRet = a.Label("decompressRet")
	a.REP(0x10)
	e.decompressRetY = a.Label("decompressRetY") + 1
	a.LDY_imm16_w(0)
	a.SEP(0x10)
	e.decompressRetX = a.Label("decompressRetX") + 1
	a.LDX_imm8_b(0)
	a.LDA_imm8_b(0xFF)
	a.CMP_imm8_b(0xFF)
	a.RTS()

	// the end of InitializeTilesets, which its patch for hookRoutines replaced:
	e.pcPLBRTL = a.Label("plbRTL")
	a.PLB()
	a.RTL()

	return a.Finalize()
}

// call runs the routine at pc until it executes STP.
func (e *emu) call(pc uint32) error {
	c := &e.sys.CPU
	c.Stopped, c.Waiting = false, false
	c.SP = 0x01FF
	c.RD = 0
	c.RDBR = 0
	e.sys.SetPC(pc)
	for cycles := uint64(0); cycles < maxCycles; {
		n, stopped := e.sys.Step()
		if stopped {
			return nil
		}
		cycles += uint64(n)
	}
	return fmt.Errorf("routine at $%06x ran too long; stopped at $%06x", pc, e.sys.GetPC())
}

// runUntil runs the CPU until it reaches pc in either of its mirrors, $00..$3F or $80..$BF.
func (e *emu) runUntil(pc uint32, maxCycles uint64) error {
	for cycles := uint64(0); cycles < maxCycles; {
		if e.sys.GetPC()&^0x80_0000 == pc&^0x80_0000 {
			return nil
		}
		n, stopped := e.sys.Step()
		if stopped {
			break
		}
		cycles += uint64(n)
	}
	return fmt.Errorf("did not reach $%06x; stopped at $%06x", pc, e.sys.GetPC())
}

// restore puts the system back in its state after initialization.
func (e *emu) restore() {
	e.sys.WRAM = e.snapWRAM
	e.sys.SRAM = e.snapSRAM
	*e.ppu = e.snapPPU
}

// loadEntrance enters the underworld through entrance id and returns the room it starts in.
func (e *emu) loadEntrance(id uint8) (room uint16, err error) {
	e.restore()
	e.scratch00[e.entranceIDOperand-(0x00_0000|scratchBase)] = id
	if err = e.call(e.pcLoadEntrance); err != nil {
		return
	}
	room = e.wram16(0xA0)
	return
}

// peekEntrance returns the room entrance id starts in, without loading it, as most entrances start in a room the
// map has already drawn.
func (e *emu) peekEntrance(id uint8) (room uint16, err error) {
	if e.pcPeekEntrance == 0 {
		return e.loadEntrance(id)
	}
	e.restore()
	e.scratch00[e.entranceIDOperand-(0x00_0000|scratchBase)] = id
	if err = e.call(e.pcPeekEntrance); err != nil {
		return
	}
	room = e.wram16(0xA0)
	return
}

// loadRoom loads and draws room with the graphics state of the last entrance loaded.
func (e *emu) loadRoom(room uint16) error {
	e.setWRAM16(0xA0, room)
	return e.call(e.pcLoadRoom)
}

func (e *emu) wram8(addr uint32) uint8 { return e.sys.WRAM[addr] }

func (e *emu) wram16(addr uint32) uint16 {
	return binary.LittleEndian.Uint16(e.sys.WRAM[addr : addr+2])
}

func (e *emu) setWRAM16(addr uint32, v uint16) {
	binary.LittleEndian.PutUint16(e.sys.WRAM[addr:addr+2], v)
}

func (e *emu) rom16(addr uint32) uint16 {
	o := romOffset(addr)
	return binary.LittleEndian.Uint16(e.sys.ROM[o : o+2])
}
