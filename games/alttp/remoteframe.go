package alttp

import (
	"fmt"

	"github.com/alttpo/snes/asm"
)

// remoteFrame
//
// The trampoline calls one routine (emitRemoteFrame) after the module drew its sprites, which captures the local
// player's sparkles, dust and projectiles for o2 to send (see emitCaptureExtras' comment in remotesprites.go for what
// is captured) and draws the remote players into the OAM buffer (see the comment at the top of remotesprites.go). It
// runs every frame, so it is written for speed:
//
//   - it runs from FastROM (see fastROMBank);
//   - one unrolled pass over the 128 OAM entries finds both the VRAM slots that the game's sprites show and the
//     entries to capture; an unused entry (Y = $F0) costs a compare with X and a branch, and a used one a lookup of
//     its chr in captureChrTable masked with rcMask, so that the entry handler only runs for the entries it acts on;
//   - the VRAM slots are handed out to the parts with a table lookup (remoteAllocTable);
//   - o2 does what doesn't depend on the frame: it sorts the remote sprites table into lists of one kind of sprite
//     each (see rtExtrasEnd), which are drawn with a loop for that kind (emitDrawLoop), and writes each entry's
//     extended attributes but for x bit 8. What o2 can't do is the rest: the scroll offsets change every frame and o2
//     reads them frames late, and the unused OAM entries and the free VRAM slots are only known once the module drew
//     its sprites;
//   - each drawing loop keeps an unused OAM entry at hand (Y) and writes x and y as they are computed, so a sprite
//     costs about 150 cycles;
//   - its scratch is the direct page $00..$0F, which the game only uses as scratch within a routine;
//   - the tables it looks up are in ROM, in the same bank as the code, and read with absolute addressing while DB is
//     the program bank, which also reaches the OAM buffer ($0800..$0A9F) and the IO registers. The remote sprites table
//     in SRAM is read with long addressing while drawing, and with absolute addressing while DB = $70 otherwise.

// direct page scratch of remoteFrame:
const (
	// scan: the bits of captureChrTable to act on (rcMask, u8: the capture bits while capturing, and scanChrSlot while
	// finding the VRAM slots in use), whether to draw (rcDraw), whether to find the VRAM slots in use (rcSlots), the
	// slots found in use (rcUsed), the bits of the entry being looked at (rcHit), its chr (rcChr, u16):
	rcMask = 0x00
	// non-zero once the dying enemies are listed this frame (see emitDyingEnemiesList):
	rcDying = 0x01
	rcDraw  = 0x02
	rcSlots = 0x03
	rcUsed  = 0x04
	rcHit   = 0x05
	rcChr   = 0x06
	// copying a captured entry: its OAM buffer offset and index (u16):
	rcOff   = 0x08
	rcIndex = 0x0A
	// the byte length of the entries captured so far (u16), stored to the copy's length once the scan is done (a read
	// of the copy during the scan saw no entries when the length was cleared first and counted up):
	rcExLen = 0x0C
	// handing out the VRAM slots: scratch (u8):
	rcAllocTmp = 0x06
	// drawing (u16): the scroll offsets, the end of the list being drawn, added to the chr of blobs, the extended
	// attributes of the entry being drawn, its table offset, and the chr of its part's VRAM slot:
	rcScrX = 0x00
	rcScrY = 0x02
	rcEnd  = 0x04
	rcAnim = 0x06
	rcExt  = 0x08
	rcTbl  = 0x0A
	rcSlot = 0x0C
	// bits of the parts drawn (u16):
	rcDrawn = 0x0E
	// listing the rows to upload (u16): a part's VRAM address, its WRAM bit, its flags and its slot's bit:
	rcUpVRAM  = 0x00
	rcUpWRAM  = 0x02
	rcUpFlags = 0x04
	rcUpBit   = 0x06
)

// raw emits an instruction that asm.Emitter has no method for, with its text as a comment.
func raw(a *asm.Emitter, text string, b ...byte) {
	a.Comment(text)
	a.EmitBytes(b)
}

func rawAbs(a *asm.Emitter, text string, op byte, addr uint16) {
	raw(a, fmt.Sprintf(text, addr), op, uint8(addr), uint8(addr>>8))
}

func rawDP(a *asm.Emitter, text string, op byte, dp uint8) {
	raw(a, fmt.Sprintf(text, dp), op, dp)
}

