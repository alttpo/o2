package alttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"o2/snes"
	"sort"
	"time"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
)

// Remote player sprites
//
// o2 draws other players that are in the same room or overworld area as the local player into the game's own OAM
// buffer. This is simpler than what alttpo does (it draws every sprite a remote player sends, with their custom
// graphics, on top of the emulated picture):
//
//   - One remote player is drawn with Link's head and body, the two 16x16 sprites whose tiles come from LinkGraphics in
//     ROM. alttpo and o2 send the pose as "unique tile" indices (see sprites.go), which are offsets into LinkGraphics,
//     so the tiles can be copied from the local ROM: the remote player's pose is shown with the local player's Link
//     graphics. Sword, shield and items are not drawn.
//   - The shadow (chr $6C on the ground, $28 or $38 while recoiling or jumping off a ledge), sword sparkles and dash
//     dust (captureChrs) that the same player sends are drawn too, from the local static and common sprite sheets.
//   - Every other remote player in the same location is drawn as a single 16x16 sprite at their coordinates, chr $64
//     or $66 alternating every 8 frames: an animated blob with eyes from the common sprite sheet.
//
// VRAM: Link and the items he uses have their tiles uploaded every frame by NMI_DoUpdates into the first sprite sheet,
// chrs $00..$3F. Enemies and NPCs use the static sprite sheets that are loaded per area (chrs $40..$1FF), except for
// followers (Zelda, the old man, the bomb...) and a few NPC sprites with large animations (Kiki, the crystal maidens,
// the old mountain man, the returning smithy, Blind's maiden form) which share chrs $20..$23 and $30..$33: whoever draws
// sets the graphics to upload there in dma_var6 ($0AE8, chrs $20,$21,$30,$31) and dma_var7 ($0AEA, chrs $22,$23,$32,$33)
// and the game uploads them every frame whether or not anything uses them. That is exactly room for two 16x16 sprites.
// The frame trampoline sets both variables to a sentinel before the module runs and restores them afterwards; if both
// still hold the sentinel, or no sprite in the OAM buffer shows any of those chrs (something set them without drawing
// itself, like Kiki after it fled out of view), the remote player's head and body are uploaded over them in the same
// NMI, right after the game's own upload. Otherwise the remote player is drawn as a blob instead.
//
// OAM: after the module drew everything (and before NMI_PrepareSprites packs the extended attributes), the trampoline
// fills unused OAM entries (Y = $F0, as ClearOAMBuffer leaves them) from the end of the OAM buffer, so remote players
// are drawn behind all of the game's own sprites. Positions are kept as world coordinates (alttpo's convention: the
// OAM coordinate plus the scroll offset `BG2 scroll - shake offset`) and converted to screen coordinates with the
// local scroll offsets every frame, so remote players don't wobble when the local screen scrolls.
//
// Drawing is switched off with Game.SyncSprites; o2 then writes an empty table.
//
// o2 writes the table at remoteTableAddr whenever it changes, and at least every remoteTableRefresh. The game counts its
// TTL down every frame and stops drawing when it reaches zero, so remote players disappear when o2 stops updating it.
// The table also holds the location it was made for, which the game compares with the current room or area so that
// nothing is drawn in the wrong place while o2 catches up with a room change.

// SRAM layout of the remote sprites table, written by o2:
const (
	remoteTableAddr = uint32(0x706210)

	// frames left to draw the table, counted down every frame:
	rtTTL = 0
	// frames left to capture the local player's sparkles and dust for o2 to send (see emitCaptureExtras), counted down
	// every frame; o2 keeps it up while any remote player is in the same location:
	rtCaptureTTL = 1
	// $1B (1 = underworld) and $A0 (underworld room) or $8A (overworld area) the table is for:
	rtIndoors  = 2
	rtLocation = 4
	// byte offsets into rtEntries: the end of the extra sprites behind the body, the end of the body sprites, the end
	// of the blobs and the end of the extra sprites in front of everything else:
	rtExtrasEnd = 6
	rtBodyEnd   = 8
	rtEnd       = 10
	// LinkGraphics DMA source addresses: top row of chrs $20/$21, $22/$23, bottom row of chrs $30/$31, $32/$33:
	rtSrc      = 12
	rtFrontEnd = 20
	// sprites to draw in order, each drawn in front of the ones before: world x, world y, chr, attributes, size ($02 for
	// 16x16). Extra sprites (shadows, sparkles, dust, projectiles, carried and thrown objects; see captureChrs) the
	// remote player draws behind Link and 2 body sprites (chrs $20 and $22) are followed by one blob per player (chr
	// $64), starting with the body's player, whose blob is only drawn when the body can't be, and the extra sprites the
	// remote player draws in front of Link; up to remoteExtraMax extras in all:
	rtEntries      = 22
	rtEntrySize    = 7
	remoteExtraMax = 14
	remoteBlobMax  = 8
	remoteEntryMax = remoteExtraMax + 2 + remoteBlobMax

	remoteTableSize = rtEntries + remoteEntryMax*rtEntrySize
)

// SRAM layout of the remote sprites state, written by the game code:
const (
	remoteStateAddr = remoteTableAddr + remoteTableSize

	// dma_var6 and dma_var7 saved before the module runs:
	rsSaved6 = 0
	rsSaved7 = 1
	// non-zero when the NMI hook should upload the body tiles from rsNMISrc:
	rsNMIEnable = 2
	// 2 when neither dma_var6 nor dma_var7 was set by the module this frame, or no sprite drawn shows chrs $20..$23:
	rsFree = 3
	// copy of rtSrc for the NMI hook, consistent with the sprites drawn this frame:
	rsNMISrc = 4
	// local scroll offsets: BG2 scroll ($E2, $E8) - shake offsets ($011A, $011C):
	rsScrollX = 12
	rsScrollY = 14
	// screen coordinates of the sprite being drawn:
	rsTmpSX = 16
	rsTmpSY = 18
	// ORed into the chr of the sprites being drawn; 0 or 2 for blobs, alternating every 8 frames:
	rsAnim = 20
	// blob animation for this frame:
	rsBlobAnim = 22

	remoteStateSize = 24
)

// VRAM and OAM constants:
const (
	// dma_var6 and dma_var7 in WRAM; the game only ever writes their low bytes:
	wramDMAVar6 = 0x7E0AE8
	wramDMAVar7 = 0x7E0AEA
	// dmaVarSentinel is never a value the game writes to dma_var6/7 (they are multiples of $20):
	dmaVarSentinel = 0xFF

	// remoteBodyChr is the first of the chrs that the body sprites are uploaded to:
	remoteBodyChr = 0x20
	// VRAM word addresses of the top and bottom rows of chrs $20..$23:
	remoteBodyVRAMTop    = 0x4200
	remoteBodyVRAMBottom = 0x4300

	// blob chrs, alternating every 8 frames:
	remoteBlobChr0 = 0x64
	// blob OAM attributes: priority 2 and palette 4, as the game draws the blob (sprite $8F) that magic powder turns an
	// enemy into (Sprite_GiveDamage sets its OAM flags to 8):
	remoteBlobAttr = 0x28

	// nmiHookPatchAddr is where NMI_DoUpdates sets up the animated tiles upload, right after uploading the follower
	// and NPC tiles; patched with a JSL to the NMI hook:
	nmiHookPatchAddr = uint32(0x008B50)
	// nmiEarlyPatchAddr is at the start of NMI_DoUpdates, before it checks whether core updates are disabled; patched
	// with a JSL to the early NMI hook:
	nmiEarlyPatchAddr = uint32(0x0089E2)
	// linkGfxBankAddr is the `LDA.b #LinkGraphics>>16` in NMI_DoUpdates, which randomizers replace with `LDA.b $BC`:
	linkGfxBankAddr = uint32(0x008A01)
)

