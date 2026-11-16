package alttp

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"io"
	"log"
	"o2/snes"
	"strings"
)

// SRAM layout in bank $70. $707400..$707FFF is free in vanilla and in z3randomizer based ROMs, which use
// $700000..$705FFF:
//
//	$707400..$7077FF  update routine buffer A
//	$707800..$707BFF  update routine buffer B
//	$707C00..$707F7F  common code and data, copied from ROM by the patched init code:
//	  $707C00           update stub: RTS, which the trampoline's JSR points at after a reset
//	  $707C02           preMain trampoline, called by the frame hook instead of `JSL Module_MainRouting`
//	  $707C80           map16 apply routine, called by generated map16 update routines
//	  $707D50           remote sprites drawing routine, called by the trampoline; see remotesprites.go
//	  $707E70           remote sprites drawing subroutine for one sprite
//	  $707EE8..$707F5D  remote sprites table, written by o2 (zeroed on reset)
//	  $707F5E..$707F75  remote sprites state, written by the game code (zeroed on reset)
//	$707F80..$707FD5  copy of Link's OAM entries, written by the trampoline every frame
//	$707FD6..$707FEF  the local player's sword sparkle and dash dust OAM entries, written by the trampoline every frame
//	$707FF0..$707FFF  patch signature, copied from ROM by the patched init code; see signature.go
const (
	// SRAM buffers that generated update routines are written to, alternately. The trampoline's JSR is only pointed
	// at one of these buffers after a routine was written there:
	updateBufAAddr = uint32(0x707400)
	updateBufBAddr = uint32(0x707800)
	// updateBufSize is the size of each buffer and the maximum size of a generated update routine. Routines larger
	// than 255 bytes are written in several chunks; the trampoline's JSR is only redirected after all of them:
	updateBufSize = 0x400

	// sramCodeAddr is where the patched init code copies the common code to:
	sramCodeAddr = uint32(0x707C00)
	sramCodeEnd  = linkOAMCopyAddr
	// updateStubAddr is an RTS that the trampoline's JSR points at after a reset, until the first update routine is
	// written. o2 only rewrites the high byte of the JSR's operand (a single byte write cannot be torn), so the low
	// byte must be the same as the update buffers':
	updateStubAddr = sramCodeAddr
	// SRAM address of preMain routine called every frame instead of `JSL Module_MainRouting`:
	preMainAddr = uint32(0x707C02)
	// SRAM address of the high byte of the preMain routine's `JSR updateStubAddr` operand:
	preMainJSRAddr = uint32(0x707C1A)
	// map16ApplyAddr is the routine that generated map16 update routines JSR to; see emitMap16Apply:
	map16ApplyAddr = uint32(0x707C80)
	// remote sprites routines; see remotesprites.go:
	remoteDrawAddr       = uint32(0x707D50)
	remoteDrawSpriteAddr = uint32(0x707E70)

	// linkOAMCopyAddr is where the trampoline copies Link's OAM entries to every frame, after the module drew them
	// and before the next frame's ClearOAMBuffer sets all their Y coordinates to $F0. See linkOAMCopy* for the layout:
	linkOAMCopyAddr = uint32(0x707F80)

	// bus address of unused/garbage area in ROM (JP and US confirmed):
	romGarbageStart = uint32(0x1BB1D7)
	romGarbageEnd   = uint32(0x1BB800)
)

// layout of the copy of Link's OAM entries at linkOAMCopyAddr:
const (
	// $0352: byte offset of Link's first OAM entry in the OAM buffer at $0800:
	linkOAMCopyStart = 0
	// linkOAMCount entries copied from $0800+[$0352]: x, y, chr, attributes:
	linkOAMCopyEntries = linkOAMCopyStart + 2
	// linkOAMCount extended attributes copied from $0A20+[$0352]/4: size, x bit 8:
	linkOAMCopyExt = linkOAMCopyEntries + linkOAMCount*4
	// $0AC0..$0AD7: sword, shield and Link's graphics DMA source addresses for the copied entries' tiles:
	linkOAMCopyDMA     = linkOAMCopyExt + linkOAMCount
	linkOAMCopyDMAWRAM = 0x0AC0
	linkOAMCopyDMASize = 0x0AD8 - linkOAMCopyDMAWRAM
	linkOAMCopySize    = linkOAMCopyDMA + linkOAMCopyDMASize
	// sword sparkle and dash dust OAM entries (see emitCaptureExtras): byte length, then up to linkOAMExtrasMax
	// entries of x, y, chr, attributes, OAM index << 1 | x bit 8 (all 8x8):
	linkOAMCopyExtras   = linkOAMCopySize
	linkOAMExtrasMax    = 5
	linkOAMExtraSize    = 5
	linkOAMCopyReadSize = linkOAMCopyExtras + 1 + linkOAMExtrasMax*linkOAMExtraSize
)

