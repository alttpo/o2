package alttp

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/emulator"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

func rtSRAM(e *emulator.System, o uint32) []byte {
	return e.SRAM[(runTimingAddr+o)&0x7FFF:]
}

func setU24(b []byte, v uint32) {
	b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16)
}

// runTestNMI runs the start of the test ROM's NMI handler up to the instruction after the ones the hook replaced.
func runTestNMI(t *testing.T, e *emulator.System) {
	t.Helper()
	e.CPU.Reset()
	e.SetPC(testROMNMI)
	if !e.RunUntil(nmiCountHookReturn, 0x1_000) {
		t.Fatalf("NMI hook did not return; PC=$%06x", e.GetPC())
	}
}

// runTestFrame runs one main loop iteration of the test ROM, which calls the patched frame hook.
func runTestFrame(t *testing.T, e *emulator.System) {
	t.Helper()
	e.CPU.Reset()
	e.SetPC(testROMMainGameLoop)
	if !e.RunUntil(testROMBreakPoint, 0x2_000) {
		t.Fatalf("frame did not finish; PC=$%06x", e.GetPC())
	}
}

func TestRunTiming_NMICounter(t *testing.T) {
	e, _, err := createTestEmulator("ZELDANODENSETSU", testWriter{t})
	if err != nil {
		t.Fatal(err)
	}
	nmi := rtSRAM(e, rtmNMIFrames)
	setU24(nmi, 0x00FFFE)
	s := e.CPU.SP
	runTestNMI(t, e)
	if v := u24(nmi); v != 0x00FFFF {
		t.Errorf("NMI frames $%06x; expected $00FFFF", v)
	}
	if e.CPU.SP != s-8 {
		t.Errorf("S $%04x after the hook; expected $%04x (A, X, Y and D pushed)", e.CPU.SP, s-8)
	}
	runTestNMI(t, e)
	if v := u24(nmi); v != 0x010000 {
		t.Errorf("NMI frames $%06x; expected the carry into the high byte: $010000", v)
	}
	setU24(nmi, 0xFFFFFF)
	runTestNMI(t, e)
	if v := u24(nmi); v != 0 {
		t.Errorf("NMI frames $%06x; expected the counter to wrap to 0", v)
	}
}

// Randomizers jump to their own NMI frame counter from the same instructions; the hook jumps there in their place.
func TestRunTiming_NMICounterChain(t *testing.T) {
	var b [0x10_0000]byte
	rom, err := MakeTestROM("VT TEST", b[:])
	if err != nil {
		t.Fatal(err)
	}
	pak, _ := lorom.BusAddressToPak(nmiCountHookAddr)
	copy(rom.Contents[pak:], []byte{0x5C, 0x56, 0x34, 0x12})
	if ok, chain := nmiCountHookSite(rom); !ok || chain != 0x123456 {
		t.Fatalf("hook site ok %v, chain $%06x; expected true, $123456", ok, chain)
	}
	if err = NewPatcher(rom).Patch(); err != nil {
		t.Fatal(err)
	}
	c := rom.Contents[pak : pak+4]
	if c[0] != 0x5C {
		t.Fatalf("no JML at $%06x: % x", nmiCountHookAddr, c)
	}
	hook := uint32(c[1]) | uint32(c[2])<<8 | uint32(c[3])<<16
	hpak, _ := lorom.BusAddressToPak(hook)
	a := asm.NewEmitter(make([]byte, 0x40), false)
	a.SetBase(hook)
	emitNMICount(a, 0x123456)
	if err = a.Finalize(); err != nil {
		t.Fatal(err)
	}
	code := rom.Contents[hpak : hpak+uint32(a.Len())]
	if tail := code[len(code)-4:]; tail[0] != 0x5C || tail[1] != 0x56 || tail[2] != 0x34 || tail[3] != 0x12 {
		t.Errorf("the hook ends in % x; expected JML $123456", tail)
	}
	for i := 0; i < len(code)-4; i++ {
		if code[i] == 0x0B {
			t.Errorf("the chained hook pushes D itself (PHD at +%d)", i)
		}
	}
}

func TestRunTiming_ROMWithoutHookSite(t *testing.T) {
	rom := &snes.ROM{Contents: make([]byte, 0x10_0000)}
	pak, _ := lorom.BusAddressToPak(nmiCountHookAddr)
	copy(rom.Contents[pak:], []byte{0xEA, 0xEA, 0xEA, 0xEA})
	if ok, _ := nmiCountHookSite(rom); ok {
		t.Errorf("hook site accepted over NOPs")
	}
}

