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
// still hold the sentinel, nothing used those chrs this frame and the remote player's head and body are uploaded over
// them in the same NMI, right after the game's own upload. Otherwise the remote player is drawn as a blob instead.
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
	remoteTableAddr = uint32(0x707EE8)

	// frames left to draw the table, counted down every frame:
	rtTTL = 0
	// frames left to capture the local player's sparkles and dust for o2 to send (see emitCaptureExtras), counted down
	// every frame; o2 keeps it up while any remote player is in the same location:
	rtCaptureTTL = 1
	// $1B (1 = underworld) and $A0 (underworld room) or $8A (overworld area) the table is for:
	rtIndoors  = 2
	rtLocation = 4
	// byte offsets into rtEntries: the end of the shadow and effect sprites, the end of the body sprites and the end
	// of all entries:
	rtExtrasEnd = 6
	rtBodyEnd   = 8
	rtEnd       = 10
	// LinkGraphics DMA source addresses: top row of chrs $20/$21, $22/$23, bottom row of chrs $30/$31, $32/$33:
	rtSrc = 12
	// sprites to draw in order, each drawn in front of the ones before: world x, world y, chr, attributes, size ($02 for
	// 16x16). Up to 2 shadow sprites, remoteEffectMax sparkle and dust sprites and 2 body sprites (chrs $20 and $22)
	// of one player are followed by one blob per player (chr $64), starting with the body's player, whose blob is only
	// drawn when the body can't be:
	rtEntries       = 20
	rtEntrySize     = 7
	remoteShadowMax = 2
	remoteEffectMax = 4
	remoteBlobMax   = 6
	remoteEntryMax  = remoteShadowMax + remoteEffectMax + 2 + remoteBlobMax

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
	// 2 when neither dma_var6 nor dma_var7 was set by the module this frame:
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
	// blob OAM attributes: priority 2 (like Link), palette 7 (Link's):
	remoteBlobAttr = 0x2E

	// nmiHookPatchAddr is where NMI_DoUpdates sets up the animated tiles upload, right after uploading the follower
	// and NPC tiles; patched with a JSL to the NMI hook:
	nmiHookPatchAddr = uint32(0x008B50)
	// linkGfxBankAddr is the `LDA.b #LinkGraphics>>16` in NMI_DoUpdates, which randomizers replace with `LDA.b $BC`:
	linkGfxBankAddr = uint32(0x008A01)
)

// nmiHookPatchExpected is the original code at nmiHookPatchAddr: LDX.w $0ADC; STX.w DMA0ADDRL
var nmiHookPatchExpected = []byte{0xAE, 0xDC, 0x0A, 0x8E, 0x02, 0x43}

// remoteTTLFrames is the TTL o2 writes to the table, in frames:
const remoteTTLFrames = 60

// remoteTableRefresh is how often o2 rewrites an unchanged table to keep its TTL from running out:
const remoteTableRefresh = 250 * time.Millisecond

// remoteSpritesStale is how long received sprites are used to draw a remote player's body:
const remoteSpritesStale = 500 * time.Millisecond

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

