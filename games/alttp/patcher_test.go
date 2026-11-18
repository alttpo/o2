package alttp

import (
	"fmt"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/emulator"
	"github.com/alttpo/snes/mapping/lorom"
	"log"
	"o2/snes"
	"os"
	"testing"
)

const testROMMainGameLoop = 0x00_8034
const testROMDoFrame = 0x00_8051
const testROMJSLMainRouting = 0x00_8056

// testROMModuleRouting stands in for Module_MainRouting with just an RTL:
const testROMModuleRouting = 0x00_8100

// testROMBreakPoint is where a frame ends, after the patched frame hook returned:
const testROMBreakPoint = 0x00_805A

func MakeTestROM(title string, b []byte) (rom *snes.ROM, err error) {
	rom, err = snes.NewROM("test.sfc", b)
	if err != nil {
		return
	}

	// fill the free ROM sections with $FF like the real ROMs have them, so that the patcher uses them:
	for _, spans := range [][]romSpan{freeROMJP10, freeROMUS10, {patchSignatureSpan}} {
		for _, s := range spans {
			fillTestROM(rom, s, 0xFF)
		}
	}

	// build ROM header:
	copy(rom.Header.Title[:], title)
	rom.EmulatedVectors.RESET = 0x8000
	rom.Header.RAMSize = 5 // 1024 << 5 = 32768 bytes

	err = rom.WriteHeader()
	if err != nil {
		return
	}

	// write RESET vector:
	var pakAddr uint32
	pakAddr, err = lorom.BusAddressToPak(0x00_8000)
	a := asm.NewEmitter(rom.Slice(pakAddr, 0x2F), true)
	a.SetBase(0x00_8000)
	a.SEP(0x30)
	a.BRA_imm8(0x2F - 0x04)
	if err = a.Finalize(); err != nil {
		return
	}
	a.WriteTextTo(log.Writer())

	// write the $802F code that will be patched over:
	pakAddr, err = lorom.BusAddressToPak(0x00_802F)
	a = asm.NewEmitter(rom.Slice(pakAddr, 0x50), true)
	a.SetBase(0x00_802F)
	a.AssumeSEP(0x30)
	a.LDA_imm8_b(0x81)
	a.STA_abs(0x4200)
	//a.BRA_imm8(0x56 - 0x34 - 2)
	if err = a.Finalize(); err != nil {
		return
	}
	a.WriteTextTo(log.Writer())

	// write the $8034 code as main game loop:
	pakAddr, err = lorom.BusAddressToPak(testROMMainGameLoop)
	a = asm.NewEmitter(rom.Slice(pakAddr, 0x30), true)
	a.SetBase(testROMMainGameLoop)
	a.AssumeSEP(0x30)
	a.BRA("do_frame")
	for a.PC() < testROMDoFrame {
		a.NOP()
	}
	if a.Label("do_frame") != testROMDoFrame {
		panic(fmt.Errorf("do_frame label must be at %#06x", testROMDoFrame))
	}
	// increment frame counter:
	a.INC_dp(0x1A)
	for a.PC() < testROMJSLMainRouting {
		a.NOP()
	}
	a.JSL(testROMModuleRouting)
	if a.PC() != testROMBreakPoint {
		panic(fmt.Errorf("frame breakpoint must be at %#06x", testROMBreakPoint))
	}
	a.STP()
	if err = a.Finalize(); err != nil {
		return
	}
	a.WriteTextTo(log.Writer())

	// NMI_DoUpdates code that the patcher looks for: the LinkGraphics bank and the NMI hook location:
	for addr, code := range map[uint32][]byte{
		linkGfxBankAddr:            {0xA9, 0x10},
		nmiHookPatchAddr:           nmiHookPatchExpected,
		nmiEarlyPatchAddr:          nmiEarlyPatchExpected,
		nmiFollowerPatchAddr:       nmiFollowerPatchExpected,
		nmiFollowerBottomPatchAddr: nmiFollowerBottomPatchExpected,
	} {
		pakAddr, err = lorom.BusAddressToPak(addr)
		if err != nil {
			return
		}
		copy(rom.Slice(pakAddr, uint32(len(code))), code)
	}

	// the start of Overworld_HandleOverlaysAndBombDoors that the patch hooks, at the US and JP addresses; RTL instead of
	// the rest of it:
	for _, addr := range []uint32{0x02EF29, 0x02EC8D} {
		pakAddr, err = lorom.BusAddressToPak(addr)
		if err != nil {
			return
		}
		copy(rom.Slice(pakAddr, uint32(len(map16StageHookExpected)+1)), append(append([]byte(nil), map16StageHookExpected...), 0x6B))
	}

	// the code in Dungeon_LoadRoom that the patch hooks; RTL instead of the rest of it:
	pakAddr, err = lorom.BusAddressToPak(manipStageHookAddr)
	if err != nil {
		return
	}
	copy(rom.Slice(pakAddr, uint32(len(manipStageHookExpected)+1)), append(append([]byte(nil), manipStageHookExpected...), 0x6B))

	// module routing:
	pakAddr, err = lorom.BusAddressToPak(testROMModuleRouting)
	a = asm.NewEmitter(rom.Slice(pakAddr, 1), true)
	a.SetBase(testROMModuleRouting)
	a.RTL()
	if err = a.Finalize(); err != nil {
		return
	}

	return
}

