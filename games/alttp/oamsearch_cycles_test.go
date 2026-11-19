package alttp

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/emulator"
)

// the ends of the game's 6 OAM regions (Oam_GetBufferPosition: each is allocated upward from its base), as OAM buffer
// offsets: C $000..$030, A $030..$170, D $030..$0C0, E $120..$140, F $140..$1D0, B $1D0..$200:
var oamRegionEnds = []uint16{0x030, 0x0C0, 0x140, 0x170, 0x1D0, 0x200}

// regionJumpTable: for the offset p of a used entry, the end of the highest region at or below p (0 below them all).
func regionJumpTable(ends []uint16) (t [0x200]byte) {
	for p := 0; p < 0x200; p += 4 {
		e := uint16(0)
		for _, x := range ends {
			if x <= uint16(p) && x > e {
				e = x
			}
		}
		t[p], t[p+1] = byte(e), byte(e>>8)
	}
	return
}

type searchVariant struct {
	name string
	// emit the search: called with JSR, A 16-bit, X and Y 16-bit, Y = the entry offset to search below, DB = $80, D = 0;
	// returns with A 16-bit and carry set and Y = an unused entry, or carry clear if none. jt is the jump table's address.
	emit   func(a *asm.Emitter, jt uint16)
	tables [][]uint16
}

const fallbackDP = 0x0E

func emitRegionSearch(a *asm.Emitter, p string, jt uint16, fallback bool) {
	a.SEP(0x20)
	if fallback {
		a.LDA_dp(fallbackDP)
		a.BEQ(p + "_loop")
		a.JMP_abs(p + "_lin")
	}
	a.Label(p + "_loop")
	a.DEY()
	a.DEY()
	a.DEY()
	a.DEY()
	a.BMI(p + "_below")
	a.LDA_abs_y(0x0801)
	a.CMP_imm8_b(0xF0)
	a.BEQ(p + "_found")
	a.Comment("used: the end of the region at or below it:")
	a.REP(0x20)
	a.LDA_abs_y(jt)
	a.TAY()
	a.SEP(0x20)
	a.BRA(p + "_loop")
	a.Label(p + "_found")
	a.REP(0x20)
	a.SEC()
	a.RTS()
	a.AssumeSEP(0x20)
	a.Label(p + "_below")
	if fallback {
		a.Comment("every region's top is used: search the rest from the top:")
		a.INC_dp(fallbackDP)
		a.LDY_imm16_w(0x0200)
		a.Label(p + "_lin")
		emitFindUnused(a, p+"_l")
		a.JMP_abs(p + "_found")
	} else {
		a.REP(0x20)
		a.CLC()
		a.RTS()
	}
}

var searchVariants = []searchVariant{
	{name: "current: down from $200, 8 at a time", emit: func(a *asm.Emitter, jt uint16) {
		a.SEP(0x20)
		emitFindUnused(a, "v0")
		a.REP(0x20)
		a.SEC()
		a.RTS()
	}},
	{name: "down from $200, 1 at a time", emit: func(a *asm.Emitter, jt uint16) {
		a.SEP(0x20)
		a.Label("v1_loop")
		a.DEY()
		a.DEY()
		a.DEY()
		a.DEY()
		a.BMI("v1_full")
		a.LDA_abs_y(0x0801)
		a.CMP_imm8_b(0xF0)
		a.BNE("v1_loop")
		a.REP(0x20)
		a.SEC()
		a.RTS()
		a.AssumeSEP(0x20)
		a.Label("v1_full")
		a.REP(0x20)
		a.CLC()
		a.RTS()
	}},
	{name: "down from each region's end, then the rest from $200", emit: func(a *asm.Emitter, jt uint16) {
		emitRegionSearch(a, "v2", jt, true)
	}},
	{name: "down from each region's end only (no fallback)", emit: func(a *asm.Emitter, jt uint16) {
		emitRegionSearch(a, "v3", jt, false)
	}},
}

type searchBench struct {
	e              *emulator.System
	start, end     uint32
	findLo, findHi uint32
}

// newSearchBench assembles a harness in bank $80 (FastROM): find up to K unused entries ($00:0000 = K), marking each
// found one used as drawing does, then STP. The variant's code is at $80:9000.., the jump table at $80:A000.
func newSearchBench(t *testing.T, v searchVariant, ends []uint16) *searchBench {
	b := &searchBench{e: &emulator.System{}}
	if err := b.e.CreateEmulator(); err != nil {
		t.Fatal(err)
	}
	jt := regionJumpTable(ends)
	copy(b.e.ROM[0x2000:], jt[:]) // $00:A000 (LoROM: bank 0 $8000 = ROM 0)

	h := asm.NewEmitter(make([]byte, 0x100), false)
	h.SetBase(0x808000)
	h.REP(0x30)
	h.LDA_imm16_w(0x0000)
	h.TCD()
	h.PEA(0x8080)
	h.PLB()
	h.PLB()
	h.SEP(0x20)
	h.LDA_imm8_b(0x01)
	h.STA_abs(0x420D)
	h.REP(0x20)
	h.STZ_dp(fallbackDP)
	h.LDY_imm16_w(0x0200)
	h.LDX_dp(0x00)
	h.Label("loop")
	h.PHX()
	h.JSR_abs(0x9000)
	h.PLX()
	h.BCC("done")
	h.SEP(0x20)
	h.LDA_imm8_b(0x00)
	h.STA_abs_y(0x0801)
	h.REP(0x20)
	h.INC_dp(0x02)
	h.DEX()
	h.BNE("loop")
	h.Label("done")
	b.end = h.Label("end")
	h.STP()
	if err := h.Finalize(); err != nil {
		t.Fatal(err)
	}
	copy(b.e.ROM[0:], h.Bytes())

	a := asm.NewEmitter(make([]byte, 0x1000), false)
	a.SetBase(0x809000)
	a.AssumeREP(0x30)
	v.emit(a, 0xA000)
	if err := a.Finalize(); err != nil {
		t.Fatal(err)
	}
	copy(b.e.ROM[0x1000:], a.Bytes())
	b.start, b.findLo, b.findHi = 0x808000, 0x809000, 0x809000+uint32(a.Len())
	return b
}

