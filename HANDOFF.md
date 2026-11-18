# Handoff: run timer

Branch `feature/run-timer`, based on `feature/alttpo-compat` at `9e088b9`. The goal is a run timer that measures a
whole group run on the server's clock, from the first player's start (first `Module05_LoadFile`) to the last player's
finish (first `Module19_TriforceRoom`). It has to keep every player honest, and o2 and alttpo players have to get the
same timing.

**Read [RUN_TIMING.md](RUN_TIMING.md) first.** It is the contract shared with alttpo: SRAM layout, gate bits,
message 0x84, clock sync, discontinuities, verification and display.

## Decisions already made

- **Official time is wall time on the server's clock.** Frame counters make start and finish frame-exact and detect
  dishonest clocks, but they don't replace wall time. A discrepancy never helps a player: running fast is charged
  back as a penalty. Rewinding savestates and pausing already cost wall time, so they're only flagged.
- **Everyone runs the same o2-patched ROM, alttpo players included.** The patch's per-frame code can cause lag frames,
  so different ROMs would give different players different costs. Players are compared by the patch signature at
  `$707FF0`; `alttpo-patcher` writes the patched ROM for alttpo players.
- **Don't pay for what you don't use.** Each optional patch feature sits behind a bit of the gate byte `$70700D`.
  The init code zeroes it on reset. With a bit clear, the feature costs one long load and a branch, then the game's
  original instructions run. The gates are a run setting, and every player should have the same ones.
- **`syncSprites` off now turns the whole feature off**, including capturing and sending your own sprites, not just
  drawing other players.
- **The server clock comes from echo round trips**: the offset from the echo with the shortest round trip out of the
  last 30. The server itself is unchanged.

## Done