// nmiHookPatchExpected is the original code at nmiHookPatchAddr: LDX.w $0ADC; STX.w DMA0ADDRL
var nmiHookPatchExpected = []byte{0xAE, 0xDC, 0x0A, 0x8E, 0x02, 0x43}

// nmiEarlyPatchExpected is the original code at nmiEarlyPatchAddr: LDA.b #$80; STA.w VMAIN
var nmiEarlyPatchExpected = []byte{0xA9, 0x80, 0x8D, 0x15, 0x21}

// remoteTTLFrames is the TTL o2 writes to the table, in frames:
const remoteTTLFrames = 60

// remoteTableRefresh is how often o2 rewrites an unchanged table to keep its TTL from running out:
const remoteTableRefresh = 250 * time.Millisecond

// remoteSpritesStale is how long received sprites are used to draw a remote player. alttpo sends its sprites every
// frame to the sector of the room or area it is actually in, so an alttpo player in the local player's (reported)
// location that stops sending sprites has left it, even though its reported location stays on the old room or area
// until a screen transition or warp finishes. alttpo also only draws remote players from the sprites it receives.
const remoteSpritesStale = 250 * time.Millisecond

// emitRemoteSave saves dma_var6/7 and sets them to the sentinel, before the module runs. A and X are 8-bit.
func emitRemoteSave(a *asm.Emitter) {
	a.Comment("save dma_var6/7 and set them to a sentinel to see if anything draws with chrs $20..$23 this frame:")
	a.LDA_long(wramDMAVar6)
	a.STA_long(remoteStateAddr + rsSaved6)
	a.LDA_long(wramDMAVar7)
	a.STA_long(remoteStateAddr + rsSaved7)
	a.LDA_imm8_b(dmaVarSentinel)
	a.STA_long(wramDMAVar6)
	a.STA_long(wramDMAVar7)
}

// emitRemoteDrawRoutines emits remoteDrawSprite and then remoteDraw, which calls it with JSR, and returns the address
// of remoteDraw. They run from any bank.
func emitRemoteDrawRoutines(a *asm.Emitter) (remoteDraw uint32) {
	drawSprite := emitRemoteDrawSprite(a)
	return emitRemoteDraw(a, uint16(drawSprite))
}

// emitRemoteDraw emits the routine that the trampoline calls with JSL after the module ran. It restores dma_var6/7 and
// draws the remote sprites table into the OAM buffer, with the routine at drawSprite in the same bank. Called with A
// and X 8-bit; preserves P and DB.
func emitRemoteDraw(a *asm.Emitter, drawSprite uint16) (addr uint32) {
	// the table and state are accessed with absolute addressing while DB = $70:
	st := func(o uint32) uint16 { return uint16((remoteStateAddr + o) & 0xFFFF) }
	tb := func(o uint32) uint16 { return uint16((remoteTableAddr + o) & 0xFFFF) }

	addr = a.Label("remoteDraw")
	a.PHP()
	a.PHB()
	a.PEA(uint16(remoteStateAddr>>16) * 0x0101)
	a.PLB()
	a.PLB()
	a.SEP(0x30)

	a.Comment("restore dma_var6/7 if nothing set them; X counts the ones still free:")
	a.LDX_imm8_b(0x00)
	for i, v := range []uint32{wramDMAVar6, wramDMAVar7} {
		next := fmt.Sprintf("rd_var%d", i)
		a.LDA_long(v)
		a.CMP_imm8_b(dmaVarSentinel)
		a.BNE(next)
		a.LDA_abs(st(rsSaved6 + uint32(i)))
		a.STA_long(v)
		a.INX()
		a.Label(next)
	}
	a.STX_abs(st(rsFree))
	a.STZ_abs(st(rsNMIEnable))

	a.Comment("count down the table's TTL:")
	a.LDA_abs(tb(rtTTL))
	a.BNE("rd_ttl_ok")
	a.Label("rd_exit")
	a.PLB()
	a.PLP()
	a.RTL()
	a.Label("rd_ttl_ok")
	a.DEC_abs(tb(rtTTL))

	a.Comment("only while the game draws the underworld or overworld sprites:")
	a.LDA_long(0x7E0010)
	for _, m := range []uint8{0x07, 0x09, 0x0B, 0x0F, 0x10, 0x13, 0x16} {
		a.CMP_imm8_b(m)
		a.BEQ("rd_module_ok")
	}
	a.CMP_imm8_b(0x0E)
	a.BNE("rd_exit2")
	a.Comment("dialogs and menus (Module0E_Interface): not on the dungeon or overworld map or the flute menu:")
	a.LDA_long(0x7E001B)
	a.BEQ("rd_interface_ow")
	a.LDA_long(0x7E0011)
	a.CMP_imm8_b(0x03)
	a.BNE("rd_module_ok")
	a.LDA_long(0x7E0200)
	a.BEQ("rd_module_ok")
	a.CMP_imm8_b(0x07)
	a.BEQ("rd_module_ok")
	a.BRA("rd_exit2")
	a.Label("rd_interface_ow")
	a.LDA_long(0x7E0011)
	a.CMP_imm8_b(0x07)
	a.BEQ("rd_interface_map")
	a.CMP_imm8_b(0x0A)
	a.BNE("rd_module_ok")
	a.Label("rd_interface_map")
	a.LDA_long(0x7E0200)
	a.BEQ("rd_module_ok")
	a.Label("rd_exit2")
	a.PLB()
	a.PLP()
	a.RTL()
	a.Label("rd_module_ok")

	a.Comment("only in the room or area the table was made for:")
	a.LDA_long(0x7E001B)
	a.CMP_abs(tb(rtIndoors))
	a.BNE("rd_exit2")
	a.LDA_long(0x7E001B)
	a.REP(0x30)
	a.BEQ("rd_overworld")
	a.LDA_long(0x7E00A0)
	a.BRA("rd_location")
	a.Label("rd_overworld")
	a.LDA_long(0x7E008A)
	a.Label("rd_location")
	a.CMP_abs(tb(rtLocation))
	a.BNE("rd_exit2")

	emitRemoteChrsVisible(a, st(rsFree))

	a.Comment("local scroll offsets:")
	a.LDA_long(0x7E00E2)
	a.SEC()
	a.SBC_long(0x7E011A)
	a.STA_abs(st(rsScrollX))
	a.LDA_long(0x7E00E8)
	a.SEC()
	a.SBC_long(0x7E011C)
	a.STA_abs(st(rsScrollY))
	a.Comment("blob chr alternates every 8 frames:")
	a.LDA_long(0x7E001A)
	a.AND_imm16_w(0x0008)
	a.LSR()
	a.LSR()
	a.STA_abs(st(rsBlobAnim))

	a.Comment("X = OAM buffer offset to search down from; Y = table entry offset:")
	a.LDX_imm16_w(0x0200)
	a.LDY_imm16_w(0x0000)
	a.STZ_abs(st(rsAnim))

	a.Comment("shadows, sparkles and dust:")
	a.Label("rd_shadow_loop")
	a.TYA()
	a.CMP_abs(tb(rtExtrasEnd))
	a.BCS("rd_shadows_done")
	a.JSR_abs(drawSprite)
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.TAY()
	a.BRA("rd_shadow_loop")
	a.Label("rd_shadows_done")

	a.Comment("draw the body if there is one and nothing else uses chrs $20..$23; the late NMI hook uploads them while")
	a.Comment("core NMI updates are enabled, the early one while they are disabled:")
	a.SEP(0x20)
	a.LDA_abs(st(rsFree))
	a.CMP_imm8_b(0x02)
	a.BNE("rd_no_body")
	a.REP(0x20)
	a.TYA()
	a.CMP_abs(tb(rtBodyEnd))
	a.BCS("rd_blobs")
	a.Label("rd_body_loop")
	a.JSR_abs(drawSprite)
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.TAY()
	a.CMP_abs(tb(rtBodyEnd))
	a.BCC("rd_body_loop")
	a.Comment("skip the body player's blob; carry is set:")
	a.ADC_imm16_w(rtEntrySize - 1)
	a.TAY()
	a.Comment("let the NMI hook upload the body tiles:")
	for i := uint32(0); i < 8; i += 2 {
		a.LDA_abs(tb(rtSrc + i))
		a.STA_abs(st(rsNMISrc + i))
	}
	a.INC_abs(st(rsNMIEnable))
	a.BRA("rd_blobs")

	a.Label("rd_no_body")
	a.REP(0x20)
	a.LDY_abs(tb(rtBodyEnd))

	a.Label("rd_blobs")
	a.LDA_abs(st(rsBlobAnim))
	a.STA_abs(st(rsAnim))
	a.Label("rd_blob_loop")
	a.TYA()
	a.CMP_abs(tb(rtEnd))
	a.BCS("rd_front")
	a.JSR_abs(drawSprite)
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.TAY()
	a.BRA("rd_blob_loop")

	a.Comment("extra sprites in front:")
	a.Label("rd_front")
	a.STZ_abs(st(rsAnim))
	a.Label("rd_front_loop")
	a.TYA()
	a.CMP_abs(tb(rtFrontEnd))
	a.BCS("rd_done")
	a.JSR_abs(drawSprite)
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.TAY()
	a.BRA("rd_front_loop")

	a.Label("rd_done")
	a.PLB()
	a.PLP()
	a.RTL()
	a.AssumeSEP(0x30)
	return
}

