package alttp

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/alttpo/snes/emulator"
	"o2/util"
)

// Cycle budgets: the worst-case CPU cycles of the patch's per-frame routines (RUN_TIMING.md, Equal cost). The
// emulator counts CPU cycles, not master clocks; a cycle takes 6 to 12 master clocks (8 for SRAM, WRAM and slow ROM),
// so a frame of 357,366 master clocks is roughly 45,000 cycles. A routine that exceeds its budget fails the test; the
// budgets are documented next to each routine's code.

// cycleProfile attributes the cycles of every instruction run to the patch's routine (or the part of preMain) its
// address is in.
type cycleProfile struct {
	starts []uint32
	names  []string
	// the cycles of the current run, and the most of any run so far, by routine:
	run map[string]uint64
	max map[string]uint64
}

// preMainParts are the labels in preMain that start its inline parts, with the names they are reported by:
var preMainParts = map[string]string{
	"preMain":       "preMain: run timing",
	"rtDone":        "preMain: module check, update routine call",
	"syncExit":      "preMain: module call, link color",
	"linkColorExit": "preMain: sfx",
	"sfxExit":       "preMain: manip pending call, gate, Link OAM copy",
	"preMainExit":   "preMain: exit",
}

var (
	listingBlock = regexp.MustCompile(`^; (.+): \$([0-9a-f]{6})\.\.\$([0-9a-f]{6})`)
	listingLabel = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*):$`)
	listingIns   = regexp.MustCompile(`; \$([0-9a-f]{6})`)
)

// newCycleProfile patches a test ROM with the title and reads the routines' addresses from the patch listing.
func newCycleProfile(t *testing.T, romTitle string) *cycleProfile {
	t.Helper()
	dir := t.TempDir()
	util.LogPath = filepath.Join(dir, "o2.log")
	_, _, err := createTestEmulator(romTitle, nil)
	util.LogPath = ""
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "o2-patch.asm"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return parseCycleProfile(t, f)
}

func parseCycleProfile(t *testing.T, r io.Reader) *cycleProfile {
	p := &cycleProfile{run: map[string]uint64{}, max: map[string]uint64{}}
	type mark struct {
		addr uint32
		name string
	}
	var marks []mark
	var pending []string
	block := ""
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if m := listingBlock.FindStringSubmatch(line); m != nil {
			block = m[1]
			start, _ := strconv.ParseUint(m[2], 16, 32)
			end, _ := strconv.ParseUint(m[3], 16, 32)
			if block != "preMain" {
				marks = append(marks, mark{uint32(start), block})
			}
			marks = append(marks, mark{uint32(end) + 1, ""})
			continue
		}
		if m := listingLabel.FindStringSubmatch(line); m != nil {
			if name, ok := preMainParts[m[1]]; ok && block == "preMain" {
				pending = append(pending, name)
			}
			continue
		}
		if m := listingIns.FindStringSubmatch(line); m != nil && len(pending) > 0 {
			a, _ := strconv.ParseUint(m[1], 16, 32)
			for _, name := range pending {
				marks = append(marks, mark{uint32(a), name})
			}
			pending = nil
		}
	}
	sort.SliceStable(marks, func(i, j int) bool { return marks[i].addr < marks[j].addr })
	for _, m := range marks {
		if n := len(p.starts); n > 0 && p.starts[n-1] == m.addr {
			// a routine starting where the previous ends:
			if m.name != "" {
				p.names[n-1] = m.name
			}
			continue
		}
		p.starts = append(p.starts, m.addr)
		p.names = append(p.names, m.name)
	}
	if len(p.starts) == 0 {
		t.Fatal("no routines in the patch listing")
	}
	return p
}

// routine names the code at the address.
func (p *cycleProfile) routine(pc uint32) string {
	pc &^= 0x800000
	bank := pc >> 16
	if bank >= 0x70 && bank < 0x7E {
		return "update routine (SRAM)"
	}
	i := sort.Search(len(p.starts), func(i int) bool { return p.starts[i]&^0x800000 > pc }) - 1
	if i < 0 || p.names[i] == "" {
		return "game"
	}
	return p.names[i]
}

// runUntil runs the CPU like emulator.System.RunUntil, counting the cycles of each routine.
func (p *cycleProfile) runUntil(e *emulator.System, target uint32, maxCycles uint64) bool {
	clear(p.run)
	for cycles := uint64(0); cycles < maxCycles; {
		pc := e.GetPC()
		if pc == target {
			break
		}
		n, _ := e.CPU.Step()
		cycles += uint64(n)
		p.run[p.routine(pc)] += uint64(n)
	}
	for name, n := range p.run {
		p.max[name] = max(p.max[name], n)
	}
	return e.GetPC() == target
}

// the profile that the scenario helpers run the CPU with, if any:
var testCycleProfile *cycleProfile

// runUntil runs the CPU until target, with testCycleProfile if set.
func runUntil(e *emulator.System, target uint32, maxCycles uint64) bool {
	if testCycleProfile != nil {
		return testCycleProfile.runUntil(e, target, maxCycles)
	}
	return e.RunUntil(target, maxCycles)
}

func (p *cycleProfile) report(t *testing.T, what string) {
	names := make([]string, 0, len(p.max))
	for name := range p.max {
		names = append(names, name)
	}
	slices.Sort(names)
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "  %6d  %s\n", p.max[name], name)
	}
	t.Logf("worst-case cycles, %s:\n%s", what, b.String())
}

func (p *cycleProfile) expectWithin(t *testing.T, budgets map[string]uint64) {
	t.Helper()
	for name, budget := range budgets {
		if n := p.max[name]; n > budget {
			t.Errorf("%s: %d cycles; budget %d", name, n, budget)
		}
	}
}

// The run timing code and NMI counter, with every gate off, across all of preMain's run timing paths.
func TestCycleBudgets_GatesOff(t *testing.T) {
	p := newCycleProfile(t, "ZELDANODENSETSU")
	e, _, err := createTestEmulator("ZELDANODENSETSU", nil)
	if err != nil {
		t.Fatal(err)
	}
	e.SRAM[gatesAddr&0x7FFF] = 0
	nmi, latches := rtSRAM(e, rtmNMIFrames), rtSRAM(e, rtmLatches)
	frame := func(module uint8, nmiFrames, loopFrames uint32) {
		t.Helper()
		setU24(nmi, nmiFrames)
		setU24(rtSRAM(e, rtmLoopFrames), loopFrames)
		e.WRAM[0x10] = module
		e.CPU.Reset()
		e.SetPC(testROMMainGameLoop)
		if !p.runUntil(e, testROMBreakPoint, 0x2_000) {
			t.Fatalf("frame did not finish; PC=$%06x", e.GetPC())
		}
	}
	latches[0] = 0
	frame(0x01, 0x100, 0x00FFFF)
	// the start latch, with an NMI's carry into the high byte to re-read:
	frame(runStartModule, 0x00FFFF, 0x00FFFF)
	frame(0x07, 0x200, 0x00FFFF)
	frame(runFinishModule, 0x300, 0x00FFFF)
	frame(runFinishModule, 0x301, 0x00FFFF)

	for _, n := range []uint32{0x00FFFE, 0x00FFFF} {
		setU24(nmi, n)
		e.CPU.Reset()
		e.SetPC(testROMNMI)
		if !p.runUntil(e, nmiCountHookReturn, 0x1_000) {
			t.Fatalf("NMI hook did not return; PC=$%06x", e.GetPC())
		}
	}

	p.report(t, "gates off")
	p.expectWithin(t, map[string]uint64{
		"preMain: run timing": 120,
		"NMI count":           70,
		// the gate test and branch:
		"preMain: manip pending call, gate, Link OAM copy": 30,
	})
}

// addRSPlayer adds a remote player to the remote sprites scenario, in the same room as g1, with Link's sprites.
func addRSPlayer(h *skScenario, name string, x, y uint16) {
	t := h.t
	gs, err := createTestGameSync("VT test", name, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	i := len(h.gs)
	h.gs = append(h.gs, &gs)
	h.n = append(h.n, nil)
	h.s.Clients = append(h.s.Clients, gs.c)
	h.s.HandleAllClients(nil)
	gameHandleNet(gs.g)
	h.inRoom(i, rsRoom)
	h.setScroll(i, x, y, 0x1000, 0x0200)
	e := gs.e
	e.WRAM[0x0352], e.WRAM[0x0353] = 0x90, 0x01
	for i := 0; i < 0x80; i++ {
		e.WRAM[0x0800+i*4+1] = 0xF0
	}
	copy(e.WRAM[0x0800+0x64*4:], []byte{0x30, 0x38, 0x00, 0x2E})
	copy(e.WRAM[0x0800+0x65*4:], []byte{0x30, 0x40, 0x02, 0x6E})
	e.WRAM[0x0A20+0x64], e.WRAM[0x0A20+0x65] = 0x02, 0x02
	for k, v := range []uint16{0x8400, 0x8600, 0x9000, 0x9200, 0x9400, 0x9420} {
		binary.LittleEndian.PutUint16(e.WRAM[0x0ACC+k*2:], v)
	}
}

// The remote sprites code with 3 remote players in the room: one drawn with Link's sprites and two as blobs.
func TestCycleBudgets_RemoteSprites(t *testing.T) {
	p := newCycleProfile(t, "VT test")
	h := newRSScenario(t)
	addRSPlayer(h, "g3", 0x1030, 0x0230)
	addRSPlayer(h, "g4", 0x1080, 0x0240)
	h.frames(8)
	if h.gs[0].e.SRAM[gatesAddr&0x7FFF] != gateSprites {
		t.Fatalf("the sprites gate is off")
	}

	// profile g1's frames and NMI uploads:
	testCycleProfile = p
	defer func() { testCycleProfile = nil }()
	for f := 0; f < 8; f++ {
		h.frame()
		runNMIHook(t, h.gs[0].e, h.gs[0].g)
	}

	p.report(t, "3 remote players")
	p.expectWithin(t, map[string]uint64{
		"remoteFrame": 15_000,
		"NMI hooks":   50,
		"NMI upload":  600,
		"preMain: manip pending call, gate, Link OAM copy": 200,
	})
}
