# Run timing

This is the shared contract that o2 and alttpo follow to time a group run. A run lasts from the first player's start
to the last player's finish, on the server's clock. Both clients must implement every section marked **(both)**.

## Definitions

- **Start**: the first main loop iteration, after the run was reset, in which the module ($7E0010) is $05
  (Module05_LoadFile). Later loads, such as reloading the file after a save & quit or a console reset, do not move it.
- **Finish**: the first main loop iteration after the start in which the module is $19 (Module19_TriforceRoom).
- **Player time**: finish minus start, both on the server clock.
- **Run time**: the latest finish (plus any penalty, see [Verification](#verification-both)) minus the earliest start,
  over every player who started.
- The official time is **wall time on the server's clock**. The frame counters below make the start and finish
  frame-exact and let peers detect a clock that doesn't match the frames, but they don't replace wall time.

## Equal cost: one ROM for everyone (both)

The o2 patch runs code every frame. Any extra cycles can turn a frame into a lag frame, so every player in a timed run
must pay exactly the same:

1. **Every player runs the same o2-patched ROM**, alttpo players included. Players are compared by the 16 byte patch
   signature that the patched init code copies to SRAM at `$707FF0` (magic, patch version, CRC32 of the unpatched ROM;
   see `signature.go`). alttpo reads it from SRAM the same way. o2's `alttpo-patcher` command writes the patched ROM.
2. **Don't pay for what you don't use.** Every optional feature of the patch is behind a bit of the gate byte at
   `$70700D` (see [SNES state](#snes-state-both)). With its bit clear, a feature costs only the test of that bit (a long load and a branch) and runs the
   game's original instructions. The init code zeroes the gate byte on every reset, so the ROM costs (almost)
   nothing until a client turns features on.
3. **Every player runs with the same gates for the whole run.** The gates are a run setting: each client writes the
   gate byte from its settings, reads it back, and reports it in its run timing message. A player whose gates differ
   from the others', or changed during the run, is shown as such.
4. **Costs that depend on what's on screen remain.** For example, drawing three remote players costs more than
   drawing none. To make that visible, every player's lag frame count over the run is published (NMI frames minus
   main loop frames).
5. The patch's per-frame routines are written for the lowest worst-case cycle count, and each one's worst case is
   documented next to its code.

### Gate bits (`$70700D`)

| Bit | Feature | Code behind it |
|---|---|---|
| 0 | remote sprites (o2's `syncSprites`) | copying Link's OAM entries, capturing the local player's sparkles, dust and other effects, drawing remote players, and the 4 NMI hooks that upload their tiles |
| 1..7 | reserved, 0 | |

alttpo draws remote players itself, outside of the emulated SNES. To pay what o2 players pay, it still sets bit 0
whenever the run uses remote sprites.

## SNES state (both)

These are in SRAM, outside of the range the init code zeroes, so they keep counting across console resets; the init
code zeroes only the gate byte. All values are little-endian:

| Address | Size | Name | Written by | Meaning |
|---|---|---|---|---|
| `$707000` | u24 | `nmiFrames` | NMI entry | incremented on every NMI, including lag frames |
| `$707003` | u24 | `loopFrames` | preMain | incremented once per main loop iteration; `nmiFrames - loopFrames` counts lag frames |
| `$707006` | u24 | `startFrame` | preMain | `nmiFrames` at the start |
| `$707009` | u24 | `finishFrame` | preMain | `nmiFrames` at the finish |
| `$70700C` | u8 | `latches` | preMain, clients | bit 0: `startFrame` is set; bit 1: `finishFrame` is set |
| `$70700D` | u8 | `gates` | clients (zeroed by the init code) | see [Gate bits](#gate-bits-70700d) |

- The NMI entry hook replaces `PHA : PHX : PHY : PHD` at `$0080CC`. Randomizers (z3randomizer) already put a
  `JML NMIHookAction` there: the hook then jumps to their routine instead of running the replaced instructions. Any
  other code at `$0080CC` means that ROM has no counter; clients report that with flag bit 5.
- preMain latches before it calls the module, so the latched value is the NMI count of the frame the module first ran
  in. It writes the counter first and the latch bit last.
- A client resets the run by writing 0 to `latches` (and nothing else).
- The counters are 24-bit: at 60.0988 frames per second they wrap after 77.5 hours. Readers compute differences
  modulo 2²⁴.
- SNES devices don't read SRAM atomically. A reader accepts a latch only after two consecutive reads return the same
  `latches` and frame values. It drops a counter sample that is off by more than 2 seconds of frames from what the
  wall clock since the last sample predicts, unless the next sample confirms it (see the
  [discontinuities](#discontinuities-both)).

## Clock (both)

Each client estimates the server clock from its echo round trips (one every 500 ms), NTP style:

```
offset   = serverTime - (sentAt + rtt/2)     // serverTime from the echo reply, sentAt and rtt on the local clock
serverNow = localNow + offset
```

The client uses the offset of the echo with the lowest round trip time out of the last 30. This removes the downlink
delay that "last server timestamp + time since it arrived" adds, which differs per player and would bias their start
and finish times differently.

## Measuring start and finish

A client may measure however it likes, but its `startAt` and `finishAt` must be within one frame of when the start
and finish frames ran, on its server clock estimate.

- **o2** reads the SNES over a device with a variable latency of tens of milliseconds. It collects
  `(nmiFrames, serverNow)` pairs from every read. A read only ever returns later than the frame it saw, so o2 fits the
  line `serverTime = offset + frame × period` along the lower edge of the samples (taking the minimum of
  `serverNow - frame × period` over a sliding window, with `period` fitted over the run). Then
  `startAt = offset + startFrame × period`, and the same for `finishAt`.
- **alttpo** runs inside the emulator. It reads `latches` in its `post_frame()` callback and stamps `server_now()` as
  soon as a latch bit appears, minus one frame period for each frame the latched value is behind `nmiFrames`.

## Discontinuities (both)

The counters are part of SRAM, so loading an emulator savestate rewinds them, and replacing the SRAM file changes
them. A client classifies each new counter sample by comparing the frames since the last sample with the wall time
since the last sample:

| Frames vs. wall time | Likely cause | Flag bit |
|---|---|---|
| within ±1% of 60.0988 per second | normal play | |
| counter went backwards | savestate load, SRAM replaced | 2, and 4 if the client knows a state was loaded |
| more frames than wall time allows | fast-forward, forward savestate | 3 (more than 1% fast), else 2 |
| fewer frames than wall time allows | emulator paused or slowed down, console off | 3 if the client is still connected, else none |

The player's time is always wall time; these flags only mark it. Fast-forward is the only one of these that can make
wall time shorter, so a player who ran more than 1% fast over the run has the wall time they saved added as
`penaltyMs`: `(frames run fast × period) - (wall time of those samples)`.

## Run timing message 0x84 (both)

Sent by every player every 500 ms (with the echo), immediately when a latch is accepted, and when a reset is
finalized. alttpo stops parsing a packet at a message type it doesn't know, so senders put 0x84 in its own packet
until all supported clients know it.

| Field | Type | Meaning |
|---|---|---|
| version | u8 | 1 |
| size | u8 | the number of bytes after this field (72 for version 1); receivers skip messages of versions they don't know by it, and ignore bytes past the fields they know |
| flags | u8 | bit 0 started, bit 1 finished, bit 2 counter discontinuity, bit 3 rate out of tolerance, bit 4 savestate load detected, bit 5 no NMI counter in this ROM |
| resetID | u64 | id of the last group reset the player finalized (0x81), 0 if none |
| resetInitiator | u16 | its initiator's player index |
| signature | [16]byte | the patch signature read from `$707FF0` |
| gates | u8 | the gate byte read from `$70700D` |
| gatesChanged | u8 | 1 if the gate byte differed from the current value at any time since the start |
| startAt | i64 | server clock, Unix ns; 0 if not started |
| finishAt | i64 | server clock, Unix ns; 0 if not finished |
| startFrame | u24 | |
| finishFrame | u24 | |
| nmiFrames | u24 | latest sample |
| loopFrames | u24 | latest sample |
| sampleAt | i64 | server clock, Unix ns, of the latest sample |
| lagFrames | u24 | lag frames since the start (to the finish once finished) |
| penaltyMs | u32 | see [Discontinuities](#discontinuities-both) |

o2 keeps sending its old o2-only 0x80 run times for one release, so that older o2 clients still show a timer.

## Group reset (both)

Run timing follows the group reset (0x81, see `reset.go`). When a player clears its state it also clears its run
timing and writes 0 to `latches`. alttpo implements the 0x81 state machine to take part. A peer ignores 0x84 messages
whose `resetID`/`resetInitiator` differ from its own once both are past the reset.

## Verification (both)

Each client checks every other player's messages itself, using the server's timestamp on each relayed message (which
the sender can't forge, and which is always later than the event it reports):

1. **No backdating.** The first message with a `startAt` (or `finishAt`) must arrive with a server timestamp no more
   than 2 seconds after it, and the claim must not be earlier than the server timestamp minus that player's one-way
   delay. The one-way delay is bounded by the minimum of `serverTimestamp - sampleAt` over its messages.
2. **Rate.** The receiver fits frames per second from `(nmiFrames, serverTimestamp)` of the player's messages,
   ignoring the sender's own `sampleAt`. More than 1% over 60.0988 means fast-forward.
3. **Module agreement.** The player's location messages (0x01) carry its module and are timestamped by the server
   too: `startAt` must be before the first one showing module $05 or later, and `finishAt` before the first one
   showing $19.
4. **Equal cost.** Its signature and gates must equal everyone else's, and `gatesChanged` must be 0.

A player who fails a check is shown as unverified, with the reason. Receivers don't change the reported times, so
every client shows the same run time from the same messages.

## Display (both)

- Per player: player time, lag frames and the verification state.
- Run: `max(finishAt + penaltyMs) - min(startAt)` over the players who started, counting up from the earliest start
  until every started player has finished.
- A warning when the signatures or gates differ between players.