// emitRemoteChrsVisible sets rsFree to 2 when dma_var6/7 were set this frame but no sprite in the OAM buffer shows
// any of chrs $20..$23 or $30..$33: whatever set them isn't visible, e.g. Kiki keeps setting them every frame after
// it fled out of view on the Palace of Darkness area, and its upload can be overwritten. Called with A, X and Y
// 16-bit; leaves them 16-bit and clobbers A and X.
func emitRemoteChrsVisible(a *asm.Emitter, rsFreeAbs uint16) {
	a.Comment("chrs $20..$23/$30..$33 are also free if dma_var6/7 were set but no sprite drawn this frame shows them:")
	a.SEP(0x20)
	a.LDA_abs(rsFreeAbs)
	a.CMP_imm8_b(0x02)
	a.BEQ("rd_chrs_done")
	a.LDX_imm16_w(0x01FC)
	a.Label("rd_chrs_loop")
	a.LDA_long_x(0x7E0801)
	a.CMP_imm8_b(0xF0)
	a.BEQ("rd_chrs_next")
	a.Comment("first name table only:")
	a.LDA_long_x(0x7E0803)
	a.LSR()
	a.BCS("rd_chrs_next")
	a.Comment("chr $20..$23 or $30..$33:")
	a.LDA_long_x(0x7E0802)
	a.AND_imm8_b(0xEC)
	a.CMP_imm8_b(0x20)
	a.BEQ("rd_chrs_done")
	a.Comment("a 16x16 sprite also shows the chrs right of (wrapping in the row) and below its own: rows $1x..$3x,")
	a.Comment("columns 0..3 and $F:")
	a.LDA_long_x(0x7E0802)
	a.CLC()
	a.ADC_imm8_b(0x01)
	a.AND_imm8_b(0x0F)
	a.CMP_imm8_b(0x05)
	a.BCS("rd_chrs_next")
	a.LDA_long_x(0x7E0802)
	a.SEC()
	a.SBC_imm8_b(0x10)
	a.CMP_imm8_b(0x30)
	a.BCS("rd_chrs_next")
	a.REP(0x20)
	a.PHX()
	a.TXA()
	a.LSR()
	a.LSR()
	a.TAX()
	a.SEP(0x20)
	a.LDA_long_x(0x7E0A20)
	a.PLX()
	a.AND_imm8_b(0x02)
	a.BNE("rd_chrs_done")
	a.Label("rd_chrs_next")
	a.DEX()
	a.DEX()
	a.DEX()
	a.DEX()
	a.BPL("rd_chrs_loop")
	a.LDA_imm8_b(0x02)
	a.STA_abs(rsFreeAbs)
	a.Label("rd_chrs_done")
	a.REP(0x20)
}

// emitRemoteDrawSprite emits the subroutine that draws the sprite of the table entry at offset Y into the first
// unused OAM entry below offset X, and updates X. Called with A, X and Y 16-bit and DB = $70; preserves Y.
func emitRemoteDrawSprite(a *asm.Emitter) (addr uint32) {
	st := func(o uint32) uint16 { return uint16((remoteStateAddr + o) & 0xFFFF) }
	tb := func(o uint32) uint16 { return uint16((remoteTableAddr + o) & 0xFFFF) }

	addr = a.Label("remoteDrawSprite")
	a.AssumeREP(0x30)
	a.Comment("screen x = world x - scroll x, visible if -16 < x < 256:")
	a.LDA_abs_y(tb(rtEntries + 0))
	a.SEC()
	a.SBC_abs(st(rsScrollX))
	a.STA_abs(st(rsTmpSX))
	a.CLC()
	a.ADC_imm16_w(0x0010)
	a.CMP_imm16_w(0x0110)
	a.BCS("rds_return")
	a.Comment("screen y = world y - scroll y, visible if -16 < y < 224:")
	a.LDA_abs_y(tb(rtEntries + 2))
	a.SEC()
	a.SBC_abs(st(rsScrollY))
	a.STA_abs(st(rsTmpSY))
	a.CLC()
	a.ADC_imm16_w(0x0010)
	a.CMP_imm16_w(0x00F0)
	a.BCS("rds_return")

	a.Comment("find the next unused OAM entry (Y = $F0) below X:")
	a.Label("rds_find")
	a.DEX()
	a.DEX()
	a.DEX()
	a.DEX()
	a.BMI("rds_full")
	a.LDA_long_x(0x7E0801)
	a.AND_imm16_w(0x00FF)
	a.CMP_imm16_w(0x00F0)
	a.BNE("rds_find")

	a.LDA_abs_y(tb(rtEntries + 4))
	a.ORA_abs(st(rsAnim))
	a.STA_long_x(0x7E0802)
	a.SEP(0x20)
	a.LDA_abs(st(rsTmpSX))
	a.STA_long_x(0x7E0800)
	a.LDA_abs(st(rsTmpSY))
	a.STA_long_x(0x7E0801)
	a.Comment("extended attributes: size, x bit 8:")
	a.REP(0x20)
	a.PHX()
	a.TXA()
	a.LSR()
	a.LSR()
	a.TAX()
	a.SEP(0x20)
	a.LDA_abs(st(rsTmpSX + 1))
	a.AND_imm8_b(0x01)
	a.ORA_abs_y(tb(rtEntries + 6))
	a.STA_long_x(0x7E0A20)
	a.REP(0x20)
	a.PLX()
	a.Label("rds_return")
	a.RTS()

	a.Label("rds_full")
	a.Comment("no unused entries left; stay at the start for any following sprites:")
	a.LDX_imm16_w(0x0000)
	a.RTS()
	a.AssumeSEP(0x30)
	return
}