func TestRunTiming_Latches(t *testing.T) {
	e, _, err := createTestEmulator("ZELDANODENSETSU", testWriter{t})
	if err != nil {
		t.Fatal(err)
	}
	nmi, latches := rtSRAM(e, rtmNMIFrames), rtSRAM(e, rtmLatches)
	setU24(rtSRAM(e, rtmLoopFrames), 0x00FFFF)
	latches[0] = 0

	frame := func(module uint8, nmiFrames uint32) {
		t.Helper()
		setU24(nmi, nmiFrames)
		e.WRAM[0x10] = module
		runTestFrame(t, e)
	}

	frame(0x01, 0x100)
	if v := u24(rtSRAM(e, rtmLoopFrames)); v != 0x010000 {
		t.Errorf("loop frames $%06x; expected $010000", v)
	}
	if latches[0] != 0 {
		t.Fatalf("latches $%02x at file select; expected 0", latches[0])
	}
	frame(runFinishModule, 0x101)
	if latches[0] != 0 {
		t.Fatalf("latches $%02x: finished before the start", latches[0])
	}

	frame(runStartModule, 0x123456)
	if latches[0] != rtmLatchStart || u24(rtSRAM(e, rtmStartFrame)) != 0x123456 {
		t.Fatalf("latches $%02x, start $%06x; expected $01, $123456", latches[0], u24(rtSRAM(e, rtmStartFrame)))
	}
	// the following frames of the module, and loading the file again after a save & quit, don't move it:
	frame(runStartModule, 0x123457)
	frame(0x07, 0x200000)
	frame(runStartModule, 0x300000)
	if u24(rtSRAM(e, rtmStartFrame)) != 0x123456 {
		t.Errorf("start $%06x; expected it to stay $123456", u24(rtSRAM(e, rtmStartFrame)))
	}

	frame(runFinishModule, 0x400000)
	frame(runFinishModule, 0x400001)
	if latches[0] != rtmLatchStart|rtmLatchFinish || u24(rtSRAM(e, rtmFinishFrame)) != 0x400000 {
		t.Fatalf("latches $%02x, finish $%06x; expected $03, $400000", latches[0], u24(rtSRAM(e, rtmFinishFrame)))
	}

	// a client resets the run:
	latches[0] = 0
	frame(runStartModule, 0x500000)
	if latches[0] != rtmLatchStart || u24(rtSRAM(e, rtmStartFrame)) != 0x500000 {
		t.Errorf("latches $%02x, start $%06x after the reset; expected $01, $500000", latches[0], u24(rtSRAM(e, rtmStartFrame)))
	}
}

// The init code clears the gate byte on reset, and leaves the counters and latches alone.
func TestRunTiming_InitClearsOnlyGates(t *testing.T) {
	e, _, err := createTestEmulator("ZELDANODENSETSU", testWriter{t})
	if err != nil {
		t.Fatal(err)
	}
	rt := rtSRAM(e, 0)
	for i := 0; i < runTimingReadSize; i++ {
		rt[i] = byte(0x11 * (i + 1))
	}
	e.CPU.Reset()
	if !e.RunUntil(0x00_8034, 0x4_000) {
		t.Fatalf("init did not finish")
	}
	for i := 0; i < runTimingReadSize; i++ {
		want := byte(0x11 * (i + 1))
		if i == rtmGates {
			want = 0
		}
		if rt[i] != want {
			t.Errorf("run timing byte %d $%02x after reset; expected $%02x", i, rt[i], want)
		}
	}
}

func testSamples(r *runTimingLocal, t0 time.Time, nmi0 uint32, n int, latency func(i int) time.Duration) (last rtSample) {
	for i := 0; i < n; i++ {
		f := nmi0 + uint32(i*3)
		at := t0.Add(framesToDuration(int64(i*3)) + latency(i))
		last = rtSample{nmi: f & 0xFFFFFF, loop: f & 0xFFFFFF, at: at}
		r.sample(last)
	}
	return
}