// emitRemoteDraw emits the routine that the trampoline calls with JSR after the module ran. It restores dma_var6/7 and
// draws the remote sprites table into the OAM buffer. Called with A and X 8-bit; preserves P and DB.
func emitRemoteDraw(a *asm.Emitter) {
	// the table and state are accessed with absolute addressing while DB = $70:
	st := func(o uint32) uint16 { return uint16((remoteStateAddr + o) & 0xFFFF) }
	tb := func(o uint32) uint16 { return uint16((remoteTableAddr + o) & 0xFFFF) }

	if a.Label("remoteDraw") != remoteDrawAddr {
		panic(fmt.Errorf("remoteDrawAddr expected to be $%06x but got $%06x", a.PC(), remoteDrawAddr))
	}
	a.PHP()
	a.PHB()
	a.PHK()
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
	a.RTS()
	a.Label("rd_ttl_ok")
	a.DEC_abs(tb(rtTTL))

	a.Comment("only in the underworld, overworld and special overworld modules:")
	a.LDA_long(0x7E0010)
	a.CMP_imm8_b(0x07)
	a.BEQ("rd_module_ok")
	a.CMP_imm8_b(0x09)
	a.BEQ("rd_module_ok")
	a.CMP_imm8_b(0x0B)
	a.BNE("rd_exit")
	a.Label("rd_module_ok")

	a.Comment("only in the room or area the table was made for:")
	a.LDA_long(0x7E001B)
	emitCMPAbs(a, tb(rtIndoors))
	a.BNE("rd_exit")
	a.LDA_long(0x7E001B)
	a.REP(0x30)
	a.BEQ("rd_overworld")
	a.LDA_long(0x7E00A0)
	a.BRA("rd_location")
	a.Label("rd_overworld")
	a.LDA_long(0x7E008A)
	a.Label("rd_location")
	emitCMPAbs(a, tb(rtLocation))
	a.BNE("rd_exit")

	a.Comment("local scroll offsets:")
	a.LDA_long(0x7E00E2)
	a.SEC()
	emitSBCLong(a, 0x7E011A)
	a.STA_abs(st(rsScrollX))
	a.LDA_long(0x7E00E8)
	a.SEC()
	emitSBCLong(a, 0x7E011C)
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
	emitCMPAbs(a, tb(rtExtrasEnd))
	a.BCS("rd_shadows_done")
	a.JSR_abs(uint16(remoteDrawSpriteAddr & 0xFFFF))
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.TAY()
	a.BRA("rd_shadow_loop")
	a.Label("rd_shadows_done")

	a.Comment("draw the body if there is one, nothing else uses chrs $20..$23, and it's safe to upload them:")
	a.SEP(0x20)
	a.LDA_abs(st(rsFree))
	a.CMP_imm8_b(0x02)
	a.BNE("rd_no_body")
	a.LDA_long(0x7E0011)
	a.BNE("rd_no_body")
	a.LDA_long(0x7E0710)
	a.BNE("rd_no_body")
	a.REP(0x20)
	a.TYA()
	emitCMPAbs(a, tb(rtBodyEnd))
	a.BCS("rd_blobs")
	a.Label("rd_body_loop")
	a.JSR_abs(uint16(remoteDrawSpriteAddr & 0xFFFF))
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.TAY()
	emitCMPAbs(a, tb(rtBodyEnd))
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
	emitCMPAbs(a, tb(rtEnd))
	a.BCS("rd_done")
	a.JSR_abs(uint16(remoteDrawSpriteAddr & 0xFFFF))
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(rtEntrySize)
	a.TAY()
	a.BRA("rd_blob_loop")

	a.Label("rd_done")
	a.PLB()
	a.PLP()
	a.RTS()
	a.AssumeSEP(0x30)
}

// emitRemoteDrawSprite emits the subroutine that draws the sprite of the table entry at offset Y into the first
// unused OAM entry below offset X, and updates X. Called with A, X and Y 16-bit and DB = $70; preserves Y.
func emitRemoteDrawSprite(a *asm.Emitter) {
	st := func(o uint32) uint16 { return uint16((remoteStateAddr + o) & 0xFFFF) }
	tb := func(o uint32) uint16 { return uint16((remoteTableAddr + o) & 0xFFFF) }

	if a.Label("remoteDrawSprite") != remoteDrawSpriteAddr {
		panic(fmt.Errorf("remoteDrawSpriteAddr expected to be $%06x but got $%06x", a.PC(), remoteDrawSpriteAddr))
	}
	a.AssumeREP(0x30)
	a.Comment("screen x = world x - scroll x, visible if -16 < x < 256:")
	emitLDAAbsY(a, tb(rtEntries+0))
	a.SEC()
	emitSBCAbs(a, st(rsScrollX))
	a.STA_abs(st(rsTmpSX))
	a.CLC()
	a.ADC_imm16_w(0x0010)
	a.CMP_imm16_w(0x0110)
	a.BCS("rds_return")
	a.Comment("screen y = world y - scroll y, visible if -16 < y < 224:")
	emitLDAAbsY(a, tb(rtEntries+2))
	a.SEC()
	emitSBCAbs(a, st(rsScrollY))
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

	emitLDAAbsY(a, tb(rtEntries+4))
	emitORAAbs(a, st(rsAnim))
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
	emitORAAbsY(a, tb(rtEntries+6))
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
}

