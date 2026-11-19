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
8. remoteFrame's drawing split between o2 and the patch: 5,664 cycles with an otherwise empty OAM buffer, 11,607
   with a nearly full one. o2 can't write finished OAM entries (the scroll offsets change every frame and o2 reads
   them frames late; the unused OAM entries and free VRAM slots are only known after the module drew), so it does
   what doesn't depend on the frame: it sorts the table into lists of one kind each (extras, slotted, shadows,
   blobs, front; the first player's shadow and blob are skipped when the body is drawn), and writes each entry's
   extended attributes but for x bit 8. Each kind has its own loop (`emitDrawLoop`) that keeps an unused OAM entry
   in Y and writes x and y as it computes them: about 150 cycles per sprite, down from about 230. The scan compares
   y with X = $F0 (7 cycles per unused entry). All shadows are now drawn behind all blobs.
9. alttpo (`../alttpo/alttpo/RunTiming.as`, branch `esync`, commit `bbc2d8c`): reads the `$707000` block and the
   signature in `post_frame`, stamps latches, detects jumps, savestate loads (sets flag bit 4) and running fast,
   writes the gate byte (bit 0 with SyncSprites, clearing the remote sprites NMI state first), min-RTT clock in
   `receive.as`, sends/receives 0x84, the full 0x81 state machine including "Reset run", and shows the run timer and
   its own time in the settings window. It compiles in bsnes-as and, on an alttpo-patcher ROM, reads the signature,
   counts frames and writes the gate. ALTTPO_COMPAT.md updated.

10. First real test (2026-11-18), o2 on an FX Pak and alttpo in bsnes-as, after loading a newly patched ROM into a
    running bsnes-as. alttpo showed a finish without reaching the triforce room, 16,776,239 lag frames and "frame rate
    out of tolerance". The SRAM file bsnes-as wrote for the new ROM showed why: fresh SRAM reads `$FF`, so
    `latches = $FF` and `startFrame = finishFrame = $FFFFFF`. bsnes-as keeps the script loaded across ROM loads, so
    alttpo was still in the run it had started on the previous ROM. Three bugs, fixed in both clients:
    - A finish latch was taken whatever start the SRAM held: the fresh SRAM's `$FF` was taken for the finish (at the
      read's time, since `$FFFFFF` isn't in the counter's stretch). Now, during the run, SRAM that doesn't hold the
      run's start (start latch clear or a different `startFrame`: another ROM or SRAM file, or a savestate from before
      the start) gets the run's start frame and only the start latch written back, and only a finish latched against
      it is taken. This also fixes a savestate from before the start, after which the patch would never latch the
      finish.
    - Lag frames were `nmi - loop - lagBase` with `lagBase` from the start, so a counter from another SRAM (or a
      savestate with fewer lag frames) made them negative: 16,776,239 is -977 mod 2²⁴. They now carry over each new
      stretch of the counter, counted from that stretch's own `nmi - loop`.
    - Running more than 1% slow over a 10 s window set the rate flag. bsnes-as itself is on rate (measured with a
      script: 0.998 to 1.002 of 60.0988 fps over 5 s windows, audio sync, OpenAL at 48 kHz). But booting a ROM runs
      frames without NMIs (about 80 frames after loading it), and a stall of more than 100 ms is 1% of a window; any one
      of these during the run set the flag for good. Slow can't shorten the wall time, so it's now only logged.
    - alttpo also follows a new ROM's counter from scratch when the patch signature changes, rather than charge an
      SRAM counter that happens to be ahead as fast-forward.

    o2: `runtiming.go` (`restoreStart`, `lagAcc`), tests `TestRunTiming_RestoreStart` and
    `TestRunTiming_SlowNotFlagged`. alttpo: `RunTiming.as` (compiles in bsnes-as).

11. The group reset asks first, in o2's web UI (a dialog) and in alttpo (`ResetRunWindow.as`, from both "Reset run"
    buttons): it resets the run timer for every player and clears all synchronized game state for everyone, and to
    reset fully every player first resets their console or emulator and then one player presses Reset, so that no
    player still in the old game syncs its state back.

