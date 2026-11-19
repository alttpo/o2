package alttp

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alttpo/snes/emulator"
)

// apuStub answers the APU ports $2140..$2143 enough for the game's sound uploads (the SPC700's IPL loader): $AA $BB
// to the loader's wait for it (the read at $00:8891), else an echo of what the CPU wrote last to each port.
type apuStub struct {
	s    *emulator.System
	last [4]byte
}

func (a *apuStub) Attach(s *emulator.System) error {
	a.s = s
	s.MapIO(0x2140, 0x217F, a.read, a.write)
	return nil
}
func (a *apuStub) read(offs uint16) byte {
	i := offs & 3
	if a.s.GetPC() == 0x008891 {
		return []byte{0xAA, 0xBB, 0, 0}[i]
	}
	return a.last[i]
}
func (a *apuStub) write(offs uint16, v byte) { a.last[offs&3] = v }

type realGame struct {
	t     *testing.T
	e     *emulator.System
	ppu   *emulator.PPU
	pad   *emulator.Joypads
	frame int
	// crashed is set once a frame panicked (the game ran off into unmapped memory) or never returned to the frame hook:
	crashed bool
}

func newRealGame(t *testing.T) *realGame {
	romData, err := os.ReadFile(jpROMPath)
	if err != nil {
		t.Skipf("no JP ROM: %v", err)
	}
	g := &realGame{t: t, ppu: &emulator.PPU{}, pad: &emulator.Joypads{}}
	g.e = &emulator.System{Devices: []emulator.Device{g.ppu, g.pad, &emulator.Math{}, &apuStub{}}}
	if err := g.e.CreateEmulator(); err != nil {
		t.Fatal(err)
	}
	copy(g.e.ROM[:], romData)
	copy(g.e.ROM[len(romData):], romData)
	if srm, err := os.ReadFile("../../../alttp-jp.srm"); err == nil {
		copy(g.e.SRAM[:], srm)
	}
	g.e.Reset()
	return g
}

// runFrame runs until the module returns to the frame hook (the OAM buffer as remoteFrame sees it).
func (g *realGame) runFrame(input uint16) (ok bool) {
	if g.crashed {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			g.t.Logf("crashed: %v: %s", r, g.where())
			g.crashed, ok = true, false
		}
	}()
	g.pad.Input[0] = input
	// step off the hook first:
	for i := 0; i < 4 && g.e.GetPC() == 0x00805A; i++ {
		g.e.Step()
	}
	ok = g.e.RunUntil(0x00805A, 20_000_000)
	g.frame++
	if !ok {
		g.t.Logf("crashed: no frame: %s PC=$%06x", g.where(), g.e.GetPC())
		g.crashed = true
	}
	return ok
}

func TestOAMSearch_Boot(t *testing.T) {
	g := newRealGame(t)
	for f := 0; f < 600; f++ {
		var in uint16
		if f%30 == 0 {
			in = 0x1000 // start
		}
		if !g.runFrame(in) {
			t.Fatalf("frame %d: stuck at $%06x module=%02x", f, g.e.GetPC(), g.e.WRAM[0x10])
		}
		if f%30 == 0 {
			t.Logf("frame %d module=%02x sub=%02x", f, g.e.WRAM[0x10], g.e.WRAM[0x11])
		}
	}
}

func (g *realGame) where() string {
	w := g.e.WRAM[:]
	used := 0
	for i := 0; i < 0x80; i++ {
		if w[0x0801+i*4] != 0xF0 {
			used++
		}
	}
	sprites := 0
	for i := 0; i < 16; i++ {
		if w[0x0DD0+i] != 0 {
			sprites++
		}
	}
	return fmt.Sprintf("f%d mod=%02x/%02x indoors=%d room=%04x area=%02x dungeon=%02x link=(%04x,%04x) sprites=%d oamUsed=%d",
		g.frame, w[0x10], w[0x11], w[0x1B], uint16(w[0xA0])|uint16(w[0xA1])<<8, w[0x8A], w[0x040C],
		uint16(w[0x22])|uint16(w[0x23])<<8, uint16(w[0x20])|uint16(w[0x21])<<8, sprites, used)
}