// emitNMIHook emits the routine that NMI_DoUpdates calls with JSL from nmiHookPatchAddr, after it uploaded the
// follower and NPC tiles. It uploads the remote player's head and body tiles over chrs $20..$23 and $30..$33 when the
// trampoline enabled it this frame, then runs the code it was patched over. Called with A 8-bit, X and Y 16-bit, DB =
// $00 (or $80), D = $0000; DMA channels 0 and 1 are set up for VRAM writes ($1801) and VMAIN is $80.
//
// linkGfxBankIns is the 2 byte instruction at linkGfxBankAddr that loads the bank of Link's graphics.
func emitNMIHook(a *asm.Emitter, linkGfxBankIns []byte) {
	st := func(o uint32) uint32 { return remoteStateAddr + o }

	a.Label("nmiHook")
	a.AssumeSEP(0x20)
	a.AssumeREP(0x10)
	a.LDA_long(st(rsNMIEnable))
	a.BEQ("nmi_done")

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

	a.Label("nmi_done")
	a.Comment("the code patched over:")
	a.LDX_abs(0x0ADC)
	a.STX_abs(0x4302)
	a.RTL()
	a.AssumeSEP(0x30)
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

// remoteBodySprites returns up to two 16x16 sprites of Link's head and body from a remote player's sprites, ordered by
// OAM index (front to back).
func remoteBodySprites(list []remoteSprite) (body []remoteSprite) {
	sorted := make([]remoteSprite, len(list))
	copy(sorted, list)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].index < sorted[j].index })

	for _, sp := range sorted {
		if sp.size == 0 || sp.b[1] == 0xF0 {
			continue
		}
		// Link's head and body are drawn from chrs $00..$03 and $10..$13:
		chr := uint16(sp.b[2]) | uint16(sp.b[3]&1)<<8
		if chr >= 0x20 || chr&0x0F >= 0x04 {
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

// isRemoteShadowChr reports whether chr is one of Link's shadow tiles: $6C on the ground, $28 and $38 while recoiling
// or jumping off a ledge (kLinkShadows_Chardata).
func isRemoteShadowChr(chr uint8) bool {
	return chr == 0x6C || chr == 0x28 || chr == 0x38
}

// isRemoteEffectChr reports whether chr is a sword sparkle or dash dust tile.
func isRemoteEffectChr(chr uint8) bool {
	for _, c := range captureChrs {
		if uint16(chr) == c {
			return true
		}
	}
	return false
}

// remoteExtraSprites returns up to remoteShadowMax 8x8 shadow sprites followed by up to remoteEffectMax 8x8 sparkle
// and dust sprites from a remote player's sprites, each ordered by OAM index.
func remoteExtraSprites(list []remoteSprite) (extras []remoteSprite) {
	sorted := make([]remoteSprite, len(list))
	copy(sorted, list)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].index < sorted[j].index })

	var effects []remoteSprite
	shadows := 0
	for _, sp := range sorted {
		if sp.b[1] == 0xF0 || sp.size != 0 || sp.b[3]&1 != 0 {
			continue
		}
		switch chr := sp.b[2]; {
		case isRemoteShadowChr(chr) && shadows < remoteShadowMax:
			extras = append(extras, sp)
			shadows++
		case isRemoteEffectChr(chr) && len(effects) < remoteEffectMax:
			effects = append(effects, sp)
		}
	}
	return append(extras, effects...)
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
}

// remoteBodyOK reports whether the patched ROM has the NMI hook that uploads the body tiles: a JSL into the ROM area
// the patcher uses.
func (g *Game) remoteBodyOK() bool {
	pak, err := lorom.BusAddressToPak(nmiHookPatchAddr)
	if err != nil || int(pak)+4 > len(g.rom.Contents) {
		return false
	}
	c := g.rom.Contents[pak : pak+4]
	target := uint32(c[1]) | uint32(c[2])<<8 | uint32(c[3])<<16
	return c[0] == 0x22 && target >= romGarbageStart && target < romGarbageEnd
}