// emitNMIHooks emits the two routines that NMI_DoUpdates calls with JSL to upload the remote player's head and body
// tiles over chrs $20..$23 and $30..$33 when the trampoline enabled it this frame, and the upload subroutine they
// share. Both are called with A 8-bit, X and Y 16-bit, DB = $00 (or $80), D = $0000. It returns their addresses:
//
//   - late, from nmiHookPatchAddr, when core updates are enabled ($0710 = 0): right after the game uploaded the
//     follower and NPC tiles to the same chrs; DMA channels 0 and 1 are set up for VRAM writes ($1801). It runs the
//     code it was patched over.
//   - early, from nmiEarlyPatchAddr at the start of NMI_DoUpdates, when core updates are disabled ($0710 != 0) and
//     the game skips all of its sprite uploads, e.g. on every frame a dialog renders text (Text_Render). It runs the
//     code it was patched over (setting VMAIN) and sets up DMA channels 0 and 1 itself.
//
// linkGfxBankIns is the 2 byte instruction at linkGfxBankAddr that loads the bank of Link's graphics.
func emitNMIHooks(a *asm.Emitter, linkGfxBankIns []byte) (late, early uint32) {
	st := func(o uint32) uint32 { return remoteStateAddr + o }
	a.AssumeSEP(0x20)
	a.AssumeREP(0x10)

	late = a.Label("nmiHook")
	a.LDA_long(st(rsNMIEnable))
	a.BEQ("nmi_late_done")
	a.JSR_abs(0xFFFF) // fixed up below
	lateJSR := a.PC() - 2
	a.Label("nmi_late_done")
	a.Comment("the code patched over:")
	a.LDX_abs(0x0ADC)
	a.STX_abs(0x4302)
	a.RTL()

	early = a.Label("nmiHookEarly")
	a.Comment("the code patched over:")
	a.LDA_imm8_b(0x80)
	a.STA_abs(0x2115)
	a.Comment("only when core updates are disabled; otherwise the late hook uploads:")
	a.LDA_long(0x7E0710)
	a.BEQ("nmi_early_done")
	a.LDA_long(st(rsNMIEnable))
	a.BEQ("nmi_early_done")
	a.LDX_imm16_w(0x1801)
	a.STX_abs(0x4300)
	a.STX_abs(0x4310)
	a.JSR_abs(0xFFFF) // fixed up below
	earlyJSR := a.PC() - 2
	a.Label("nmi_early_done")
	a.RTL()

	upload := a.Label("nmiUpload")
	a.Comment("bank of Link's graphics, the same way NMI_DoUpdates gets it:")
	a.EmitBytes(linkGfxBankIns)
	a.STA_abs(0x4304)
	a.STA_abs(0x4314)
	for i, vram := range []uint16{remoteBodyVRAMTop, remoteBodyVRAMBottom} {
		a.REP(0x20)
		a.LDA_imm16_w(vram)
		a.STA_abs(0x2116)
		a.LDA_long(st(rsNMISrc + uint32(i)*4 + 0))
		a.STA_abs(0x4302)
		a.LDA_long(st(rsNMISrc + uint32(i)*4 + 2))
		a.STA_abs(0x4312)
		a.LDA_imm16_w(0x0040)
		a.STA_abs(0x4305)
		a.STA_abs(0x4315)
		a.SEP(0x20)
		a.LDA_imm8_b(0x03)
		a.STA_abs(0x420B)
	}
	a.Comment("the following uploads expect WRAM as the source bank:")
	a.LDA_imm8_b(0x7E)
	a.STA_abs(0x4304)
	a.STA_abs(0x4314)
	a.RTS()
	a.AssumeSEP(0x30)

	// fix up the JSRs to the upload subroutine:
	code := a.Bytes()
	base := a.GetBase()
	for _, op := range []uint32{lateJSR, earlyJSR} {
		code[op-base+0] = uint8(upload)
		code[op-base+1] = uint8(upload >> 8)
	}
	return
}

// linkGfxBankInstruction returns the instruction at linkGfxBankAddr if it is `LDA.b #imm` or `LDA.b dp`, else
// `LDA.b #$10`.
func linkGfxBankInstruction(rom *snes.ROM) []byte {
	if pak, err := lorom.BusAddressToPak(linkGfxBankAddr); err == nil && int(pak)+2 <= len(rom.Contents) {
		if op := rom.Contents[pak]; op == 0xA9 || op == 0xA5 {
			return []byte{op, rom.Contents[pak+1]}
		}
	}
	return []byte{0xA9, 0x10}
}

// remoteSprite is a sprite received from a remote player in message 0x03/0x04.
type remoteSprite struct {
	// OAM index:
	index uint8
	// OAM table bytes: x, y, chr, attributes; b4 = (size << 1) | x bit 8:
	b      [5]uint8
	size   uint8
	absidx [4]uint16
}

type remoteSprites struct {
	list     []remoteSprite
	received time.Time
	// the player's scroll offsets when the list arrived. Senders send their location (with the offsets) before their
	// sprites in the same frame, so these are the offsets the sprites were drawn with; the player's latest offsets
	// can be a frame ahead or behind, which shifts the sprites by however far the screen scrolled meanwhile:
	xoffs, yoffs int16
}

// deserializeRemoteSprites reads message 0x03 (start = 0) or 0x04 into p.Sprites, like alttpo's
// GameState::deserialize_sprites.
func (g *Game) deserializeRemoteSprites(p *Player, r io.Reader, start int) (err error) {
	var length uint8
	if err = binary.Read(r, binary.LittleEndian, &length); err != nil {
		return fmt.Errorf("error deserializing sprites: %w", err)
	}

	s := &p.Sprites
	if n := start + int(length); n <= cap(s.list) {
		s.list = s.list[:n]
	} else {
		s.list = append(s.list[:cap(s.list)], make([]remoteSprite, n-cap(s.list))...)
	}
	s.received = time.Now()
	if start == 0 {
		s.xoffs, s.yoffs = p.XOffs, p.YOffs
	}

	for i := 0; i < int(length); i++ {
		// OAM index (bit 7 = chr data follows) and 5 OAM table bytes (b4 bit 7 = palette data follows):
		var spr [6]byte
		if _, err = io.ReadFull(r, spr[:]); err != nil {
			return fmt.Errorf("error deserializing sprite %d: %w", i, err)
		}

		sp := remoteSprite{index: spr[0] & 0x7F}
		copy(sp.b[:], spr[1:6])
		sp.b[4] &= 0x7F
		// matches alttpo's Sprite::decodeOAMTableBytes; size is not decoded for disabled sprites:
		sp.size = (sp.b[4] >> 1) & 1
		if sp.b[1] == 0xF0 {
			sp.size = 0
		}

		// uniq tile absolute indices:
		n := 1
		if sp.size != 0 {
			n = 4
		}
		sp.absidx = [4]uint16{noUniqTile, noUniqTile, noUniqTile, noUniqTile}
		for k := 0; k < n; k++ {
			if sp.absidx[k], err = readU16(r); err != nil {
				return fmt.Errorf("error deserializing sprite %d uniqtile indices: %w", i, err)
			}
		}

		if spr[0]&0x80 != 0 {
			// sprite graphics data 4bpp; 1 or 4 chrs of 32 bytes each:
			if err = discard(r, 32*n); err != nil {
				return fmt.Errorf("error deserializing sprite %d gfx: %w", i, err)
			}
		}
		if spr[5]&0x80 != 0 {
			// palette data:
			if err = discard(r, 32); err != nil {
				return fmt.Errorf("error deserializing sprite %d palette: %w", i, err)
			}
		}

		s.list[start+i] = sp
	}

	return
}