// startGame presses Start until the first file is loaded and Link can play (module $07 or $09).
func (g *realGame) startGame() {
	for f := 0; f < 1200; f++ {
		m := g.e.WRAM[0x10]
		if (m == 0x07 || m == 0x09) && g.e.WRAM[0x11] == 0 {
			return
		}
		var in uint16
		if f%20 == 0 && m <= 0x01 {
			in = 0x1000
		}
		if f%20 == 0 && m == 0x1B {
			// the spawn location menu: the first choice
			in = 0x0080
		}
		g.runFrame(in)
	}
	g.t.Fatalf("game did not start: %s", g.where())
}

// editSave changes file 1 in SRAM and fixes its checksum.
func (g *realGame) editSave(edit func(f []byte)) {
	f := g.e.SRAM[0:0x500]
	edit(f)
	sum := uint16(0)
	for o := 0; o < 0x4FE; o += 2 {
		sum += uint16(f[o]) | uint16(f[o+1])<<8
	}
	ck := 0x5A5A - sum
	f[0x4FE], f[0x4FF] = byte(ck), byte(ck>>8)
}

// oamSnap is the OAM buffer's Y bytes as remoteFrame sees them, after the module drew its sprites.
type oamSnap struct {
	// kind: 0 underworld, 1 overworld; scene: see sceneName ($FFFF unknown):
	kind, module, sub uint8
	scene             uint16
	y                 [0x80]uint8
}

func (s *oamSnap) used(i int) bool { return s.y[i] != 0xF0 }

// rng is a small deterministic generator for the random walks:
type rng uint32

func (r *rng) next() uint32 {
	*r ^= *r << 13
	*r ^= *r >> 17
	*r ^= *r << 5
	return uint32(*r)
}

// randomWalk plays n frames: walks in a random direction (diagonals too) until Link stops moving for 8 frames (or after
// 600), so that he explores rather than circles, and swings the sword now and then (and holds it to charge a spin some of the time), with health kept full, and snapshots the OAM
// buffer each frame of modules $07 and $09.
func (g *realGame) randomWalk(seed uint32, n int) (snaps []oamSnap) {
	r := rng(seed)
	dirs := []uint16{0x0800, 0x0400, 0x0200, 0x0100, 0x0900, 0x0A00, 0x0500, 0x0600}
	var dir uint16
	hold, swing, stuck := 0, 0, 0
	var lastPos uint32
	for f := 0; f < n; f++ {
		pos := uint32(g.e.WRAM[0x22]) | uint32(g.e.WRAM[0x23])<<8 | uint32(g.e.WRAM[0x20])<<16 | uint32(g.e.WRAM[0x21])<<24
		if pos == lastPos {
			stuck++
		} else {
			stuck = 0
		}
		lastPos = pos
		if hold == 0 || stuck >= 8 {
			dir = dirs[r.next()%8]
			hold, stuck = 600, 0
		}
		hold--
		in := dir
		if swing > 0 {
			swing--
			in |= 0x8000
		} else if r.next()%24 == 0 {
			swing = 1
			if r.next()%4 == 0 {
				swing = 60
			}
		}
		m := g.e.WRAM[0x10]
		if m != 0x07 && m != 0x09 {
			// dialogs, the menu: press A and B now and then to get back to playing:
			in = 0
			if f%8 == 0 {
				in = 0x8080
			}
		}
		g.e.WRAM[0xF36D] = g.e.WRAM[0xF36C]
		if !g.runFrame(in) {
			g.t.Fatalf("stuck: %s PC=$%06x", g.where(), g.e.GetPC())
		}
		m = g.e.WRAM[0x10]
		if m == 0x07 || m == 0x09 {
			var s oamSnap
			s.module, s.sub = m, g.e.WRAM[0x11]
			for i := range s.y {
				s.y[i] = g.e.WRAM[0x0801+i*4]
			}
			snaps = append(snaps, s)
		}
	}
	return
}

type oamScenario struct {
	name  string
	setup func(g *realGame)
}

