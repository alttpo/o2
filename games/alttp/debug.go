package alttp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"o2/interfaces"
	"sync"
	"time"

	"github.com/alttpo/snes/asm"
)

// Debug tools
//
// The web UI's /debug page gives and takes away items, fills up the player and kills enemies to speed up debugging.
// Every change is made by an asm routine the game runs in a frame (the same way syncs are applied), so that a change
// can't be lost to a write racing with the game. Like any change the local player makes, a change to the SRAM
// copy in WRAM ($7EF340..$7EF3FF) is seen by the sync code and shared with the group according to the sync settings.
//
// The page has two parts:
//
//   - "game/debug", a view model with the SRAM copy at $7EF340..$7EF3FF so the page shows what the player has;
//   - the "debug" command with a list of operations, and the "debugGfx" command, which returns the HUD's 2bpp graphics
//     and palette that the item menu draws its icons with. The web UI draws the icons from them in canvases, so that no
//     image is stored: they come from the ROM the game was patched from.

const (
	// debugSRAMFirst..debugSRAMLast is the part of the SRAM copy in WRAM the page can change and shows:
	debugSRAMFirst = 0xF340
	debugSRAMLast  = 0xF3FF
	debugSRAMSize  = debugSRAMLast - debugSRAMFirst + 1

	// debugOpTTL is how long a debug operation waits for the game to be in a module that runs update routines:
	debugOpTTL = 5 * time.Second
	// debugMaxOps is how many operations are queued at most:
	debugMaxOps = 64
)

// Debug operation types:
const (
	// debugOpSet stores value V at SRAM copy address A.
	debugOpSet = "set"
	// debugOpOr sets the bits of mask M at address A; debugOpAnd keeps only the bits of mask M.
	debugOpOr  = "or"
	debugOpAnd = "and"
	// debugOpGfx refreshes the graphics and palette of Link's sword, shield or armor and gloves (W).
	debugOpGfx = "gfx"
	// debugOpKeys adds D (+1 or -1) to the small keys of the dungeon Link is in.
	debugOpKeys = "keys"
	// debugOpHearts adds C heart containers and P heart pieces (each +1 or -1).
	debugOpHearts = "hearts"
	// debugOpFill fills what W says up: hearts, magic, rupees, arrows or bombs.
	debugOpFill = "fill"
	// debugOpKill kills the enemies (not the items) of the room or area.
	debugOpKill = "kill"
)

// debugOp is an operation the web UI asks for.
type debugOp struct {
	T string `json:"t"`
	A uint16 `json:"a,omitempty"`
	V uint8  `json:"v,omitempty"`
	M uint8  `json:"m,omitempty"`
	W string `json:"w,omitempty"`
	D int    `json:"d,omitempty"`
	C int    `json:"c,omitempty"`
	P int    `json:"p,omitempty"`
}

// debugQueued is a debugOp waiting for the next update routine.
type debugQueued struct {
	op  debugOp
	due time.Time
}

// debugState is what the page shows of the game.
type debugState struct {
	// SRAM is the SRAM copy in WRAM, $7EF340..$7EF3FF:
	SRAM []uint8 `json:"sram"`
	// Dungeon is $040C, the dungeon Link is in ($FF in the overworld):
	Dungeon uint8 `json:"dungeon"`
	// InGame is whether Link is in a module the debug operations run in:
	InGame bool `json:"inGame"`
}

// debugLocal is the debug tools' state in the Game.
type debugLocal struct {
	lock  sync.Mutex
	queue []debugQueued
	// the state last sent to the view, to send it only when it changes:
	sent    debugState
	sentOK  bool
	labelNo int
}

func (g *Game) debugSnapshot() debugState {
	s := debugState{
		SRAM:    make([]uint8, debugSRAMSize),
		Dungeon: g.wram[0x040C],
		InGame:  g.local != nil && g.local.IsInGame(),
	}
	copy(s.SRAM, g.wram[debugSRAMFirst:debugSRAMLast+1])
	return s
}

// notifyDebugState sends the state the debug page shows when it changed. Called from the game loop.
func (g *Game) notifyDebugState() {
	if g.viewModels == nil || !g.notFirstFrame {
		return
	}

	g.stateLock.Lock()
	s := g.debugSnapshot()
	g.stateLock.Unlock()

	d := &g.debug
	d.lock.Lock()
	same := d.sentOK && d.sent.Dungeon == s.Dungeon && d.sent.InGame == s.InGame && string(d.sent.SRAM) == string(s.SRAM)
	if !same {
		d.sent, d.sentOK = s, true
	}
	d.lock.Unlock()
	if same {
		return
	}

	g.viewModels.NotifyView("game/debug", &s)
}