// isLinkBodySprite reports whether sp is one of Link's head and body sprites: 16x16, drawn from chrs $00..$03 and
// $10..$13, and not disabled.
func isLinkBodySprite(sp *remoteSprite) bool {
	if sp.size == 0 || sp.b[1] == 0xF0 {
		return false
	}
	chr := uint16(sp.b[2]) | uint16(sp.b[3]&1)<<8
	return chr < 0x20 && chr&0x0F < 0x04
}

// hasLinkBody reports whether a remote player's last sprite list has Link's head or body. alttpo always sends Link's
// 12 reserved OAM entries, so while Link is invisible (flickering) the list still has his shadow but no head or body.
func hasLinkBody(list []remoteSprite) bool {
	for i := range list {
		if isLinkBodySprite(&list[i]) {
			return true
		}
	}
	return false
}

// remoteBodySprites returns up to two 16x16 sprites of Link's head and body from a remote player's sprites, ordered by
// OAM index (front to back).
func remoteBodySprites(list []remoteSprite) (body []remoteSprite) {
	sorted := make([]remoteSprite, len(list))
	copy(sorted, list)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].index < sorted[j].index })

	for _, sp := range sorted {
		if !isLinkBodySprite(&sp) {
			continue
		}
		// both rows of tiles must be from LinkGraphics, uploaded 2 tiles at a time:
		ok := true
		for _, idx := range sp.absidx {
			if idx >= linkUniqTileCount {
				ok = false
			}
		}
		if !ok || sp.absidx[1] != sp.absidx[0]+1 || sp.absidx[3] != sp.absidx[2]+1 {
			continue
		}
		body = append(body, sp)
		if len(body) == 2 {
			break
		}
	}
	return
}

// remoteExtraSprites returns up to max sprites with captured chrs (Link's shadow or the water ripples or grass under
// his feet, sparkles, dust, projectiles, carried and thrown objects; see captureChrs) from a remote player's sprites,
// ordered by OAM index.
func remoteExtraSprites(list []remoteSprite, max int) (extras []remoteSprite) {
	sorted := make([]remoteSprite, len(list))
	copy(sorted, list)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].index < sorted[j].index })

	for _, sp := range sorted {
		if len(extras) >= max {
			break
		}
		if sp.b[1] == 0xF0 || sp.b[3]&1 != 0 || !(isCaptureChr(sp.b[2]) || isLinkFootChr(uint16(sp.b[2]))) {
			continue
		}
		extras = append(extras, sp)
	}
	return
}

// isBadTimeToDraw reports whether a player is in a module where alttpo doesn't draw or show them; same as alttpo's
// GameState::is_it_a_bad_time.
func (p *Player) isBadTimeToDraw() bool {
	m := p.Module
	if m <= 0x05 || m == 0x14 || m >= 0x1B {
		return true
	}
	return m == 0x0E && p.SubModule == 0x07
}

// locationsEqual is alttpo's locations_equal: in the underworld, light and dark world don't matter.
func locationsEqual(a, b uint32) bool {
	if a == b {
		return true
	}
	if a&0x010000 != 0 && b&0x010000 != 0 {
		return a&0xFFFF == b&0xFFFF
	}
	return false
}

type remoteSpritesLocal struct {
	// table last written to SRAM, with rtTTL = 0, and when:
	written     [remoteTableSize]byte
	writtenOK   bool
	writtenTime time.Time

	// player and body sprite counts last logged, and counts that differ from them and since when; a count is logged
	// once it holds for remoteLogSettle, so flickering players don't spam the log:
	logged, pending [2]int
	loggedOK        bool
	pendingSince    time.Time
}

const remoteLogSettle = 500 * time.Millisecond

// logRemoteCounts logs the number of players and body sprites drawn once it changed and held for remoteLogSettle.
func (s *remoteSpritesLocal) logRemoteCounts(t *[remoteTableSize]byte, now time.Time) {
	var c [2]int
	c[0], c[1] = remoteTableCounts(t)
	if s.loggedOK && c == s.logged {
		s.pending = c
		return
	}
	if c != s.pending {
		s.pending, s.pendingSince = c, now
		return
	}
	if now.Sub(s.pendingSince) < remoteLogSettle {
		return
	}
	log.Printf("alttp: remote sprites: drawing %d players (%d body sprites)\n", c[0], c[1])
	s.logged, s.loggedOK = c, true
}

// remoteBodyOK reports whether the patched ROM has the NMI hooks that upload the body tiles: JSLs into the free ROM
// the patcher uses.
func (g *Game) remoteBodyOK() bool {
	for _, addr := range []uint32{nmiHookPatchAddr, nmiEarlyPatchAddr} {
		pak, err := lorom.BusAddressToPak(addr)
		if err != nil || int(pak)+4 > len(g.rom.Contents) {
			return false
		}
		c := g.rom.Contents[pak : pak+4]
		target := uint32(c[1]) | uint32(c[2])<<8 | uint32(c[3])<<16
		if c[0] != 0x22 || !isFreeROM(target) {
			return false
		}
	}
	return true
}