func tsbDP(a *asm.Emitter, dp uint8) { rawDP(a, "tsb.b $%02x", 0x04, dp) }
func andDP(a *asm.Emitter, dp uint8) { rawDP(a, "and.b $%02x", 0x25, dp) }
func oraDP(a *asm.Emitter, dp uint8) { rawDP(a, "ora.b $%02x", 0x05, dp) }
func adcDP(a *asm.Emitter, dp uint8) { rawDP(a, "adc.b $%02x", 0x65, dp) }
func ldyDP(a *asm.Emitter, dp uint8) { rawDP(a, "ldy.b $%02x", 0xA4, dp) }
func ldyAbsX(a *asm.Emitter, addr uint16) {
	rawAbs(a, "ldy.w $%04x,x", 0xBC, addr)
}
func cpyAbs(a *asm.Emitter, addr uint16) { rawAbs(a, "cpy.w $%04x", 0xCC, addr) }
func andAbsX(a *asm.Emitter, addr uint16) {
	rawAbs(a, "and.w $%04x,x", 0x3D, addr)
}
func decA(a *asm.Emitter)            { raw(a, "dec", 0x3A) }
func stxDP(a *asm.Emitter, dp uint8) { rawDP(a, "stx.b $%02x", 0x86, dp) }
func trbDP(a *asm.Emitter, dp uint8) { rawDP(a, "trb.b $%02x", 0x14, dp) }
func cpxDP(a *asm.Emitter, dp uint8) { rawDP(a, "cpx.b $%02x", 0xE4, dp) }
func ldxDP(a *asm.Emitter, dp uint8) { rawDP(a, "ldx.b $%02x", 0xA6, dp) }
func oraLongX(a *asm.Emitter, addr uint32) {
	raw(a, fmt.Sprintf("ora.l $%06x,x", addr), 0x1F, uint8(addr), uint8(addr>>8), uint8(addr>>16))
}
func staAbsY(a *asm.Emitter, addr uint16) { a.STA_abs_y(addr) }

// remoteAllocTable returns, for each set of VRAM slots in use (bits of remoteSlots), the first chrs of the first
// remotePartMax free slots in order ($FF for none), and the number of free slots.
func remoteAllocTable() (slots [0x80 * remotePartMax]byte, free [0x80]byte) {
	for used := 0; used < 0x80; used++ {
		n := 0
		for i := 0; i < remotePartMax; i++ {
			slots[used*remotePartMax+i] = 0xFF
		}
		for j, chr := range remoteSlots {
			if used&(1<<j) != 0 {
				continue
			}
			if n < remotePartMax {
				slots[used*remotePartMax+n] = chr
			}
			n++
		}
		free[used] = uint8(n)
	}
	return
}

// scanChrSlot is the bit of captureChrTable set for the chrs $00..$3F, which show the VRAM slots (remoteSlots):
const scanChrSlot = 0x80

// captureTables returns the capture tables: per ancilla type $00..$3F the bits of captureChrTable to capture while one
// exists (1, or 3 for captureShadowCasters), and per chr $00..$FF its bits (1 for captureChrs, 2 for
// captureShadowChrs, or'ed with captureChrEnemyFX for enemyDeathChrs and with scanChrSlot for $00..$3F).
func captureTables() (types [0x40]byte, chrs [0x100]byte) {
	for _, t := range captureAncillae {
		types[t] = 1
	}
	for _, t := range captureShadowCasters {
		types[t] = 3
	}
	for _, c := range captureChrs {
		chrs[c] = 1
	}
	for _, c := range captureShadowChrs {
		chrs[c] = 2
	}
	for _, c := range enemyDeathChrs {
		chrs[c] |= captureChrEnemyFX
	}
	for c := 0; c < 0x40; c++ {
		chrs[c] |= scanChrSlot
	}
	return
}

// remoteFrameTables are the addresses of the ROM tables remoteFrame reads, in its own bank:
type remoteFrameTables struct {
	captureTypes, captureChrs    uint32
	chrSlotsSmall, chrSlotsLarge uint32
	allocSlots, allocFree        uint32
	partBits                     uint32
	followerSlotBits             uint32
}

// emitRemoteFrameTables emits the tables of remoteFrameTables in this order and returns their addresses.
func emitRemoteFrameTables(a *asm.Emitter) (t remoteFrameTables) {
	types, chrs := captureTables()
	small, large := remoteSlotChrTables()
	slots, free := remoteAllocTable()
	t.captureTypes = a.Label("captureTypeTable")
	a.EmitBytes(types[:])
	t.captureChrs = a.Label("captureChrTable")
	a.EmitBytes(chrs[:])
	t.chrSlotsSmall = a.Label("chrSlotsSmall")
	a.EmitBytes(small[:])
	t.chrSlotsLarge = a.Label("chrSlotsLarge")
	a.EmitBytes(large[:])
	t.allocSlots = a.Label("allocSlots")
	a.EmitBytes(slots[:])
	t.allocFree = a.Label("allocFree")
	a.EmitBytes(free[:])
	t.partBits = a.Label("partBits")
	for i := 0; i < remotePartMax; i++ {
		a.EmitBytes([]byte{1 << i, 0})
	}
	fol := followerSlotBits()
	t.followerSlotBits = a.Label("followerSlotBits")
	a.EmitBytes(fol[:])
	return
}

// drawKind is the kind of sprites a list of the remote sprites table holds, which remoteFrame draws with a loop of
// their own:
type drawKind int

const (
	// drawn as they are:
	drawPlain drawKind = iota
	// blobs, whose chr alternates every 8 frames (rcAnim is added):
	drawBlob
	// chrs relative to their part's VRAM slot; a sprite of a part without a slot is not drawn:
	drawSlotted
)