type debugCmd struct{ g *Game }
type debugArgs struct {
	Ops []debugOp `json:"ops"`
}

func (c *debugCmd) CreateArgs() interfaces.CommandArgs { return &debugArgs{} }

func (c *debugCmd) Execute(args interfaces.CommandArgs) error {
	f, ok := args.(*debugArgs)
	if !ok {
		return fmt.Errorf("invalid args type for command")
	}

	g := c.g
	d := &g.debug
	due := time.Now().Add(debugOpTTL)

	d.lock.Lock()
	defer d.lock.Unlock()
	for _, op := range f.Ops {
		if err := op.validate(); err != nil {
			log.Printf("alttp: debug: %v\n", err)
			continue
		}
		if len(d.queue) >= debugMaxOps {
			log.Printf("alttp: debug: too many operations queued; dropped %s\n", op.T)
			continue
		}
		d.queue = append(d.queue, debugQueued{op: op, due: due})
	}
	return nil
}

func (op *debugOp) validate() error {
	switch op.T {
	case debugOpSet, debugOpOr, debugOpAnd:
		if op.A < debugSRAMFirst || op.A > debugSRAMLast {
			return fmt.Errorf("%s: address $%04x is not in the SRAM copy $%04x..$%04x", op.T, op.A, debugSRAMFirst, debugSRAMLast)
		}
	case debugOpGfx:
		if op.W != "sword" && op.W != "shield" && op.W != "armor" {
			return fmt.Errorf("gfx: unknown equipment '%s'", op.W)
		}
	case debugOpKeys:
		if op.D != 1 && op.D != -1 {
			return fmt.Errorf("keys: bad change %d", op.D)
		}
	case debugOpHearts:
		if op.C < -1 || op.C > 1 || op.P < -1 || op.P > 1 {
			return fmt.Errorf("hearts: bad change %d, %d", op.C, op.P)
		}
	case debugOpFill:
		switch op.W {
		case "hearts", "magic", "rupees", "arrows", "bombs":
		default:
			return fmt.Errorf("fill: unknown thing '%s'", op.W)
		}
	case debugOpKill:
	default:
		return fmt.Errorf("unknown operation '%s'", op.T)
	}
	return nil
}

// generateDebugAsm appends the queued operations that fit to a, and returns whether it appended any. The operations
// that do not fit wait for the next routine. Called with the state lock held, while an update routine is generated.
func (g *Game) generateDebugAsm(a *asm.Emitter, newEmitter func() *asm.Emitter) bool {
	const epilogSize = 12
	// room for the other updates, so that debug operations don't starve them:
	const reserve = 0x100

	d := &g.debug
	d.lock.Lock()
	defer d.lock.Unlock()

	if len(d.queue) == 0 {
		return false
	}

	now := time.Now()
	updated := false
	n := 0
	for n < len(d.queue) {
		q := d.queue[n]
		if now.After(q.due) {
			log.Printf("alttp: debug: %s expired\n", q.op.T)
			n++
			continue
		}

		ta := newEmitter()
		d.labelNo++
		g.emitDebugOp(ta, &q.op, d.labelNo)
		if ta.Len() == 0 {
			// nothing to do, e.g. not in a dungeon:
			n++
			continue
		}
		if a.Len()+ta.Len()+epilogSize+reserve > updateBufSize {
			if !updated {
				log.Printf("alttp: debug: %s too big; dropped\n", q.op.T)
				n++
				continue
			}
			break
		}

		a.Comment(fmt.Sprintf("debug %s:", q.op.T))
		a.Append(ta)
		updated = true
		n++
	}
	d.queue = append(d.queue[:0], d.queue[n:]...)
	return updated
}

const debugMaxHealth = 0xA0

var (
	debugMaxArrows = [8]uint8{30, 35, 40, 45, 50, 55, 60, 70}
	debugMaxBombs  = [8]uint8{10, 15, 20, 25, 30, 35, 40, 50}
)

func sramLong(offs uint16) uint32 { return 0x7E0000 + uint32(offs) }

