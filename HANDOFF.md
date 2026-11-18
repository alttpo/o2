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

## Done (3 commits)

1. `b02dc4c` patch and o2 runtime (`games/alttp/runtiming.go`):
   - NMI entry hook at `$0080CC`. It runs the replaced `PHA PHX PHY PHD` itself, or chains to a randomizer's `JML`
     at that address.
   - A loop counter, and start/finish latches in preMain (`emitRunTiming`).
   - The sprites gate in preMain and all 4 NMI hooks (`remotesprites.go`).
   - Sampling the counters in o2, mapping frames to server time (`frameTime`: the earliest projection of the last 32
     samples), discontinuities and rate checks, and accepting a latch only after two identical reads.
   - Writing the gate. When the sprites gate turns on, o2 first zeroes `rsNMILen`, `rsSkipSlots` and `rsKept`.
   - Clearing the latches on a group reset, plus skipping the reads that were issued before the clear completed.
   - `patchFormat` is 10.
2. `95de147` `clock.go`: min-RTT echo clock. Tests set `clock.lastMessageOnly`, because the test server's clock is
   simulated.
3. `93a807a` message 0x84: serialize/deserialize, sent with the slowbeat and right after a latch is accepted, and it
   takes precedence over 0x80. Also `Player.lastServerStamp` and `RunTiming.StartSeenAt`/`FinishSeenAt`, the penalty
   in the run's end, and lag frames, penalty and flags in `PlayerViewModel`.

Tests: `runtiming_test.go`, `clock_test.go`, and the rewritten `TestRemoteSpritesScenario_SyncSpritesOff`. The test
ROM now has an NMI handler at `$0080C9` (`testROMNMI`). `go test ./...` passes. None of this has been run on an FX Pak
or a real emulator.

## Next steps

1. **Try it on hardware and an emulator.**
   - Check that `nmiFrames` advances at about 60.0988/s and that `nmiFrames - loopFrames` grows only on lag frames.
   - Check that start and finish latch on a vanilla ROM and on a randomizer ROM.
   - Toggle `syncSprites` mid-game and look for graphics glitches around the toggle.
   - Measure how many frames the game runs with NMI disabled (boot, loads) against wall time; that sets
     `rtRateTolerance`.
2. **Peer verification** (RUN_TIMING.md, Verification), in a new function run from `updatePlayersList`. Mark players
   unverified, with a reason; never change their reported times.
   - No backdating: compare `StartSeenAt`/`FinishSeenAt` with `StartAt`/`FinishAt` (within 2 s, and not earlier than
     the server stamp minus the player's minimum `serverStamp - SampleAt`).
   - Rate fitted from `(NMIFrames, lastServerStamp)` of each 0x84 message.
   - Module agreement with location messages 0x01: record the server stamp of the first one showing module ≥ $05,
     then $19.
   - Signatures and gates equal to everyone else's, and `GatesChanged` false.
3. **UI:** show lag frames, penalty and flags, a verification badge per player, and a warning when signatures or gates
   differ. `PlayerViewModel` already carries the first three; find where the web UI renders `game/players`.
4. **Gates as a group setting**, so one player can't run with different gates by accident. At minimum, warn when they
   differ.
5. **Cycle budgets.** Measure the worst-case cycles of every per-frame routine in the test emulator: preMain's
   `emitRunTiming` (about 50), `emitLinkColor`, `emitSfx`, `manipPending`, `remoteFrame`, the NMI hooks and
   `nmiCount` (about 35). Then trim the worst and document each one's budget next to its code. Consider gates for
   tunic color (`syncTunicColor`) and sfx.
6. **alttpo** (`../alttpo/alttpo/*.as`):
   - Read the `$707000` block from SRAM.
   - Stamp `server_now()` minus the frames the latch is behind when a latch bit appears (in `post_frame`).
   - Write the gate byte from its settings, and set bit 0 whenever the run uses remote sprites, even though alttpo
     draws them itself.
   - Port the min-RTT clock into `receive.as`.
   - Send and receive 0x84, and port the 0x81 group reset state machine.
   - Then flip the 0x84 row in ALTTPO_COMPAT.md to "yes". Compile check: run bsnes-as headless under a pty (see
     memory).
7. **Smaller things:**
   - Lag frames are counted from when o2 accepted the start, a few frames late. Latching `loopFrames` with the start
     would fix that (it would need a bigger SRAM block).
   - If o2 restarts mid-run, the run is lost (latches from before o2 saw the counter are cleared). Possible recovery:
     accept a latch older than the counter stretch when the group's 0x84 history for this player has it.
   - The flag for a detected savestate load (bit 4) is never set by o2. Only alttpo can tell, by comparing its own
     script counter with SRAM.

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