// emitFindUnused emits the search for the next unused OAM entry (Y = $F0) below the OAM buffer offset Y, 8 entries at
// a time while there are 8 below, with A 8-bit and X and Y 16-bit. It continues after the emitted code with Y = the
// entry found, or returns with A 16-bit and carry clear if there is none.
func emitFindUnused(a *asm.Emitter, p string) {
	a.Label(p + "_8")
	a.CPY_imm16_w(0x0020)
	a.BCC(p + "_1")
	for i := 1; i <= 8; i++ {
		a.LDA_abs_y(uint16(0x0801 - 4*i))
		a.CMP_imm8_b(0xF0)
		a.BEQ(fmt.Sprintf("%s_found%d", p, i))
	}
	a.JMP_abs(p + "_next8")
	for i := 2; i >= 1; i-- {
		a.Label(fmt.Sprintf("%s_found%d", p, i))
		a.DEY()
		a.DEY()
		a.DEY()
		a.DEY()
	}
	a.JMP_abs(p + "_found")
	a.Comment("one at a time:")
	a.Label(p + "_1")
	a.DEY()
	a.DEY()
	a.DEY()
	a.DEY()
	a.BMI(p + "_full")
	a.LDA_abs_y(0x0801)
	a.CMP_imm8_b(0xF0)
	a.BNE(p + "_1")
	a.JMP_abs(p + "_found")
	a.Label(p + "_full")
	a.REP(0x20)
	a.CLC()
	a.RTS()
	a.AssumeSEP(0x20)
	for i := 3; i <= 8; i++ {
		a.Label(fmt.Sprintf("%s_found%d", p, i))
		a.REP(0x20)
		a.TYA()
		a.CLC()
		a.ADC_imm16_w(uint16(-4 * i))
		a.TAY()
		a.SEP(0x20)
		a.JMP_abs(p + "_found")
	}
	a.Label(p + "_next8")
	a.REP(0x20)
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(0xFFE0) // -$20
	a.TAY()
	a.SEP(0x20)
	a.JMP_abs(p + "_8")
	a.Label(p + "_found")
}

// emitDrawLoop emits the loop that draws a list of the remote sprites table of the kind (see the drawing loops in
// emitRemoteFrame for how they are called), and returns its address.
func emitDrawLoop(a *asm.Emitter, kind drawKind, t remoteFrameTables) uint32 {
	p := []string{"rf_plain", "rf_blob", "rf_slotted"}[kind]
	e := func(o uint32) uint32 { return remoteTableAddr + rtEntries + o }
	a.AssumeREP(0x30)
	start := a.Label(p)
	a.STA_dp(rcEnd)
	a.BRA(p + "_loop")
	a.Comment("visible in x but not in y: the entry is unused again:")
	a.Label(p + "_cull_y")
	a.SEP(0x20)
	a.LDA_imm8_b(0xF0)
	staAbsY(a, 0x0801)
	a.REP(0x20)
	a.Label(p + "_cull")
	if kind == drawSlotted {
		a.PLA()
	}
	a.Label(p + "_next")
	a.TXA()
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.TAX()
	a.Label(p + "_loop")
	cpxDP(a, rcEnd)
	a.BCC(p + "_entry")
	a.RTS()
	a.Label(p + "_entry")
	if kind == drawSlotted {
		a.Comment("part = chr bits 6-7; a sword or shield without a VRAM slot is not drawn; its bit (partBits) is pushed:")
		a.LDA_long_x(e(4))
		a.AND_imm16_w(0x00C0)
		a.ASL()
		a.ASL()
		a.XBA()
		stxDP(a, rcTbl)
		a.TAX()
		a.LDA_long_x(remoteStateAddr + rsPartSlot)
		a.AND_imm16_w(0x00FF)
		a.CMP_imm16_w(0x00FF)
		a.BNE(p + "_slot")
		ldxDP(a, rcTbl)
		a.BRA(p + "_next")
		a.Label(p + "_slot")
		a.STA_dp(rcSlot)
		a.TXA()
		a.ASL()
		a.TAX()
		a.LDA_abs_x(uint16(t.partBits))
		a.PHA()
		ldxDP(a, rcTbl)
	}
	a.Comment("screen x = world x - scroll x, visible if -16 <= x < 256; x and x bit 8 (still in y) into the entry:")
	a.LDA_long_x(e(0))
	a.SEC()
	a.SBC_dp(rcScrX)
	a.CMP_imm16_w(0x0100)
	a.BCC(p + "_x")
	a.CMP_imm16_w(0xFFF0)
	a.BCC(p + "_cull")
	a.Label(p + "_x")
	staAbsY(a, 0x0800)
	a.Comment("the extended attributes: x bit 8 and the size:")
	a.XBA()
	a.AND_imm16_w(0x0001)
	oraLongX(a, e(6))
	a.STA_dp(rcExt)
	a.Comment("screen y = world y - scroll y, visible if -16 <= y < 224 (its high byte goes into the chr, written next):")
	a.LDA_long_x(e(2))
	a.SEC()
	a.SBC_dp(rcScrY)
	a.CMP_imm16_w(0x00E0)
	a.BCC(p + "_y")
	a.CMP_imm16_w(0xFFF0)
	a.BCC(p + "_cull_y")
	a.Label(p + "_y")
	staAbsY(a, 0x0801)
	a.Comment("chr and attributes:")
	a.LDA_long_x(e(4))
	switch kind {
	case drawBlob:
		a.CLC()
		adcDP(a, rcAnim)
	case drawSlotted:
		a.AND_imm16_w(0xFF11)
		a.CLC()
		adcDP(a, rcSlot)
	}
	staAbsY(a, 0x0802)
	if kind == drawSlotted {
		a.PLA()
		tsbDP(a, rcDrawn)
	}
	a.Comment("the extended attributes, at the entry's index:")
	stxDP(a, rcTbl)
	a.TYA()
	a.LSR()
	a.LSR()
	a.TAX()
	a.SEP(0x20)
	a.LDA_dp(rcExt)
	a.STA_abs_x(0x0A20)
	a.Comment("the next unused entry:")
	emitFindUnused(a, p+"_find")
	a.REP(0x20)
	ldxDP(a, rcTbl)
	a.JMP_abs(p + "_next")
	return start
}