1. `8bdecaf` patch and o2 runtime (`games/alttp/runtiming.go`): NMI entry hook at `$0080CC` (or chaining to a
   randomizer's `JML` there), loop counter and start/finish latches in preMain, the sprites gate in preMain and the 4
   NMI hooks, sampling and mapping frames to server time, discontinuities and rate checks, writing the gate, clearing
   the latches on a group reset. `patchFormat` is 10.
2. `93d02f6` `clock.go`: min-RTT echo clock.
3. `f73c9b3` message 0x84, sent with the slowbeat and right after a latch is accepted.
4. Peer verification (`verify.go`), run from `updatePlayersList`: no backdating or forward dating (2 s, and the
   minimum one-way delay), module agreement with the location messages (the last pre-game to in-game change before the
   start, the first $19 after it), the rate from server stamps (offset `stamp - frames × period` only goes down when
   frames run ahead; the gain must stay within the player's penalty + 2 s + 1%), and equal cost against the most
   common signature and gates (the local player's on a tie). 0x84 of another group reset is ignored once both sides
   finalized one. Tests in `verify_test.go`.
5. UI: lag frames, penalty after the finish, a verification badge with the reasons and flags in its tooltip, and a
   warning row when signatures or gates differ (`game/run/warning`).
6. Cycle budgets (`cycles_test.go`): a profiler that charges each instruction to the patch routine (or part of
   preMain) its address is in, using label addresses from the patch listing. Measured and documented next to each
   routine: NMI count 63, run timing 110 worst (40 usual), gate test 26, and with 3 remote players NMI upload 546,
   NMI hooks 40.
7. remoteFrame trimmed. The old 13,867 was measured on the other players' emulators, whose OAM buffers are never
   cleared; the budget tests now profile only g1, in two defined cases: an otherwise empty OAM buffer (7,385) and a
   nearly full one (12,953, which was 15,925 before trimming). The scan looks each used entry's chr up in
   captureChrTable (with `scanChrSlot` for $00..$3F) and calls the handler only for the entries it acts on; the
   handler is split in two halves so that it runs with 8-bit index registers; a capture's copy keeps its offsets in
   the direct page; drawing stops once the OAM buffer is full; the search for unused entries looks at 8 at a time.
   This also found an emulator bug (`alttpo/snes`): setting the x flag didn't clear the high bytes of X and Y. Fixed
   in `../snes` (local commit on `main`, used through go.work); `go.mod` still pins the older version until it's
   pushed.
7. alttpo (`../alttpo/alttpo/RunTiming.as`, branch `esync`, commit `bbc2d8c`): reads the `$707000` block and the
   signature in `post_frame`, stamps latches, detects jumps, savestate loads (sets flag bit 4) and running fast,
   writes the gate byte (bit 0 with SyncSprites, clearing the remote sprites NMI state first), min-RTT clock in
   `receive.as`, sends/receives 0x84, the full 0x81 state machine including "Reset run", and shows the run timer and
   its own time in the settings window. It compiles in bsnes-as and, on an alttpo-patcher ROM, reads the signature,
   counts frames and writes the gate. ALTTPO_COMPAT.md updated.

`go test ./...` passes. Nothing has been run on an FX Pak, or with o2 and alttpo connected to a server together.

## Next steps

1. **Try it for real.** o2 on hardware and an emulator, and a mixed o2 + alttpo group on a local server:
   - `nmiFrames` advances at about 60.0988/s; `nmiFrames - loopFrames` grows only on lag frames.
   - Start and finish latch on a vanilla ROM and a randomizer ROM, and o2 and alttpo show the same run time.
   - A group reset started from either client clears both, and both then report the same reset id in 0x84.
   - Toggle `syncSprites` mid-game and look for glitches; measure frames run with NMI off to set `rtRateTolerance`.
   - Fresh SRAM reads `latches = $FF`; both clients clear it as a stale latch once they have the server clock. Check
     that the first start after that latches.
2. **Per-frame costs.** remoteFrame is still the biggest (7,385 cycles usually, 12,953 with a nearly full OAM
   buffer). What's left to trim there is drawing each sprite (about 170 cycles: the extended attribute write alone
   is about 40) and the per-entry scan. Also measure the busy paths of link color, sfx and manip pending (only their
   idle paths are measured), and consider gates for tunic color and sfx. Push the `snes` emulator fix and bump
   `go.mod`; without go.work, `TestRemoteSpritesScenario_OnlyOwnShadowsSent` fails on the old emulator.
3. **Peer verification in alttpo** (port `verify.go`); alttpo only warns about differing signatures or gates.
4. **Gates as a group setting**, so a player can't run with different gates by accident. Only the warning exists.
5. **Smaller things:**
   - Lag frames are counted from when o2 accepted the start, a few frames late. Latching `loopFrames` with the start
     would fix that (it would need a bigger SRAM block).
   - If o2 restarts mid-run, the run is lost. Possible recovery: accept a latch older than the counter stretch when
     the group's 0x84 history for this player has it.
   - o2 never sets flag bit 4 (savestate load); alttpo sets it whenever the counter goes back.
   - alttpo's echo round trips include up to a frame of waiting for `pre_frame` to receive them; the min-RTT pick
     mostly hides that.

## Where things are

| What | Where |
|---|---|
| SRAM layout, hook sites, emitters, o2 sampling and latches, 0x84 | `games/alttp/runtiming.go` |
| preMain gating, NMI count hook install, init clearing the gates | `games/alttp/patcher.go` (`emitPreMain`, `Patch`, `emitInit`) |
| NMI hook gating | `games/alttp/remotesprites.go` (`emitNMIHooks`) |
| Server clock | `games/alttp/clock.go`, `Game.ServerNow` in `game.go` |
| Reads and writes in the game loop | `games/alttp/run.go` (`enqueueWRAMReads`, `readMainComplete`, `writeRunTiming` call) |
| Players list and run timer display | `games/alttp/viewmodel.go` |
| Group reset clearing the run | `games/alttp/reset.go` (`resetClearState`) |
| Peer verification | `games/alttp/verify.go` |
| Cycle budgets | `games/alttp/cycles_test.go` |
| alttpo side | `../alttpo/alttpo/RunTiming.as` |
