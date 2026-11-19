package alttp

import (
	"os"
	"testing"

	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

const (
	scBushTile   = uint16(0x0036)
	scBushToss   = uint8(2)
	scDefsAddr   = uint32(0x0F8000)
	scTypesAddr  = uint32(0x0E9459)
	scBushChr    = uint16(0x0100)
	scSecretItem = uint8(0x0B) // heart
	scPotItem    = uint8(0x08) // small key
	// the stubs record their calls at $7FE000:
	scRecord = uint32(0x1E000)
	// overworld area origin: $0708 = Y, $070C = X >> 3:
	scOriginY = uint16(0x0400)
	scOriginX = uint16(0x0080)
)

func (h *skScenario) romWrite(i int, addr uint32, b ...byte) {
	pak, err := lorom.BusAddressToPak(addr)
	if err != nil {
		h.t.Fatal(err)
	}
	copy(h.gs[i].e.ROM[pak:], b)
}

// stubSecrets (also called by newManipScenario) stubs Overworld_RevealSecret, RevealPotItem and Sprite_SpawnThrowableTerrain_silently to record their
// calls at $7FE000 (MakeTestROM made the bush tile a small liftable object; see writeTestLiftables).
func (h *skScenario) stubSecrets() {
	for i, gs := range h.gs {
		fns := gs.g.romFunctions
		h.romWrite(i, fns[fnBank1BRTL], 0x6B)
		h.romWrite(i, fns[fnBank01RTL], 0x6B)
		h.romWrite(i, fns[fnOverworldRevealSecret],
			0x8A,                   // TXA
			0x8F, 0x00, 0xE0, 0x7F, // STA $7FE000
			0xAD, 0x9C, 0x0B, // LDA $0B9C
			0x29, 0x00, 0xFF, // AND #$FF00
			0x09, scSecretItem, 0x00, // ORA #item
			0x8D, 0x9C, 0x0B, // STA $0B9C
			0x60, // RTS
		)
		h.romWrite(i, fns[fnRevealPotItem],
			0x8F, 0x10, 0xE0, 0x7F, // STA $7FE010
			0xA5, 0x06, // LDA $06
			0x8F, 0x12, 0xE0, 0x7F, // STA $7FE012
			0xAD, 0x9C, 0x0B, // LDA $0B9C
			0x29, 0x00, 0xFF, // AND #$FF00
			0x09, scPotItem, 0x00, // ORA #item
			0x8D, 0x9C, 0x0B, // STA $0B9C
			0x60, // RTS
		)
		h.romWrite(i, fns[fnSpawnThrowableTerrainSilently],
			0x8F, 0x02, 0xE0, 0x7F, // STA $7FE002: type
			0xAD, 0x9C, 0x0B, // LDA $0B9C
			0x8F, 0x03, 0xE0, 0x7F, // STA $7FE003: secret
			0xA5, 0x00, 0x8F, 0x04, 0xE0, 0x7F, // $00..$03: X, Y
			0xA5, 0x01, 0x8F, 0x05, 0xE0, 0x7F,
			0xA5, 0x02, 0x8F, 0x06, 0xE0, 0x7F,
			0xA5, 0x03, 0x8F, 0x07, 0xE0, 0x7F,
			0xAF, 0x08, 0xE0, 0x7F, // LDA $7FE008
			0x1A,                   // INC A
			0x8F, 0x08, 0xE0, 0x7F, // STA $7FE008: calls
			0xA2, 0x05, // LDX #$05
			0xA9, 0x0A, // LDA #$0A
			0x9D, 0xD0, 0x0D, // STA $0DD0,X: carried
			0xA9, 0x02, // LDA #$02
			0x8D, 0x14, 0x03, // STA $0314
			0x8D, 0xB2, 0x0F, // STA $0FB2
			0x6B, // RTL
		)
	}
}

// writeTestLiftables makes the bush tile the only small liftable object, of thrown object type 2: its top left 8x8
// tile has the bush tile type $50. MakeTestROM calls it before the ROM is patched, since the patch reads the liftable
// objects from the ROM (see emitMap16Update).
func writeTestLiftables(rom *snes.ROM) {
	write := func(addr uint32, b ...byte) {
		pak, err := lorom.BusAddressToPak(addr)
		if err != nil {
			panic(err)
		}
		copy(rom.Contents[pak:], b)
	}
	write(owLiftableTileOperands[0], uint8(scBushTile), uint8(scBushTile>>8))
	write(owMap16DefinitionsOperand, uint8(scDefsAddr&0xFF), uint8(scDefsAddr>>8&0xFF), uint8(scDefsAddr>>16))
	write(owOverworldTileTypesOperand, uint8(scTypesAddr&0xFF), uint8(scTypesAddr>>8&0xFF), uint8(scTypesAddr>>16))
	write(scDefsAddr+uint32(scBushTile)<<3, uint8(scBushChr&0xFF), uint8(scBushChr>>8))
	write(scTypesAddr+uint32(scBushChr), 0x50)
}

func (h *skScenario) spawnCalls(i int) int { return int(h.gs[i].e.WRAM[scRecord+8]) }

// bushArea puts the player in an overworld area with a bush at m16Pos.
func (h *skScenario) bushArea(i int, area uint16) {
	h.inArea(i, area)
	h.setWRAMU16(i, 0x0708, scOriginY)
	h.setWRAMU16(i, 0x070C, scOriginX)
	h.setWRAMU16(i, 0x2000+uint32(m16Pos), scBushTile)
	h.setWRAMU16(i, 0x0B9C, 0x1200)
	h.setWRAMU16(i, 0x0314, 0x0000)
	h.setWRAMU16(i, 0x0FB2, 0x0000)
}

func TestSecretsScenario_LiftBushRevealsSecret(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.stubSecrets()
	h.bushArea(0, m16Area)
	h.bushArea(1, m16Area)
	h.frames(map16LiveAfter + skSettleFrames)

	h.changeMap16(0, m16Pos, m16Bush)
	h.frames(skSettleFrames)
	h.expectTile("g1 lifted the bush", 1, m16Pos, m16Bush, true)
	if n := h.spawnCalls(1); n != 1 {
		t.Fatalf("g2: expected the secret spawned once; got %d", n)
	}
	if n := h.spawnCalls(0); n != 0 {
		t.Errorf("g1: expected nothing spawned; got %d", n)
	}
	e := h.gs[1].e
	if pos := h.wramU16(1, scRecord); pos != m16Pos {
		t.Errorf("g2: expected Overworld_RevealSecret at $%04x; got $%04x", m16Pos, pos)
	}
	if typ := e.WRAM[scRecord+2]; typ != scBushToss {
		t.Errorf("g2: expected thrown object type %d; got %d", scBushToss, typ)
	}
	if s := e.WRAM[scRecord+3]; s != scSecretItem {
		t.Errorf("g2: expected secret $%02x; got $%02x", scSecretItem, s)
	}
	x, y := h.wramU16(1, scRecord+4), h.wramU16(1, scRecord+6)
	if wx, wy := (m16Pos&0x7E+scOriginX)<<3, (m16Pos&0x1F80)>>3+scOriginY; x != wx || y != wy {
		t.Errorf("g2: expected the secret at $%04x,$%04x; got $%04x,$%04x", wx, wy, x, y)
	}
	if e.WRAM[0x0DD0+5] != 0 {
		t.Errorf("g2: expected the carried object sprite removed")
	}
	if b := h.wramU16(1, 0x0B9C); b != 0x1200 {
		t.Errorf("g2: expected $0B9C restored; got $%04x", b)
	}
	if v := h.wramU16(1, 0x0314) | h.wramU16(1, 0x0FB2); v != 0 {
		t.Errorf("g2: expected $0314 and $0FB2 restored")
	}

	// nothing is revealed again, also not when the area reloads and the change applies again:
	h.frames(skSettleFrames * 4)
	h.gs[1].e.WRAM[skSubModule] = 0x01
	h.frames(2)
	h.bushArea(1, m16Area)
	h.frames(map16LiveAfter + skSettleFrames)
	h.expectTile("g2 reloaded the area", 1, m16Pos, m16Bush, true)
	if n := h.spawnCalls(1); n != 1 {
		t.Errorf("g2: expected the secret spawned once; got %d", n)
	}
}

func TestSecretsScenario_EnterAreaDoesNotRevealSecrets(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.stubSecrets()
	h.bushArea(0, m16Area)
	h.bushArea(1, m16OtherArea)
	h.frames(map16LiveAfter + skSettleFrames)

	h.changeMap16(0, m16Pos, m16Bush)
	h.frames(skSettleFrames)

	h.gs[1].e.WRAM[skSubModule] = 0x01
	h.frames(2)
	h.bushArea(1, m16Area)
	h.frames(map16LiveAfter + skSettleFrames)
	h.expectTile("g2 entered", 1, m16Pos, m16Bush, true)
	if n := h.spawnCalls(1); n != 0 {
		t.Errorf("g2: expected nothing spawned for a bush lifted before it came; got %d", n)
	}
}

func TestSecretsScenario_BushAlreadyGoneNotRevealed(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.stubSecrets()
	h.bushArea(0, m16Area)
	h.bushArea(1, m16Area)
	// g2's bush is gone already (e.g. cut with the sword, which is not logged):
	h.setWRAMU16(1, 0x2000+uint32(m16Pos), 0x0DC5)
	h.frames(map16LiveAfter + skSettleFrames)

	h.changeMap16(0, m16Pos, m16Bush)
	h.frames(skSettleFrames)
	h.expectTile("g1 lifted the bush", 1, m16Pos, m16Bush, true)
	if n := h.spawnCalls(1); n != 0 {
		t.Errorf("g2: expected nothing spawned; got %d", n)
	}
}

func (h *skScenario) potRoom(i int, room uint16, pot mnObj) {
	h.loadRoom(i, room, pot)
	h.setWRAMU16(i, 0x0B9C, 0x1200)
}

func TestSecretsScenario_LiftPotRevealsItem(t *testing.T) {
	h := newManipScenario(t, 2)
	pot := mnObj{0x1111, mnPotObjX, mnPotPos, mnPotTiles}
	h.potRoom(0, mnRoom, pot)
	h.potRoom(1, mnRoom, pot)
	h.settleStar(skSettleFrames * 6)

	h.liftPot(0, mnPotObjX)
	h.settleStar(skSettleFrames * 4)
	h.expectPotGone("g1 lifted", 1, mnPotObjX, mnPotTiles)
	if n := h.spawnCalls(1); n != 1 {
		t.Fatalf("g2: expected the item spawned once; got %d", n)
	}
	e := h.gs[1].e
	if pos := h.wramU16(1, scRecord+0x10); pos != mnPotPos {
		t.Errorf("g2: expected RevealPotItem at $%04x; got $%04x", mnPotPos, pos)
	}
	if a := h.wramU16(1, scRecord+0x12); uint32(a) != manipAttrIndex(mnPotPos) {
		t.Errorf("g2: expected RevealPotItem with $06 = $%04x; got $%04x", manipAttrIndex(mnPotPos), a)
	}
	if s := e.WRAM[scRecord+3]; s != scPotItem {
		t.Errorf("g2: expected secret $%02x; got $%02x", scPotItem, s)
	}
	x, y := h.wramU16(1, scRecord+4), h.wramU16(1, scRecord+6)
	if wx, wy := potCoords(mnRoom, mnPotPos); x != wx || y != wy {
		t.Errorf("g2: expected the item at $%04x,$%04x; got $%04x,$%04x", wx, wy, x, y)
	}
	if e.WRAM[0x0DD0+5] != 0 {
		t.Errorf("g2: expected the carried object sprite removed")
	}
	if b := h.wramU16(1, 0x0B9C); b != 0x1200 {
		t.Errorf("g2: expected $0B9C restored; got $%04x", b)
	}
	h.settleStar(skSettleFrames * 6)
	if n := h.spawnCalls(1); n != 1 {
		t.Errorf("g2: expected the item spawned once; got %d", n)
	}
}

func TestSecretsScenario_EnterRoomDoesNotRevealPotItems(t *testing.T) {
	h := newManipScenario(t, 2)
	pot := mnObj{0x1111, mnPotObjX, mnPotPos, mnPotTiles}
	h.potRoom(0, mnRoom, pot)
	h.potRoom(1, mnOtherRoom, pot)
	h.settleStar(skSettleFrames * 6)

	h.liftPot(0, mnPotObjX)
	h.settleStar(skSettleFrames * 6)

	h.gs[1].e.WRAM[skSubModule] = 0x02
	h.settleStar(2)
	h.potRoom(1, mnRoom, pot)
	h.settleStar(skSettleFrames * 6)
	h.expectPotGone("g2 entered", 1, mnPotObjX, mnPotTiles)
	if n := h.spawnCalls(1); n != 0 {
		t.Errorf("g2: expected nothing spawned for a pot lifted before it came; got %d", n)
	}
}

func TestSecretsScenario_LiftPegRevealsNothing(t *testing.T) {
	h := newManipScenario(t, 2)
	peg := mnObj{0x4040, 0x0040, 0x0610, [4]uint16{0x19D8, 0x19D9, 0x59D8, 0x59D9}}
	h.loadRoom(0, mnRoom, peg)
	h.loadRoom(1, mnRoom, peg)
	h.settleStar(skSettleFrames * 6)

	h.liftPot(0, peg.objx)
	h.settleStar(skSettleFrames * 4)
	h.expectPotGone("g1 pounded", 1, peg.objx, peg.tiles)
	if n := h.spawnCalls(1); n != 0 {
		t.Errorf("g2: expected nothing spawned for a peg; got %d", n)
	}
}

// TestReadOwLiftables reads the small liftable objects from US and JP ROMs; see TestSwordShieldGfx_MatchesGame for the
// ROMs used.
func TestReadOwLiftables(t *testing.T) {
	for _, tc := range []struct {
		name, env, path string
	}{
		{"US", "O2_TEST_ROM_US", "../../../alttp-us.sfc"},
		{"JP", "O2_TEST_ROM_JP", "../../../alttp-jp.sfc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := os.Getenv(tc.env)
			if path == "" {
				path = tc.path
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("no %s ROM available (set %s): %v", tc.name, tc.env, err)
			}
			rom, err := snes.NewROM(path, contents)
			if err != nil {
				t.Fatal(err)
			}
			list := readOwLiftables(rom)
			dark := uint16(0x072A)
			if tc.name == "JP" {
				dark = 0x0727
			}
			want := []owLiftable{{0x0036, 2}, {dark, 4}, {0x020F, 1}, {0x0239, 5}, {0x0101, 0}}
			if len(list) != len(want) {
				t.Fatalf("expected %v; got %v", want, list)
			}
			for k := range want {
				if list[k] != want[k] {
					t.Errorf("expected %v; got %v", want, list)
					break
				}
			}
		})
	}
}

func TestSecretsScenario_ManyBushesAtOnce(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.stubSecrets()
	h.bushArea(0, m16Area)
	h.bushArea(1, m16Area)
	for i := 0; i < 2; i++ {
		for k := uint16(1); k < map16MaxPerUpdate+2; k++ {
			h.setWRAMU16(i, 0x2000+uint32(m16Pos+k*2), scBushTile)
		}
	}
	h.frames(map16LiveAfter + skSettleFrames)

	for k := uint16(0); k < map16MaxPerUpdate+2; k++ {
		h.changeMap16(0, m16Pos+k*2, m16Bush)
	}
	h.frames(skSettleFrames * 2)
	for k := uint16(0); k < map16MaxPerUpdate+2; k++ {
		h.expectTile("g1 lifted the bushes", 1, m16Pos+k*2, m16Bush, true)
	}
	if n := h.spawnCalls(1); n != map16MaxPerUpdate+2 {
		t.Errorf("g2: expected %d secrets spawned; got %d", map16MaxPerUpdate+2, n)
	}
}
