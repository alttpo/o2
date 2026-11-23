package alttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math/bits"
	"o2/client/protocol05"
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
//     graphics.
//   - The same player's sword and shield are drawn in their pose with the local player's sword and shield tiles, which
//     the game keeps in WRAM $7E9000..$7E947F (see swordshield.go), when the player's sword or shield is the same
//     level. A sword or shield the local player doesn't have is not drawn. The item in use (rod, hammer, bow,
//     hookshot, net, cane, book, shovel...) is drawn the same way from the item buffer the game builds in WRAM
//     $7E9480..$7EA17F. With the head and body, at most remotePartMax of these get a part: the later ones in the
//     order sword, shield, item are left out.
//   - With the player's own graphics: players bind their chrs to tiles of their Link graphics fact (fact 0x00, see
//     sprites.go and linkfact.go). Once o2 has a player's fact and it differs from the local player's (a custom Link
//     sprite), or the player's sword or shield is another level, o2 copies the rows of tiles the parts show from the
//     fact into an SRAM buffer (remoteTilesAddr) and the NMI hooks upload them from there. Until the fact is here, the
//     player is drawn with the local graphics as above.
//   - The shadow (chr $6C on the ground, $28 or $38 while recoiling or jumping off a ledge), sword sparkles and dash
//     dust (captureChrs) that the same player sends are drawn too, from the local static and common sprite sheets.
//   - Every other remote player in the same location is drawn as a single 16x16 sprite at their coordinates, chr $64
//     or $66 alternating every 8 frames: an animated blob with eyes from the common sprite sheet, with a 16x16 shadow
//     (chr $6C) under it like the game draws under enemies.
//
// VRAM: the head, body, sword, shield and item are each a "part" of 2x2 chrs that a NMI hook uploads into a VRAM slot, from
// LinkGraphics, WRAM or the SRAM buffer of rows from remote players' facts. The
// slots (remoteSlots) are chrs that the game itself uploads in every NMI with core updates enabled: Link's sword,
// shield and item chrs and the chrs that followers (Zelda, the old man, the bomb...) and a few NPC sprites with large
// animations (Kiki, the crystal maidens, the old mountain man, the returning smithy, Blind's maiden form) use. Each
// frame, after the module drew everything, the game scans the OAM buffer for the slots that any sprite shows and
// hands the rest out to the parts; overwriting a slot that nothing shows is harmless, since the game uploads its own
// tiles there again in the next NMI. If the head and body don't both get a slot, the player is drawn as a blob; a sword
// or shield without a slot is not drawn. Only parts that are drawn on screen are uploaded, and only the rows of chrs
// their sprites show. While the game disables its own uploads ($0710 != 0: dialogs rendering text, room and area
// loads), Link's own chrs are not handed out (Link may show them again before the game uploads them), and rows that
// are still in VRAM from the last upload are not uploaded again.
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
// nothing is drawn in the wrong place while o2 catches up with a room change. The table is at most 255 bytes, so it is
// written with a single FX Pak Pro VPUT and the game never draws half of an update.

// SRAM layout of the remote sprites table, written by o2 (zeroed by the init code):
const (
	remoteTableAddr = uint32(0x706AC0)

	// frames left to draw the table, counted down every frame:
	rtTTL = 0
	// frames left to capture the local player's sparkles and dust for o2 to send (see emitCaptureExtras), counted down
	// every frame; o2 keeps it up while any remote player is in the same location:
	rtCaptureTTL = 1
	// $1B (1 = underworld) and $A0 (underworld room) or $8A (overworld area) the table is for:
	rtIndoors = 2
	// the number of parts, starting with the first, that must all get a VRAM slot for the slotted sprites to be drawn
	// (Link's head and body); 0 if there are none:
	rtRequired = 3
	rtLocation = 4
	// byte offsets into rtEntries of the end of each list of sprites, drawn in this order, each in front of the ones
	// before. Each list holds one kind of sprite, so that remoteFrame draws it with a loop for that kind:
	//
	//   - the extra sprites (shadows, sparkles, dust, projectiles, carried and thrown objects; see captureChrs) the
	//     remote players draw behind Link;
	//   - the slotted sprites: the head, body, sword, shield and item of the first player, drawn if the required parts got a
	//     VRAM slot;
	//   - the shadows under the blobs, the first player's first if there are slotted sprites (it is skipped when they
	//     are drawn);
	//   - the blobs, in the same order as their shadows;
	//   - the extra sprites the first player draws in front of Link.
	rtExtrasEnd  = 6
	rtSlottedEnd = 8
	rtShadowsEnd = 10
	rtBlobsEnd   = 12
	rtFrontEnd   = 14
	// the number of parts (u8):
	rtParts = 16
	// the first scanline on which drawing the table starts too late to finish before the NMI (rfDrawLateLine of its
	// lists), computed by o2 when it makes the table, which knows how many sprites of each kind it holds:
	rtDrawLate = 17
	// per part, the DMA source addresses of its top and bottom row of chrs:
	rtPartSrc = 18
	// per part, partTopRow, partBottomRow and partWRAM:
	rtPartFlags = rtPartSrc + remotePartMax*4
	// sprites to draw: world x, world y, chr, attributes, and the extended attributes but for x bit 8 (entrySize16).
	// The chr of a slotted sprite is relative to its part's VRAM slot: bits 6-7 are the part and bits 0 and 4 the chr
	// in its 2x2 chrs:
	rtEntries   = rtPartFlags + remotePartMax
	rtEntrySize = 7

	remotePartMax  = 4
	remoteExtraMax = 14
	remoteBlobMax  = 8
	remoteEntryMax = (0xFF - rtEntries) / rtEntrySize

	remoteTableSize = rtEntries + remoteEntryMax*rtEntrySize
)

// part flags:
const (
	// the sprites show the top or bottom row of the part's chrs, which is uploaded:
	partTopRow    = 0x01
	partBottomRow = 0x02
	// the part's tiles are in WRAM bank $7E (the sword, shield and item), else in LinkGraphics:
	partWRAM = 0x80
	// the part's tiles are in the SRAM buffer of rows from remote players' facts (remoteTilesAddr), else as partWRAM
	// says. With partWRAM, the bits of the VRAM word address of rsNMIList's rows that say where the source is:
	partSRAM = 0x20
)

// the SRAM buffer of rows of tiles from remote players' facts, which o2 writes and the NMI hooks upload (see
// remoteTileRows): remoteTileRowMax rows of 2 tiles. A part's row is a DMA source address in it, with partSRAM:
const (
	remoteTilesAddr   = uint32(0x706D80)
	remoteTileRowSize = 0x40
	remoteTileRowMax  = 10
	remoteTilesEnd    = remoteTilesAddr + remoteTileRowMax*remoteTileRowSize
)

// the buffer is between the rupee log and the run timing, in the bank of the remote sprites table and state:
const _ = remoteTilesAddr - rupeeLogEnd
const _ = runTimingAddr - remoteTilesEnd
const _ = uint8(remoteTilesAddr>>16) - uint8(remoteStateAddr>>16)

// the extended attributes of an entry:
const (
	// a 16x16 sprite:
	entrySize16 = 0x02
)