var oamScenarios = []oamScenario{
	{"Hyrule Castle escape (the save as is)", func(g *realGame) {}},
	{"dark world from the pyramid", func(g *realGame) {
		g.editSave(func(f []byte) {
			f[0x3C5] = 3    // Agahnim beaten
			f[0x3CA] = 0x40 // dark world
			f[0x357] = 1    // moon pearl
			f[0x359] = 2    // master sword
			f[0x35A] = 2    // shield
			f[0x36C], f[0x36D] = 0x60, 0x60
		})
	}},
	{"light world from Link's house", func(g *realGame) {
		g.editSave(func(f []byte) {
			f[0x3C5] = 2 // Zelda rescued
			f[0x3C8] = 0 // spawn: Link's house
			f[0x3CA] = 0
			f[0x359] = 1
			f[0x35A] = 1
			f[0x36C], f[0x36D] = 0x60, 0x60
		})
	}},
}

// enterEntrance loads the entrance (as walking through its door does) and runs until Link can play.
func (g *realGame) enterEntrance(entrance uint8) bool {
	w := g.e.WRAM[:]
	w[0x010E], w[0x010F] = entrance, 0
	w[0x1B] = 0
	w[0x10], w[0x11] = 0x06, 0x00
	return g.settle()
}

// exitToOverworld leaves the room as walking out of its exit door does.
func (g *realGame) exitToOverworld() bool {
	w := g.e.WRAM[:]
	w[0x10], w[0x11] = 0x0F, 0x00
	return g.settle()
}

func (g *realGame) settle() bool {
	for f := 0; f < 600; f++ {
		g.runFrame(0)
		m := g.e.WRAM[0x10]
		if (m == 0x07 || m == 0x09) && g.e.WRAM[0x11] == 0 {
			return true
		}
	}
	return false
}

// oamSnapsPath is where TestOAMSearch_Collect saves the snapshots and the other OAM search tests read them: $OAM_SNAPS,
// or o2-oamsnaps.bin in the temporary directory.
func oamSnapsPath() string {
	if p := os.Getenv("OAM_SNAPS"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "o2-oamsnaps.bin")
}

// walk plays n frames holding the input.
func (g *realGame) walk(in uint16, n int) {
	for f := 0; f < n; f++ {
		g.e.WRAM[0xF36D] = g.e.WRAM[0xF36C]
		g.runFrame(in)
	}
}

// TestOAMSearch_Collect plays every entrance: the rooms behind it (a random walk from the door), then the overworld
// outside it (walks out of the door, then a random walk), in the light world and in the dark world, plus the three
// start scenarios, and saves the OAM snapshots (1 byte scenario kind: 0 underworld, 1 overworld; module; sub; 128 Y).
func TestOAMSearch_Collect(t *testing.T) {
	if os.Getenv("OAM_COLLECT") == "" {
		t.Skip("set OAM_COLLECT=1")
	}
	var out []byte
	add := func(snaps []oamSnap) {
		for _, s := range snaps {
			kind := uint8(0)
			if s.module == 0x09 {
				kind = 1
			}
			out = append(out, kind, s.module, s.sub)
			out = append(out, s.y[:]...)
		}
	}
	for i, sc := range oamScenarios {
		g := newRealGame(t)
		sc.setup(g)
		g.startGame()
		add(g.randomWalk(uint32(i+1)*7919, 3000))
	}
	for _, world := range []uint8{0x00, 0x40} {
		g := newRealGame(t)
		g.editSave(func(f []byte) {
			f[0x3C5] = 3
			f[0x3CA] = world
			f[0x357], f[0x359], f[0x35A] = 1, 2, 2
			f[0x36C], f[0x36D] = 0xA0, 0xA0
		})
		g.startGame()
		for e := 1; e < 0x85; e++ {
			if !g.enterEntrance(uint8(e)) {
				t.Logf("entrance %02x: did not load: %s", e, g.where())
				g = newRealGame(t)
				g.startGame()
				continue
			}
			add(g.randomWalk(uint32(e)*31+uint32(world), 500))
			if !g.enterEntrance(uint8(e)) {
				continue
			}
			g.walk(0x0400, 90)
			if g.e.WRAM[0x10] == 0x09 {
				add(g.randomWalk(uint32(e)*37+uint32(world), 500))
			}
		}
	}
	if err := os.WriteFile(oamSnapsPath(), out, 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d snapshots", len(out)/131)
}