// buildRemoteSpritesTable makes the remote sprites table for the local player's current location, with both TTLs 0.
// draw reports whether there is anything to draw; capture whether any remote player is in the same location (and
// would see the local player's sparkles and dust).
func (g *Game) buildRemoteSpritesTable(now time.Time) (t [remoteTableSize]byte, draw, capture bool) {
	local := g.local
	// the game itself stops drawing while the overworld map is shown (see emitRemoteDraw), so the mode 7 map does not
	// count as a bad time for the local player; it keeps drawing remote players while the map opens and closes:
	if local == nil || !local.IsInGame() || (local.isBadTimeToDraw() && !(local.Module == 0x0E && local.SubModule == 0x07)) {
		return
	}

	remotes := append([]*Player(nil), g.RemotePlayers()...)
	sort.Slice(remotes, func(i, j int) bool { return remotes[i].Index() < remotes[j].Index() })

	// players to draw, the one drawn with Link's head and body first:
	var players []*Player
	var body []remoteSprite
	bodyOK := g.remoteBodyOK()
	for _, p := range remotes {
		// alttpo's rule for remote players: IsInGame() can't be used since it depends on the module before a dialog or
		// menu, which remote players don't send, and remote players would disappear during dialogs:
		if p == local || p.TTL() <= 0 || p.isBadTimeToDraw() {
			continue
		}
		// a player scrolling to another room or area, or warping to the other world (the game switches their area on
		// the first frame of the warp, Module09_MirrorWarp), is leaving it; their reported location only changes once
		// that finishes (canSampleLocation):
		if p.isInScreenTransition() {
			continue
		}
		if !locationsEqual(local.Location, p.Location) {
			continue
		}
		capture = true
		if !g.SyncSprites || len(players) >= remoteBlobMax {
			continue
		}
		// players that stopped sending sprites left the room or area:
		if now.Sub(p.Sprites.received) >= remoteSpritesStale {
			continue
		}
		// Link is invisible (e.g. flickering after a warp): draw nothing for this player, not even a blob or his
		// shadow, which would flash in and out with the flicker:
		if !hasLinkBody(p.Sprites.list) {
			continue
		}
		if bodyOK && body == nil {
			if body = remoteBodySprites(p.Sprites.list); body != nil {
				players = append([]*Player{p}, players...)
				continue
			}
		}
		players = append(players, p)
	}
	if len(players) == 0 {
		return
	}
	draw = true

	if local.Location&0x010000 != 0 {
		t[rtIndoors] = 1
	}
	binary.LittleEndian.PutUint16(t[rtLocation:], uint16(local.Location))

	o := rtEntries
	entry := func(x, y uint16, chr, attr, size uint8) {
		binary.LittleEndian.PutUint16(t[o+0:], x)
		binary.LittleEndian.PutUint16(t[o+2:], y)
		t[o+4], t[o+5], t[o+6] = chr, attr, size<<1
		o += rtEntrySize
	}
	// world coordinates of a remote sprite:
	world := func(p *Player, sp remoteSprite) (uint16, uint16) {
		x := int(sp.b[0]) | int(sp.b[4]&1)<<8
		if x >= 0x100 {
			x -= 0x200
		}
		y := int(sp.b[1])
		if y >= 0xE0 {
			y -= 0x100
		}
		return uint16(int(p.Sprites.xoffs) + x), uint16(int(p.Sprites.yoffs) + y)
	}

	// every player's shadow, sparkles, dust, projectiles and carried and thrown objects; those the body's player
	// draws in front of Link (with a lower OAM index than the body) are drawn after everything else:
	type extra struct {
		p  *Player
		sp remoteSprite
	}
	var behind, front []extra
	for _, p := range players {
		if now.Sub(p.Sprites.received) >= remoteSpritesStale {
			continue
		}
		for _, sp := range remoteExtraSprites(p.Sprites.list, remoteExtraMax-len(behind)-len(front)) {
			if body != nil && p == players[0] && sp.index < body[0].index {
				front = append(front, extra{p, sp})
			} else {
				behind = append(behind, extra{p, sp})
			}
		}
	}
	// the sprite in front (lowest OAM index) is drawn last, into a lower OAM index:
	extras := func(list []extra) {
		for j := len(list) - 1; j >= 0; j-- {
			x, y := world(list[j].p, list[j].sp)
			entry(x, y, list[j].sp.b[2], list[j].sp.b[3], list[j].sp.size)
		}
	}
	extras(behind)
	binary.LittleEndian.PutUint16(t[rtExtrasEnd:], uint16(o-rtEntries))

	if body != nil {
		p := players[0]
		for j := range body {
			// the body sprite in front (lowest OAM index) is drawn last, into a lower OAM index:
			sp := body[len(body)-1-j]
			x, y := world(p, sp)
			entry(x, y, uint8(remoteBodyChr+2*j), sp.b[3]&^1, 1)
			binary.LittleEndian.PutUint16(t[rtSrc+2*j:], 0x8000+sp.absidx[0]<<5)
			binary.LittleEndian.PutUint16(t[rtSrc+4+2*j:], 0x8000+sp.absidx[2]<<5)
		}
		if len(body) == 1 {
			copy(t[rtSrc+2:rtSrc+4], t[rtSrc+0:rtSrc+2])
			copy(t[rtSrc+6:rtSrc+8], t[rtSrc+4:rtSrc+6])
		}
	}
	binary.LittleEndian.PutUint16(t[rtBodyEnd:], uint16(o-rtEntries))

	// a blob over each player's lower 16 pixels:
	for _, p := range players {
		entry(p.X, p.Y+8, remoteBlobChr0, remoteBlobAttr, 1)
	}
	binary.LittleEndian.PutUint16(t[rtEnd:], uint16(o-rtEntries))

	extras(front)
	binary.LittleEndian.PutUint16(t[rtFrontEnd:], uint16(o-rtEntries))
	return
}

// remoteTableCounts returns the number of players and body sprites in a remote sprites table.
func remoteTableCounts(t *[remoteTableSize]byte) (players, body int) {
	shadowEnd := int(binary.LittleEndian.Uint16(t[rtExtrasEnd:]))
	bodyEnd := int(binary.LittleEndian.Uint16(t[rtBodyEnd:]))
	end := int(binary.LittleEndian.Uint16(t[rtEnd:]))
	return (end - bodyEnd) / rtEntrySize, (bodyEnd - shadowEnd) / rtEntrySize
}

// remoteTablePak is the FX Pak Pro address of the remote sprites table:
var remoteTablePak = func() uint32 {
	pak, err := lorom.BusAddressToPak(remoteTableAddr)
	if err != nil {
		panic(err)
	}
	return pak
}()

// writeRemoteSprites writes the remote sprites table to SRAM when it changed or its TTLs need refreshing.
func (g *Game) writeRemoteSprites(now time.Time) {
	if !g.ROMVerified || g.IsResetting() {
		return
	}
	q := g.getQueue()
	if q == nil {
		return
	}

	s := &g.remoteSprites
	t, draw, capture := g.buildRemoteSpritesTable(now)
	s.logRemoteCounts(&t, now)
	if capture {
		// only for comparing with the last write; the game counts it down:
		t[rtCaptureTTL] = 1
	}
	if s.writtenOK && t == s.written {
		if !(draw || capture) || now.Sub(s.writtenTime) < remoteTableRefresh {
			return
		}
	}

	data := t
	if draw {
		data[rtTTL] = remoteTTLFrames
	}
	if capture {
		data[rtCaptureTTL] = remoteTTLFrames
	}
	if err := g.enqueueUnlocked(func() error {
		return q.MakeWriteCommands(
			[]snes.Write{{Address: remoteTablePak, Size: remoteTableSize, Data: data[:]}},
			nil,
		).EnqueueTo(q)
	}); err != nil {
		log.Printf("alttp: remote sprites: error enqueuing snes write: %v\n", err)
		return
	}
	s.written, s.writtenOK, s.writtenTime = t, true, now
}

// Captured sprites: sword sparkles, dash dust, projectiles, carried and thrown objects
//
// alttpo players see everything o2 sends of the local player (see sprites.go). Besides Link's own OAM entries, o2 sends
// the sparkles of charging a spin attack, spinning and poking walls with the sword, the dust of dashing, Link's
// projectiles (arrows, boomerang, bombs, fire and ice rod shots, sword beams, Cane of Somaria blocks and blasts, and
// their impacts) and the objects he carries and throws (pots, bushes, rocks, signs and their debris). These are
// ancillae, sprites and garnishes drawn into whichever OAM entries the game hands out, so the trampoline finds them by
// their chrs and copies up to linkOAMExtrasMax of them to SRAM after the copy of Link's entries.
//
// Only chrs in sprite page 0 (name table 0, chrs $000..$0FF) that no NMI DMA overwrites are captured: Link's, his
// items' and the bird's tiles ($00..$1F), and the follower and NPC tiles ($20..$25, $30..$35) change every frame. The
// rest of the first sheet ($26..$2F, $36..$3F) is static, and $40..$FF are the common sprite sheets, which are loaded
// per area and the same for everyone in that area. Receivers draw them from their own VRAM.
//
// The scan of the OAM buffer only runs on frames where one of those ancillae, a carried (state $0A) or thrown
// scenery ($EC) sprite, or thrown object debris (garnish $16) exists, and while o2 keeps rtCaptureTTL up because a
// remote player is in the same location. Matching chrs from other sprites on screen at the same time (e.g. an
// enemy's explosion) are captured too, like alttpo does.