// SRAM layout of the remote sprites state, written by the game code (zeroed by the init code):
const (
	remoteStateAddr = remoteTableAddr + 0x100

	// local scroll offsets: BG2 scroll ($E2, $E8) - shake offsets ($011A, $011C):
	rsScrollX = 0
	rsScrollY = 2
	// screen coordinates of the sprite being drawn:
	rsTmpSX = 4
	rsTmpSY = 6
	// added to the chr of blobs: 0 or 2, alternating every 8 frames:
	rsBlobAnim = 8
	// scratch:
	rsTmpChr   = 10
	rsTmpAdd   = 12
	rsTmpPart  = 14
	rsTmpExt   = 16
	rsTmpShift = 18
	rsTmpVRAM  = 20
	rsTmpFlags = 22
	rsTmpEnd   = rsBodyOK + 2
	// bits of the slots that sprites in the OAM buffer show this frame:
	rsUsed = 24
	// bits of the parts drawn on screen this frame:
	rsDrawnParts = 26
	// non-zero if the required parts got a VRAM slot:
	rsBodyOK = 28
	// per part, the first chr of the slot it got this frame, $FF if none:
	rsPartSlot = rsTmpEnd + 2
	// the byte length of the list of rows for the NMI hooks to upload (u16):
	rsNMILen = rsPartSlot + remotePartMax
	// how many more frames in a row a late drawing check may replay the last frame drawn (see remoteReplayAddr); 0
	// once nothing was drawn, or the recording can't be replayed. Cleared with rsNMILen before turning the sprites gate
	// on (u8; the high byte is unused):
	rsReplay = rsNMILen + 2
	// the rows to upload: VRAM word address (bit 15 set if the source is in WRAM bank $7E, bit 13 if it is in the SRAM
	// buffer of rows from remote players' facts, else in LinkGraphics) and DMA source address. The VRAM addresses of
	// the slots are $4000..$45FF, so bit 13 is free:
	rsNMIList = rsNMILen + 4
	// the byte length of rsCache (u16), and the last rows the NMI hooks uploaded, in the same format as rsNMIList; rows
	// still in VRAM while the game doesn't upload its own (core updates disabled) are not uploaded again:
	rsCacheLen = rsNMIList + remoteUploadMax*nmiRowSize
	rsCache    = rsCacheLen + 2
	// the follower slots ($20, $22, $24: bits 0, 1, 2) the parts got this frame, written by remoteFrame; no sprite of
	// the game shows them, so the follower hook skips the game's upload of its own tiles there and the rows of the
	// parts already in them are not uploaded again (u8):
	rsSkipSlots = rsCache + remoteUploadMax*nmiRowSize
	// how many more frames in a row remoteFrame may draw the parts in the VRAM slots they last got, when the scan is
	// too late to tell which slots are in use (see rfReuseFrames); 0 once nothing was drawn. It is the high byte of
	// rsSkipSlots, so that the clear before turning the sprites gate on clears it too (u8):
	rsReuse = rsSkipSlots + 1
	// the slots whose rows in rsCache are still in VRAM, as rsNMIBits: the follower hook clears the bits of the slots
	// the game uploads its own tiles into (all but the kept follower slots), the upload sets them all (u16):
	rsKept = rsSkipSlots + 2
	// per row of rsNMIList, at the same offset: the bit of the slot it is in, $01, $02 or $04 for the follower slots
	// ($20, $22, $24), else $08 (u16):
	rsNMIBits = rsKept + 2

	remoteStateSize = rsNMIBits + remoteUploadMax*nmiRowSize

	remoteUploadMax = remotePartMax * 2
	nmiRowSize      = 4
)

// the table and state are between the capture routine's list of dying enemies and the update routine buffers:
const _ = remoteTableAddr - captureDyingEnd
const _ = remoteStateAddr - (remoteTableAddr + remoteTableSize)
const _ = updateBufAAddr - (remoteStateAddr + remoteStateSize)

// remoteSlots are the first chrs of the VRAM slots (2x2 chrs: chr, chr+1, chr+$10, chr+$11) that parts are uploaded
// to, in the order they are handed out. NMI_DoUpdates uploads all of them every frame while core updates are enabled:
// the follower and NPC chrs from dma_var6 ($0AE8), dma_var7 ($0AEA) and $7EBD40, Link's item chrs ($0AD8, $0AC8), and
// his sword ($0AC0) and shield ($0AC4).
var remoteSlots = []uint8{0x20, 0x22, 0x24, 0x0C, 0x09, 0x05, 0x07}

// remoteLinkSlots are the bits of the slots that hold Link's own chrs:
const remoteLinkSlots = 0x78

