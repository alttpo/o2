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
//     entries to capture; an unused entry (Y = $F0) costs a load, a compare and a branch;
//   - the VRAM slots are handed out to the parts with a table lookup (remoteAllocTable);
//   - its scratch is the direct page $00..$0F, which the game only uses as scratch within a routine;
//   - the tables it looks up are in ROM, in the same bank as the code, and read with absolute addressing while DB is
//     the program bank, which also reaches the OAM buffer ($0800..$0A9F) and the IO registers. The remote sprites table
//     in SRAM is read with absolute addressing while DB = $70.

// direct page scratch of remoteFrame:
const (
	// scan: the capture mask (rcMask, u8; 0 while not capturing), whether to draw (rcDraw), whether to find the VRAM
	// slots in use (rcSlots), the slots found in use (rcUsed), the chr of the entry being looked at (rcChr, u16):
	rcMask = 0x00
	// non-zero once the dying enemies are listed this frame (see emitDyingEnemiesList):
	rcDying = 0x01
	rcDraw  = 0x02
	rcSlots = 0x03
	rcUsed  = 0x04
	rcChr   = 0x06
	// drawing: screen x and y (u16), chr and attributes (u16), scratch (u16):
	rcSX   = 0x00
	rcSY   = 0x02
	rcCA   = 0x04
	rcTmp  = 0x06
	rcScrX = 0x08
	rcScrY = 0x0A
	rcAnim = 0x0C
	// bits of the parts drawn (u16):
	rcDrawn = 0x0E
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
func decA(a *asm.Emitter) { raw(a, "dec", 0x3A) }

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

// captureTables returns the capture tables: per ancilla type $00..$3F the bits of captureChrTable to capture while one
// exists (1, or 3 for captureShadowCasters), and per chr $00..$FF its bits (1 for captureChrs, 2 for
// captureShadowChrs, or'ed with captureChrEnemyFX for enemyDeathChrs).
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

// emitRemoteFrame emits the routine the trampoline calls with JSL after the module ran, and returns its entry point
// (after its subroutines); t are the tables emitted by emitRemoteFrameTables in the same bank, dyingList and nearDying
// the routines of emitDyingEnemiesList and emitNearDyingEnemySub. Called with any register sizes; preserves P, DB and
// D.
func emitRemoteFrame(a *asm.Emitter, t remoteFrameTables, dyingList, nearDying uint32) (entry uint32) {
	lo := func(addr uint32) uint16 { return uint16(addr) }
	st := func(o uint32) uint16 { return uint16((remoteStateAddr + o) & 0xFFFF) }
	tb := func(o uint32) uint16 { return uint16((remoteTableAddr + o) & 0xFFFF) }
	ex := linkOAMCopyAddr + linkOAMCopyExtras

	// --- the OAM entry handler, called by the scan with JSR: X = OAM buffer offset, Y = entry index; A 8-bit (B = 0),
	// X and Y 16-bit, DB = program bank. Preserves X and Y. ---
	a.AssumeSEP(0x20)
	a.AssumeREP(0x10)
	handler := a.Label("rf_entry")
	a.Comment("name table 1: neither a VRAM slot nor captured:")
	a.LDA_abs_x(0x0803)
	a.LSR()
	a.BCS("rf_entry_ret")
	a.LDA_abs_x(0x0802)
	a.STA_dp(rcChr)
	a.Comment("VRAM slots shown by chrs $00..$3F:")
	a.LDA_dp(rcSlots)
	a.BEQ("rf_entry_capture")
	a.LDA_dp(rcChr)
	a.CMP_imm8_b(0x40)
	a.BCS("rf_entry_capture")
	a.PHY()
	a.LDA_abs_y(0x0A20)
	ldyDP(a, rcChr)
	a.AND_imm8_b(0x02)
	a.BEQ("rf_entry_small")
	a.LDA_abs_y(lo(t.chrSlotsLarge))
	a.BRA("rf_entry_slots")
	a.Label("rf_entry_small")
	a.LDA_abs_y(lo(t.chrSlotsSmall))
	a.Label("rf_entry_slots")
	a.PLY()
	tsbDP(a, rcUsed)
	a.Label("rf_entry_capture")
	a.LDA_dp(rcMask)
	a.BEQ("rf_entry_ret")
	a.PHY()
	ldyDP(a, rcChr)
	a.LDA_abs_y(lo(t.captureChrs))
	a.PLY()
	andDP(a, rcMask)
	a.BNE("rf_entry_copy")
	a.Label("rf_entry_ret")
	a.RTS()

	a.Comment("a captured chr: not one of Link's own entries, which are copied already:")
	a.Label("rf_entry_copy")
	a.PHX()
	a.PHY()
	a.REP(0x30)
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
	raw(a, "lda $03,s: the entry offset", 0xA3, 0x03)
	a.TAY()
	a.JSL(nearDying)
	a.BCC("rf_copy")
	a.JMP_abs("rf_copy_done")
	a.Label("rf_copy")
	a.Comment("Y = the entry offset; copy x, y, chr, attributes, extended attributes and the OAM index:")
	raw(a, "lda $03,s: the entry offset", 0xA3, 0x03)
	a.TAY()
	a.LDA_long(ex)
	a.TAX()
	a.LDA_abs_y(0x0800)
	a.STA_long_x(ex + 2 + 0)
	a.LDA_abs_y(0x0802)
	a.STA_long_x(ex + 2 + 2)
	a.PHX()
	a.TYA()
	a.LSR()
	a.LSR()
	a.TAX()
	a.LDA_abs_x(0x0A20)
	a.AND_imm16_w(0x0003)
	a.PLX()
	a.STA_long_x(ex + 2 + 4)
	a.TYA()
	a.LSR()
	a.LSR()
	a.SEP(0x20)
	a.STA_long_x(ex + 2 + 5)
	a.REP(0x20)
	a.TXA()
	a.CLC()
	a.ADC_imm16_w(linkOAMExtraSize)
	a.STA_long(ex)
	a.CMP_imm16_w(linkOAMExtrasMax * linkOAMExtraSize)
	a.BCC("rf_copy_done")
	a.Comment("full: capture nothing more:")
	a.STZ_dp(rcMask)
	a.Label("rf_copy_done")
	a.Comment("B = 0 for the 16-bit index loads of the chr:")
	a.LDA_imm16_w(0)
	a.SEP(0x20)
	a.PLY()
	a.PLX()
	a.RTS()

	// --- the sprite drawing routine, called with JSR: Y = table entry offset, X = OAM buffer offset to search down
	// from; A, X and Y 16-bit, DB = $70. Preserves Y; updates X. ---
	a.AssumeREP(0x30)
	a.Label("rf_sprite_noslot")
	a.PLX()
	a.Label("rf_sprite_ret")
	a.RTS()
	drawSprite := a.Label("rf_sprite")
	a.Comment("screen x = world x - scroll x, visible if -16 < x < 256:")
	a.LDA_abs_y(tb(rtEntries + 0))
	a.SEC()
	a.SBC_dp(rcScrX)
	a.STA_dp(rcSX)
	a.CLC()
	a.ADC_imm16_w(0x0010)
	a.CMP_imm16_w(0x0110)
	a.BCS("rf_sprite_ret")
	a.Comment("screen y = world y - scroll y, visible if -16 < y < 224:")
	a.LDA_abs_y(tb(rtEntries + 2))
	a.SEC()
	a.SBC_dp(rcScrY)
	a.STA_dp(rcSY)
	a.CLC()
	a.ADC_imm16_w(0x0010)
	a.CMP_imm16_w(0x00F0)
	a.BCS("rf_sprite_ret")
	a.Comment("chr and attributes; blobs are animated, slotted chrs are relative to their part's VRAM slot:")
	a.LDA_abs_y(tb(rtEntries + 4))
	a.STA_dp(rcCA)
	a.LDA_abs_y(tb(rtEntries + 6))
	a.AND_imm16_w(entrySlotted | entryBlobAnim)
	a.BEQ("rf_sprite_find")
	a.CMP_imm16_w(entryBlobAnim)
	a.BNE("rf_sprite_slotted")
	a.LDA_dp(rcCA)
	a.CLC()
	adcDP(a, rcAnim)
	a.STA_dp(rcCA)
	a.BRA("rf_sprite_find")
	a.Label("rf_sprite_slotted")
	a.Comment("part = chr bits 6-7; a sword or shield without a VRAM slot is not drawn:")
	a.LDA_dp(rcCA)
	a.AND_imm16_w(0x00C0)
	a.ASL()
	a.ASL()
	a.XBA()
	a.PHX()
	a.TAX()
	a.LDA_abs_x(st(rsPartSlot))
	a.AND_imm16_w(0x00FF)
	a.CMP_imm16_w(0x00FF)
	a.BEQ("rf_sprite_noslot")
	a.STA_dp(rcTmp)
	a.LDA_dp(rcCA)
	a.AND_imm16_w(0xFF11)
	a.CLC()
	adcDP(a, rcTmp)
	a.STA_dp(rcCA)
	a.TXA()
	a.ASL()
	a.TAX()
	a.LDA_long_x(t.partBits)
	tsbDP(a, rcDrawn)
	a.PLX()
	a.Label("rf_sprite_find")
	a.Comment("find the next unused OAM entry (Y = $F0) below X, 4 entries at a time while there are 4 below:")
	a.SEP(0x20)
	a.Label("rf_sprite_find4")
	a.CPX_imm16_w(0x0010)
	a.BCC("rf_sprite_find_loop")
	for i := 1; i <= 4; i++ {
		a.LDA_long_x(uint32(0x7E0801 - 4*i))
		a.CMP_imm8_b(0xF0)
		a.BEQ(fmt.Sprintf("rf_sprite_found%d", i))
	}
	a.REP(0x20)
	a.TXA()
	a.CLC()
	a.ADC_imm16_w(0xFFF0) // -$10
	a.TAX()
	a.SEP(0x20)
	a.BRA("rf_sprite_find4")
	for i := 4; i >= 1; i-- {
		a.Label(fmt.Sprintf("rf_sprite_found%d", i))
		a.DEX()
		a.DEX()
		a.DEX()
		a.DEX()
	}
	a.BRA("rf_sprite_found")
	a.Label("rf_sprite_find_loop")
	a.DEX()
	a.DEX()
	a.DEX()
	a.DEX()
	a.BMI("rf_sprite_full")
	a.LDA_long_x(0x7E0801)
	a.CMP_imm8_b(0xF0)
	a.BNE("rf_sprite_find_loop")
	a.Label("rf_sprite_found")
	a.LDA_dp(rcSX)
	a.STA_long_x(0x7E0800)
	a.LDA_dp(rcSY)
	a.STA_long_x(0x7E0801)
	a.LDA_dp(rcCA)
	a.STA_long_x(0x7E0802)
	a.LDA_dp(rcCA + 1)
	a.STA_long_x(0x7E0803)
	a.Comment("extended attributes: size, x bit 8:")
	a.LDA_abs_y(tb(rtEntries + 6))
	a.AND_imm8_b(entrySize16)
	a.STA_dp(rcTmp)
	a.LDA_dp(rcSX + 1)
	a.AND_imm8_b(0x01)
	oraDP(a, rcTmp)
	a.REP(0x20)
	a.PHX()
	a.PHA()
	a.TXA()
	a.LSR()
	a.LSR()
	a.TAX()
	a.PLA()
	a.SEP(0x20)
	a.STA_long_x(0x7E0A20)
	a.REP(0x20)
	a.PLX()
	a.RTS()
	a.Label("rf_sprite_full")
	a.Comment("no unused entries left; stay at the start for any following sprites:")
	a.REP(0x20)
	a.LDX_imm16_w(0x0000)
	a.RTS()

	// --- draws the table entries from offset Y up to offset A with drawSprite; A, X and Y 16-bit, DB = $70 ---
	drawUntil := a.Label("rf_until")
	a.STA_abs(st(rsTmpEnd))
	a.Label("rf_until_loop")
	cpyAbs(a, st(rsTmpEnd))
	a.BCS("rf_until_done")
	a.JSR_abs(uint16(drawSprite))
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.TAY()
	a.BRA("rf_until_loop")
	a.Label("rf_until_done")
	a.RTS()

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
	a.STA_long(ex)
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

	a.Label("rf_scan_check")
	a.SEP(0x30)
	a.LDA_dp(rcMask)
	oraDP(a, rcSlots)
	a.BNE("rf_scan")
	a.JMP_abs("rf_scan_done")

	a.Label("rf_scan")
	a.Comment("one pass over the OAM buffer, from the last entry down (the order captured entries are copied in):")
	a.REP(0x10)
	for k := 0x7F; k >= 0; k-- {
		next := fmt.Sprintf("rf_scan_%02x", k)
		a.LDA_abs(uint16(0x0801 + k*4))
		a.CMP_imm8_b(0xF0)
		a.BEQ(next)
		a.LDX_imm16_w(uint16(k * 4))
		a.LDY_imm16_w(uint16(k))
		a.JSR_abs(uint16(handler))
		a.Label(next)
	}
	a.SEP(0x10)

	a.Label("rf_scan_done")
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
	a.STZ_dp(rcTmp)
	for i := 0; i < remotePartMax; i++ {
		a.LDA_long(remoteStateAddr + rsPartSlot + uint32(i))
		a.CMP_imm8_b(0xFF)
		a.BEQ(fmt.Sprintf("rf_skip_%d", i))
		a.REP(0x20)
		a.AND_imm16_w(0x003F)
		a.TAX()
		a.SEP(0x20)
		a.LDA_abs_x(lo(t.followerSlotBits))
		tsbDP(a, rcTmp)
		a.Label(fmt.Sprintf("rf_skip_%d", i))
	}
	a.LDA_dp(rcTmp)
	a.STA_long(remoteStateAddr + rsSkipSlots)
	a.Comment("the body is drawn if the required parts all got a slot: min(parts, free) >= required > 0:")
	a.LDA_long(remoteTableAddr + rtRequired)
	a.BEQ("rf_alloc_done")
	a.STA_dp(rcTmp)
	a.LDA_long(remoteTableAddr + rtParts)
	rawAbs(a, "cmp.w $%04x,y", 0xD9, lo(t.allocFree))
	a.BCC("rf_alloc_min")
	a.LDA_abs_y(lo(t.allocFree))
	a.Label("rf_alloc_min")
	a.CMP_dp(rcTmp)
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
	a.Comment("the remote sprites table: DB = $70")
	a.PEA(uint16(remoteStateAddr>>16) * 0x0101)
	a.PLB()
	a.PLB()
	a.LDX_imm16_w(0x0200)
	a.LDY_imm16_w(0x0000)
	a.Comment("extra sprites behind Link:")
	a.LDA_abs(tb(rtExtrasEnd))
	a.JSR_abs(uint16(drawUntil))
	a.Comment("the head, body, sword and shield if the head and body got VRAM slots, else the player's shadow and blob:")
	a.LDA_abs(st(rsBodyOK))
	a.AND_imm16_w(0x00FF)
	a.BEQ("rf_no_body")
	a.LDA_abs(tb(rtSlottedEnd))
	a.JSR_abs(uint16(drawUntil))
	a.LDY_abs(tb(rtP0BlobEnd))
	a.BRA("rf_blobs")
	a.Label("rf_no_body")
	a.LDY_abs(tb(rtSlottedEnd))
	a.Label("rf_blobs")
	a.Comment("the other players' shadows and blobs, and the extra sprites in front of Link:")
	a.LDA_abs(tb(rtFrontEnd))
	a.JSR_abs(uint16(drawUntil))

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
		a.STA_dp(rcSX)
		a.Comment("the bit of its slot for the NMI hooks (rsNMIBits):")
		a.LDA_abs(st(rsPartSlot + p))
		a.AND_imm16_w(0x003F)
		a.TAX()
		a.LDA_long_x(t.followerSlotBits)
		a.AND_imm16_w(0x00FF)
		a.BNE(fmt.Sprintf("rf_up_bit_%d", p))
		a.LDA_imm16_w(0x0008)
		a.Label(fmt.Sprintf("rf_up_bit_%d", p))
		a.STA_dp(rcAnim)
		a.LDA_abs(tb(rtPartFlags + p))
		a.STA_dp(rcTmp)
		a.AND_imm16_w(partWRAM)
		a.XBA()
		a.STA_dp(rcSY)
		for row := uint32(0); row < 2; row++ {
			skipRow := fmt.Sprintf("rf_up_%d_%d", p, row)
			a.LDA_dp(rcTmp)
			a.AND_imm16_w(uint16(partTopRow << row))
			a.BEQ(skipRow)
			a.LDA_dp(rcSX)
			if row == 1 {
				a.CLC()
				a.ADC_imm16_w(0x0100)
			}
			oraDP(a, rcSY)
			a.STA_abs_y(st(rsNMIList + 0))
			a.LDA_abs(tb(rtPartSrc + p*4 + row*2))
			a.STA_abs_y(st(rsNMIList + 2))
			a.LDA_dp(rcAnim)
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
