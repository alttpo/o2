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
	"slices"
	"strings"
)

// SRAM layout in bank $70. $706000..$707FFF is free in vanilla, which uses $700000..$701FFF, and in z3randomizer
// based ROMs, which use $700000..$705FFF (their save backup is $704000..$705FFF):
//
//	$706000..$7062FF  zeroed by the patched init code on reset:
//	  $706000           capture tables: ancilla types ($40 bytes) and chrs ($100 bytes); see emitCaptureExtras
//	  $706140..$70620E  copy of Link's OAM entries and the local player's captured sparkles, dust, projectiles and
//	                    carried and thrown objects and sound effects, written by the trampoline every frame
//	  $706210..$7062E5  remote sprites table (written by o2) and state (written by the game code)
//	  $7062F0..$7062F3  the player's colors for Link's palette (written by o2); see linkcolor.go
//	  $7062F4..$7062FC  remote sound effects for the game to play (written by o2); see sfx.go
//	$706300..$706605  remote map16 changes staged for area loads (written by o2; both tables are emptied by the
//	                  init code); see map16stage.go
//	$707400..$7077FF  update routine buffer A
//	$707800..$707BFF  update routine buffer B
//	$707C00..$707C08  SRAM stub, copied from ROM by the patched init code; see emitSRAMStub:
//	  $707C00           update stub: RTS, which callUpdate's JSR points at after a reset
//	  $707C01           callUpdate: `JSR` to the update routine o2 wrote last, called by preMain
//	  $707C05           `JML map16Apply`, called by generated map16 update routines
//	$707FF0..$707FFF  patch signature, copied from ROM by the patched init code; see signature.go
const (
	// SRAM buffers that generated update routines are written to, alternately. callUpdate's JSR is only pointed
	// at one of these buffers after a routine was written there:
	updateBufAAddr = uint32(0x707400)
	updateBufBAddr = uint32(0x707800)
	// updateBufSize is the size of each buffer and the maximum size of a generated update routine. Routines larger
	// than 255 bytes are written in several chunks; callUpdate's JSR is only redirected after all of them:
	updateBufSize = 0x400

	// the SRAM stub: the only code in SRAM, since o2 rewrites part of it at runtime. The rest of the patch's code runs
	// from ROM (see patchROM), at addresses that depend on the ROM; the stub's JML lets o2's generated update routines
	// call map16Apply without knowing where it is:
	sramStubAddr = uint32(0x707C00)
	sramStubEnd  = uint32(0x707C09)
	// updateStubAddr is an RTS that callUpdate's JSR points at after a reset, until the first update routine is
	// written. o2 only rewrites the high byte of the JSR's operand (a single byte write cannot be torn), so the low
	// byte must be the same as the update buffers':
	updateStubAddr = sramStubAddr
	// callUpdateAddr is `JSR updateStubAddr; RTL`, which preMain calls with JSL to run the update routine:
	callUpdateAddr = uint32(0x707C01)
	// updateJSRAddr is the high byte of callUpdate's JSR operand, which o2 points at the update routine it wrote:
	updateJSRAddr = callUpdateAddr + 2
	// map16ApplyStubAddr is `JML map16Apply`, which generated map16 update routines call with JSL:
	map16ApplyStubAddr = uint32(0x707C05)
	// map16TableBank is the bank of the tables that map16Apply applies, in the update routine buffers:
	map16TableBank = updateBufAAddr & 0xFF0000

	// linkOAMCopyAddr is where the trampoline copies Link's OAM entries to every frame, after the module drew them
	// and before the next frame's ClearOAMBuffer sets all their Y coordinates to $F0. See linkOAMCopy* for the layout:
	linkOAMCopyAddr = uint32(0x706140)

	// sramZeroAddr..sramZeroEnd is zeroed by the patched init code on reset:
	sramZeroAddr = uint32(0x706000)
	sramZeroEnd  = uint32(0x706300)

	// the ROM area that versions of o2 before the free ROM tables (see freerom.go) patched their init code into:
	legacyPatchStart = uint32(0x1BB1D7)
	legacyPatchEnd   = uint32(0x1BB800)
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
	// captured OAM entries (see emitCaptureExtras): byte length (u16), then up to linkOAMExtrasMax entries of x, y,
	// chr, attributes, extended attributes (size << 1 | x bit 8), OAM index, and a spare byte:
	linkOAMCopyExtras    = linkOAMCopySize
	linkOAMExtrasMax     = 16
	linkOAMExtraSize     = 6
	linkOAMCopyExtrasEnd = linkOAMCopyExtras + 2 + linkOAMExtrasMax*linkOAMExtraSize + 1
	// the scroll offsets the copied entries were drawn with, as alttpo samples them: BG2 scroll minus the shake
	// offsets, x ($E2 - $011A) then y ($E8 - $011C). o2 sends these as its XOffs/YOffs so that receivers place the
	// sprites with the offsets of the same frame; offsets sampled from another frame shift the sprites by however far
	// the screen scrolled in between, e.g. during room transitions:
	linkOAMCopyScroll = linkOAMCopyExtrasEnd
	// the local sound effects the trampoline captured, and its count of remote sound effects played (see sfx.go):
	linkOAMCopySfx      = linkOAMCopyScroll + 4
	linkOAMCopyReadSize = linkOAMCopySfx + sfxLocalSize
)