// emitDebugOp emits the routine of an operation in 8-bit mode (SEP #$30).
func (g *Game) emitDebugOp(a *asm.Emitter, op *debugOp, no int) {
	lbl := func(name string) string { return fmt.Sprintf("dbg%d_%s", no, name) }

	switch op.T {
	case debugOpSet:
		a.LDA_imm8_b(op.V)
		a.STA_long(sramLong(op.A))

	case debugOpOr:
		addr := sramLong(op.A)
		a.LDA_long(addr)
		a.ORA_imm8_b(op.M)
		a.STA_long(addr)

	case debugOpAnd:
		addr := sramLong(op.A)
		a.LDA_long(addr)
		a.AND_imm8_b(op.M)
		a.STA_long(addr)

	case debugOpGfx:
		switch op.W {
		case "sword":
			a.JSL(g.romFunctions[fnDecompGfxSword])
			a.JSL(g.romFunctions[fnUpdatePaletteSword])
		case "shield":
			a.JSL(g.romFunctions[fnDecompGfxShield])
			a.JSL(g.romFunctions[fnUpdatePaletteShield])
		case "armor":
			a.JSL(g.romFunctions[fnUpdatePaletteArmorGloves])
		}

	case debugOpKeys:
		// $7EF36F is the live key count in a dungeon, and $FF otherwise. The dungeon's slot is stale in the dungeon:
		done := lbl("done")
		a.LDA_abs(0x040C)
		a.CMP_imm8_b(0xFF)
		a.BEQ(done)
		a.LDA_long(0x7EF36F)
		if op.D > 0 {
			a.CMP_imm8_b(smallKeyMax)
			a.BCS(done)
			a.INC()
		} else {
			a.BEQ(done)
			a.CMP_imm8_b(0xFF)
			a.BEQ(done)
			a.DEC()
		}
		a.STA_long(0x7EF36F)
		a.Label(done)

	case debugOpHearts:
		g.emitDebugHearts(a, op, lbl)

	case debugOpFill:
		switch op.W {
		case "hearts":
			a.LDA_long(0x7EF36C)
			a.STA_long(0x7EF36D)
		case "magic":
			a.LDA_imm8_b(0x80)
			a.STA_long(0x7EF36E)
		case "rupees":
			// the rupees count up to the goal:
			a.LDA_imm8_b(0xE7)
			a.STA_long(0x7EF360)
			a.LDA_imm8_b(0x03)
			a.STA_long(0x7EF361)
		case "arrows":
			i := g.wram[0xF371]
			if i > 7 {
				i = 7
			}
			a.LDA_imm8_b(debugMaxArrows[i])
			a.STA_long(0x7EF377)
		case "bombs":
			i := g.wram[0xF370]
			if i > 7 {
				i = 7
			}
			a.LDA_imm8_b(debugMaxBombs[i])
			a.STA_long(0x7EF343)
		}

	case debugOpKill:
		// like PrepareSpriteToDie: the death state with the poof timer. The sprites that are not enemies (the items
		// $D8..$E6 and up, and the sprites with no health: NPCs, doors, ...) are left alone, and so are the dead and
		// the dying ones. A sprite that is still initializing ($08) is freed instead:
		loop, next, kill := lbl("loop"), lbl("next"), lbl("kill")
		a.LDX_imm8_b(0x0F)
		a.Label(loop)
		a.LDA_abs_x(0x0DD0)
		a.CMP_imm8_b(0x09)
		a.BEQ(kill)
		a.CMP_imm8_b(0x0B)
		a.BNE(next)
		a.Label(kill)
		a.LDA_abs_x(0x0E20)
		a.CMP_imm8_b(0xD8)
		a.BCS(next)
		a.LDA_abs_x(0x0E50)
		a.BEQ(next)
		a.LDA_imm8_b(killDeadEnemyTimer)
		a.STA_abs_x(0x0DF0)
		a.LDA_imm8_b(killDeadEnemyPoof)
		a.STA_abs_x(0x0DD0)
		a.Label(next)
		a.DEX()
		a.BPL(loop)
	}
}