type Patcher struct {
	rom *snes.ROM
	r   io.Reader
	w   io.Writer
}

func NewPatcher(rom *snes.ROM) *Patcher {
	return &Patcher{rom: rom}
}

// Patch patches the ROM for O2 support.
//
// NOTE: increment patchFormat (signature.go) for any change to the init code or frame hook emitted here; changes to
// the code copied to SRAM (emitSRAMCode) and to the SRAM layout constants change the patch version automatically.
func (p *Patcher) Patch() (err error) {
	// identify the ROM before it is modified:
	signature := makePatchSignature(p.rom.Contents)

	// free bytes in JP 1.0 rom:
	// $00:89C2 - 30 bytes
	// $00:E892 - 30 bytes
	// $00:F7E1 - 31 bytes
	// $00:FFB7 - 9 bytes

	// $1B:B1D7 - 1577 bytes free
	// the last valid SPC data is at $1B:B1D3: dw 0, $0800
	// if you do go looking for that 5.8k free space, you'll see this sequence of bytes most likely
	// $C0, $00, $00, $00, $00, $01, $FF, $00, $00
	// I can assure you it's garbage

	// patch header to expand SRAM size:
	hdr := &p.rom.Header
	if hdr.RAMSize < 5 {
		// 1024 << 5 = 32768 bytes, aka $70:0000-7FFF
		hdr.RAMSize = 5
		if err = p.rom.WriteHeader(); err != nil {
			return
		}
	}

	// read from $00:802F which is where NMI should be enabled in the reset routine:
	p.readAt(0x00802F)
	var code802F []byte
	code802F, err = p.read(5)
	if err != nil {
		return
	}

	// this is what code at 802F should look like in vanilla ROMs:
	expected802F := []byte{
		// CODE_00802F:	A9 81       LDA #$81   ;\ Enable NMI, Auto-Joypad read enable.
		// CODE_008031:	8D 00 42    STA $4200  ;/
		0xA9, 0x81,
		0x8D, 0x00, 0x42,
	}

	if !bytes.Equal(code802F, expected802F) {
		// let's at least check that it's a JSL/JML followed by a NOP:
		// JSL = 0x22, JML = 0x5c
		if (code802F[0] != 0x22 && code802F[0] != 0x5c) || code802F[4] != 0xEA {
			// it's not vanilla code nor is it a JSL/JML + NOP combo:
			return fmt.Errorf("unexpected code at $00:802F: %s", hex.Dump(code802F))
		}
	}

	textBuf := &strings.Builder{}
	defer func() {
		log.Print(textBuf.String())
	}()

	// overwrite $00:802F with `JSL $1BB1D7`
	const initHook = romGarbageStart

	var pcAddr uint32
	pcAddr, err = lorom.BusAddressToPak(0x00_802F)
	a := asm.NewEmitter(p.rom.Slice(pcAddr, uint32(len(expected802F))), true)
	a.SetBase(0x00802F)
	if code802F[0] == 0x5c {
		// NOTE: FastROM randomizer replaces JSL with JML to its init and then JMLs back to $8034.
		// doing a JML to our own init and appending the randomizer's copy of 802F code to our init
		// will still JML back to $808034 and everything should work as intended:
		a.JML(initHook)
	} else {
		a.JSL(initHook)
	}
	a.NOP()
	if err := a.Finalize(); err != nil {
		return err
	}
	a.WriteTextTo(textBuf)
	if a.Len() != len(expected802F) {
		return fmt.Errorf("assembler failed to produce exactly %d bytes to patch", len(expected802F))
	}

	// frame hook:
	const frameHook = 0x008056
	// 008056 is 22 B5 80 00   JSL Module_MainRouting
	p.readAt(frameHook)
	var frameJSL []byte
	frameJSL, err = p.read(4)
	if err != nil {
		return
	}
	if frameJSL[0] != 0x22 {
		return fmt.Errorf("frame hook $008056 does not contain a JSL instruction: %s", hex.Dump(frameJSL))
	}
	gameModes := frameJSL[1:]

	// we can't write to SRAM from this program because we only have access to the ROM contents, so the patched init
	// code copies the common code (the update stub, the frame trampoline which ends with the original JSL from the
	// frameHook, and the map16 apply routine) from ROM to SRAM with MVN:
	fns := make(map[romFunction]uint32)
	fillRomFunctions(p.rom, fns)
	ta := asm.NewEmitter(make([]byte, sramCodeEnd-sramCodeAddr), true)
	ta.SetBase(sramCodeAddr)
	if err = p.emitSRAMCode(ta, gameModes, fns); err != nil {
		return
	}
	ta.WriteTextTo(textBuf)

	// start writing at the end of the ROM after music data:
	pcAddr, err = lorom.BusAddressToPak(initHook)
	a = asm.NewEmitter(p.rom.Slice(pcAddr, patchSignatureROMAddr-romGarbageStart), true)
	a.SetBase(initHook)
	a.Comment("copy the common code to SRAM:")
	a.PHP()
	a.PHB()
	a.REP(0x30)
	a.LDX_imm16_w(0xFFFF) // source address; fixed up below
	srcOperand := a.PC() - 2
	a.LDY_imm16_w(uint16(sramCodeAddr & 0xFFFF))
	a.LDA_imm16_w(uint16(ta.Len() - 1))
	a.MVN(uint8(sramCodeAddr>>16), 0xFF) // source bank; fixed up below
	srcBankOperand := a.PC() - 1
	a.Comment("copy the patch signature to SRAM:")
	a.LDX_imm16_w(uint16(patchSignatureROMAddr & 0xFFFF))
	a.LDY_imm16_w(uint16(patchSignatureAddr & 0xFFFF))
	a.LDA_imm16_w(patchSignatureSize - 1)
	a.MVN(uint8(patchSignatureAddr>>16), uint8(patchSignatureROMAddr>>16))
	a.PLB()
	a.PLP()
	// append the original 802F code to our custom init hook:
	a.EmitBytes(code802F)
	// follow by `RTL`
	// NOTE: in FastROM randomizer, this will never be reached because its 802F code JMLs back to 8034:
	a.RTL()
	// the common code to copy follows:
	src := a.PC()
	if src&0xFFFF+uint32(ta.Len()) > 0x10000 {
		return fmt.Errorf("SRAM code at $%06x crosses a bank boundary", src)
	}
	a.Comment("common code for SRAM:")
	a.EmitBytes(ta.Bytes())
	// routines that run from ROM:
	if a.PC() > captureTypesAddr {
		return fmt.Errorf("SRAM code copy at $%06x overlaps ROM routines at $%06x", a.PC(), captureTypesAddr)
	}
	for a.PC() < captureTypesAddr {
		a.EmitBytes([]byte{0})
	}
	emitCaptureExtras(a)
	nmiHook := a.PC()
	emitNMIHook(a, linkGfxBankInstruction(p.rom))
	if err := a.Finalize(); err != nil {
		return err
	}
	code := a.Bytes()
	code[srcOperand-initHook+0] = uint8(src)
	code[srcOperand-initHook+1] = uint8(src >> 8)
	code[srcBankOperand-initHook] = uint8(src >> 16)
	a.WriteTextTo(textBuf)

	// write the patch signature:
	pcAddr, err = lorom.BusAddressToPak(patchSignatureROMAddr)
	if err != nil {
		return
	}
	copy(p.rom.Slice(pcAddr, patchSignatureSize), signature[:])
	textBuf.WriteString(fmt.Sprintf("patch signature at $%06x: % x\n", patchSignatureROMAddr, signature[:]))
	p.rom.PatchVersion = patchVersionString()

	// overwrite the frame hook with a JSL to the SRAM trampoline:
	pcAddr, err = lorom.BusAddressToPak(frameHook)
	a = asm.NewEmitter(p.rom.Slice(pcAddr, 4), true)
	a.SetBase(frameHook)
	a.JSL(preMainAddr)
	if err := a.Finalize(); err != nil {
		return err
	}
	a.WriteTextTo(textBuf)

	// NMI hook in NMI_DoUpdates that uploads a remote player's head and body tiles; without it, remote players are only
	// drawn as blobs (see Game.remoteBodyOK):
	pcAddr, err = lorom.BusAddressToPak(nmiHookPatchAddr)
	if err != nil {
		return
	}
	nmiCode := p.rom.Slice(pcAddr, uint32(len(nmiHookPatchExpected)))
	if bytes.Equal(nmiCode, nmiHookPatchExpected) {
		a = asm.NewEmitter(nmiCode, true)
		a.SetBase(nmiHookPatchAddr)
		a.JSL(nmiHook)
		a.NOP()
		a.NOP()
		if err := a.Finalize(); err != nil {
			return err
		}
		a.WriteTextTo(textBuf)
	} else {
		log.Printf("alttp: patch: unexpected code at $%06x: % x; remote players are drawn without Link's graphics\n", nmiHookPatchAddr, nmiCode)
	}

	return nil
}