// Frames map to the time of the read with the least latency.
func TestRunTiming_FrameTime(t *testing.T) {
	var r runTimingLocal
	t0 := time.Unix(1_700_000_000, 0)
	testSamples(&r, t0, 0xFFFF00, 20, func(i int) time.Duration {
		if i == 7 {
			return 2 * time.Millisecond
		}
		return time.Duration(10+i%5) * time.Millisecond
	})
	at, ok := r.frameTime(0xFFFF00 + 21)
	if !ok {
		t.Fatal("no frame time")
	}
	want := t0.Add(framesToDuration(21) + 2*time.Millisecond)
	if d := at.Sub(want); d < -time.Microsecond || d > time.Microsecond {
		t.Errorf("frame time off by %v", d)
	}
	// across the counter wrapping around:
	at, _ = r.frameTime(0x000010)
	want = t0.Add(framesToDuration(0x110) + 2*time.Millisecond)
	if d := at.Sub(want); d < -time.Microsecond || d > time.Microsecond {
		t.Errorf("frame time across the wrap off by %v", d)
	}
}

// A single torn read is dropped; a jump confirmed by the next read starts a new stretch and is flagged during the run.
func TestRunTiming_Jumps(t *testing.T) {
	var r runTimingLocal
	t0 := time.Unix(1_700_000_000, 0)
	last := testSamples(&r, t0, 0x10000, 10, func(int) time.Duration { return 0 })

	// torn: the low word wrapped but not the high byte:
	r.sample(rtSample{nmi: 0x10000 + 30 - 0x10000, at: last.at.Add(50 * time.Millisecond)})
	r.sample(rtSample{nmi: last.nmi + 6, at: last.at.Add(100 * time.Millisecond)})
	if len(r.samples) != 11 || r.stretchStart.nmi != 0x10000 {
		t.Fatalf("%d samples, stretch from $%06x; expected the torn read dropped", len(r.samples), r.stretchStart.nmi)
	}

	r.started = true
	last = r.samples[len(r.samples)-1]
	// a savestate load rewinds the counter:
	back := rtSample{nmi: 0x8000, at: last.at.Add(time.Second)}
	r.sample(back)
	r.sample(rtSample{nmi: 0x8000 + 60, at: back.at.Add(framesToDuration(60))})
	if r.stretchStart.nmi != 0x8000 || len(r.samples) != 2 {
		t.Fatalf("stretch from $%06x with %d samples; expected a new stretch from $008000", r.stretchStart.nmi, len(r.samples))
	}
	if r.flags&rtFlagDiscontinuity == 0 {
		t.Errorf("flags $%02x; expected the discontinuity flagged", r.flags)
	}
	if r.penalty != 0 {
		t.Errorf("penalty %v for going back; expected none", r.penalty)
	}

	// a latch from before the new stretch can't be mapped:
	g := &Game{}
	g.runTiming.samples, g.runTiming.stretchStart = r.samples, r.stretchStart
	if _, ok := g.latchTime(0x10000, back.at); ok {
		t.Errorf("latch from before the jump mapped")
	}
	if _, ok := g.latchTime(0x8010, back.at); !ok {
		t.Errorf("latch within the stretch not mapped")
	}
}

// Running fast during the run is flagged and the wall time it saved is charged.
func TestRunTiming_FastForward(t *testing.T) {
	var r runTimingLocal
	r.started = true
	t0 := time.Unix(1_700_000_000, 0)
	// 5% fast for 20 seconds, sampled every 100ms:
	var s rtSample
	for i := 0; i <= 200; i++ {
		wall := time.Duration(i) * 100 * time.Millisecond
		frames := durationToFrames(wall * 105 / 100)
		s = rtSample{nmi: uint32(frames) & 0xFFFFFF, at: t0.Add(wall)}
		r.sample(s)
	}
	if r.flags&rtFlagRate == 0 {
		t.Fatalf("flags $%02x; expected the rate flagged", r.flags)
	}
	if r.penalty < 900*time.Millisecond || r.penalty > 1100*time.Millisecond {
		t.Errorf("penalty %v; expected about 1s (5%% of 20s)", r.penalty)
	}
}