12. Remote players flickered while Link walked diagonally in the rain in the castle courtyard. The overworld scrolling
    there runs the frame late or makes it lag, and remoteFrame's time checks then drew nothing at all. Changes:
    - A lag frame no longer skips the work. The NMI that came during the frame starts the next frame's time, and the
      scanline counts from there. A scanline read before an NMI is at most 224, which is late for any check, so the
      read can't race the NMI. preMain no longer stores the NMI count (`rsFrameNMI` is gone).
    - The drawing check's deadline depends on the table's size (`drawLateTable`, indexed by `rtFrontEnd`):
      2,100 + 240 cycles per sprite (1,873 + about 233 measured, slotted sprites past a full OAM buffer). One remote
      player with Link's sprites is about 6 to 10 sprites, late from line 190 to 184; it used to be 167 whatever the
      size. The old 7,000-cycle budget was also too low for a table full of slotted sprites (9,286).
    - Listing the dying enemies has its own time check (`rf_dying_time`, line 175). If it's late, the patch writes
      `$FFFF`, so o2 leaves out all death chrs that frame. Without the list, a scan block costs 1,808 cycles, so the
      scan is late from line 200 instead of 175.
    - A late scan no longer stops drawing. The parts are drawn in the VRAM slots they got last time (`rsReuse`, the
      high byte of `rsSkipSlots`, so the clear o2 and alttpo already do before turning the sprites gate on also
      clears it), up to `rfReuseFrames` (8) frames in a row. After that, the first player is drawn as a blob. The kept
      follower slots stay skipped, so the NMI uploads only rows that aren't still in VRAM.
    - Not done: holding the previous frame's OAM entries when the drawing check itself is late. Saving them for that
      costs about 65 cycles per sprite every frame, and it would only help when the drawing check starts within about
      9 lines of the deadline.

`go test ./...` passes. Not yet run with o2 and alttpo connected to a server together since these fixes.

## Next steps

1. **Try it for real.** o2 on hardware and an emulator, and a mixed o2 + alttpo group on a local server:
   - `nmiFrames` advances at about 60.0988/s; `nmiFrames - loopFrames` grows only on lag frames.
   - Start and finish latch on a vanilla ROM and a randomizer ROM, and o2 and alttpo show the same run time.
   - A group reset started from either client clears both, and both then report the same reset id in 0x84.
   - Toggle `syncSprites` mid-game and look for glitches.
   - Load another ROM in bsnes-as mid-run: alttpo logs "SRAM lost the run's start", the run goes on, and the triforce
     room finishes it with the lag frames still counting up from before.
   - Fresh SRAM reads `latches = $FF`; both clients clear it as a stale latch once they have the server clock. Check
     that the first start after that latches.
2. **Per-frame costs.** remoteFrame is still the biggest (5,977 cycles usually, 11,716 with a nearly full OAM
   buffer; measured 2026-11-18 at `7174cd0`, 3 remote players). By part, empty / nearly full OAM buffer: setup, capture
   checks and module checks 555 / 561; the scan 1,180 / 6,201 (with its time checks); storing the captured length 22;
   handing out VRAM slots 247; drawing 3,531 for 20 sprites / 4,243 for the 16 that fit (the search for unused entries
   grows with a full buffer); listing the rows to upload 423; exit 19. These come from checkpoints at `rf_start`,
   `rf_scan_check`, `rf_scan_done`, `rf_drawing`, `rf_alloc_done`, `rf_draw_done` and `rf_exit` with the
   `TestCycleBudgets_RemoteSprites*` scenarios (`cycleProfile.checkpoints`). The time checks measure a worst case scan
   of 5,517 cycles (budget 5,800) and drawing of 6,740 (budget 7,000). It now reads the scanline (and the NMI count, for lag) before each 16 entries of the scan and before
   drawing, and skips the rest rather than finish after the NMI: from line 175 for the scan and 167 for drawing
   (`rfScanCycles`, `rfDrawCycles`, `timecheck_test.go`). o2 now leaves out enemy death chrs near dying enemies
   (`nearDyingEnemy`); the patch only lists the dying enemies with the captured entries (`linkOAMCopyDying`), so
   death poofs now take capture slots (16) until o2 drops them. To check on hardware: no lag
   frames from remote sprites in busy rooms, and how often drawing is skipped (remote players flicker). What's left:
   the scan (1,180 cycles even with an empty OAM buffer, run whenever there are parts), drawing (about 150 cycles per
   sprite, of which about 40 are the extended attributes: x bit 8 and the index of the entry), listing the rows to
   upload (423; o2 could precompute each row's WRAM bit and row offset, at the cost of about 2 table entries), and the capture checks of carried sprites and ancillae (about 300 while a remote player is in the
   location; o2 could set the capture mask from its own WRAM reads, a few frames late). Also measure the busy paths of link color, sfx and manip pending (only their
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