// run finds up to k entries in the snapshot and returns the CPU cycles and master clocks spent in the search, and the
// entries found.
func (b *searchBench) run(s *oamSnap, k int) (cycles, clocks uint64, found int) {
	e := b.e
	for i := 0; i < 0x80; i++ {
		e.WRAM[0x0800+i*4], e.WRAM[0x0801+i*4] = 0, s.y[i]
	}
	e.WRAM[0], e.WRAM[1], e.WRAM[2], e.WRAM[3] = byte(k), byte(k>>8), 0, 0
	e.CPU.Reset()
	e.CPU.SetFlags(0x34)
	e.CPU.SP = 0x01FF
	e.SetPC(b.start)
	for n := 0; ; n++ {
		pc := e.GetPC()
		if pc == b.end {
			break
		}
		if n > 100_000 {
			panic("runaway")
		}
		c0 := e.Clocks
		cy, _ := e.Step()
		if pc >= b.findLo && pc < b.findHi {
			cycles += uint64(cy)
			clocks += e.Clocks - c0
		}
	}
	return cycles, clocks, int(e.WRAM[2]) | int(e.WRAM[3])<<8
}

func loadOAMSnaps(t *testing.T) (snaps []oamSnap, kinds []uint8) {
	b, err := os.ReadFile(oamSnapsPath())
	if err != nil {
		t.Skipf("no snapshots (run TestOAMSearch_Collect with OAM_COLLECT=1): %v", err)
	}
	for o := 0; o+131 <= len(b); o += 131 {
		var s oamSnap
		s.module, s.sub = b[o+1], b[o+2]
		copy(s.y[:], b[o+3:o+131])
		if (o/131)%4 == 0 {
			snaps = append(snaps, s)
			kinds = append(kinds, b[o])
		}
	}
	return
}

func pct(v []uint64, p float64) uint64 {
	if len(v) == 0 {
		return 0
	}
	return v[int(float64(len(v)-1)*p)]
}

// TestOAMSearch_Occupancy shows how the game fills the OAM buffer: the entries used per frame, and how often each
// entry is used.
func TestOAMSearch_Occupancy(t *testing.T) {
	snaps, kinds := loadOAMSnaps(t)
	for kind, kname := range []string{"underworld", "overworld"} {
		var counts []uint64
		var freq [0x80]int
		n := 0
		for i, s := range snaps {
			if int(kinds[i]) != kind {
				continue
			}
			n++
			u := 0
			for j := 0; j < 0x80; j++ {
				if s.used(j) {
					u++
					freq[j]++
				}
			}
			counts = append(counts, uint64(u))
		}
		sort.Slice(counts, func(i, j int) bool { return counts[i] < counts[j] })
		t.Logf("%s: %d frames; entries used: p50 %d, p90 %d, p99 %d, max %d", kname, n, pct(counts, .5), pct(counts, .9),
			pct(counts, .99), pct(counts, 1))
		var sb strings.Builder
		for j := 0; j < 0x80; j++ {
			if j%16 == 0 {
				fmt.Fprintf(&sb, "\n  $%03x:", j*4)
			}
			fmt.Fprintf(&sb, " %3d", freq[j]*100/max(n, 1))
		}
		t.Logf("%s: %% of frames each entry is used:%s", kname, sb.String())
	}
}

// TestOAMSearch_Cycles runs each search variant over every snapshot for k entries (the remote sprites to draw).
func TestOAMSearch_Cycles(t *testing.T) {
	snaps, kinds := loadOAMSnaps(t)
	for _, k := range []int{6, 12, 18, 36} {
		t.Logf("--- %d entries wanted ---", k)
		for _, v := range searchVariants {
			b := newSearchBench(t, v, oamRegionEnds)
			for kind, kname := range []string{"underworld", "overworld", "all"} {
				var cyc, clk []uint64
				short := 0
				for i := range snaps {
					if kind < 2 && int(kinds[i]) != kind {
						continue
					}
					c, m, f := b.run(&snaps[i], k)
					free := 0
					for j := 0; j < 0x80; j++ {
						if !snaps[i].used(j) {
							free++
						}
					}
					if f < min(k, free) {
						short++
					}
					cyc = append(cyc, c)
					clk = append(clk, m)
				}
				var sum uint64
				for _, c := range cyc {
					sum += c
				}
				sort.Slice(cyc, func(i, j int) bool { return cyc[i] < cyc[j] })
				sort.Slice(clk, func(i, j int) bool { return clk[i] < clk[j] })
				t.Logf("%-55s %-10s cycles mean %5d p50 %5d p90 %5d p99 %5d max %5d | clocks p99 %6d | frames short of entries: %d",
					v.name, kname, sum/uint64(len(cyc)), pct(cyc, .5), pct(cyc, .9), pct(cyc, .99), pct(cyc, 1), pct(clk, .99), short)
			}
		}
	}
}