// emitDebugHearts adds heart containers ($7EF36C is the capacity in eighths of a heart) and heart pieces ($7EF36B,
// 0..3). Four pieces make a container.
func (g *Game) emitDebugHearts(a *asm.Emitter, op *debugOp, lbl func(string) string) {
	// add a container, or take one away and keep the health within the capacity:
	container := func(delta int, name string) {
		done := lbl(name + "_done")
		a.LDA_long(0x7EF36C)
		if delta > 0 {
			a.CMP_imm8_b(debugMaxHealth)
			a.BCS(done)
			a.CLC()
			a.ADC_imm8_b(0x08)
			a.STA_long(0x7EF36C)
		} else {
			// keep at least one heart:
			a.CMP_imm8_b(0x10)
			a.BCC(done)
			a.SEC()
			a.SBC_imm8_b(0x08)
			a.STA_long(0x7EF36C)
			a.CMP_long(0x7EF36D)
			a.BCS(done)
			a.STA_long(0x7EF36D)
		}
		a.Label(done)
	}

	if op.C != 0 {
		container(op.C, "c")
	}

	switch op.P {
	case 1:
		done, wrap := lbl("p_done"), lbl("p_wrap")
		a.LDA_long(0x7EF36B)
		a.INC()
		a.CMP_imm8_b(0x04)
		a.BCS(wrap)
		a.STA_long(0x7EF36B)
		a.BRA(done)
		a.Label(wrap)
		a.LDA_imm8_b(0x00)
		a.STA_long(0x7EF36B)
		container(1, "pw")
		a.Label(done)
	case -1:
		done, wrap := lbl("m_done"), lbl("m_wrap")
		a.LDA_long(0x7EF36B)
		a.BEQ(wrap)
		a.DEC()
		a.STA_long(0x7EF36B)
		a.BRA(done)
		a.Label(wrap)
		a.LDA_imm8_b(0x03)
		a.STA_long(0x7EF36B)
		container(-1, "mw")
		a.Label(done)
	}
}

// debugGfx is the HUD graphics and palette the item menu draws its icons with.
type debugGfx struct {
	// Tiles is the base64 of the 2bpp tiles of graphics packs $6A, $6B and $69, 128 tiles of 16 bytes each, which the
	// game uploads to the VRAM of BG3 at $7000, $7400 and $7800: tile numbers $000..$17F of the HUD's tile maps:
	Tiles string `json:"tiles"`
	// Palette is the HUD's 32 colors (BGR555): 8 palettes of 4 colors, palette p for tile map words with p in
	// bits 10-12:
	Palette []uint16 `json:"palette"`
}

const (
	// PaletteData.hud_00 in bank $1B, which PaletteLoad_HUD loads to CGRAM $00..$1F:
	romHudPalette  = 0x1BD660
	hudPaletteSize = 32
	hudPackSize    = 0x800
)

var hudPacks = [...]uint8{0x6A, 0x6B, 0x69}

// debugGfx builds the HUD graphics from the ROM, like the game's Graphics_LoadSharedSprites does in the SNES'
// VRAM.
func (g *Game) debugGfx() (*debugGfx, error) {
	if !g.swordShield.ok {
		return nil, fmt.Errorf("the graphics routines are not in the ROM")
	}

	tiles := make([]byte, 0, len(hudPacks)*hudPackSize)
	for _, pack := range hudPacks {
		bank := make([]byte, 0x10000)
		if err := g.decompressSpriteSheet(pack, bank, 0); err != nil {
			return nil, err
		}
		tiles = append(tiles, bank[:hudPackSize]...)
	}

	gfx := &debugGfx{
		Tiles:   base64.StdEncoding.EncodeToString(tiles),
		Palette: make([]uint16, hudPaletteSize),
	}
	for i := range gfx.Palette {
		c, ok := g.romU16(romHudPalette + uint32(i)*2)
		if !ok {
			return nil, fmt.Errorf("the HUD palette is not in the ROM")
		}
		gfx.Palette[i] = c
	}
	return gfx, nil
}

type debugGfxCmd struct{ g *Game }

func (c *debugGfxCmd) CreateArgs() interfaces.CommandArgs { return nil }

// Execute stores the JSON of the graphics in the []byte args points to.
func (c *debugGfxCmd) Execute(args interfaces.CommandArgs) error {
	out, ok := args.(*[]byte)
	if !ok {
		return fmt.Errorf("invalid args type for command")
	}

	gfx, err := c.g.debugGfx()
	if err != nil {
		return err
	}
	*out, err = json.Marshal(gfx)
	return err
}