// the copy must end before the remote sprites table:
const _ = remoteTableAddr - (linkOAMCopyAddr + linkOAMCopyReadSize)

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

	// the free ROM sections for this ROM (see freerom.go); only those that are still all $FF:
	layoutName, layout := freeROMLayout(p.rom)
	free := verifiedFreeROM(p.rom, layout)
	if !isROMSpanFree(p.rom, patchSignatureSpan) {
		return fmt.Errorf("ROM at $%06x, where o2 writes its patch signature, is in use", patchSignatureSpan.start)
	}

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
		// our init code appends the code it was patched over, so patching a ROM that o2 already patched would make it
		// call itself:
		target := uint32(code802F[1]) | uint32(code802F[2])<<8 | uint32(code802F[3])<<16
		if isFreeROM(target&^0x800000) || (target >= legacyPatchStart && target < legacyPatchEnd) {
			return fmt.Errorf("ROM is already patched by o2; select the original ROM")
		}
	}

	textBuf := &strings.Builder{}
	defer func() {
		log.Print(textBuf.String())
	}()

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

	// the routines and tables written to free ROM, measured with placeholder addresses and then placed by
	// allocateROM:
	fns := make(map[romFunction]uint32)
	fillRomFunctions(p.rom, fns)
	r := &patchROM{
		code802F:       code802F,
		gameModes:      gameModes,
		fnDrawMap16:    fns[fnOverworldDrawMap16Persist],
		linkGfxBankIns: linkGfxBankInstruction(p.rom),
	}
	stageAddr := fns[fnOverworldHandleOverlays]
	stageOK, stageChain := map16StageHookSite(p.rom, stageAddr)
	r.stageChain = stageChain
	if err = r.measure(); err != nil {
		return
	}
	if err = allocateROM(free, r.blocks()); err != nil {
		return fmt.Errorf("%s ROM: %w", layoutName, err)
	}
	if err = r.write(p.rom, textBuf); err != nil {
		return
	}

	// overwrite $00:802F with a JSL (or JML, see below) to our init code:
	var pcAddr uint32
	pcAddr, err = lorom.BusAddressToPak(0x00_802F)
	a := asm.NewEmitter(p.rom.Slice(pcAddr, uint32(len(expected802F))), true)
	a.SetBase(0x00802F)
	if code802F[0] == 0x5c {
		// NOTE: FastROM randomizer replaces JSL with JML to its init and then JMLs back to $8034.
		// doing a JML to our own init and appending the randomizer's copy of 802F code to our init
		// will still JML back to $808034 and everything should work as intended:
		a.JML(r.init.addr)
	} else {
		a.JSL(r.init.addr)
	}
	a.NOP()
	if err := a.Finalize(); err != nil {
		return err
	}
	a.WriteTextTo(textBuf)
	if a.Len() != len(expected802F) {
		return fmt.Errorf("assembler failed to produce exactly %d bytes to patch", len(expected802F))
	}

	// write the patch signature:
	pcAddr, err = lorom.BusAddressToPak(patchSignatureROMAddr)
	if err != nil {
		return
	}
	copy(p.rom.Slice(pcAddr, patchSignatureSize), signature[:])
	textBuf.WriteString(fmt.Sprintf("patch signature at $%06x: % x\n", patchSignatureROMAddr, signature[:]))
	p.rom.PatchVersion = patchVersionString()

	// overwrite the frame hook with a JSL to the trampoline:
	pcAddr, err = lorom.BusAddressToPak(frameHook)
	a = asm.NewEmitter(p.rom.Slice(pcAddr, 4), true)
	a.SetBase(frameHook)
	a.JSL(r.preMain.addr)
	if err := a.Finalize(); err != nil {
		return err
	}
	a.WriteTextTo(textBuf)

	// NMI hooks in NMI_DoUpdates that upload a remote player's head and body tiles; without them, remote players are
	// only drawn as blobs (see Game.remoteBodyOK):
	hooks := []struct {
		addr     uint32
		expected []byte
		target   uint32
	}{
		{nmiHookPatchAddr, nmiHookPatchExpected, r.nmiHookLate},
		{nmiEarlyPatchAddr, nmiEarlyPatchExpected, r.nmiHookEarly},
	}
	hooksOK := true
	for _, h := range hooks {
		if pcAddr, err = lorom.BusAddressToPak(h.addr); err != nil {
			return
		}
		if c := p.rom.Slice(pcAddr, uint32(len(h.expected))); !bytes.Equal(c, h.expected) {
			log.Printf("alttp: patch: unexpected code at $%06x: % x; remote players are drawn without Link's graphics\n", h.addr, c)
			hooksOK = false
		}
	}
	if hooksOK {
		for _, h := range hooks {
			pcAddr, _ = lorom.BusAddressToPak(h.addr)
			a = asm.NewEmitter(p.rom.Slice(pcAddr, uint32(len(h.expected))), true)
			a.SetBase(h.addr)
			a.JSL(h.target)
			for a.PC() < h.addr+uint32(len(h.expected)) {
				a.NOP()
			}
			if err := a.Finalize(); err != nil {
				return err
			}
			a.WriteTextTo(textBuf)
		}
	}

	// the hook that applies staged remote map16 changes to every overworld area load; without it, they are applied
	// once the area finished loading. Randomizers already replace the same instructions with a JSL to their own
	// routine; that JSL is pointed at the hook instead, which jumps to their routine when done:
	if !stageOK {
		pcAddr, _ = lorom.BusAddressToPak(stageAddr)
		log.Printf("alttp: patch: unexpected code at $%06x: % x; remote map16 changes appear after area loads\n", stageAddr, p.rom.Slice(pcAddr, uint32(len(map16StageHookExpected))))
	} else {
		pcAddr, _ = lorom.BusAddressToPak(stageAddr)
		n := uint32(len(map16StageHookExpected))
		if stageChain != 0 {
			n = 4
		}
		a = asm.NewEmitter(p.rom.Slice(pcAddr, n), true)
		a.SetBase(stageAddr)
		a.JSL(r.map16StageHook.addr)
		if stageChain == 0 {
			a.NOP()
		}
		if err := a.Finalize(); err != nil {
			return err
		}
		a.WriteTextTo(textBuf)
	}

	return nil
}