// emitSRAMCode emits the common code that the patched init code copies to SRAM at sramCodeAddr.
func (p *Patcher) emitSRAMCode(a *asm.Emitter, gameModes []byte, fns map[romFunction]uint32) (err error) {
	// pad emits zero bytes up to addr, which must not be behind the PC:
	pad := func(addr uint32) {
		if a.PC() > addr {
			panic(fmt.Errorf("SRAM code at $%06x overlaps $%06x", a.PC(), addr))
		}
		if n := addr - a.PC(); n > 0 {
			a.EmitBytes(make([]byte, n))
		}
	}

	// assume 8-bit mode for accumulator and index registers:
	a.AssumeSEP(0x30)

	if a.Label("updateStub") != updateStubAddr {
		panic(fmt.Errorf("updateStubAddr expected to be $%06x but got $%06x", a.PC(), updateStubAddr))
	}
	a.RTS()

	pad(preMainAddr)
	a.Label("preMain")
	a.Comment("only update during safe modules:")
	a.LDA_dp(0x10)
	// >= $19    : bad
	a.CMP_imm8_b(0x19)
	a.BCS("syncExit")
	// <= $06    : bad
	a.CMP_imm8_b(0x07)
	a.BCC("syncExit")
	// == $08    : bad
	a.CMP_imm8_b(0x08)
	a.BEQ("syncExit")
	// == $0A    : bad
	a.CMP_imm8_b(0x0A)
	a.BEQ("syncExit")
	// == $14    : bad
	a.CMP_imm8_b(0x14)
	a.BEQ("syncExit")

	jsr := a.Label("syncStart")
	if preMainJSRAddr != jsr+2 {
		panic(fmt.Errorf("preMainJSRAddr expected to be $%06x but got $%06x", jsr+2, preMainJSRAddr))
	}
	a.JSR_abs(uint16(updateStubAddr & 0xFFFF))

	a.Label("syncExit")
	emitRemoteSave(a)

	// jsl to mainrouting that we patched over:
	a.JSL_lhb(gameModes[0], gameModes[1], gameModes[2])

	p.emitLinkOAMCopy(a)
	a.Comment("copy sparkles and dust for o2 to send:")
	a.JSL(captureExtrasAddr)
	a.Comment("draw remote players:")
	a.JSR_abs(uint16(remoteDrawAddr & 0xFFFF))
	a.RTL()

	pad(map16ApplyAddr)
	if err = emitMap16Apply(a, fns[fnOverworldDrawMap16Persist]); err != nil {
		return
	}

	pad(remoteDrawAddr)
	emitRemoteDraw(a)
	pad(remoteDrawSpriteAddr)
	emitRemoteDrawSprite(a)

	// the remote sprites table and state start out zeroed (TTL 0, nothing to upload):
	pad(remoteTableAddr)
	pad(remoteStateAddr + remoteStateSize)

	if err = a.Finalize(); err != nil {
		return
	}
	return
}