func TestPatcher_Patch(t *testing.T) {
	// 1 MiB size matches ALTTP JP 1.0 ROM size
	var b [0x10_0000]byte

	var rom, err = MakeTestROM("ZELDANODENSETSU", b[:])
	if err != nil {
		t.Errorf("MakeTestROM() error = %v", err)
	}

	p := NewPatcher(rom)
	if err = p.Patch(); err != nil {
		t.Errorf("Patch() error = %v", err)
	}
}

func TestPatcher_FastROMRandomizer(t *testing.T) {
	fileName := "alttpr - NoGlitches-standard-ganon_ry08z8Q5y5.sfc"
	fileNamePatched := "alttpr - NoGlitches-standard-ganon_ry08z8Q5y5.o2.sfc"

	var err error

	// read the ROM contents:
	var fileContents []byte
	fileContents, err = os.ReadFile(fileName)
	if err != nil {
		t.Skip("could not load randomized ROM to test with")
		return
	}

	// create a ROM struct out of it and parse the header:
	var rom *snes.ROM
	rom, err = snes.NewROM(fileName, fileContents)
	if err != nil {
		t.Errorf("snes.NewROM() error = %v", err)
	}

	// patch the ROM:
	p := NewPatcher(rom)
	if err = p.Patch(); err != nil {
		t.Errorf("Patch() error = %v", err)
	}

	// write out the patched ROM file:
	err = os.WriteFile(fileNamePatched, rom.Contents, 0644)
	if err != nil {
		t.Logf("error writing patched ROM: %v", err)
	}
}

func TestPatcher_ModuleTests(t *testing.T) {
	var err error
	var rom *snes.ROM
	var e *emulator.System

	e, rom, err = CreateTestEmulator("ZELDANODENSETSU", t)
	if err != nil {
		t.Fatal(err)
	}

	_ = rom
	// write the update routine to buffer A and point callUpdate's JSR at it, like o2 does:
	a := asm.NewEmitter(e.SRAM[updateBufAAddr&0x7FFF:], false)
	e.SRAM[updateJSRAddr&0x7FFF] = uint8(updateBufAAddr >> 8 & 0xFF)
	// negative match case keeps $0F at #$00:
	// positive match case sets  $0F to #$FF:
	a.DEC_dp(0x0F)
	a.RTS()
	if err = a.Finalize(); err != nil {
		return
	}

	for m := 0; m <= 0xFF; m++ {
		m := uint8(m)
		t.Run(fmt.Sprintf("module $%02X", m), func(t *testing.T) {
			e.WRAM[0x0F] = 0
			// set module for testing:
			e.WRAM[0x10] = m

			// test the module for inclusion:
			e.CPU.Reset()
			e.CPU.SetFlags(0x30)
			e.SetPC(0x008056)
			t.Logf("$0F = $%02X\n", e.WRAM[0x0F])
			if !e.RunUntil(testROMModuleRouting, 10_000) {
				t.Fatal("ran too long")
			}
			t.Logf("$0F = $%02X\n", e.WRAM[0x0F])

			// check if the module should have passed:
			if _, ok := modulesOKForSync[m]; ok {
				if e.WRAM[0x0F] == 0 {
					t.Fatal("unexpected fail!")
				}
			} else {
				if e.WRAM[0x0F] != 0 {
					t.Fatal("unexpected pass!")
				}
			}
		})
	}
}

// fillTestROM fills the span s of the ROM with v.
func fillTestROM(rom *snes.ROM, s romSpan, v byte) {
	pak, err := lorom.BusAddressToPak(s.start)
	if err != nil {
		panic(err)
	}
	for i := range rom.Slice(pak, s.size()) {
		rom.Contents[pak+uint32(i)] = v
	}
}