// Latches are only accepted from two identical reads, and a start latched before o2 followed the counter is cleared.
func TestRunTiming_AcceptLatches(t *testing.T) {
	gs, err := createTestGameSync("ZELDANODENSETSU", "g1", testWriter{t})
	if err != nil {
		t.Fatal(err)
	}
	g := gs.g
	g.ROMVerified = true
	t0 := time.Unix(1_700_000_000, 0)
	read := func(i int, nmi uint32, latches uint8, start uint32) {
		d := make([]byte, runTimingReadSize)
		setU24(d[rtmNMIFrames:], nmi)
		setU24(d[rtmLoopFrames:], nmi-uint32(i/10))
		setU24(d[rtmStartFrame:], start)
		d[rtmLatches] = latches
		g.readRunTimingResponse(snes.Response{Address: runTimingPak, Size: uint8(len(d)), Data: d}, t0.Add(framesToDuration(int64(nmi-0x1000))))
	}

	// latched long before o2 saw the counter:
	read(0, 0x1000, rtmLatchStart, 0x0100)
	read(1, 0x1001, rtmLatchStart, 0x0100)
	if g.runTiming.started || !g.runTiming.clearLatches {
		t.Fatalf("started %v, clear %v; expected a stale start cleared", g.runTiming.started, g.runTiming.clearLatches)
	}
	g.runTiming.clearLatches = false

	read(2, 0x1002, 0, 0)
	read(3, 0x1010, rtmLatchStart, 0x1008)
	if g.runTiming.started {
		t.Fatalf("started from a single read")
	}
	read(4, 0x1011, rtmLatchStart, 0x1008)
	if !g.runTiming.started || g.runTiming.startFrame != 0x1008 {
		t.Fatalf("started %v at $%06x; expected $001008", g.runTiming.started, g.runTiming.startFrame)
	}
	if want := t0.Add(framesToDuration(8)); !g.runTiming.startAt.Equal(want) || !g.local.GameStartTime.Equal(want) {
		t.Errorf("start at %v (player %v); expected %v", g.runTiming.startAt, g.local.GameStartTime, want)
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestRunTiming_MessageRoundTrip(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 123_456_789)
	m := &runTimingRemote{
		Flags:          rtFlagStarted | rtFlagFinished | rtFlagRate,
		ResetID:        0x0102030405060708,
		ResetInitiator: 3,
		Gates:          gateSprites,
		GatesChanged:   true,
		StartAt:        t0,
		FinishAt:       t0.Add(2 * time.Hour),
		StartFrame:     0x123456,
		FinishFrame:    0xABCDEF,
		NMIFrames:      0xABCDF0,
		LoopFrames:     0xAB0000,
		SampleAt:       t0.Add(2*time.Hour + time.Second),
		LagFrames:      0x00CDEF,
		Penalty:        1234 * time.Millisecond,
	}
	copy(m.Signature[:], "O2-PATCH12345678")

	var b bytes.Buffer
	if err := SerializeRunTiming(&b, m); err != nil {
		t.Fatal(err)
	}
	if b.Len() != 3+runTimingMessageSize {
		t.Fatalf("message is %d bytes; expected %d", b.Len(), 3+runTimingMessageSize)
	}
	data := b.Bytes()
	if data[0] != byte(MsgRunTiming) {
		t.Fatalf("type $%02x", data[0])
	}

	g := &Game{}
	p := &Player{lastServerStamp: t0.Add(time.Millisecond)}
	// followed by another message, which must still be parsed:
	r := bytes.NewReader(append(append([]byte(nil), data[1:]...), 0xEE))
	if err := g.DeserializeRunTiming(p, r); err != nil {
		t.Fatal(err)
	}
	if rest, _ := io.ReadAll(r); len(rest) != 1 || rest[0] != 0xEE {
		t.Fatalf("left % x unread; expected ee", rest)
	}
	got := p.RunTiming
	got.OK, got.StartSeenAt, got.FinishSeenAt = false, time.Time{}, time.Time{}
	if got != *m {
		t.Errorf("got %+v\nexpected %+v", got, *m)
	}
	if !p.GameStartTime.Equal(m.StartAt) || !p.GameFinishTime.Equal(m.FinishAt) {
		t.Errorf("player times %v, %v", p.GameStartTime, p.GameFinishTime)
	}
	if !p.RunTiming.StartSeenAt.Equal(p.lastServerStamp) {
		t.Errorf("start seen at %v; expected the message's server stamp", p.RunTiming.StartSeenAt)
	}

	// a later version with more fields is skipped by its size:
	p2 := &Player{}
	later := []byte{runTimingVersion + 1, 2, 0xAA, 0xBB, 0xEE}
	r = bytes.NewReader(later)
	if err := g.DeserializeRunTiming(p2, r); err != nil {
		t.Fatal(err)
	}
	if p2.RunTiming.OK {
		t.Errorf("accepted version %d", runTimingVersion+1)
	}
	if rest, _ := io.ReadAll(r); len(rest) != 1 || rest[0] != 0xEE {
		t.Errorf("left % x unread after a later version; expected ee", rest)
	}
}