// emitSRAMStub emits the SRAM stub at sramStubAddr; map16Apply is the ROM address of emitMap16Apply's routine.
func emitSRAMStub(a *asm.Emitter, map16Apply uint32) {
	a.AssumeSEP(0x30)
	if a.Label("updateStub") != updateStubAddr {
		panic(fmt.Errorf("updateStubAddr expected to be $%06x but got $%06x", a.PC(), updateStubAddr))
	}
	a.RTS()
	if a.Label("callUpdate") != callUpdateAddr {
		panic(fmt.Errorf("callUpdateAddr expected to be $%06x but got $%06x", a.PC(), callUpdateAddr))
	}
	a.Comment("o2 points this JSR at the update routine it wrote last:")
	a.JSR_abs(uint16(updateStubAddr & 0xFFFF))
	a.RTL()
	if a.Label("map16ApplyStub") != map16ApplyStubAddr {
		panic(fmt.Errorf("map16ApplyStubAddr expected to be $%06x but got $%06x", a.PC(), map16ApplyStubAddr))
	}
	a.JML(map16Apply)
	if a.PC() != sramStubEnd {
		panic(fmt.Errorf("sramStubEnd expected to be $%06x but got $%06x", a.PC(), sramStubEnd))
	}
}

// emitPreMain emits the trampoline that the frame hook calls with JSL every frame instead of `JSL Module_MainRouting`
// (gameModes is its operand). It runs the update routine during safe modules, the module, and then copies Link's OAM
// entries and the captured sprites for o2 and draws the remote players with the routines at captureExtras and
// remoteDraw. Called with A and X 8-bit.
func emitPreMain(a *asm.Emitter, gameModes []byte, captureExtras, remoteDraw uint32) {
	a.AssumeSEP(0x30)
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
	a.JSL(callUpdateAddr)

	a.Label("syncExit")
	emitRemoteSave(a)

	// jsl to mainrouting that we patched over:
	a.JSL_lhb(gameModes[0], gameModes[1], gameModes[2])

	emitLinkOAMCopy(a)
	emitLinkColor(a)
	emitSfx(a)
	a.Comment("copy sparkles and dust for o2 to send:")
	a.JSL(captureExtras)
	a.Comment("draw remote players:")
	a.JSL(remoteDraw)
	a.RTL()
}