// SRAM and ROM addresses of the capture tables and routine:
const (
	// captureTypesAddr is a table in SRAM of the ancilla types $00..$3F whose sprites are captured, built from
	// captureAncillae by the init code: 1 for most, 3 for captureShadowCasters:
	captureTypesAddr = sramZeroAddr
	// captureChrsAddr is a table in SRAM of the chrs $00..$FF that are captured, built from captureChrs: 1 for most,
	// 2 for captureShadowChrs:
	captureChrsAddr = captureTypesAddr + 0x40
	// captureMaskAddr is the bits of captureChrsAddr captured this frame (u16; the high byte stays 0):
	captureMaskAddr = sramZeroEnd - 2

	// the sizes of the lists in ROM that the init code builds the tables from, padded with their last entry (see
	// captureLists). The patcher places them and the capture routine in free ROM:
	captureTypeListSize = 0x18
	captureChrListSize  = 0x40
)

// captureAncillae are the ancilla types whose sprites are captured:
var captureAncillae = []uint8{
	0x01, // Cane of Somaria blast
	0x02, // fire rod shot
	0x04, // sword beam hit
	0x05, // boomerang
	0x06, // boomerang and hookshot wall hit
	0x07, // bomb, including carried and thrown
	0x09, // arrow
	0x0A, // arrow stuck in a wall
	0x0B, // ice rod shot
	0x0C, // sword beam
	0x0D, // spin attack fully charged spark
	0x11, // ice rod wall hit
	0x13, // ice rod sparkle
	0x1B, // sword tapping (poking) a wall
	0x1E, // dash dust
	0x26, // sword swing sparkle (stabbing with the tempered sword or better)
	0x2A, // spin attack sparkle
	0x2B, // spin attack sparkle
	0x2C, // Cane of Somaria block, including carried and thrown
	0x2D, // Cane of Somaria block fizzle
	0x2E, // Cane of Somaria block splitting into blasts
	0x2F, // lamp flame
	0x3C, // spin attack charging sparkle
}

// captureChrs are the page 0 chrs those draw with; none are subject to per-frame DMA:
var captureChrs = []uint16{
	// sword sparkles, wall hits, sword beam, ice rod:
	0x80, 0x81, 0x82, 0x83, 0x90, 0x92, 0x93, 0xB6, 0xB7, 0xB9, 0xD7,
	// dash dust, ice rod spread:
	0xA9, 0xCF, 0xDF,
	// boomerang, arrows:
	0x26, 0x2A, 0x2B, 0x2C, 0x2D, 0x3A, 0x3B, 0x3C, 0x3D,
	// fire rod shot and its flame when it hits (also the lamp flame's):
	0x8D, 0x8E, 0x9C, 0x9D, 0xA0, 0xA2, 0xA4, 0xA5,
	// lamp flame:
	0xB2, 0xB3, 0xE3, 0xF3,
	// sword beam hit, the four sparks flying apart diagonally (Ancilla04_BeamHit draws its chrs $53/$54 + $82); $D6 is
	// also a spin attack and Cane of Somaria spark:
	0xD5, 0xD6,
	// bomb and its explosion:
	0x6E, 0x84, 0x86, 0x88, 0x8A, 0x8C, 0x9B,
	// Cane of Somaria blast, block, fizzle, split:
	0x40, 0x41, 0x43, 0x50, 0x51, 0x52, 0xE9, 0xF9, 0xC4, 0xC5, 0xC6, 0xD2,
	// carried and thrown bush, pot, rock, sign, large rock:
	0x42, 0x44, 0x46, 0x4A,
	// their debris:
	0x48, 0x58, 0x59,
}

// captureShadowChrs are the shadows under bombs and carried and thrown objects. Enemies and NPCs use the same
// shadows, so they are only captured while one of those exists (captureShadowCasters, or a carried or thrown sprite),
// and o2 only sends the exact shadow entries that the object itself drew (see isCasterShadow).
var captureShadowChrs = []uint16{0x6C, 0x28, 0x38}

// captureShadowCasters are the ancilla types that cast those shadows: bombs and Cane of Somaria blocks (which draw
// none, but are also carried and thrown):
var captureShadowCasters = []uint8{0x07, 0x2C}

// isCaptureChr reports whether chr is one of captureChrs or captureShadowChrs.
func isCaptureChr(chr uint8) bool {
	for _, c := range append(captureChrs, captureShadowChrs...) {
		if uint16(chr) == c {
			return true
		}
	}
	return false
}

func isCaptureShadowChr(chr uint8) bool {
	for _, c := range captureShadowChrs {
		if uint16(chr) == c {
			return true
		}
	}
	return false
}

// oamX returns the signed 9-bit X coordinate of an OAM entry.
func oamX(sp *linkSprite) int {
	x := int(sp.b[0]) | int(sp.b[4]&1)<<8
	if x >= 0x100 {
		x -= 0x200
	}
	return x
}

// isCasterShadow reports whether the captured shadow sh is one that a captured object in sprites drew for itself, the
// way the game draws them; any other shadow belongs to an enemy or NPC on screen at the same time:
//
//   - a bomb ($6E, 16x16, Bomb_Draw) draws two 8x8 halves ($6C, then $6C flipped 8 pixels to the right) or a single
//     8x8 $38 4 pixels to the right while in the air, in the OAM entries right after it (or the two before it while
//     on grass or in shallow water);
//   - a carried or thrown bush, pot or rock (sprite $EC, SpriteDraw_SingleLarge) draws a 16x16 $6C at the same X in
//     the next OAM entry (sprite_flags2 & $1F = 1);
//   - a large rock (SpriteDraw_ThrownItem_Gigantic) draws four 16x16 $4A around its center (X -8 and +8, the left
//     ones unflipped) and three 16x16 $6C with attributes $24 at X -6, 0 and +6 from its center, in another OAM
//     region. Enemy shadows always use palette 4 ($08).
func isCasterShadow(sh linkSprite, sprites []linkSprite) bool {
	sx := oamX(&sh)
	for i := range sprites {
		o := &sprites[i]
		if o.b[3]&1 != 0 || o.b[1] == 0xF0 {
			continue
		}
		ox := oamX(o)
		di := int(sh.index) - int(o.index)
		switch o.b[2] {
		case 0x6E:
			if o.size == 0 || sh.size != 0 || di == 0 || di < -2 || di > 2 {
				continue
			}
			if (sh.b[2] == 0x6C && (sx == ox || sx == ox+8)) || (sh.b[2] == 0x38 && sx == ox+4) {
				return true
			}
		case 0x42, 0x44, 0x46:
			if o.size != 0 && sh.size != 0 && sh.b[2] == 0x6C && di == 1 && sx == ox {
				return true
			}
		case 0x4A:
			if o.size == 0 || sh.size == 0 || sh.b[2] != 0x6C || sh.b[3] != 0x24 {
				continue
			}
			cx := ox + 8
			if o.b[3]&0x40 != 0 {
				cx = ox - 8
			}
			if d := sx - cx; d == -6 || d == 0 || d == 6 {
				return true
			}
		}
	}
	return false
}

// captureLists returns the ROM lists of captureAncillae and captureChrs, padded with their last entries.
func captureLists() (types [captureTypeListSize]byte, chrs [captureChrListSize]byte) {
	if len(captureAncillae) > len(types) || len(captureChrs) > len(chrs) {
		panic("capture lists too long")
	}
	for i := range types {
		types[i] = captureAncillae[min(i, len(captureAncillae)-1)]
	}
	for i := range chrs {
		chrs[i] = uint8(captureChrs[min(i, len(captureChrs)-1)])
	}
	return
}

