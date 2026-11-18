package alttp

import (
	"bytes"
	"hash/crc32"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// HUD updates
//
// HandleItemRefills, which both the underworld and overworld modules run every frame, ends by rebuilding the numbers
// and hearts of the HUD in its tilemap buffer (UpdateHUDBuffer_skip_item: the hearts, magic meter, rupees, bombs,
// arrows and keys, with HexToDec and UpdateHearts) and setting $16 for NMI to upload the buffer. That takes about 18k
// master clocks (5% of a frame) although the numbers rarely change. The patch skips the rebuild while its inputs are
// the same as when it last ran, on the frame before:
//
//   - the inputs, which it reads nothing else of: $7EF36C..$7EF36F (health, its maximum, magic, keys), $7EF362
//     (rupees shown), $7EF343 (bombs), $7EF377 (arrows) and $7EF37B (half magic);
//   - only while it ran on the frame before ($1A) and on that path: any frame that HandleItemRefills ran another way
//     (the heart refill animation, which draws hearts itself) or didn't run (menus, transitions, other modules, which
//     may rebuild the whole HUD) makes it rebuild once again. RebuildHUD rebuilds the buffer from its template and the
//     same inputs itself.
//
// $16 is still set every frame, so NMI uploads the buffer as before (other code may have changed other parts of it).
//
// Only ROMs whose UpdateHUDBuffer_skip_item is vanilla JP 1.0 or US 1.0 get the hook: randomizers draw their own HUD
// (counters, timers) from other inputs.

// hudHookAddr is HandleItemRefills' `.heart_refill_done` tail, the same in JP 1.0, US 1.0 and randomizers:
//
//	REP #$30 : LDA.w #$FFFF : STA.b $0E : JSR UpdateHUDBuffer_skip_item : SEP #$30 : INC.b $16 : PLB : RTL
const hudHookAddr = uint32(0x0DDD1A)

// hudHookExpected is the code at hudHookAddr, with the JSR's operand (bytes 8 and 9) left out of the comparison:
var hudHookExpected = []byte{0xC2, 0x30, 0xA9, 0xFF, 0xFF, 0x85, 0x0E, 0x20, 0x00, 0x00, 0xE2, 0x30, 0xE6, 0x16, 0xAB, 0x6B}

// hudSkipItemSize is the size of UpdateHUDBuffer_skip_item, through its RTS, and hudSkipItemCRCs the CRC-32s of the
// vanilla JP 1.0 and US 1.0 routines:
const hudSkipItemSize = 0x166

var hudSkipItemCRCs = []uint32{0x852E5ABA, 0xBE889916}

// SRAM cache of the inputs the HUD was last built from (in the unused capture tables area; see captureTypesAddr):
const (
	hudCacheAddr = captureTypesAddr
	// $7EF36C..$7EF36F, $7EF362..$7EF363, $7EF343, $7EF377, $7EF37B:
	hudCacheInputs = hudCacheAddr + 0
	// $1A of the frame the HUD was last checked on, and non-zero if the cache holds the HUD's inputs:
	hudCacheFrame = hudCacheAddr + 9
	hudCacheValid = hudCacheAddr + 10
	hudCacheEnd   = hudCacheAddr + 11
)

const _ = captureChrsAddr - hudCacheEnd

// hudHookSite returns the address of UpdateHUDBuffer_skip_item (in bank $0D) if the ROM has the vanilla code at
// hudHookAddr and the vanilla routine, else 0.
func hudHookSite(rom *snes.ROM) uint32 {
	pak, err := lorom.BusAddressToPak(hudHookAddr)
	if err != nil || int(pak)+len(hudHookExpected) > len(rom.Contents) {
		return 0
	}
	c := append([]byte(nil), rom.Contents[pak:pak+uint32(len(hudHookExpected))]...)
	target := 0x0D0000 | uint32(c[8]) | uint32(c[9])<<8
	c[8], c[9] = 0, 0
	if !bytes.Equal(c, hudHookExpected) {
		return 0
	}
	tpak, err := lorom.BusAddressToPak(target)
	if err != nil || int(tpak)+hudSkipItemSize > len(rom.Contents) {
		return 0
	}
	crc := crc32.ChecksumIEEE(rom.Contents[tpak : tpak+hudSkipItemSize])
	for _, v := range hudSkipItemCRCs {
		if crc == v {
			return target
		}
	}
	return 0
}

// emitHUDHookSite emits the code written over hudHookAddr: JSL check, then the JSR to UpdateHUDBuffer_skip_item
// (skipItem) unless check returned carry set, and the rest of the original code.
func emitHUDHookSite(a *asm.Emitter, check, skipItem uint32) {
	a.JSL(check)
	a.BCS("hud_skip")
	a.JSR_abs(uint16(skipItem))
	a.Label("hud_skip")
	a.SEP(0x30)
	a.INC_dp(0x16)
	a.PLB()
	a.RTL()
}

// emitHUDCheck emits the routine the hook calls with JSL: it returns carry set if the HUD buffer is up to date, else
// carry clear after caching the inputs. It returns with A, X and Y 16-bit and $0E = $FFFF, as the code it replaced
// left them.
func emitHUDCheck(a *asm.Emitter) {
	a.Label("hudCheck")
	a.REP(0x30)
	a.LDA_imm16_w(0xFFFF)
	a.STA_dp(0x0E)
	a.SEP(0x20)
	a.Comment("only if it was checked on the frame before, and built from the inputs in the cache:")
	a.LDA_long(hudCacheFrame)
	a.INC()
	a.CMP_dp(0x1A)
	a.BNE("hud_changed")
	a.LDA_long(hudCacheValid)
	a.BEQ("hud_changed")
	a.REP(0x20)
	for i, w := range []uint32{0x7EF36C, 0x7EF36E, 0x7EF362} {
		a.LDA_long(w)
		a.CMP_long(hudCacheInputs + uint32(i)*2)
		a.BNE("hud_changed")
	}
	a.SEP(0x20)
	for i, b := range []uint32{0x7EF343, 0x7EF377, 0x7EF37B} {
		a.LDA_long(b)
		a.CMP_long(hudCacheInputs + 6 + uint32(i))
		a.BNE("hud_changed")
	}
	a.LDA_dp(0x1A)
	a.STA_long(hudCacheFrame)
	a.REP(0x30)
	a.SEC()
	a.RTL()

	a.Label("hud_changed")
	a.REP(0x20)
	a.LDA_long(0x7EF36C)
	a.STA_long(hudCacheInputs + 0)
	a.LDA_long(0x7EF36E)
	a.STA_long(hudCacheInputs + 2)
	a.LDA_long(0x7EF362)
	a.STA_long(hudCacheInputs + 4)
	a.SEP(0x20)
	for i, b := range []uint32{0x7EF343, 0x7EF377, 0x7EF37B} {
		a.LDA_long(b)
		a.STA_long(hudCacheInputs + 6 + uint32(i))
	}
	a.LDA_dp(0x1A)
	a.STA_long(hudCacheFrame)
	a.LDA_imm8_b(0x01)
	a.STA_long(hudCacheValid)
	a.REP(0x30)
	a.CLC()
	a.RTL()
	a.AssumeSEP(0x30)
}