// patchROM is the code the patcher writes to free ROM, in blocks that allocateROM places separately:
type patchROM struct {
	// the code at $00:802F that the init code runs after its own:
	code802F []byte
	// the operand of the `JSL Module_MainRouting` at the frame hook:
	gameModes []byte
	// Overworld_DrawMap16_Persist; see emitMap16Apply:
	fnDrawMap16 uint32
	// the instruction that loads the bank of Link's graphics; see emitNMIHooks:
	linkGfxBankIns []byte
	// the listing of the SRAM stub is written here while the blocks are written:
	text io.Writer

	// init is the init code called from $00:802F; see emitInit:
	init romBlock
	// the SRAM stub that the init code copies to SRAM; see emitSRAMStub:
	sramStub romBlock
	// the trampoline called by the frame hook; see emitPreMain:
	preMain romBlock
	// called by the generated map16 update routines through the SRAM stub; see emitMap16Apply:
	map16Apply romBlock
	// remote players drawing routines, called by preMain; see emitRemoteDrawRoutines:
	remoteDraw     romBlock
	remoteDrawAddr uint32
	// the lists the init code builds the capture tables from; see emitInitCaptureTables:
	typeList romBlock
	chrList  romBlock
	// the capture routine called by preMain; see emitCaptureExtras:
	captureExtras romBlock
	// the NMI hooks; see emitNMIHooks:
	nmiHooks                  romBlock
	nmiHookLate, nmiHookEarly uint32
	// the hook in Overworld_HandleOverlaysAndBombDoors that applies staged map16 changes, and the routine it jumps to
	// when done if a randomizer had hooked the same instructions (0: it runs them itself); see emitMap16StageHook:
	map16StageHook romBlock
	stageChain     uint32
}

// patchRoutine emits a block's code at its address:
type patchRoutine struct {
	block *romBlock
	emit  func(a *asm.Emitter) error
}

func (r *patchROM) blocks() (b []*romBlock) {
	for _, rt := range r.routines() {
		b = append(b, rt.block)
	}
	return
}

// routines lists the blocks with the code to emit for each, given the addresses of the other blocks.
func (r *patchROM) routines() []patchRoutine {
	types, chrs := captureLists()
	return []patchRoutine{
		{&r.init, func(a *asm.Emitter) error {
			r.emitInit(a)
			return nil
		}},
		{&r.sramStub, func(a *asm.Emitter) error {
			s := asm.NewEmitter(make([]byte, sramStubEnd-sramStubAddr), r.text != nil)
			s.SetBase(sramStubAddr)
			emitSRAMStub(s, r.map16Apply.addr)
			if err := s.Finalize(); err != nil {
				return err
			}
			if r.text != nil {
				_ = s.WriteTextTo(r.text)
			}
			a.Comment(fmt.Sprintf("SRAM stub, copied to $%06x by the init code:", sramStubAddr))
			a.EmitBytes(s.Bytes())
			return nil
		}},
		{&r.preMain, func(a *asm.Emitter) error {
			emitPreMain(a, r.gameModes, r.captureExtras.addr, r.remoteDrawAddr)
			return nil
		}},
		{&r.map16Apply, func(a *asm.Emitter) error {
			return emitMap16Apply(a, r.fnDrawMap16)
		}},
		{&r.remoteDraw, func(a *asm.Emitter) error {
			r.remoteDrawAddr = emitRemoteDrawRoutines(a)
			return nil
		}},
		{&r.typeList, func(a *asm.Emitter) error {
			a.Comment("ancilla types whose sprites are captured:")
			a.EmitBytes(types[:])
			return nil
		}},
		{&r.chrList, func(a *asm.Emitter) error {
			a.Comment("chrs that are captured:")
			a.EmitBytes(chrs[:])
			return nil
		}},
		{&r.captureExtras, func(a *asm.Emitter) error {
			emitCaptureExtras(a)
			return nil
		}},
		{&r.nmiHooks, func(a *asm.Emitter) error {
			r.nmiHookLate, r.nmiHookEarly = emitNMIHooks(a, r.linkGfxBankIns)
			return nil
		}},
		{&r.map16StageHook, func(a *asm.Emitter) error {
			emitMap16StageHook(a, r.stageChain)
			return nil
		}},
	}
}