// emitLinkOAMCopy copies Link's OAM entries to linkOAMCopyAddr for o2 to read. It runs right after the module drew
// them. Reading them from the OAM buffer in WRAM races with the next frame's ClearOAMBuffer, which sets the Y
// coordinates of all entries to $F0 and made Link disappear for alttpo players. o2 may still read the copy while it
// is being written, which at worst mixes two consecutive frames' entries.
func (p *Patcher) emitLinkOAMCopy(a *asm.Emitter) {
	a.Comment("copy Link's OAM entries to SRAM before the next frame clears them:")
	a.PHP()
	a.PHB()
	a.REP(0x30)
	a.LDA_long(0x7E0352)
	a.STA_long(linkOAMCopyAddr + linkOAMCopyStart)
	a.CLC()
	a.ADC_imm16_w(0x0800)
	a.TAX()
	a.LDY_imm16_w(uint16((linkOAMCopyAddr + linkOAMCopyEntries) & 0xFFFF))
	a.LDA_imm16_w(linkOAMCount*4 - 1)
	a.MVN(uint8(linkOAMCopyAddr>>16), 0x7E)
	a.Comment("extended attributes; Y is now at the copy's extended attributes:")
	a.LDA_long(0x7E0352)
	a.LSR()
	a.LSR()
	a.CLC()
	a.ADC_imm16_w(0x0A20)
	a.TAX()
	a.LDA_imm16_w(linkOAMCount - 1)
	a.MVN(uint8(linkOAMCopyAddr>>16), 0x7E)
	a.Comment("graphics DMA source addresses; Y is now at the copy's DMA source addresses:")
	a.LDX_imm16_w(linkOAMCopyDMAWRAM)
	a.LDA_imm16_w(linkOAMCopyDMASize - 1)
	a.MVN(uint8(linkOAMCopyAddr>>16), 0x7E)
	a.PLB()
	a.PLP()
}

func (p *Patcher) readAt(busAddr uint32) {
	p.r = p.rom.BusReader(busAddr)
}

func (p *Patcher) read(length int) (d []byte, err error) {
	d = make([]byte, length)
	t := 0
	for t < len(d) {
		var n int
		n, err = p.r.Read(d[t:])
		if err != nil {
			return
		}
		t += n
	}
	return
}