// emitInitCaptureTables emits the init code that zeroes sramZeroAddr..sramZeroEnd and builds the capture tables
// there from the lists at typeList and chrList in ROM. Runs with A, X and Y 16-bit.
func emitInitCaptureTables(a *asm.Emitter, typeList, chrList uint32) {
	a.Comment("zero the capture tables, the copy of Link's OAM entries and the remote sprites table and state:")
	a.LDA_imm16_w(0x0000)
	a.STA_long(sramZeroAddr)
	a.LDX_imm16_w(uint16(sramZeroAddr & 0xFFFF))
	a.LDY_imm16_w(uint16((sramZeroAddr + 1) & 0xFFFF))
	a.LDA_imm16_w(uint16(sramZeroEnd - sramZeroAddr - 2))
	a.MVN(uint8(sramZeroAddr>>16), uint8(sramZeroAddr>>16))
	a.Comment("build the capture tables; DB = $70 after MVN:")
	for _, l := range []struct {
		list, size, table uint32
	}{
		{typeList, captureTypeListSize, captureTypesAddr},
		{chrList, captureChrListSize, captureChrsAddr},
	} {
		loop := fmt.Sprintf("init_capture_%06x", l.table)
		a.LDX_imm16_w(uint16(l.size - 1))
		a.Label(loop)
		a.LDA_long_x(l.list)
		a.AND_imm16_w(0x00FF)
		a.TAY()
		a.SEP(0x20)
		a.LDA_imm8_b(0x01)
		a.STA_abs_y(uint16(l.table & 0xFFFF))
		a.REP(0x20)
		a.DEX()
		a.BPL(loop)
	}
	a.Comment("shadows, and the ancillae that cast them:")
	a.SEP(0x20)
	a.LDA_imm8_b(0x02)
	for _, c := range captureShadowChrs {
		a.STA_abs(uint16((captureChrsAddr + uint32(c)) & 0xFFFF))
	}
	a.LDA_imm8_b(0x03)
	for _, t := range captureShadowCasters {
		a.STA_abs(uint16((captureTypesAddr + uint32(t)) & 0xFFFF))
	}
	a.REP(0x20)
}

// emitCaptureExtras emits the routine that copies captured OAM entries to linkOAMCopyAddr + linkOAMCopyExtras, after
// storing the scroll offsets at linkOAMCopyScroll. Called by the trampoline with JSL with A and X 8-bit; preserves P
// and DB. It runs from any bank.
func emitCaptureExtras(a *asm.Emitter) {
	a.Label("captureExtras")
	ex := linkOAMCopyAddr + linkOAMCopyExtras

	a.PHP()
	a.PHB()
	a.REP(0x30)
	a.Comment("scroll offsets Link's copied OAM entries were drawn with (linkOAMCopyScroll):")
	a.LDA_long(0x7E00E2)
	a.SEC()
	a.SBC_long(0x7E011A)
	a.STA_long(linkOAMCopyAddr + linkOAMCopyScroll)
	a.LDA_long(0x7E00E8)
	a.SEC()
	a.SBC_long(0x7E011C)
	a.STA_long(linkOAMCopyAddr + linkOAMCopyScroll + 2)
	a.LDA_imm16_w(0x0000)
	a.STA_long(ex)
	a.SEP(0x30)
	a.Comment("only while a remote player is in the same location:")
	a.LDA_long(remoteTableAddr + rtCaptureTTL)
	a.BEQ("ce_exit")
	a.SEC()
	a.SBC_imm8_b(0x01)
	a.STA_long(remoteTableAddr + rtCaptureTTL)

	a.Comment("which chrs to capture (captureMaskAddr): 1 while a captured ancilla exists, 2 (shadows) while one that")
	a.Comment("casts a shadow does:")
	a.PEA(0x7070)
	a.PLB()
	a.PLB()
	mask := uint16(captureMaskAddr & 0xFFFF)
	a.STZ_abs(mask)
	a.LDX_imm8_b(0x09)
	a.Label("ce_ancilla")
	a.LDA_long_x(0x7E0C4A)
	a.CMP_imm8_b(0x40)
	a.BCS("ce_next_ancilla")
	a.TAY()
	a.LDA_abs_y(uint16(captureTypesAddr & 0xFFFF))
	a.TSB_abs(mask)
	a.Label("ce_next_ancilla")
	a.DEX()
	a.BPL("ce_ancilla")
	a.Comment("a carried sprite or thrown scenery (both, with shadows):")
	a.LDX_imm8_b(0x0F)
	a.Label("ce_sprite")
	a.LDA_long_x(0x7E0DD0)
	a.BEQ("ce_next_sprite")
	a.CMP_imm8_b(0x0A)
	a.BEQ("ce_sprite_found")
	a.LDA_long_x(0x7E0E20)
	a.CMP_imm8_b(0xEC)
	a.BNE("ce_next_sprite")
	a.Label("ce_sprite_found")
	a.LDA_imm8_b(0x03)
	a.TSB_abs(mask)
	a.BRA("ce_sprites_done")
	a.Label("ce_next_sprite")
	a.DEX()
	a.BPL("ce_sprite")
	a.Label("ce_sprites_done")
	a.LDA_abs(mask)
	a.BNE("ce_scan")
	a.Comment("thrown object debris:")
	a.LDA_long(0x7E0FB4)
	a.BEQ("ce_exit")
	a.LDX_imm8_b(0x1D)
	a.Label("ce_garnish")
	a.LDA_long_x(0x7FF800)
	a.CMP_imm8_b(0x16)
	a.BEQ("ce_garnish_found")
	a.DEX()
	a.BPL("ce_garnish")
	a.Label("ce_exit")
	a.PLB()
	a.PLP()
	a.RTL()
	a.Label("ce_garnish_found")
	a.LDA_imm8_b(0x01)
	a.TSB_abs(mask)

	a.Label("ce_scan")
	a.Comment("DB = $7E for the OAM buffer; Y = OAM buffer offset:")
	a.PEA(0x7E7E)
	a.PLB()
	a.PLB()
	a.REP(0x30)
	a.LDY_imm16_w(0x01FC)
	a.Label("ce_loop")
	a.Comment("drawn this frame (most entries are not):")
	a.LDA_abs_y(0x0800)
	a.AND_imm16_w(0xFF00)
	a.CMP_imm16_w(0xF000)
	a.BEQ("ce_next")
	a.Comment("a captured chr in name table 0:")
	a.LDA_abs_y(0x0802)
	a.BIT_imm16_w(0x0100)
	a.BNE("ce_next")
	a.AND_imm16_w(0x00FF)
	a.TAX()
	a.LDA_long_x(captureChrsAddr)
	a.AND_long(captureMaskAddr)
	a.BEQ("ce_next")
	a.Comment("not one of Link's own entries, which are copied already:")
	a.TYA()
	a.SEC()
	a.SBC_abs(0x0352)
	a.CMP_imm16_w(linkOAMCount * 4)
	a.BCC("ce_next")
	a.Comment("copy x, y, chr, attributes, extended attributes and the OAM index:")
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
	a.BCS("ce_done")
	a.Label("ce_next")
	a.DEY()
	a.DEY()
	a.DEY()
	a.DEY()
	a.BPL("ce_loop")
	a.Label("ce_done")
	a.PLB()
	a.PLP()
	a.RTL()
	a.AssumeSEP(0x30)
}