// emitInit emits the init code, called from $00:802F with JSL (or JML in FastROM randomizers) on reset. It copies the
// SRAM stub and the patch signature to SRAM, builds the capture tables and then runs the code it was patched over.
func (r *patchROM) emitInit(a *asm.Emitter) {
	a.AssumeSEP(0x30)
	a.Label("o2Init")
	a.PHP()
	a.PHB()
	a.REP(0x30)
	a.Comment("copy the SRAM stub to SRAM:")
	emitMVNCopy(a, r.sramStub.addr, sramStubAddr, sramStubEnd-sramStubAddr)
	a.Comment("copy the patch signature to SRAM:")
	emitMVNCopy(a, patchSignatureROMAddr, patchSignatureAddr, patchSignatureSize)
	emitInitCaptureTables(a, r.typeList.addr, r.chrList.addr)
	emitMap16StageInit(a)
	a.PLB()
	a.PLP()
	a.Comment("the code patched over at $00:802F:")
	a.EmitBytes(r.code802F)
	// NOTE: in FastROM randomizer, this will never be reached because its 802F code JMLs back to 8034:
	a.RTL()
}

// emitMVNCopy copies size bytes from src to dst with MVN. A, X and Y must be 16-bit. Leaves DB set to dst's bank.
func emitMVNCopy(a *asm.Emitter, src, dst, size uint32) {
	a.LDX_imm16_w(uint16(src))
	a.LDY_imm16_w(uint16(dst))
	a.LDA_imm16_w(uint16(size - 1))
	a.MVN(uint8(dst>>16), uint8(src>>16))
}

// measure assembles every block with placeholder addresses to find its size.
func (r *patchROM) measure() (err error) {
	r.init.name = "init"
	r.sramStub.name = "SRAM stub"
	r.preMain.name = "preMain"
	r.map16Apply.name = "map16Apply"
	r.remoteDraw.name = "remoteDraw"
	r.typeList.name = "capture type list"
	r.chrList.name = "capture chr list"
	r.captureExtras.name = "captureExtras"
	r.nmiHooks.name = "NMI hooks"
	r.map16StageHook.name = "map16 stage hook"

	for _, rt := range r.routines() {
		a := asm.NewEmitter(make([]byte, 0x1000), false)
		a.SetBase(0x008000)
		if err = rt.emit(a); err != nil {
			return fmt.Errorf("%s: %w", rt.block.name, err)
		}
		if err = a.Finalize(); err != nil {
			return fmt.Errorf("%s: %w", rt.block.name, err)
		}
		rt.block.size = uint32(a.Len())
	}
	return
}

// write assembles every block at the address allocateROM placed it at and writes it to the ROM. The blocks that are
// called by others are emitted first, so that their entry points are known.
func (r *patchROM) write(rom *snes.ROM, text io.Writer) (err error) {
	r.text = text
	defer func() { r.text = nil }()

	list := r.routines()
	slices.SortStableFunc(list, func(x, y patchRoutine) int {
		first := func(b *romBlock) int {
			if b == &r.remoteDraw {
				return 0
			}
			return 1
		}
		return first(x.block) - first(y.block)
	})
	for _, rt := range list {
		b := rt.block
		_, _ = fmt.Fprintf(text, "; %s: $%06x..$%06x ($%x bytes)\n", b.name, b.addr, b.addr+b.size-1, b.size)
		var pak uint32
		if pak, err = lorom.BusAddressToPak(b.addr); err != nil {
			return
		}
		a := asm.NewEmitter(rom.Slice(pak, b.size), true)
		a.SetBase(b.addr)
		if err = rt.emit(a); err != nil {
			return fmt.Errorf("%s: %w", b.name, err)
		}
		if err = a.Finalize(); err != nil {
			return fmt.Errorf("%s: %w", b.name, err)
		}
		if uint32(a.Len()) != b.size {
			return fmt.Errorf("%s: assembled $%x bytes at $%06x but measured $%x", b.name, a.Len(), b.addr, b.size)
		}
		_ = a.WriteTextTo(text)
	}
	return
}

// emitLinkOAMCopy copies Link's OAM entries to linkOAMCopyAddr for o2 to read. It runs right after the module drew
// them. Reading them from the OAM buffer in WRAM races with the next frame's ClearOAMBuffer, which sets the Y
// coordinates of all entries to $F0 and made Link disappear for alttpo players. o2 may still read the copy while it
// is being written, which at worst mixes two consecutive frames' entries.
func emitLinkOAMCopy(a *asm.Emitter) {
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
