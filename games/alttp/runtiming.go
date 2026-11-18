package alttp

import (
	"bytes"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Run timing: the SNES side. See RUN_TIMING.md for the contract shared with alttpo.
//
// The patch counts every NMI (including lag frames) and every main loop iteration in SRAM, and latches the NMI count
// of the frame the run started in (the first main loop iteration in module $05, Module05_LoadFile) and finished in
// (the first one in module $19, Module19_TriforceRoom, after the start). These are outside of the range the init code
// zeroes, so they keep counting across console resets; clients reset the run by writing 0 to the latches byte.
//
// Optional features of the patch are behind the bits of the gate byte, which the init code zeroes on reset (it is
// next to the latches so that o2 reads them together): with a
// feature's bit clear, its code costs only the test of the bit, so that players who don't use a feature don't get
// lag frames from it.
const (
	runTimingAddr = uint32(0x707000)

	// u24, incremented by the NMI entry hook on every NMI:
	rtmNMIFrames = 0
	// u24, incremented by preMain on every main loop iteration:
	rtmLoopFrames = 3
	// u24, rtmNMIFrames at the start and at the finish:
	rtmStartFrame  = 6
	rtmFinishFrame = 9
	// u8, bit 0: rtmStartFrame is set; bit 1: rtmFinishFrame is set; zeroed by clients:
	rtmLatches = 0x0C
	// u8, the gate byte, written by clients (zeroed by the init code):
	rtmGates = 0x0D

	runTimingSize = 0x10
	// the bytes o2 reads:
	runTimingReadSize = rtmGates + 1

	rtmLatchStart  = 0x01
	rtmLatchFinish = 0x02

	// the modules that start and finish the run:
	runStartModule  = 0x05
	runFinishModule = 0x19

	gatesAddr = runTimingAddr + rtmGates
	// gateSprites enables copying Link's OAM entries, capturing the local player's effects, drawing remote players
	// and uploading their tiles in NMI (see remotesprites.go):
	gateSprites = uint8(0x01)

	// nmiCountHookAddr is in the NMI handler right after it switched to 16-bit registers, where it pushes them:
	nmiCountHookAddr = uint32(0x0080CC)
	// nmiCountHookReturn is the instruction after the ones the hook replaces:
	nmiCountHookReturn = nmiCountHookAddr + 4
)

// the run timing state is between the remote sprites state and the update routine buffers:
const _ = runTimingAddr - (remoteStateAddr + remoteStateSize)
const _ = updateBufAAddr - (runTimingAddr + runTimingSize)

// nmiCountHookExpected is the original code at nmiCountHookAddr: PHA; PHX; PHY; PHD
var nmiCountHookExpected = []byte{0x48, 0xDA, 0x5A, 0x0B}

// nmiCountHookSite checks the code at nmiCountHookAddr. ok is false if it is neither the original code nor a JML (which
// z3randomizer puts there for its own NMI frame counter); chain is the JML's target, which the hook jumps to instead
// of running the original code, or 0 for the original code.
func nmiCountHookSite(rom *snes.ROM) (ok bool, chain uint32) {
	pak, err := lorom.BusAddressToPak(nmiCountHookAddr)
	if err != nil || int(pak)+len(nmiCountHookExpected) > len(rom.Contents) {
		return false, 0
	}
	c := rom.Contents[pak : pak+uint32(len(nmiCountHookExpected))]
	if bytes.Equal(c, nmiCountHookExpected) {
		return true, 0
	}
	if c[0] == 0x5C {
		return true, uint32(c[1]) | uint32(c[2])<<8 | uint32(c[3])<<16
	}
	return false, 0
}

// emitNMICount emits the routine that the NMI handler jumps to with JML from nmiCountHookAddr, with A, X and Y 16-bit
// and any DB and D. It increments the NMI frame counter and then runs the replaced instructions and jumps back, or
// jumps to chain (the routine a randomizer had hooked there, which runs them itself). About 35 cycles.
func emitNMICount(a *asm.Emitter, chain uint32) {
	a.AssumeREP(0x30)
	a.Label("nmiCount")
	a.PHA()
	emitIncU24(a, runTimingAddr+rtmNMIFrames, "nmiCountHigh")
	a.PLA()
	if chain != 0 {
		a.JML(chain)
		return
	}
	a.Comment("the replaced instructions:")
	a.PHA()
	a.PHX()
	a.PHY()
	a.PHD()
	a.JML(nmiCountHookReturn)
}

// emitIncU24 increments the u24 at addr. A must be 16-bit; clobbers A and leaves it 16-bit. label names the skipped
// high byte increment.
func emitIncU24(a *asm.Emitter, addr uint32, label string) {
	a.LDA_long(addr)
	raw(a, "inc", 0x1A)
	a.STA_long(addr)
	a.BNE(label)
	a.SEP(0x20)
	a.LDA_long(addr + 2)
	raw(a, "inc", 0x1A)
	a.STA_long(addr + 2)
	a.REP(0x20)
	a.Label(label)
}

// emitRunTiming emits preMain's code, run before the module, that counts the main loop iteration and latches the NMI
// frame counter when the run starts or finishes. Called with A and X 8-bit and D = $0000; leaves them 8-bit and
// clobbers A. About 50 cycles on most frames.
func emitRunTiming(a *asm.Emitter) {
	a.AssumeSEP(0x30)
	rt := func(o uint32) uint32 { return runTimingAddr + o }
	a.Comment("run timing: count main loop iterations (NMI frames minus these are lag frames):")
	a.REP(0x20)
	emitIncU24(a, rt(rtmLoopFrames), "rtLoopHigh")
	a.SEP(0x20)

	a.Comment("latch the NMI frame counter in the first frame of the start module, then of the finish module:")
	a.LDA_long(rt(rtmLatches))
	a.LSR()
	a.BCS("rtStarted")
	a.LDA_dp(0x10)
	a.CMP_imm8_b(runStartModule)
	a.BNE("rtDone")
	emitLatchNMIFrames(a, rt(rtmStartFrame), "rtStartRead")
	a.LDA_imm8_b(rtmLatchStart)
	a.STA_long(rt(rtmLatches))
	a.BRA("rtDone")

	a.Label("rtStarted")
	a.LSR()
	a.BCS("rtDone")
	a.LDA_dp(0x10)
	a.CMP_imm8_b(runFinishModule)
	a.BNE("rtDone")
	emitLatchNMIFrames(a, rt(rtmFinishFrame), "rtFinishRead")
	a.LDA_imm8_b(rtmLatchStart | rtmLatchFinish)
	a.STA_long(rt(rtmLatches))
	a.Label("rtDone")
}

// emitLatchNMIFrames copies the NMI frame counter to dst. An NMI may increment it between reading its low word and its
// high byte, so it reads the high byte before and after the low word and tries again if it changed. Called with A
// 8-bit; leaves it 8-bit.
func emitLatchNMIFrames(a *asm.Emitter, dst uint32, label string) {
	src := runTimingAddr + rtmNMIFrames
	a.Label(label)
	a.LDA_long(src + 2)
	a.PHA()
	a.REP(0x20)
	a.LDA_long(src)
	a.STA_long(dst)
	a.SEP(0x20)
	a.PLA()
	a.CMP_long(src + 2)
	a.BNE(label)
	a.STA_long(dst + 2)
}

// emitGateTest branches to skip if the gate bit is clear. Called with A 8-bit; clobbers A.
func emitGateTest(a *asm.Emitter, gate uint8, skip string) {
	a.LDA_long(gatesAddr)
	a.AND_imm8_b(gate)
	a.BEQ(skip)
}

// the SNES's frame rate (NTSC) and frame period:
const (
	snesFPS = 60.0988
	// 1 / 60.0988 seconds (the frame is 357,366 master clocks at 21.477 MHz):
	snesFramePeriod = 16639356 * time.Nanosecond
)

const (
	// the samples of the NMI frame counter that frames are mapped to the server clock with:
	rtSampleMax = 32
	// a sample whose frames since the last one are off by more than this from what the wall time predicts starts a
	// new continuous stretch of the counter, once the next sample confirms it:
	rtJumpTolerance = 2 * time.Second
	// the rate is checked over at least this long a stretch:
	rtRateWindow = 10 * time.Second
	// the tolerance of the rate:
	rtRateTolerance = 0.01
	// how often to re-send the gates if they don't read back as written:
	rtGatesRewrite = time.Second
)

// run timing message flags (RUN_TIMING.md):
const (
	rtFlagStarted       = 0x01
	rtFlagFinished      = 0x02
	rtFlagDiscontinuity = 0x04
	rtFlagRate          = 0x08
	rtFlagStateLoad     = 0x10
	rtFlagNoCounter     = 0x20
)

// rtSample is a read of the NMI and main loop frame counters, at the server time the read completed:
type rtSample struct {
	nmi, loop uint32
	at        time.Time
}

// runTimingLocal is the local player's run timing.
type runTimingLocal struct {
	// the gate byte last read from SRAM, and the last one written and when:
	gates            uint8
	gatesOK          bool
	gatesWritten     uint8
	gatesWrittenTime time.Time

	// the last read of the frames and latches, to accept latches only from two identical reads:
	latchRaw   [rtmLatches + 1 - rtmStartFrame]byte
	latchRawOK bool
	// write 0 to the latches; the write is in flight; reads to skip after it completed, which may have been issued
	// before it (they are set by the write's completion, on another goroutine):
	clearLatches bool
	clearPending atomic.Bool
	clearSkips   atomic.Int32

	// the samples of the current continuous stretch of the counter, oldest first:
	samples []rtSample
	// a sample that didn't fit the last one, waiting for the next to confirm it:
	candidate   rtSample
	candidateOK bool
	// the first sample of the current stretch; latches from before it can't be mapped to the server clock:
	stretchStart rtSample
	// the start of the window the rate is checked over:
	rateFrom rtSample
	// the loop frame counter advanced while the NMI frame counter didn't: the ROM has no NMI counter:
	noCounter bool

	started, finished       bool
	startFrame, finishFrame uint32
	startAt, finishAt       time.Time
	// NMI frames minus loop frames at the start, and the lag frames between the start and the finish:
	lagBase   uint32
	lagFrames uint32
	// the gates at the start, and whether they changed since:
	gatesAtStart uint8
	gatesChanged bool

	flags   uint8
	penalty time.Duration
}

// u24Diff returns b - a for the u24 counters a and b, from -2^23 to 2^23-1.
func u24Diff(a, b uint32) int32 {
	return int32((b-a)<<8) >> 8
}

func u24(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
}

// framesToDuration converts a frame count to time at the SNES's frame rate.
func framesToDuration(frames int64) time.Duration {
	return time.Duration(frames * int64(snesFramePeriod))
}

// durationToFrames converts a duration to the frames that run in it.
func durationToFrames(d time.Duration) int64 {
	return int64(d) / int64(snesFramePeriod)
}

// frameTime maps an NMI frame of the current stretch to the server clock: reads complete after the frame they saw, so
// the earliest projection of the samples to the same frame is the closest to the true time.
func (r *runTimingLocal) frameTime(frame uint32) (t time.Time, ok bool) {
	if len(r.samples) == 0 {
		return
	}
	for i, s := range r.samples {
		p := s.at.Add(framesToDuration(int64(u24Diff(s.nmi, frame))))
		if i == 0 || p.Before(t) {
			t = p
		}
	}
	return t, true
}

// inRun reports whether the run started and didn't finish.
func (r *runTimingLocal) inRun() bool {
	return r.started && !r.finished
}

// sample adds a read of the counters.
func (r *runTimingLocal) sample(s rtSample) {
	if len(r.samples) == 0 {
		r.startStretch(s)
		return
	}
	last := r.samples[len(r.samples)-1]
	if fits(last, s) {
		r.candidateOK = false
		r.samples = append(r.samples, s)
		if len(r.samples) > rtSampleMax {
			r.samples = r.samples[len(r.samples)-rtSampleMax:]
		}
		r.checkCounter(last, s)
		r.checkRate(s)
		return
	}
	if r.candidateOK && fits(r.candidate, s) {
		// the counter jumped (a savestate load, the SRAM replaced, the console off or paused):
		log.Printf("alttp: run timing: NMI frames jumped from $%06x to $%06x in %v\n", last.nmi, s.nmi, s.at.Sub(last.at))
		if r.inRun() {
			r.flags |= rtFlagDiscontinuity
			// frames that ran faster than the wall time can't be told from a jump; charge what the wall time would
			// have saved, as for fast-forward:
			if d := framesToDuration(int64(u24Diff(last.nmi, r.candidate.nmi))) - r.candidate.at.Sub(last.at); d > rtJumpTolerance {
				r.penalty += d
			}
		}
		r.candidateOK = false
		r.startStretch(r.candidate)
		r.samples = append(r.samples, s)
		return
	}
	// a torn read or the first sample after a jump:
	r.candidate, r.candidateOK = s, true
}

func (r *runTimingLocal) startStretch(s rtSample) {
	r.samples = append(r.samples[:0], s)
	r.stretchStart = s
	r.rateFrom = s
}

// fits reports whether the frames between a and b are about what the wall time between them predicts.
func fits(a, b rtSample) bool {
	frames := framesToDuration(int64(u24Diff(a.nmi, b.nmi)))
	wall := b.at.Sub(a.at)
	d := frames - wall
	return d > -rtJumpTolerance && d < rtJumpTolerance
}

// checkCounter notices a ROM without the NMI frame counter: its main loop counter runs but its NMI counter doesn't.
func (r *runTimingLocal) checkCounter(a, b rtSample) {
	if b.nmi == a.nmi && u24Diff(a.loop, b.loop) > 0 && b.at.Sub(a.at) > 10*snesFramePeriod {
		if !r.noCounter {
			log.Printf("alttp: run timing: the ROM doesn't count NMI frames; timing from module changes only\n")
		}
		r.noCounter = true
	}
}

// checkRate compares the frames with the wall time over the last rtRateWindow during the run, and charges the wall
// time that running fast saved.
func (r *runTimingLocal) checkRate(s rtSample) {
	wall := s.at.Sub(r.rateFrom.at)
	if wall < rtRateWindow {
		return
	}
	frames := framesToDuration(int64(u24Diff(r.rateFrom.nmi, s.nmi)))
	r.rateFrom = s
	if !r.inRun() {
		return
	}
	ratio := float64(frames) / float64(wall)
	if ratio > 1+rtRateTolerance {
		log.Printf("alttp: run timing: running %.2f%% fast\n", (ratio-1)*100)
		r.flags |= rtFlagRate
		r.penalty += frames - wall
	} else if ratio < 1-rtRateTolerance {
		log.Printf("alttp: run timing: running %.2f%% slow\n", (1-ratio)*100)
		r.flags |= rtFlagRate
	}
}

// resetRun clears the run and has the latches in SRAM cleared.
func (r *runTimingLocal) resetRun() {
	r.started, r.finished = false, false
	r.startFrame, r.finishFrame = 0, 0
	r.startAt, r.finishAt = time.Time{}, time.Time{}
	r.lagBase, r.lagFrames = 0, 0
	r.gatesAtStart, r.gatesChanged = 0, false
	r.flags, r.penalty = 0, 0
	r.latchRawOK = false
	r.clearLatches = true
}

// runTimingPak is the FX Pak Pro address of the run timing state:
var runTimingPak = func() uint32 {
	pak, err := lorom.BusAddressToPak(runTimingAddr)
	if err != nil {
		panic(err)
	}
	return pak
}()

// enqueueRunTimingRead reads the frame counters, the latches and the gate byte.
func (g *Game) enqueueRunTimingRead(q []snes.Read) []snes.Read {
	return g.readEnqueue(q, runTimingPak, runTimingReadSize, nil)
}

// readRunTimingResponse keeps the gate byte, samples the frame counters and accepts the latches. at is the server time
// the read completed, zero if unknown.
func (g *Game) readRunTimingResponse(rsp snes.Response, at time.Time) {
	if rsp.Address != runTimingPak || len(rsp.Data) != runTimingReadSize {
		return
	}
	if !g.ROMVerified {
		return
	}
	r := &g.runTiming
	d := rsp.Data
	r.gates, r.gatesOK = d[rtmGates], true
	if r.inRun() && r.gates != r.gatesAtStart && !r.gatesChanged {
		log.Printf("alttp: run timing: gates changed from $%02x to $%02x during the run\n", r.gatesAtStart, r.gates)
		r.gatesChanged = true
	}
	if at.IsZero() {
		return
	}
	s := rtSample{nmi: u24(d[rtmNMIFrames:]), loop: u24(d[rtmLoopFrames:]), at: at}
	r.sample(s)

	var raw [len(r.latchRaw)]byte
	copy(raw[:], d[rtmStartFrame:rtmLatches+1])
	consistent := r.latchRawOK && raw == r.latchRaw
	r.latchRaw, r.latchRawOK = raw, true
	if r.clearLatches || r.clearPending.Load() {
		return
	}
	if r.clearSkips.Load() > 0 {
		r.clearSkips.Add(-1)
		r.latchRawOK = false
		return
	}
	if !consistent {
		return
	}

	latches := raw[rtmLatches-rtmStartFrame]
	startFrame := u24(raw[0:])
	finishFrame := u24(raw[rtmFinishFrame-rtmStartFrame:])
	local := g.local
	if !r.started && latches&rtmLatchStart != 0 {
		startAt, ok := g.latchTime(startFrame, at)
		if !ok {
			// latched before o2 saw the counter run (e.g. before o2 connected or across a jump); start over:
			log.Printf("alttp: run timing: start latched at NMI frame $%06x before the counter was followed; clearing\n", startFrame)
			r.clearLatches = true
			return
		}
		r.started, r.startFrame, r.startAt = true, startFrame, startAt
		r.lagBase = s.nmi - s.loop
		r.gatesAtStart, r.gatesChanged = r.gates, false
		r.flags |= rtFlagStarted
		local.GameStartTime = startAt
		g.shouldUpdatePlayersList = true
		g.PushNotification("game started")
		log.Printf("alttp: run timing: started at NMI frame $%06x, %s\n", startFrame, FormatTime(startAt))
	}
	if r.inRun() {
		r.lagFrames = (s.nmi - s.loop - r.lagBase) & 0xFFFFFF
	}
	if r.inRun() && latches&rtmLatchFinish != 0 {
		finishAt, ok := g.latchTime(finishFrame, at)
		if !ok {
			// a jump since the start; the wall time of this read is the best estimate left:
			finishAt = at
			r.flags |= rtFlagDiscontinuity
		}
		r.finished, r.finishFrame, r.finishAt = true, finishFrame, finishAt
		r.flags |= rtFlagFinished
		local.GameFinishTime = finishAt
		g.shouldUpdatePlayersList = true
		g.PushNotification(fmt.Sprintf("game finished in %s", local.FormatTimer(finishAt)))
		log.Printf("alttp: run timing: finished at NMI frame $%06x, %s\n", finishFrame, FormatTime(finishAt))
	}
}

// latchTime maps a latched frame to the server clock. at is the time of the read that saw it. Without the NMI counter,
// or for a frame from before the current stretch of the counter, ok is false, except that without the counter the
// latch is taken to have happened at the time of the read.
func (g *Game) latchTime(frame uint32, at time.Time) (t time.Time, ok bool) {
	r := &g.runTiming
	if r.noCounter {
		r.flags |= rtFlagNoCounter
		return at, true
	}
	if len(r.samples) == 0 {
		return
	}
	// within the stretch: not before its first sample (allowing for the reads before it) nor after its last (allowing
	// for the frame advancing while the read was in progress); a rewound counter makes frames from before the jump
	// look like frames after its first sample:
	if u24Diff(r.stretchStart.nmi, frame) < -int32(durationToFrames(rtJumpTolerance)) {
		return
	}
	if u24Diff(r.samples[len(r.samples)-1].nmi, frame) > 2 {
		return
	}
	return r.frameTime(frame)
}

// writeRunTiming writes the gate byte when it differs from what o2 wants, and clears the latches when asked to.
func (g *Game) writeRunTiming(now time.Time) {
	if !g.ROMVerified || g.IsResetting() {
		return
	}
	q := g.getQueue()
	if q == nil {
		return
	}
	r := &g.runTiming

	var writes []snes.Write
	if r.clearLatches {
		writes = append(writes, snes.Write{Address: runTimingPak + rtmLatches, Size: 1, Data: []byte{0}})
	}

	want := uint8(0)
	if g.SyncSprites {
		want |= gateSprites
	}
	if r.gatesOK && r.gates != want && (r.gatesWritten != want || now.Sub(r.gatesWrittenTime) >= rtGatesRewrite) {
		if want&^r.gates&gateSprites != 0 {
			// turning the remote sprites on: what remoteFrame last left for the NMI hooks is stale. Clear it first,
			// so that the NMIs before the next remoteFrame upload and skip nothing:
			st := remoteStatePak
			writes = append(writes,
				snes.Write{Address: st + rsNMILen, Size: 2, Data: []byte{0, 0}},
				snes.Write{Address: st + rsSkipSlots, Size: 4, Data: []byte{0, 0, 0, 0}},
			)
		}
		writes = append(writes, snes.Write{Address: runTimingPak + rtmGates, Size: 1, Data: []byte{want}})
		r.gatesWritten, r.gatesWrittenTime = want, now
	}
	if len(writes) == 0 {
		return
	}

	clear := r.clearLatches
	var done snes.Completion
	if clear {
		r.clearPending.Store(true)
		done = func(cmd snes.Command, err error) {
			// reads issued before the write may still be waiting to be processed:
			r.clearSkips.Store(2)
			r.clearPending.Store(false)
		}
	}
	if err := g.enqueueUnlocked(func() error {
		return q.MakeWriteCommands(writes, done).EnqueueTo(q)
	}); err != nil {
		log.Printf("alttp: run timing: error enqueuing snes write: %v\n", err)
		r.clearPending.Store(false)
		return
	}
	if clear {
		r.clearLatches = false
		r.latchRawOK = false
	}
}

// remoteStatePak is the FX Pak Pro address of the remote sprites state:
var remoteStatePak = remoteTablePak + (remoteStateAddr - remoteTableAddr)

// spritesGated reports whether the patch runs the remote sprites code, as last read from SRAM.
func (g *Game) spritesGated() bool {
	return g.runTiming.gatesOK && g.runTiming.gates&gateSprites != 0
}