// VRAM and OAM constants:
const (
	// blob chrs, alternating every 8 frames:
	remoteBlobChr0 = 0x64
	// blob OAM attributes: priority 2 and palette 4, as the game draws the blob (sprite $8F) that magic powder turns an
	// enemy into (Sprite_GiveDamage sets its OAM flags to 8):
	remoteBlobAttr = 0x28
	// the 16x16 shadow under a blob, 10 pixels below it, as SpriteDraw_Shadow draws it under enemies (attributes
	// `flags & $30 | 8`, the same as the blob's):
	remoteShadowChr   = 0x6C
	remoteShadowYOffs = 10

	// nmiHookPatchAddr is where NMI_DoUpdates sets up the animated tiles upload, right after uploading Link's, his
	// items' and the follower and NPC tiles; patched with a JSL to the NMI hook:
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

// nmiFollowerPatchAddr is where NMI_DoUpdates starts the upload of the follower's and the item get tiles into the
// slots $20, $22 and $24 (channels 0, 1 and 2; the bottom rows are started at $008B22 with the same A); patched with a
// JSL to the follower hook, which leaves out the channels of the slots the parts got (see rsSkipSlots):
const nmiFollowerPatchAddr = uint32(0x008AFC)

// nmiFollowerPatchExpected is the original code at nmiFollowerPatchAddr: LDA.b #$07; STA.w MDMAEN
var nmiFollowerPatchExpected = []byte{0xA9, 0x07, 0x8D, 0x0B, 0x42}

// nmiFollowerBottomPatchAddr is where NMI_DoUpdates starts the upload of the bottom rows of the same tiles; patched
// with a JSL to the follower bottom hook. The game starts the channels at once, which uploads each one's tiles right
// after the previous one's: the hooks start them one at a time, at the VRAM address of each one's slot.
const nmiFollowerBottomPatchAddr = uint32(0x008B1F)

// nmiFollowerBottomPatchExpected is the original code at nmiFollowerBottomPatchAddr: STX.w DMA2SIZE; STA.w MDMAEN
var nmiFollowerBottomPatchExpected = []byte{0x8E, 0x25, 0x43, 0x8D, 0x0B, 0x42}

// followerSlotBits returns, for each chr $00..$3F, the bit of the follower slot ($20: 1, $22: 2, $24: 4) it is in.
func followerSlotBits() (t [0x40]byte) {
	for i, base := range []int{0x20, 0x22, 0x24} {
		for _, o := range []int{0x00, 0x01, 0x10, 0x11} {
			t[base+o] = 1 << i
		}
	}
	return
}

// remoteTTLFrames is the TTL o2 writes to the table, in frames:
const remoteTTLFrames = 60

// remoteTableRefresh is how often o2 rewrites an unchanged table to keep its TTL from running out:
const remoteTableRefresh = 250 * time.Millisecond

// remoteSpritesStale is how long received sprites are used to draw a remote player. alttpo sends its sprites every
// frame to the sector of the room or area it is actually in, so an alttpo player in the local player's (reported)
// location that stops sending sprites has left it, even though its reported location stays on the old room or area
// until a screen transition or warp finishes. alttpo also only draws remote players from the sprites it receives.
const remoteSpritesStale = 250 * time.Millisecond

// remoteSlotChrTables returns, for each chr $00..$3F, the bits of the slots in remoteSlots that an 8x8 sprite and a
// 16x16 sprite with that chr show. A 16x16 sprite also shows the chr right of its own (wrapping in the row) and the
// two below. No slot has chrs from $40 on.
func remoteSlotChrTables() (small, large [0x40]byte) {
	touch := func(c int) (m byte) {
		for i, s := range remoteSlots {
			if b := int(s); c == b || c == b+1 || c == b+0x10 || c == b+0x11 {
				m |= 1 << i
			}
		}
		return
	}
	for c := 0; c < 0x40; c++ {
		right := c&0xF0 | (c+1)&0x0F
		small[c] = touch(c)
		large[c] = touch(c) | touch(right) | touch(c+0x10) | touch(right+0x10)
	}
	return
}

// emitNMIHooks emits the two routines that NMI_DoUpdates calls with JSL to upload the rows of chrs that remoteDraw
// listed this frame (the remote player's head, body, sword, shield and item) into their VRAM slots, and the upload
// subroutine they share. Both are called with A 8-bit, X and Y 16-bit, DB = $00 (or $80), D = $0000. It returns their
// addresses:
//
//   - late, from nmiHookPatchAddr, when core updates are enabled ($0710 = 0): right after the game uploaded Link's,
//     his items' and the follower and NPC tiles to the slots; DMA channel 0 is set up for VRAM writes ($1801). It
//     uploads every row and runs the code it was patched over.
//   - early, from nmiEarlyPatchAddr at the start of NMI_DoUpdates. When core updates are enabled the game uploads
//     its own tiles over every slot, so it forgets what is in them. When they are disabled the game skips all of its
//     sprite uploads, e.g. on every frame a dialog renders text (Text_Render): it uploads the rows that aren't still
//     in VRAM from the last upload. It runs the code it was patched over (setting VMAIN) and sets up DMA channel 0
//     itself.
//
// upload is the routine they call with JSL (see emitNMIUpload).
//
// Cycles (cycles_test.go): budget 50 per frame, measured 40 with 3 remote players, not counting emitNMIUpload.
func emitNMIHooks(a *asm.Emitter, upload uint32) (late, early, follower, followerBottom uint32) {
	st := func(o uint32) uint32 { return remoteStateAddr + o }
	a.AssumeSEP(0x20)
	a.AssumeREP(0x10)

	late = a.Label("nmiHook")
	a.Comment("the state below is stale while the sprites gate is clear, since remoteFrame doesn't run:")
	emitGateTest(a, gateSprites, "nmi_late_done")
	a.LDA_long(st(rsNMILen))
	a.BEQ("nmi_late_done")
	a.JSL(upload)
	a.Label("nmi_late_done")
	a.Comment("the code patched over:")
	a.LDX_abs(0x0ADC)
	a.STX_abs(0x4302)
	a.RTL()

	early = a.Label("nmiHookEarly")
	a.Comment("the code patched over:")
	a.LDA_imm8_b(0x80)
	a.STA_abs(0x2115)
	emitGateTest(a, gateSprites, "nmi_early_done")
	a.LDA_long(0x7E0710)
	a.BNE("nmi_early_disabled")
	a.Comment("core updates are enabled: the follower hook and the late hook upload")
	a.RTL()
	a.Label("nmi_early_disabled")
	a.LDA_long(st(rsNMILen))
	a.BEQ("nmi_early_done")
	a.LDX_imm16_w(0x1801)
	a.STX_abs(0x4300)
	a.JSL(upload)
	a.Label("nmi_early_done")
	a.RTL()

	a.Comment("start channels 0, 1 and 2 (slots $20, $22, $24) but those of the kept slots, each at its slot's VRAM")
	a.Comment("address from Y; called with JSR, A 8-bit, Y 16-bit; clobbers A and Y:")
	channels := a.Label("nmi_fol_channels")
	for c := 0; c < 3; c++ {
		skip := fmt.Sprintf("nmi_fol_ch%d", c)
		a.LDA_long(st(rsSkipSlots))
		a.AND_imm8_b(1 << c)
		a.BNE(skip)
		a.STY_abs(0x2116)
		a.LDA_imm8_b(1 << c)
		a.STA_abs(0x420B)
		a.Label(skip)
		if c < 2 {
			a.REP(0x20)
			a.TYA()
			a.CLC()
			a.ADC_imm16_w(0x0020)
			a.TAY()
			a.SEP(0x20)
		}
	}
	a.RTS()

	follower = a.Label("nmiHookFollower")
	emitGateTest(a, gateSprites, "nmi_fol_off")
	a.Comment("the game uploads Link's and his items' tiles over every other slot, and the follower and item get tiles")
	a.Comment("over the follower slots the parts didn't get: the rows cached in those are no longer in VRAM. With no")
	a.Comment("follower slot kept, the game's single start of all three channels uploads them in place (they are")
	a.Comment("contiguous in VRAM), which is cheaper than starting them one at a time:")
	a.LDA_long(st(rsSkipSlots))
	a.AND_imm8_b(0x07)
	a.BEQ("nmi_fol_none")
	a.AND_long(st(rsKept))
	a.STA_long(st(rsKept))
	a.Comment("the top rows, at VRAM $4200 (chr $20):")
	a.PHY()
	a.LDY_imm16_w(0x4200)
	a.JSR_abs(uint16(channels))
	a.PLY()
	a.RTL()
	a.Label("nmi_fol_none")
	a.Comment("A = 0: no rows cached in a slot are still in VRAM:")
	a.STA_long(st(rsKept))
	a.Label("nmi_fol_off")
	a.Comment("the code patched over: start channels 0, 1 and 2:")
	a.LDA_imm8_b(0x07)
	a.STA_abs(0x420B)
	a.RTL()

	followerBottom = a.Label("nmiHookFollowerBottom")
	a.Comment("the code patched over: the item get bottom row's size:")
	a.STX_abs(0x4325)
	emitGateTest(a, gateSprites, "nmi_fol_bottom_off")
	a.Comment("no follower slot kept: the game's single start, as for the top rows:")
	a.LDA_long(st(rsSkipSlots))
	a.AND_imm8_b(0x07)
	a.BEQ("nmi_fol_bottom_off")
	a.Comment("the bottom rows, at VRAM $4300 (chr $30):")
	a.PHY()
	a.LDY_imm16_w(0x4300)
	a.JSR_abs(uint16(channels))
	a.PLY()
	a.RTL()
	a.Label("nmi_fol_bottom_off")
	a.Comment("the code patched over: start channels 0, 1 and 2 (the game still has A = 7 from the top rows):")
	a.LDA_imm8_b(0x07)
	a.STA_abs(0x420B)
	a.RTL()
	a.AssumeSEP(0x30)
	return
}

// emitNMIUpload emits the routine that the NMI hooks call with JSL to upload the rows of chrs in rsNMIList, with DMA
// channel 0 set up for VRAM writes. Called with A 8-bit and X 16-bit; clobbers A and X. It skips the rows still in
// VRAM from the last upload (rsCache, rsKept). linkGfxBankIns is the 2 byte instruction at linkGfxBankAddr that loads the
// bank of Link's graphics.
//
// Cycles (cycles_test.go): budget 600, measured 562 with 3 remote players.
func emitNMIUpload(a *asm.Emitter, linkGfxBankIns []byte) {
	st := func(o uint32) uint32 { return remoteStateAddr + o }
	a.AssumeSEP(0x20)
	a.AssumeREP(0x10)
	a.Label("nmiUpload")
	a.PHY()
	a.Comment("the bank of Link's graphics, the same way NMI_DoUpdates gets it, at $03,S during the loop:")
	a.EmitBytes(linkGfxBankIns)
	a.PHA()
	a.REP(0x20)
	a.LDA_long(st(rsNMILen))
	a.BNE("nmi_up_rows")
	a.JMP_abs("nmi_up_done")
	a.Label("nmi_up_rows")
	a.Comment("the byte length of the list at $01,S; X = offset into the list:")
	a.PHA()
	a.LDX_imm16_w(0x0000)

	a.Label("nmi_up_loop")
	a.Comment("rows still in VRAM from the last upload are skipped:")
	a.TXA()
	a.CMP_long(st(rsCacheLen))
	a.BCS("nmi_up_row")
	a.LDA_long_x(st(rsNMIBits))
	a.AND_long(st(rsKept))
	a.BEQ("nmi_up_row")
	for o := uint32(0); o < nmiRowSize; o += 2 {
		a.LDA_long_x(st(rsNMIList + o))
		a.CMP_long_x(st(rsCache + o))
		a.BNE("nmi_up_row")
	}
	a.JMP_abs("nmi_up_next")
	a.Label("nmi_up_row")
	a.LDA_long_x(st(rsNMIList + 0))
	a.AND_imm16_w(^uint16((partWRAM | partSRAM) << 8))
	a.STA_abs(0x2116)
	a.LDA_long_x(st(rsNMIList + 2))
	a.STA_abs(0x4302)
	a.LDA_imm16_w(0x0040)
	a.STA_abs(0x4305)
	a.SEP(0x20)
	a.LDA_long_x(st(rsNMIList + 1))
	a.BMI("nmi_up_wram")
	a.AND_imm8_b(partSRAM)
	a.BNE("nmi_up_sram")
	a.Comment("the bank of Link's graphics:")
	a.LDA_sr(0x03)
	a.BRA("nmi_up_bank")
	a.Label("nmi_up_sram")
	a.Comment("the SRAM buffer of rows from remote players' facts:")
	a.LDA_imm8_b(uint8(remoteTilesAddr >> 16))
	a.BRA("nmi_up_bank")
	a.Label("nmi_up_wram")
	a.LDA_imm8_b(0x7E)
	a.Label("nmi_up_bank")
	a.STA_abs(0x4304)
	a.LDA_imm8_b(0x01)
	a.STA_abs(0x420B)
	a.REP(0x20)
	a.Comment("remember the row as uploaded:")
	a.LDA_long_x(st(rsNMIList + 0))
	a.STA_long_x(st(rsCache + 0))
	a.LDA_long_x(st(rsNMIList + 2))
	a.STA_long_x(st(rsCache + 2))
	a.Label("nmi_up_next")
	a.INX()
	a.INX()
	a.INX()
	a.INX()
	a.TXA()
	a.Comment("the byte length of the list:")
	a.CMP_sr(0x01)
	a.BCS("nmi_up_end")
	a.JMP_abs("nmi_up_loop")

	a.Label("nmi_up_end")
	a.Comment("every row of the list is in VRAM now:")
	a.LDA_imm16_w(0x000F)
	a.STA_long(st(rsKept))
	a.PLA()
	a.Label("nmi_up_done")
	a.STA_long(st(rsCacheLen))
	a.SEP(0x20)
	a.PLA()
	a.Comment("the following uploads expect WRAM as the source bank:")
	a.LDA_imm8_b(0x7E)
	a.STA_abs(0x4304)
	a.PLY()
	a.RTL()
	a.AssumeSEP(0x30)
}

// emitRemoteInit emits the init code that zeroes the remote sprites table and state. Runs with A, X and Y 16-bit;
// leaves DB = $70.
func emitRemoteInit(a *asm.Emitter) {
	a.Comment("zero the remote sprites table and state:")
	a.LDA_imm16_w(0x0000)
	a.STA_long(remoteTableAddr)
	a.LDX_imm16_w(uint16(remoteTableAddr & 0xFFFF))
	a.LDY_imm16_w(uint16((remoteTableAddr + 1) & 0xFFFF))
	a.LDA_imm16_w(uint16(remoteStateAddr + remoteStateSize - remoteTableAddr - 2))
	a.MVN(uint8(remoteTableAddr>>16), uint8(remoteTableAddr>>16))
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

// remoteSprite is a sprite received from a remote player in MsgSprites.
type remoteSprite struct {
	// the sprite's place in the player's list, which is in OAM order (front to back):
	index uint8
	// OAM table bytes: x, y, chr, attributes; b4 = (size << 1) | x bit 8:
	b    [5]uint8
	size uint8
	// the tiles its 1 or 4 chrs show: LinkGraphics tiles ($000..$37F), or the local player's sword and shield tiles
	// ($380..$3A3, offsets into WRAM $7E9000) when the remote player's sword or shield is the same level; see
	// remoteChrTile:
	absidx [4]uint16
	// the tiles of the player's Link graphics fact its 1 or 4 chrs are bound to, noFactTile if none:
	tiles [4]uint16
}

type remoteSprites struct {
	list     []remoteSprite
	received time.Time
	// the player's scroll offsets when the list arrived. Senders send their location (with the offsets) before their
	// sprites in the same frame, so these are the offsets the sprites were drawn with; the player's latest offsets
	// can be a frame ahead or behind, which shifts the sprites by however far the screen scrolled meanwhile:
	xoffs, yoffs int16
	// the tiles of the player's facts its chrs $00..$1F show (MsgSpriteChrs):
	chrs [0x20]remoteChr
}

// remoteChr is a remote player's chr bound to a tile of one of its facts.
type remoteChr struct {
	ok   bool
	fact uint8
	tile uint16
}

// DeserializeSpriteChrs reads MsgSpriteChrs, which replaces the player's bindings.
func (g *Game) DeserializeSpriteChrs(p *Player, r io.Reader) (err error) {
	var n uint8
	if n, err = readU8(r); err != nil {
		return
	}
	s := &p.Sprites
	s.chrs = [0x20]remoteChr{}
	for i := 0; i < int(n); i++ {
		var e [5]byte
		if _, err = io.ReadFull(r, e[:]); err != nil {
			return fmt.Errorf("error deserializing sprite chrs: %w", err)
		}
		tile := uint16(e[3]) | uint16(e[4])<<8
		for k := 0; k < int(e[1]); k++ {
			if c := int(e[0]) + k; c < len(s.chrs) {
				s.chrs[c] = remoteChr{ok: true, fact: e[2], tile: tile + uint16(k)}
			}
		}
	}
	return
}

// DeserializeSpritePalettes reads MsgSpritePalettes; o2 draws remote players with its own palettes.
func (g *Game) DeserializeSpritePalettes(p *Player, r io.Reader) (err error) {
	var mask uint8
	if mask, err = readU8(r); err != nil {
		return
	}
	return discard(r, bits.OnesCount8(mask)*32)
}

// remoteChrTile returns the tile o2 draws a remote player's chr with: a LinkGraphics tile, or one of the local
// player's sword and shield tiles if the remote player's sword or shield is the same level; noUniqTile if none.
func (g *Game) remoteChrTile(s *remoteSprites, chr uint16) uint16 {
	if chr >= 0x20 || !s.chrs[chr].ok || s.chrs[chr].fact != factLinkGraphics {
		return noUniqTile
	}
	t := int(s.chrs[chr].tile)
	ss := &g.swordShield
	switch {
	case t < lfBodySize>>5:
		return uint16(t)
	case t >= lfSwords>>5 && t < lfShields>>5:
		o := t - lfSwords>>5
		if level := uint8(o/swordTileCount + 1); ss.swordOK && level == ss.swordLevel {
			return uint16(swordShieldTileFirst + o%swordTileCount)
		}
	case t >= lfShields>>5 && t < lfItems>>5:
		o := t - lfShields>>5
		if level := uint8(o/shieldTileCount + 1); ss.shieldOK && level == ss.shieldLevel {
			return uint16(swordShieldTileFirst + swordTileCount + o%shieldTileCount)
		}
	}
	return noUniqTile
}

// DeserializeSprites reads MsgSprites into p.Sprites.
func (g *Game) DeserializeSprites(p *Player, r io.Reader) (err error) {
	var length uint8
	if length, err = readU8(r); err != nil {
		return fmt.Errorf("error deserializing sprites: %w", err)
	}

	s := &p.Sprites
	s.list = s.list[:0]
	s.received = time.Now()
	s.xoffs, s.yoffs = p.XOffs, p.YOffs

	for i := 0; i < int(length); i++ {
		var e [5]byte
		if _, err = io.ReadFull(r, e[:]); err != nil {
			return fmt.Errorf("error deserializing sprite %d: %w", i, err)
		}
		sp := remoteSprite{index: uint8(i)}
		copy(sp.b[:], e[:])
		sp.b[4] &= 3
		sp.size = (sp.b[4] >> 1) & 1
		if sp.b[1] == 0xF0 {
			sp.size = 0
		}
		sp.absidx = [4]uint16{noUniqTile, noUniqTile, noUniqTile, noUniqTile}
		sp.tiles = [4]uint16{noFactTile, noFactTile, noFactTile, noFactTile}
		if sp.b[3]&1 == 0 {
			chr := uint16(sp.b[2])
			for k, c := range [4]uint16{chr, chr + 0x01, chr + 0x10, chr + 0x11} {
				if k > 0 && sp.size == 0 {
					break
				}
				sp.absidx[k] = g.remoteChrTile(s, c)
				if c < 0x20 && s.chrs[c].ok && s.chrs[c].fact == factLinkGraphics {
					sp.tiles[k] = s.chrs[c].tile
				}
			}
		}
		s.list = append(s.list, sp)
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

// sortedByIndex returns a copy of a remote player's sprites ordered by OAM index (front to back).
func sortedByIndex(list []remoteSprite) []remoteSprite {
	sorted := make([]remoteSprite, len(list))
	copy(sorted, list)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].index < sorted[j].index })
	return sorted
}

// remoteBodySprites returns up to two 16x16 sprites of Link's head and body from a remote player's sprites, ordered by
// OAM index (front to back).
func remoteBodySprites(list []remoteSprite) (body []remoteSprite) {
	for _, sp := range sortedByIndex(list) {
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

// remotePart is 2x2 chrs that the NMI hooks upload into a VRAM slot for slotted sprites to show.
type remotePart struct {
	// DMA source addresses of the top and bottom row:
	src   [2]uint16
	flags uint8
}

// slottedSprite is a sprite drawn with the chrs of a part.
type slottedSprite struct {
	sp   remoteSprite
	part int
	// the sprite's chr in the part's chrs: $00, $01, $10 or $11 (always $00 for 16x16 sprites):
	off uint8
}

// remoteEquipBlocks are the chrs Link's sword, shield and the item in use are uploaded to, 2x2 from chr, and where
// their tiles are: from tile factFirst of fact 0x00, levelTiles tiles for each level from 1 to maxLevel, laid out as
// WRAM bank $7E from wram (see swordshield.go and linkfact.go). The item buffer has a single "level".
var remoteEquipBlocks = []struct {
	chr        uint8
	factFirst  int
	levelTiles int
	maxLevel   int
	wram       uint16
}{
	{0x05, lfSwords >> 5, swordTileCount, 4, swordShieldWRAMStart},
	{0x07, lfShields >> 5, shieldTileCount, 3, swordShieldWRAMStart + swordTileCount<<5},
	{0x09, lfItems >> 5, lfItemsSize >> 5, 1, lfItemsWRAM},
}

// remoteEquipMax is the most sprites drawn for the sword, the shield or the item (the game draws up to 3 for each):
const remoteEquipMax = 4

// remoteEquipment returns up to max parts and their slotted sprites (with part indices starting at 0) of a remote
// player's sword, shield and the item in use, in the pose the player sent. They are drawn with the local player's
// tiles in WRAM when the remote player's are the same level and graphics, else with the tiles of its fact copied to the
// SRAM buffer of rows (fact.data; see remoteTileRows). Without the fact, a sword or shield the local player doesn't
// have at the same level is not drawn; the item in use is drawn with the local item buffer, which the game builds from
// ROM whatever the player has.
func (g *Game) remoteEquipment(list []remoteSprite, fact remoteFact, max int) (parts []remotePart, sprites []slottedSprite) {
	s := &g.swordShield
	sorted := sortedByIndex(list)

	// the chrs of a sprite, relative to chr base; ok if it only shows chrs of the 2x2 chrs at base:
	covered := func(sp *remoteSprite, base uint8) (offs []uint8, ok bool) {
		if sp.b[1] == 0xF0 || sp.b[3]&1 != 0 {
			return nil, false
		}
		if sp.size != 0 {
			if sp.b[2] != base {
				return nil, false
			}
			return []uint8{0x00, 0x01, 0x10, 0x11}, true
		}
		switch o := sp.b[2] - base; o {
		case 0x00, 0x01, 0x10, 0x11:
			return []uint8{o}, true
		}
		return nil, false
	}

	for bi, b := range remoteEquipBlocks {
		if len(parts) >= max {
			break
		}
		var localLevel uint8
		var localOK bool
		switch bi {
		case 0:
			localLevel, localOK = s.swordLevel, s.ok && s.swordOK
		case 1:
			localLevel, localOK = s.shieldLevel, s.ok && s.shieldOK
		default:
			localLevel, localOK = 1, true
		}
		localOK = localOK && localLevel != 0 && int(localLevel) <= b.maxLevel

		// the level the sprites show (that of the first tile in the block), and the first tile of each row in the
		// level's tiles (-1 until a sprite shows the row, -2 if its sprites don't agree or show other tiles):
		level := 0
		rows := [2]int{-1, -1}
		var block []slottedSprite
		for _, sp := range sorted {
			offs, ok := covered(&sp, b.chr)
			if !ok {
				continue
			}
			if len(block) == remoteEquipMax {
				break
			}
			for k, o := range offs {
				row, col := int(o>>4), int(o&1)
				first := -2
				if t := int(sp.tiles[k]); sp.tiles[k] != noFactTile && t >= b.factFirst && t < b.factFirst+b.maxLevel*b.levelTiles {
					l := (t-b.factFirst)/b.levelTiles + 1
					if level == 0 {
						level = l
					}
					if idx := (t - b.factFirst) % b.levelTiles; l == level && idx >= col {
						first = idx - col
					}
				}
				if rows[row] != -1 && rows[row] != first {
					first = -2
				}
				rows[row] = first
			}
			block = append(block, slottedSprite{sp: sp, part: len(parts), off: offs[0]})
		}
		if len(block) == 0 || level == 0 {
			continue
		}

		// where the row of tiles starting at the level's tile first is:
		var src func(first int) (uint16, bool)
		var flags uint8
		switch {
		case localOK && uint8(level) == localLevel && (fact.data == nil || fact.same):
			flags = partWRAM
			src = func(first int) (uint16, bool) { return b.wram + uint16(first)<<5, true }
		case fact.data != nil:
			flags = partSRAM
			src = func(first int) (uint16, bool) {
				return g.remoteSprites.tileRow(fact, uint16(b.factFirst+(level-1)*b.levelTiles+first))
			}
		default:
			continue
		}

		part := remotePart{flags: flags}
		for row, first := range rows {
			if first == -1 {
				continue
			}
			if first < 0 {
				part.flags = 0
				break
			}
			a, ok := src(first)
			if !ok {
				part.flags = 0
				break
			}
			part.src[row] = a
			part.flags |= partTopRow << row
		}
		if part.flags&(partTopRow|partBottomRow) == 0 {
			continue
		}
		parts = append(parts, part)
		sprites = append(sprites, block...)
	}
	return
}

// remoteFact is a remote player's Link graphics fact: its contents if o2 has them (data, else nil), its hash, and
// whether it is the same as the local player's (data is then the local player's).
type remoteFact struct {
	data []byte
	hash protocol05.Hash
	same bool
}

// remoteLinkFact returns a remote player's Link graphics fact, and starts downloading it if o2 doesn't have it yet.
func (g *Game) remoteLinkFact(p *Player, now time.Time) (fact remoteFact) {
	f := &g.facts
	f.init()
	e, listed := f.lists[p.Index()][factLinkGraphics]
	if !listed {
		// asks for the fact-list:
		g.factData(p.Index(), factLinkGraphics, now)
		return
	}
	fact.hash = e.Hash
	if h, ok := g.localFactHash(factLinkGraphics); ok && h == e.Hash {
		fact.same = true
		fact.data = f.local[factLinkGraphics].data
		return
	}
	fact.data, _ = g.factData(p.Index(), factLinkGraphics, now)
	return
}

// remoteTileRow is a row of 2 tiles of a fact (by its hash), from tile:
type remoteTileRow struct {
	ok   bool
	hash protocol05.Hash
	tile uint16
}

// remoteTileRows: o2 copies the rows of tiles that parts show from remote players' facts into the SRAM buffer at
// remoteTilesAddr, before the table that uses them. A row stays where it is while tables use it, so a table that
// doesn't change doesn't change the buffer; a new row goes where no table uses one, else where the last table written
// uses one and the new one doesn't (which may show it for a frame before the new table arrives). Whenever o2 writes
// rows it clears rsKept, so that the NMI hooks don't keep a row in VRAM whose source it overwrote.

// beginRows starts the rows of the table being built from the rows in the buffer.
func (s *remoteSpritesLocal) beginRows() {
	s.next, s.nextUsed, s.nextData = s.rows, 0, [remoteTileRowMax][]byte{}
}

// tileRow returns the DMA source address of the row of 2 tiles of a fact from tile in the SRAM buffer, which the table
// being built uses, putting it there if it isn't. ok is false if the fact is too short or the buffer has no room.
func (s *remoteSpritesLocal) tileRow(fact remoteFact, tile uint16) (src uint16, ok bool) {
	o := int(tile) << 5
	if o+remoteTileRowSize > len(fact.data) {
		return 0, false
	}
	key := remoteTileRow{ok: true, hash: fact.hash, tile: tile}
	pick := -1
	for i := range s.next {
		if s.next[i] == key {
			pick = i
			break
		}
	}
	if pick < 0 {
		for pass := 0; pass < 2 && pick < 0; pass++ {
			for i := range s.next {
				bit := uint16(1) << i
				if s.nextUsed&bit != 0 || (pass == 0 && s.rowsLive&bit != 0) {
					continue
				}
				pick = i
				break
			}
		}
		if pick < 0 {
			return 0, false
		}
		s.next[pick] = key
		s.nextData[pick] = fact.data[o : o+remoteTileRowSize]
	}
	s.nextUsed |= 1 << pick
	return uint16(remoteTilesAddr + uint32(pick)*remoteTileRowSize), true
}

// remoteExtraSprites returns up to max sprites with captured chrs (Link's shadow or the water ripples or grass under
// his feet, sparkles, dust, projectiles, carried and thrown objects; see captureChrs) from a remote player's sprites,
// ordered by OAM index. The ripples and grass under a follower's feet, which alttpo sends, are left out (see
// isFollowerFootSprite).
func remoteExtraSprites(list []remoteSprite, max int) (extras []remoteSprite) {
	for _, sp := range sortedByIndex(list) {
		if len(extras) >= max {
			break
		}
		if sp.b[1] == 0xF0 || sp.b[3]&1 != 0 || !(isCaptureChr(sp.b[2]) || isLinkFootChr(uint16(sp.b[2]))) {
			continue
		}
		if isLinkFootChr(uint16(sp.b[2])) && isFollowerFootSprite(list, &sp) {
			continue
		}
		extras = append(extras, sp)
	}
	return
}

// isFollowerFootSprite reports whether sp, a water ripple or tall grass chr (isLinkFootChr), is one of the two that
// Follower_Draw draws under a follower's feet, in the OAM entries just before the follower's chr $20 or $22 (the
// follower's own tiles in name table 0). Link's are among his own entries, which never hold the follower.
func isFollowerFootSprite(list []remoteSprite, sp *remoteSprite) bool {
	for i := range list {
		n := &list[i]
		if d := int(n.index) - int(sp.index); (d == 1 || d == 2) && n.b[3]&1 == 0 && (n.b[2] == 0x20 || n.b[2] == 0x22) {
			return true
		}
	}
	return false
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

	// the rows of tiles in the SRAM buffer at remoteTilesAddr as o2 last wrote them, and the bits of those the last
	// table written uses (see remoteTileRows):
	rows     [remoteTileRowMax]remoteTileRow
	rowsLive uint16
	// the same for the table being built, and the rows to write before it:
	next     [remoteTileRowMax]remoteTileRow
	nextUsed uint16
	nextData [remoteTileRowMax][]byte

	// player and slotted sprite counts last logged, and counts that differ from them and since when; a count is logged
	// once it holds for remoteLogSettle, so flickering players don't spam the log:
	logged, pending [2]int
	loggedOK        bool
	pendingSince    time.Time
}

const remoteLogSettle = 500 * time.Millisecond

// logRemoteCounts logs the number of players and slotted sprites drawn once it changed and held for remoteLogSettle.
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
	log.Printf("alttp: remote sprites: drawing %d players (%d sprites with Link's graphics)\n", c[0], c[1])
	s.logged, s.loggedOK = c, true
}

// remoteBodyOK reports whether the patched ROM has the NMI hooks that upload the parts' tiles: JSLs into the ROM the
// patcher puts its routines in (isFreeROM).
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
	// every table, even one with nothing to draw, says when drawing it is late: the patch's first time check reads it
	// to decide whether to capture and scan at all:
	defer func() { t[rtDrawLate] = drawLateLineOf(&t) }()
	g.remoteSprites.beginRows()
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

	// the parts uploaded into VRAM slots: the body sprites back to front (they must all get a slot), then the sword,
	// shield and item in use:
	var parts []remotePart
	var slotted []slottedSprite
	if body != nil {
		// the player's own graphics once o2 has its fact, if they aren't the local player's:
		fact := g.remoteLinkFact(players[0], now)
		for j := len(body) - 1; j >= 0; j-- {
			sp := body[j]
			part := remotePart{
				src:   [2]uint16{0x8000 + sp.absidx[0]<<5, 0x8000 + sp.absidx[2]<<5},
				flags: partTopRow | partBottomRow,
			}
			if fact.data != nil && !fact.same {
				top, okTop := g.remoteSprites.tileRow(fact, sp.tiles[0])
				bottom, okBottom := g.remoteSprites.tileRow(fact, sp.tiles[2])
				if okTop && okBottom {
					part.src, part.flags = [2]uint16{top, bottom}, part.flags|partSRAM
				}
			}
			parts = append(parts, part)
			slotted = append(slotted, slottedSprite{sp: sp, part: len(parts) - 1})
		}
		t[rtRequired] = uint8(len(parts))
		eqParts, eqSprites := g.remoteEquipment(players[0].Sprites.list, fact, remotePartMax-len(parts))
		for _, es := range eqSprites {
			es.part += len(parts)
			slotted = append(slotted, es)
		}
		parts = append(parts, eqParts...)
	}
	// drawn back to front: the highest OAM index first:
	sort.SliceStable(slotted, func(i, j int) bool { return slotted[i].sp.index > slotted[j].sp.index })

	t[rtParts] = uint8(len(parts))
	for i, pt := range parts {
		binary.LittleEndian.PutUint16(t[rtPartSrc+i*4:], pt.src[0])
		binary.LittleEndian.PutUint16(t[rtPartSrc+i*4+2:], pt.src[1])
		t[rtPartFlags+i] = pt.flags
	}

	o := rtEntries
	entry := func(x, y uint16, chr, attr, flags uint8) {
		binary.LittleEndian.PutUint16(t[o+0:], x)
		binary.LittleEndian.PutUint16(t[o+2:], y)
		t[o+4], t[o+5], t[o+6] = chr, attr, flags
		o += rtEntrySize
	}
	end := func(field int) { binary.LittleEndian.PutUint16(t[field:], uint16(o-rtEntries)) }
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
	// a shadow and a blob over the player's lower 16 pixels:
	shadow := func(p *Player) {
		entry(p.X, p.Y+8+remoteShadowYOffs, remoteShadowChr, remoteBlobAttr, entrySize16)
	}
	blob := func(p *Player) {
		entry(p.X, p.Y+8, remoteBlobChr0, remoteBlobAttr, entrySize16)
	}

	// every player's shadow, sparkles, dust, projectiles and carried and thrown objects, in the entries left; those
	// the body's player draws in front of Link (with a lower OAM index than the body) are drawn after everything else:
	type extra struct {
		p  *Player
		sp remoteSprite
	}
	extrasMax := min(remoteExtraMax, remoteEntryMax-len(slotted)-2*len(players))
	var behind, front []extra
	for _, p := range players {
		for _, sp := range remoteExtraSprites(p.Sprites.list, extrasMax-len(behind)-len(front)) {
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
			entry(x, y, list[j].sp.b[2], list[j].sp.b[3], list[j].sp.size<<1)
		}
	}
	extras(behind)
	end(rtExtrasEnd)

	for _, ss := range slotted {
		x, y := world(players[0], ss.sp)
		entry(x, y, uint8(ss.part)<<6|ss.off, ss.sp.b[3]&^1, ss.sp.size<<1)
	}
	end(rtSlottedEnd)

	// the first player's shadow and blob are drawn instead of the slotted sprites if the head and body get no VRAM
	// slots; all shadows are drawn behind all blobs:
	for _, p := range players {
		shadow(p)
	}
	end(rtShadowsEnd)
	for _, p := range players {
		blob(p)
	}
	end(rtBlobsEnd)

	extras(front)
	end(rtFrontEnd)
	return
}

// remoteTableCounts returns the number of players (shadows and blobs) and slotted sprites in a remote sprites table.
func remoteTableCounts(t *[remoteTableSize]byte) (players, slotted int) {
	extrasEnd := int(binary.LittleEndian.Uint16(t[rtExtrasEnd:]))
	slottedEnd := int(binary.LittleEndian.Uint16(t[rtSlottedEnd:]))
	shadowsEnd := int(binary.LittleEndian.Uint16(t[rtShadowsEnd:]))
	blobsEnd := int(binary.LittleEndian.Uint16(t[rtBlobsEnd:]))
	return (blobsEnd - shadowsEnd) / rtEntrySize, (slottedEnd - extrasEnd) / rtEntrySize
}

// remoteTablePak is the FX Pak Pro address of the remote sprites table:
var remoteTablePak = func() uint32 {
	pak, err := lorom.BusAddressToPak(remoteTableAddr)
	if err != nil {
		panic(err)
	}
	return pak
}()

// remoteTilesPak is the FX Pak Pro address of the SRAM buffer of rows:
var remoteTilesPak = remoteTablePak + (remoteTilesAddr - remoteTableAddr)

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
	// the rows of tiles the table needs that aren't in the SRAM buffer yet, written before it:
	var writes []snes.Write
	for i, d := range s.nextData {
		if d != nil {
			writes = append(writes, snes.Write{Address: remoteTilesPak + uint32(i)*remoteTileRowSize, Size: remoteTileRowSize, Data: d})
		}
	}
	if len(writes) > 0 {
		// the NMI hooks must upload the rows again, wherever they are:
		writes = append(writes, snes.Write{Address: remoteStatePak + rsKept, Size: 2, Data: []byte{0, 0}})
	}
	if s.writtenOK && t == s.written && len(writes) == 0 {
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
	writes = append(writes, snes.Write{Address: remoteTablePak, Size: remoteTableSize, Data: data[:]})
	if err := g.enqueueUnlocked(func() error {
		return q.MakeWriteCommands(writes, nil).EnqueueTo(q)
	}); err != nil {
		log.Printf("alttp: remote sprites: error enqueuing snes write: %v\n", err)
		return
	}
	s.written, s.writtenOK, s.writtenTime = t, true, now
	s.rows, s.rowsLive = s.next, s.nextUsed
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
// remote player is in the same location. Matching chrs from other sprites on screen at the same time are captured too,
// but o2 leaves out enemy deaths: enemies are synced by alttpo and every player draws their deaths already, so the
// chrs of the death poof, explosion and poof (enemyDeathChrs) are not sent within about 24 pixels of an enemy in
// sprite state $02, $04 or $06 (see nearDyingEnemy). The trampoline lists those enemies' screen positions with the
// captured entries (linkOAMCopyDying) on frames that capture such a chr; their routines only change the state on
// frames where they draw nothing, so the enemies' positions of the same frame suffice.

// SRAM and ROM addresses of the capture tables and routine:
const (
	// captureTypesAddr, captureChrsAddr and captureMaskAddr are unused (zeroed by the init code): earlier versions of
	// the patch built the capture tables there, which are now in ROM (see captureTables):
	captureTypesAddr = sramZeroAddr
	captureChrsAddr  = captureTypesAddr + 0x40
	captureMaskAddr  = sramZeroEnd - 2

	// captureDyingAddr: scratch words of emitDyingEnemiesList (the list is in the copy of Link's OAM entries, at
	// linkOAMCopyDying, so that o2 reads it with the captured entries of the same frame):
	captureDyingAddr  = uint32(0x706A70)
	captureTmpE       = captureDyingAddr
	captureTmpZ       = captureDyingAddr + 2
	captureDyingEnd   = captureDyingAddr + 4
	captureChrEnemyFX = 0x10 // set in the capture chr table for enemyDeathChrs (see captureTables)
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

// captureSparkleChrs are the chrs of the sparkle ancilla $3C (Ancilla3C_SpinAttackChargeSparkle), also drawn by other
// sword and ice rod sparkles. A dungeon's crystal spawns the same ancilla around itself while it falls, sits on the
// floor, is held up and rises (AncillaAdd_OccasionalSparkle), and every player in the room sees the crystal and its
// sparkles already, so they are not captured while one of capturePrizeAncillae exists (see captureTables):
var captureSparkleChrs = []uint16{0x80, 0x83, 0xB7}

// ancillaSparkle is that sparkle ancilla:
const ancillaSparkle = uint8(0x3C)

// capturePrizeAncillae are the ancillae of a dungeon's prize: falling and sitting ($29), held up ($22) and rising
// ($3E). Link holds up the items he receives with $22, and can't charge his sword then.
var capturePrizeAncillae = []uint8{ancillaFallingPrize, ancillaItemReceipt, ancillaRisingPrize}

// enemyDeathChrs are the chrs of an enemy's death poof (SpriteDeath_DrawPoof), explosion (SpriteModule_Explode) and
// poof (SpriteModule_Poof); those also in captureChrs are not captured near such an enemy:
var enemyDeathChrs = []uint16{
	0xA8, 0xA9, 0x9B, 0xB4, 0xB5, 0xB8, 0xB9,
	0x4E, 0x60, 0x62, 0x64, 0x66, 0x68, 0x6A,
	0x8A, 0xB3,
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

// emitInitSRAMZero emits the init code that zeroes sramZeroAddr..sramZeroEnd. Runs with A, X and Y 16-bit; leaves DB =
// $70.
func emitInitSRAMZero(a *asm.Emitter) {
	a.Comment("zero the copy of Link's OAM entries, the player's color and the remote sound effects:")
	a.LDA_imm16_w(0x0000)
	a.STA_long(sramZeroAddr)
	a.LDX_imm16_w(uint16(sramZeroAddr & 0xFFFF))
	a.LDY_imm16_w(uint16((sramZeroAddr + 1) & 0xFFFF))
	a.LDA_imm16_w(uint16(sramZeroEnd - sramZeroAddr - 2))
	a.MVN(uint8(sramZeroAddr>>16), uint8(sramZeroAddr>>16))
}

// emitDyingEnemiesList emits the routine that lists the screen positions of the enemies drawing their death poof,
// explosion or poof (sprite state $02, $04, $06) at linkOAMCopyDying, or $FFFF for its length if more than
// linkOAMDyingMax are. Called with JSL with 16-bit registers; preserves DB.
func emitDyingEnemiesList(a *asm.Emitter) {
	a.AssumeREP(0x30)
	a.Label("captureDyingList")
	a.Comment("list the enemies drawing their death this frame:")
	a.PHB()
	a.PEA(0x7070)
	a.PLB()
	a.PLB()
	a.LDY_imm16_w(0x0000)
	a.LDX_imm16_w(0x000F)
	a.Label("ce_dy_loop")
	a.LDA_long_x(0x7E0DD0)
	a.AND_imm16_w(0x00FF)
	a.CMP_imm16_w(0x0002)
	a.BEQ("ce_dy_found")
	a.CMP_imm16_w(0x0004)
	a.BEQ("ce_dy_found")
	a.CMP_imm16_w(0x0006)
	a.BNE("ce_dy_next")
	a.Label("ce_dy_found")
	tmpE, tmpZ := uint16(captureTmpE&0xFFFF), uint16(captureTmpZ&0xFFFF)
	list := uint16((linkOAMCopyAddr + linkOAMCopyDying + 2) & 0xFFFF)
	a.CPY_imm16_w(linkOAMDyingMax * 4)
	a.BCC("ce_dy_room")
	a.Comment("more than the list holds: o2 leaves out all death chrs this frame:")
	a.LDY_imm16_w(0xFFFF)
	a.BRA("ce_dy_done")
	a.Label("ce_dy_room")
	a.Comment("x - BG2 scroll:")
	a.LDA_long_x(0x7E0D30)
	a.XBA()
	a.AND_imm16_w(0xFF00)
	a.STA_abs(tmpE)
	a.LDA_long_x(0x7E0D10)
	a.AND_imm16_w(0x00FF)
	a.ORA_abs(tmpE)
	a.SEC()
	a.SBC_long(0x7E00E2)
	a.STA_abs_y(list)
	a.Comment("y - BG2 scroll - z:")
	a.LDA_long_x(0x7E0D20)
	a.XBA()
	a.AND_imm16_w(0xFF00)
	a.STA_abs(tmpE)
	a.LDA_long_x(0x7E0D00)
	a.AND_imm16_w(0x00FF)
	a.ORA_abs(tmpE)
	a.SEC()
	a.SBC_long(0x7E00E8)
	a.STA_abs(tmpE)
	a.LDA_long_x(0x7E0F70)
	a.AND_imm16_w(0x00FF)
	a.STA_abs(tmpZ)
	a.LDA_abs(tmpE)
	a.SEC()
	a.SBC_abs(tmpZ)
	a.STA_abs_y(list + 2)
	a.INY()
	a.INY()
	a.INY()
	a.INY()
	a.Label("ce_dy_next")
	a.DEX()
	a.BPL("ce_dy_loop")
	a.Label("ce_dy_done")
	a.STY_abs(uint16((linkOAMCopyAddr + linkOAMCopyDying) & 0xFFFF))
	a.PLB()
	a.RTL()
}

// nearDyingEnemy reports whether the captured entry sp is within about 24 pixels of an enemy in dying, the screen
// positions listed at linkOAMCopyDying (the window the game's death poofs and explosions draw in).
func nearDyingEnemy(sp *linkSprite, dying [][2]int) bool {
	x := oamX(sp)
	y := int(sp.b[1])
	if y >= 0xE0 {
		y -= 0x100
	}
	for _, d := range dying {
		if dx := x - d[0]; dx < -24 || dx >= 32 {
			continue
		}
		if dy := y - d[1]; dy < -32 || dy >= 32 {
			continue
		}
		return true
	}
	return false
}

// isEnemyDeathChr reports whether chr is one of enemyDeathChrs.
func isEnemyDeathChr(chr uint8) bool {
	for _, c := range enemyDeathChrs {
		if uint16(chr) == c {
			return true
		}
	}
	return false
}