// emitRemoteFrame emits the routine the trampoline calls with JSL after the module ran, and returns its entry point
// (after its subroutines); t are the tables emitted by emitRemoteFrameTables in the same bank, dyingList and nearDying
// the routines of emitDyingEnemiesList and emitNearDyingEnemySub. Called with any register sizes; preserves P, DB and
// D.
//
// Cycles (cycles_test.go), with 3 remote players in the room: budget 6,000, measured 5,664 (about 13% of a frame)
// with the OAM buffer otherwise empty; budget 12,500, measured 11,607 (about 26%) with the game's sprites filling all
// but 16 entries, an eighth of them showing VRAM slots and an eighth captured. It is still the most expensive
// per-frame routine of the patch. Most of it is the scan (7 cycles per unused entry, about 20 per used one), drawing
// the sprites (about 150 each), and with a full OAM buffer, the search for unused entries.
func emitRemoteFrame(a *asm.Emitter, t remoteFrameTables, dyingList, nearDying uint32) (entry uint32) {
	lo := func(addr uint32) uint16 { return uint16(addr) }
	st := func(o uint32) uint16 { return uint16((remoteStateAddr + o) & 0xFFFF) }
	tb := func(o uint32) uint16 { return uint16((remoteTableAddr + o) & 0xFFFF) }
	ex := linkOAMCopyAddr + linkOAMCopyExtras

	// --- the OAM entry handlers, called by the scan with JSR for a used entry whose chr's bits in captureChrTable and
	// rcMask have any in common: rf_entry_lo for the entries $00..$3F and rf_entry_hi for $40..$7F, with A = those bits,
	// X = the entry's offset from $0800 or $0900, Y = its chr; A, X and Y 8-bit, DB = program bank. They return with
	// A, X and Y 8-bit. ---
	a.AssumeSEP(0x30)
	var handlers [2]uint32
	for h := uint16(0); h < 2; h++ {
		p := []string{"rf_entry_lo", "rf_entry_hi"}[h]
		handlers[h] = a.Label(p)
		a.STA_dp(rcHit)
		a.Comment("name table 1: neither a VRAM slot nor captured:")
		a.LDA_abs_x(0x0803 + h*0x100)
		a.LSR()
		a.BCS(p + "_ret")
		a.Comment("VRAM slots shown by chrs $00..$3F, 8x8 or 16x16 (X = the entry's index - $40 * half meanwhile):")
		a.LDA_dp(rcHit)
		a.BPL(p + "_capture")
		a.TXA()
		a.LSR()
		a.LSR()
		a.TAX()
		a.LDA_abs_x(0x0A20 + h*0x40)
		a.AND_imm8_b(0x02)
		a.BEQ(p + "_small")
		a.LDA_abs_y(lo(t.chrSlotsLarge))
		a.BRA(p + "_slots")
		a.Label(p + "_small")
		a.LDA_abs_y(lo(t.chrSlotsSmall))
		a.Label(p + "_slots")
		tsbDP(a, rcUsed)
		a.TXA()
		a.ASL()
		a.ASL()
		a.TAX()
		a.Label(p + "_capture")
		a.LDA_dp(rcHit)
		a.AND_imm8_b(^uint8(scanChrSlot))
		a.BNE(p + "_copy")
		a.Label(p + "_ret")
		a.RTS()
		a.Comment("a captured chr: X = the OAM buffer offset, Y = the entry index, 16-bit:")
		a.Label(p + "_copy")
		a.STY_dp(rcChr)
		a.REP(0x30)
		a.TXA()
		if h != 0 {
			a.CLC()
			a.ADC_imm16_w(0x0100)
		}
		a.TAX()
		a.LSR()
		a.LSR()
		a.TAY()
		a.JMP_abs("rf_entry_copy")
		a.AssumeSEP(0x30)
	}

	a.AssumeREP(0x30)
	a.Comment("not one of Link's own entries, which are copied already:")
	a.Label("rf_entry_copy")
	stxDP(a, rcOff)
	a.STY_dp(rcIndex)
	a.TXA()
	a.SEC()
	a.SBC_abs(0x0352)
	a.CMP_imm16_w(linkOAMCount * 4)
	a.BCS("rf_copy_not_link")
	a.JMP_abs("rf_copy_done")
	a.Label("rf_copy_not_link")
	a.Comment("not an enemy's death poof or explosion near a dying enemy:")
	ldyDP(a, rcChr)
	a.LDA_abs_y(lo(t.captureChrs))
	a.AND_imm16_w(captureChrEnemyFX)
	a.BEQ("rf_copy")
	a.Comment("the screen positions of the enemies drawing their death, listed once per frame on the first one seen:")
	a.LDA_dp(rcDying)
	a.AND_imm16_w(0x00FF)
	a.BNE("rf_copy_listed")
	a.JSL(dyingList)
	a.SEP(0x20)
	a.INC_dp(rcDying)
	a.REP(0x20)
	a.Label("rf_copy_listed")
	ldyDP(a, rcOff)
	a.JSL(nearDying)
	a.BCC("rf_copy")
	a.JMP_abs("rf_copy_done")
	a.Label("rf_copy")
	a.Comment("copy x, y, chr, attributes, extended attributes and the OAM index; X = the extras' offset:")
	ldxDP(a, rcExLen)
	ldyDP(a, rcOff)
	a.LDA_abs_y(0x0800)
	a.STA_long_x(ex + 2 + 0)
	a.LDA_abs_y(0x0802)
	a.STA_long_x(ex + 2 + 2)
	ldyDP(a, rcIndex)
	a.SEP(0x20)
	a.LDA_abs_y(0x0A20)
	a.AND_imm8_b(0x03)
	a.STA_long_x(ex + 2 + 4)
	a.LDA_dp(rcIndex)
	a.STA_long_x(ex + 2 + 5)
	a.REP(0x20)
	a.TXA()
	a.CLC()
	a.ADC_imm16_w(linkOAMExtraSize)
	a.STA_dp(rcExLen)
	a.CMP_imm16_w(linkOAMExtrasMax * linkOAMExtraSize)
	a.BCC("rf_copy_done")
	a.Comment("full: capture nothing more:")
	a.SEP(0x20)
	a.LDA_imm8_b(^uint8(scanChrSlot))
	trbDP(a, rcMask)
	a.Label("rf_copy_done")
	a.SEP(0x30)
	a.RTS()

	// --- the drawing loops, called with JSR: X = the offset of the first entry to draw, A = the offset of the end of
	// the list, Y = the OAM buffer offset of an unused entry to draw the first visible one into; A, X and Y 16-bit,
	// DB = program bank. They return with carry set and Y = an unused entry, or carry clear once the OAM buffer is
	// full. ---
	a.AssumeREP(0x30)
	findFirst := a.Label("rf_find_first")
	a.SEP(0x20)
	emitFindUnused(a, "rf_ff")
	a.REP(0x20)
	a.SEC()
	a.RTS()
	loops := map[drawKind]uint32{}
	for _, k := range []drawKind{drawPlain, drawBlob, drawSlotted} {
		loops[k] = emitDrawLoop(a, k, t)
	}

	// --- the routine ---
	entry = a.Label("rf_start")
	a.PHP()
	a.PHB()
	a.PHD()
	a.PEA(0x0000)
	a.PLD()
	a.PHK()
	a.PLB()
	a.REP(0x30)
	a.Comment("capture: the scroll offsets Link's copied OAM entries were drawn with (linkOAMCopyScroll), no extras yet:")
	a.LDA_dp(0xE2)
	a.SEC()
	a.SBC_abs(0x011A)
	a.STA_long(linkOAMCopyAddr + linkOAMCopyScroll)
	a.LDA_dp(0xE8)
	a.SEC()
	a.SBC_abs(0x011C)
	a.STA_long(linkOAMCopyAddr + linkOAMCopyScroll + 2)
	a.LDA_imm16_w(0x0000)
	a.STA_dp(rcExLen)
	a.STZ_dp(rcMask)
	a.STZ_dp(rcDraw)
	a.STZ_dp(rcUsed)
	a.STZ_dp(rcChr)
	a.Comment("nothing for the NMI hooks to upload unless parts are drawn, and no follower slots kept:")
	a.STA_long(remoteStateAddr + rsNMILen)
	a.STA_long(remoteStateAddr + rsSkipSlots)
	a.SEP(0x30)

	a.Comment("capture only while a remote player is in the same location:")
	a.LDA_long(remoteTableAddr + rtCaptureTTL)
	a.BEQ("rf_capture_done")
	decA(a)
	a.STA_long(remoteTableAddr + rtCaptureTTL)
	a.Comment("which chrs to capture: 1 while a captured ancilla exists, 2 (shadows) while one that casts a shadow does:")
	a.LDX_imm8_b(0x09)
	a.Label("rf_ancilla")
	ldyAbsX(a, 0x0C4A)
	if types, _ := captureTables(); types[0] == 0 {
		a.BEQ("rf_ancilla_next")
	}
	a.CPY_imm8_b(0x40)
	a.BCS("rf_ancilla_next")
	a.LDA_abs_y(lo(t.captureTypes))
	tsbDP(a, rcMask)
	a.Label("rf_ancilla_next")
	a.DEX()
	a.BPL("rf_ancilla")
	a.Comment("a carried sprite or thrown scenery (both, with shadows):")
	a.LDX_imm8_b(0x0F)
	a.Label("rf_csprite")
	a.LDA_abs_x(0x0DD0)
	a.BEQ("rf_csprite_next")
	a.CMP_imm8_b(0x0A)
	a.BEQ("rf_csprite_found")
	a.LDA_abs_x(0x0E20)
	a.CMP_imm8_b(0xEC)
	a.BNE("rf_csprite_next")
	a.Label("rf_csprite_found")
	a.LDA_imm8_b(0x03)
	tsbDP(a, rcMask)
	a.BRA("rf_csprites_done")
	a.Label("rf_csprite_next")
	a.DEX()
	a.BPL("rf_csprite")
	a.Label("rf_csprites_done")
	a.LDA_dp(rcMask)
	a.BNE("rf_capture_on")
	a.Comment("thrown object debris:")
	a.LDA_abs(0x0FB4)
	a.BEQ("rf_capture_done")
	a.LDX_imm8_b(0x1D)
	a.Label("rf_garnish")
	a.LDA_long_x(0x7FF800)
	a.CMP_imm8_b(0x16)
	a.BEQ("rf_garnish_found")
	a.DEX()
	a.BPL("rf_garnish")
	a.BRA("rf_capture_done")
	a.Label("rf_garnish_found")
	a.LDA_imm8_b(0x01)
	tsbDP(a, rcMask)
	a.Label("rf_capture_on")
	a.Label("rf_capture_done")

	a.Comment("draw only while a table is current, the game draws the underworld or overworld sprites, and in the room or")
	a.Comment("area the table was made for:")
	a.LDA_long(remoteTableAddr + rtTTL)
	a.BNE("rf_ttl_ok")
	a.JMP_abs("rf_scan_check")
	a.Label("rf_ttl_ok")
	decA(a)
	a.STA_long(remoteTableAddr + rtTTL)
	a.LDA_dp(0x10)
	for _, m := range []uint8{0x07, 0x09, 0x0B, 0x0F, 0x10, 0x13, 0x16} {
		a.CMP_imm8_b(m)
		a.BEQ("rf_module_ok")
	}
	a.CMP_imm8_b(0x0E)
	a.BNE("rf_no_draw")
	a.Comment("dialogs and menus (Module0E_Interface): not on the dungeon or overworld map or the flute menu:")
	a.LDA_dp(0x1B)
	a.BEQ("rf_interface_ow")
	a.LDA_dp(0x11)
	a.CMP_imm8_b(0x03)
	a.BNE("rf_module_ok")
	a.LDA_abs(0x0200)
	a.BEQ("rf_module_ok")
	a.CMP_imm8_b(0x07)
	a.BEQ("rf_module_ok")
	a.BRA("rf_no_draw")
	a.Label("rf_interface_ow")
	a.LDA_dp(0x11)
	a.CMP_imm8_b(0x07)
	a.BEQ("rf_interface_map")
	a.CMP_imm8_b(0x0A)
	a.BNE("rf_module_ok")
	a.Label("rf_interface_map")
	a.LDA_abs(0x0200)
	a.BEQ("rf_module_ok")
	a.Label("rf_no_draw")
	a.JMP_abs("rf_scan_check")
	a.Label("rf_module_ok")
	a.LDA_dp(0x1B)
	a.CMP_long(remoteTableAddr + rtIndoors)
	a.BNE("rf_no_draw")
	a.LDA_dp(0x1B)
	a.REP(0x30)
	a.BEQ("rf_overworld")
	a.LDA_dp(0xA0)
	a.BRA("rf_location")
	a.Label("rf_overworld")
	a.LDA_dp(0x8A)
	a.Label("rf_location")
	a.CMP_long(remoteTableAddr + rtLocation)
	a.SEP(0x30)
	a.BNE("rf_no_draw")
	a.INC_dp(rcDraw)
	a.Comment("no part has a VRAM slot yet; find the slots in use if there are parts:")
	a.REP(0x20)
	a.LDA_imm16_w(0xFFFF)
	a.STA_long(remoteStateAddr + rsPartSlot)
	a.STA_long(remoteStateAddr + rsPartSlot + 2)
	a.LDA_imm16_w(0x0000)
	a.STA_long(remoteStateAddr + rsBodyOK)
	a.SEP(0x20)
	a.LDA_long(remoteTableAddr + rtParts)
	a.STA_dp(rcSlots)
	a.BEQ("rf_scan_check")
	a.LDA_imm8_b(scanChrSlot)
	tsbDP(a, rcMask)

	a.Label("rf_scan_check")
	a.SEP(0x30)
	a.LDA_dp(rcMask)
	a.BNE("rf_scan")
	a.JMP_abs("rf_scan_done")

	a.Label("rf_scan")
	a.Comment("one pass over the OAM buffer, from the last entry down (the order captured entries are copied in), calling")
	a.Comment("the handler for the used entries whose chr has bits of rcMask; X = $F0 to compare y with:")
	a.LDX_imm8_b(0xF0)
	for k := 0x7F; k >= 0; k-- {
		next := fmt.Sprintf("rf_scan_%02x", k)
		rawAbs(a, "cpx.w $%04x", 0xEC, uint16(0x0801+k*4))
		a.BEQ(next)
		a.LDY_abs(uint16(0x0802 + k*4))
		a.LDA_abs_y(lo(t.captureChrs))
		andDP(a, rcMask)
		a.BEQ(next)
		a.LDX_imm8_b(uint8(k&0x3F) * 4)
		a.JSR_abs(uint16(handlers[k>>6]))
		a.LDX_imm8_b(0xF0)
		a.Label(next)
	}

	a.Label("rf_scan_done")
	a.Comment("the entries captured this frame, all at once:")
	a.REP(0x20)
	a.LDA_dp(rcExLen)
	a.STA_long(ex)
	a.SEP(0x20)
	a.LDA_dp(rcDraw)
	a.BNE("rf_drawing")
	a.JMP_abs("rf_exit")
	a.Label("rf_drawing")
	a.LDA_dp(rcSlots)
	a.BNE("rf_alloc")
	a.JMP_abs("rf_alloc_done")
	a.Label("rf_alloc")
	a.Comment("the slots in use: those the sprites show, and Link's own while core updates are disabled (the game")
	a.Comment("doesn't upload his chrs then, and he may show them again before it does):")
	a.LDA_abs(0x0710)
	a.BEQ("rf_used")
	a.LDA_imm8_b(remoteLinkSlots)
	tsbDP(a, rcUsed)
	a.Label("rf_used")
	a.REP(0x30)
	a.LDA_dp(rcUsed)
	a.AND_imm16_w(0x00FF)
	a.STA_long(remoteStateAddr + rsUsed)
	a.Comment("hand out the free slots to the parts in order: X = used * 4, Y = used:")
	a.TAY()
	a.ASL()
	a.ASL()
	a.TAX()
	a.SEP(0x20)
	for i := 0; i < remotePartMax; i++ {
		a.LDA_long(remoteTableAddr + rtParts)
		a.CMP_imm8_b(uint8(i + 1))
		a.BCC("rf_alloc_parts_done")
		a.LDA_abs_x(lo(t.allocSlots) + uint16(i))
		a.STA_long(remoteStateAddr + rsPartSlot + uint32(i))
	}
	a.Label("rf_alloc_parts_done")
	a.Comment("the follower slots the parts got, whose game uploads the NMI follower hook skips:")
	a.STZ_dp(rcAllocTmp)
	for i := 0; i < remotePartMax; i++ {
		a.LDA_long(remoteStateAddr + rsPartSlot + uint32(i))
		a.CMP_imm8_b(0xFF)
		a.BEQ(fmt.Sprintf("rf_skip_%d", i))
		a.REP(0x20)
		a.AND_imm16_w(0x003F)
		a.TAX()
		a.SEP(0x20)
		a.LDA_abs_x(lo(t.followerSlotBits))
		tsbDP(a, rcAllocTmp)
		a.Label(fmt.Sprintf("rf_skip_%d", i))
	}
	a.LDA_dp(rcAllocTmp)
	a.STA_long(remoteStateAddr + rsSkipSlots)
	a.Comment("the body is drawn if the required parts all got a slot: min(parts, free) >= required > 0:")
	a.LDA_long(remoteTableAddr + rtRequired)
	a.BEQ("rf_alloc_done")
	a.STA_dp(rcAllocTmp)
	a.LDA_long(remoteTableAddr + rtParts)
	rawAbs(a, "cmp.w $%04x,y", 0xD9, lo(t.allocFree))
	a.BCC("rf_alloc_min")
	a.LDA_abs_y(lo(t.allocFree))
	a.Label("rf_alloc_min")
	a.CMP_dp(rcAllocTmp)
	a.BCC("rf_alloc_done")
	a.LDA_imm8_b(0x01)
	a.STA_long(remoteStateAddr + rsBodyOK)
	a.Label("rf_alloc_done")

	a.REP(0x30)
	a.Comment("local scroll offsets; the blob chr alternates every 8 frames:")
	a.LDA_dp(0xE2)
	a.SEC()
	a.SBC_abs(0x011A)
	a.STA_dp(rcScrX)
	a.LDA_dp(0xE8)
	a.SEC()
	a.SBC_abs(0x011C)
	a.STA_dp(rcScrY)
	a.LDA_dp(0x1A)
	a.AND_imm16_w(0x0008)
	a.LSR()
	a.LSR()
	a.STA_dp(rcAnim)
	a.STZ_dp(rcDrawn)
	a.Comment("the last unused OAM entry; none: draw nothing")
	a.LDY_imm16_w(0x0200)
	a.JSR_abs(uint16(findFirst))
	a.BCS("rf_draw_extras")
	a.JMP_abs("rf_draw_done")
	a.Label("rf_draw_extras")
	a.Comment("extra sprites behind Link:")
	a.LDX_imm16_w(0x0000)
	a.LDA_long(remoteTableAddr + rtExtrasEnd)
	a.JSR_abs(uint16(loops[drawPlain]))
	a.BCC("rf_draw_done")
	a.Comment("the head, body, sword and shield if the head and body got VRAM slots, else the first player's shadow and blob")
	a.Comment("too; then every other player's shadow and blob:")
	a.LDA_long(remoteStateAddr + rsBodyOK)
	a.AND_imm16_w(0x00FF)
	a.BEQ("rf_draw_no_body")
	a.LDA_long(remoteTableAddr + rtExtrasEnd)
	a.TAX()
	a.LDA_long(remoteTableAddr + rtSlottedEnd)
	a.JSR_abs(uint16(loops[drawSlotted]))
	a.BCC("rf_draw_done")
	a.LDA_long(remoteTableAddr + rtSlottedEnd)
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.TAX()
	a.LDA_long(remoteTableAddr + rtShadowsEnd)
	a.JSR_abs(uint16(loops[drawPlain]))
	a.BCC("rf_draw_done")
	a.LDA_long(remoteTableAddr + rtShadowsEnd)
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.BRA("rf_draw_blobs")
	a.Label("rf_draw_no_body")
	a.LDA_long(remoteTableAddr + rtSlottedEnd)
	a.TAX()
	a.LDA_long(remoteTableAddr + rtShadowsEnd)
	a.JSR_abs(uint16(loops[drawPlain]))
	a.BCC("rf_draw_done")
	a.LDA_long(remoteTableAddr + rtShadowsEnd)
	a.Label("rf_draw_blobs")
	a.TAX()
	a.LDA_long(remoteTableAddr + rtBlobsEnd)
	a.JSR_abs(uint16(loops[drawBlob]))
	a.BCC("rf_draw_done")
	a.Comment("the extra sprites in front of Link:")
	a.LDA_long(remoteTableAddr + rtBlobsEnd)
	a.TAX()
	a.LDA_long(remoteTableAddr + rtFrontEnd)
	a.JSR_abs(uint16(loops[drawPlain]))
	a.Label("rf_draw_done")

	a.Comment("the remote sprites state: DB = $70")
	a.PEA(uint16(remoteStateAddr>>16) * 0x0101)
	a.PLB()
	a.PLB()
	a.Comment("list the rows of chrs of the parts drawn for the NMI hooks to upload: Y = list offset")
	a.LDA_dp(rcDrawn)
	a.STA_abs(st(rsDrawnParts))
	a.LDY_imm16_w(0x0000)
	for p := uint32(0); p < remotePartMax; p++ {
		skip := fmt.Sprintf("rf_up_%d", p)
		a.LDA_dp(rcDrawn)
		a.AND_imm16_w(1 << p)
		a.BEQ(skip)
		a.Comment(fmt.Sprintf("part %d: its slot's VRAM address, and bit 15 set if its tiles are in WRAM:", p))
		a.LDA_abs(st(rsPartSlot + p))
		a.AND_imm16_w(0x00FF)
		a.ASL()
		a.ASL()
		a.ASL()
		a.ASL()
		a.ORA_imm16_w(0x4000)
		a.STA_dp(rcUpVRAM)
		a.Comment("the bit of its slot for the NMI hooks (rsNMIBits):")
		a.LDA_abs(st(rsPartSlot + p))
		a.AND_imm16_w(0x003F)
		a.TAX()
		a.LDA_long_x(t.followerSlotBits)
		a.AND_imm16_w(0x00FF)
		a.BNE(fmt.Sprintf("rf_up_bit_%d", p))
		a.LDA_imm16_w(0x0008)
		a.Label(fmt.Sprintf("rf_up_bit_%d", p))
		a.STA_dp(rcUpBit)
		a.LDA_abs(tb(rtPartFlags + p))
		a.STA_dp(rcUpFlags)
		a.AND_imm16_w(partWRAM)
		a.XBA()
		a.STA_dp(rcUpWRAM)
		for row := uint32(0); row < 2; row++ {
			skipRow := fmt.Sprintf("rf_up_%d_%d", p, row)
			a.LDA_dp(rcUpFlags)
			a.AND_imm16_w(uint16(partTopRow << row))
			a.BEQ(skipRow)
			a.LDA_dp(rcUpVRAM)
			if row == 1 {
				a.CLC()
				a.ADC_imm16_w(0x0100)
			}
			oraDP(a, rcUpWRAM)
			a.STA_abs_y(st(rsNMIList + 0))
			a.LDA_abs(tb(rtPartSrc + p*4 + row*2))
			a.STA_abs_y(st(rsNMIList + 2))
			a.LDA_dp(rcUpBit)
			a.STA_abs_y(st(rsNMIBits))
			a.INY()
			a.INY()
			a.INY()
			a.INY()
			a.Label(skipRow)
		}
		a.Label(skip)
	}
	a.STY_abs(st(rsNMILen))

	a.Label("rf_exit")
	a.PLD()
	a.PLB()
	a.PLP()
	a.RTL()
	a.AssumeSEP(0x30)
	return
}