// buildRemoteSpritesTable makes the remote sprites table for the local player's current location, with both TTLs 0.
// draw reports whether there is anything to draw; capture whether any remote player is in the same location (and
// would see the local player's sparkles and dust).
func (g *Game) buildRemoteSpritesTable(now time.Time) (t [remoteTableSize]byte, draw, capture bool) {
	local := g.local
	if local == nil || !local.IsInGame() || local.isBadTimeToDraw() {
		return
	}

	remotes := append([]*Player(nil), g.RemotePlayers()...)
	sort.Slice(remotes, func(i, j int) bool { return remotes[i].Index() < remotes[j].Index() })

	// players to draw, the one drawn with Link's head and body first; extras is the shadow, sparkles and dust of the
	// first player with recent sprites, preferably the body's:
	var players []*Player
	var body, extras []remoteSprite
	bodyOK := g.remoteBodyOK()
	for _, p := range remotes {
		if p == local || p.TTL() <= 0 || !p.IsInGame() || p.isBadTimeToDraw() {
			continue
		}
		if !locationsEqual(local.Location, p.Location) {
			continue
		}
		capture = true
		if !g.SyncSprites || len(players) >= remoteBlobMax {
			continue
		}
		fresh := now.Sub(p.Sprites.received) < remoteSpritesStale
		if bodyOK && body == nil && fresh {
			if body = remoteBodySprites(p.Sprites.list); body != nil {
				players = append([]*Player{p}, players...)
				extras = remoteExtraSprites(p.Sprites.list)
				continue
			}
		}
		if body == nil && extras == nil && fresh {
			// until a player with a body turns up:
			if extras = remoteExtraSprites(p.Sprites.list); extras != nil {
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
		return uint16(int(p.XOffs) + x), uint16(int(p.YOffs) + y)
	}

	// shadow, sparkles and dust, behind the body:
	for _, sp := range extras {
		x, y := world(players[0], sp)
		entry(x, y, sp.b[2], sp.b[3], sp.size)
	}
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
	players, body := remoteTableCounts(&t)
	if lastPlayers, lastBody := remoteTableCounts(&s.written); !s.writtenOK || lastPlayers != players || lastBody != body {
		log.Printf("alttp: remote sprites: drawing %d players (%d body sprites)\n", players, body)
	}
	s.written, s.writtenOK, s.writtenTime = t, true, now
}

// Sword sparkles and dash dust
//
// alttpo players see everything o2 sends of the local player (see sprites.go). Besides Link's own OAM entries, o2 sends
// the sparkles of charging a spin attack, spinning and poking walls with the sword, and the dust of dashing. They are
// ancillae, drawn into whichever OAM entries the ancilla OAM regions hand out, so the trampoline finds them by their
// chrs (all in the common sprite sheets, which receivers draw from their own VRAM) and copies up to linkOAMExtrasMax
// of them to SRAM after the copy of Link's entries. The scan of the OAM buffer only runs on frames where one of those
// ancillae exists and while o2 keeps rtCaptureTTL up because a remote player is in the same location.

// SRAM code and ROM addresses of the capture routine:
const (
	// captureTypesAddr is a table in ROM of the ancilla types $00..$3F whose sprites are captured:
	captureTypesAddr = uint32(0x1BB600)
	// captureChrsAddr is a table in ROM of the chrs $80..$FF that are captured:
	captureChrsAddr = captureTypesAddr + 0x40
	// captureExtrasAddr is the capture routine in ROM, called by the trampoline with JSL:
	captureExtrasAddr = captureChrsAddr + 0x80
)

// captureAncillae are the ancilla types whose sprites are captured:
var captureAncillae = []uint8{
	0x0D, // spin attack fully charged spark
	0x1B, // sword tapping (poking) a wall
	0x1E, // dash dust
	0x26, // sword swing sparkle (stabbing with the tempered sword or better)
	0x2A, // spin attack sparkle
	0x2B, // spin attack sparkle
	0x3C, // spin attack charging sparkle
}

// captureChrs are the chrs those ancillae draw with (8x8, name table 0):
var captureChrs = []uint16{0x80, 0x81, 0x82, 0x83, 0x90, 0x92, 0x93, 0xA9, 0xB7, 0xB9, 0xCF, 0xD7, 0xDF}

// emitCaptureExtras emits the ancilla type table at captureTypesAddr, the chr table at captureChrsAddr and the routine at captureExtrasAddr that copies
// the sparkle and dust OAM entries to linkOAMCopyAddr + linkOAMCopyExtras. Called with JSL with A and X 8-bit;
// preserves P and DB.
func emitCaptureExtras(a *asm.Emitter) {
	if a.PC() != captureTypesAddr {
		panic(fmt.Errorf("captureTypesAddr expected to be $%06x but got $%06x", a.PC(), captureTypesAddr))
	}
	var types [0x40]byte
	for _, t := range captureAncillae {
		types[t] = 1
	}
	a.Comment("ancilla types whose sprites are captured:")
	a.EmitBytes(types[:])
	var chrs [0x80]byte
	for _, c := range captureChrs {
		chrs[c-0x80] = 1
	}
	a.Comment("chrs $80..$FF that are captured:")
	a.EmitBytes(chrs[:])

	if a.Label("captureExtras") != captureExtrasAddr {
		panic(fmt.Errorf("captureExtrasAddr expected to be $%06x but got $%06x", a.PC(), captureExtrasAddr))
	}
	// the extras in SRAM are accessed with absolute addressing while DB = $70:
	ex := func(o uint32) uint16 { return uint16((linkOAMCopyAddr + linkOAMCopyExtras + o) & 0xFFFF) }

	a.PHP()
	a.PHB()
	a.SEP(0x30)
	a.LDA_imm8_b(0x00)
	a.STA_long(linkOAMCopyAddr + linkOAMCopyExtras)
	a.Comment("only while a remote player is in the same location:")
	a.LDA_long(remoteTableAddr + rtCaptureTTL)
	a.BEQ("ce_exit")
	a.SEC()
	a.SBC_imm8_b(0x01)
	a.STA_long(remoteTableAddr + rtCaptureTTL)

	a.Comment("only while a sparkle or dust ancilla exists:")
	a.PHK()
	a.PLB()
	a.LDX_imm8_b(0x09)
	a.Label("ce_type")
	a.LDA_long_x(0x7E0C4A)
	a.CMP_imm8_b(0x40)
	a.BCS("ce_next_type")
	a.TAY()
	emitLDAAbsY(a, uint16(captureTypesAddr&0xFFFF))
	a.BNE("ce_scan")
	a.Label("ce_next_type")
	a.DEX()
	a.BPL("ce_type")
	a.Label("ce_exit")
	a.PLB()
	a.PLP()
	a.RTL()

	a.Label("ce_scan")
	a.Comment("DB = $70 for the extras; X = OAM buffer offset; Y = extras offset:")
	a.PEA(0x7070)
	a.PLB()
	a.PLB()
	a.REP(0x30)
	a.LDY_imm16_w(0x0000)
	a.LDX_imm16_w(0x01FC)
	a.Label("ce_loop")
	a.Comment("chr $80..$FF in name table 0:")
	a.LDA_long_x(0x7E0802)
	a.AND_imm16_w(0x0180)
	a.CMP_imm16_w(0x0080)
	a.BNE("ce_next")
	a.Comment("one of the captured chrs:")
	a.LDA_long_x(0x7E0802)
	a.PHX()
	a.AND_imm16_w(0x007F)
	a.TAX()
	a.LDA_long_x(captureChrsAddr)
	a.PLX()
	a.AND_imm16_w(0x00FF)
	a.BEQ("ce_next")
	a.Comment("drawn this frame:")
	a.LDA_long_x(0x7E0801)
	a.AND_imm16_w(0x00FF)
	a.CMP_imm16_w(0x00F0)
	a.BEQ("ce_next")
	a.Comment("8x8 only; extended attributes are at $0A20 + X/4:")
	a.PHX()
	a.TXA()
	a.LSR()
	a.LSR()
	a.TAX()
	a.SEP(0x20)
	a.LDA_long_x(0x7E0A20)
	a.CMP_imm8_b(0x02)
	a.BCS("ce_skip")
	emitSTAAbsY(a, ex(1+4))
	a.TXA()
	a.ASL()
	emitORAAbsY(a, ex(1+4))
	emitSTAAbsY(a, ex(1+4))
	a.REP(0x20)
	a.PLX()
	a.LDA_long_x(0x7E0800)
	emitSTAAbsY(a, ex(1+0))
	a.LDA_long_x(0x7E0802)
	emitSTAAbsY(a, ex(1+2))
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(linkOAMExtraSize)
	a.TAY()
	emitCPYImm16(a, linkOAMExtrasMax*linkOAMExtraSize)
	a.BCS("ce_done")
	a.BRA("ce_next")
	a.Label("ce_skip")
	a.REP(0x20)
	a.PLX()
	a.Label("ce_next")
	a.DEX()
	a.DEX()
	a.DEX()
	a.DEX()
	a.BPL("ce_loop")
	a.Label("ce_done")
	a.SEP(0x20)
	a.TYA()
	a.STA_abs(ex(0))
	a.PLB()
	a.PLP()
	a.RTL()
	a.AssumeSEP(0x30)
}
