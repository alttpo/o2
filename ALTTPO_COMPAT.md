# Compatibility with alttpo

o2 and the [alttpo](../alttpo/alttpo/) emulator scripts can join the same group on the alttp.online server and share
game state. This note records what was changed in o2 to make that work and the discrepancies that remain. alttpo was
not modified.

## Transport

| | alttpo | o2 |
|---|---|---|
| Server protocol | 02 only; silently ignores 03 | 03 for join/echo (server time); **02 for shared game state**; 03 for o2-only game state |
| Group name | 20 bytes, space padded | 20 bytes, space padded (`protocol02.MakePacket` now enforces this) |
| Player index | from protocol 02 `request_index` | from protocol 03 `joinGroup` |

The server keeps one client table per group regardless of protocol but **does not translate between protocols**: a
protocol 02 broadcast is relayed as protocol 02 and a protocol 03 broadcast as protocol 03. Because alttpo ignores
protocol 03, o2 sends all shared game state as protocol 02 `broadcast` packets. o2 understands both protocols, so
o2-to-o2 sync is unaffected. Both protocols address the same server client (same UDP socket), so indices agree.

Protocol 02 packets carry no server time; o2 keeps its server clock in sync from the protocol 03 echo (every 500ms)
and join responses only.

## Game packet header

Both: `script protocol (u8)`, `team (u8)`, `frame (u8)`, then messages of `type (u8)` + payload.

- `SerializationVersion` changed from `0x14` to `0x17` to match alttpo's `script_protocol`.
- o2 used to `panic` (stopping its game loop) on a version mismatch or malformed packet. It now ignores such packets
  and logs (rate limited), like alttpo does.

## Message types

Values match alttpo's `GameState::deserialize`.

| Type | Name | alttpo sends | o2 sends | o2 handling of received message |
|---|---|---|---|---|
| 0x01 | location | yes | yes | used |
| 0x02 | sfx | yes | no | parsed, ignored |
| 0x03/0x04 | sprites / continuation | yes | 0x03: Link's body, sword, shield and shadow | parsed, ignored |
| 0x05 | WRAM syncables | yes | yes | small keys used; others stored but unused |
| 0x06 | SRAM | yes | yes | used |
| 0x07 | tilemaps | yes | no | parsed, ignored |
| 0x08 | objects | no | no | reserved; not parsed by either |
| 0x09 | ancillae | yes | no | parsed, ignored |
| 0x0A | torches | yes | no | parsed, ignored |
| 0x0B | PvP | yes (PvP mode) | no | skipped (o2 used to panic) |
| 0x0C | player name | yes | yes | used |
| 0x0D | SM events | SMZ3 | no | skipped |
| 0x0E | SM SRAM | SMZ3 | no | skipped |
| 0x0F | SM location | SMZ3 | no | skipped |
| 0x10 | SM sprite | no | no | reserved; not parsed by alttpo |
| 0x11 | SM enemies | SMZ3 | no | skipped |
| 0x12 | enemy data | yes | no | skipped |
| 0x13 | enemy segment data | yes | no | skipped |
| 0x14 | overlord data | yes | no | skipped |
| 0x15 | unique sprite tiles | yes | yes (built from ROM) | skipped |
| 0x16 | unique sprite tiles NAK | yes | no | re-sends the requested tiles |
| 0x80 | o2: run times | n/a | yes, **protocol 03 only** | used |
| 0x81 | o2: group reset state machine | n/a | yes, **protocol 03 only** | used |

o2-only message types start at `0x80` and must only be sent in protocol 03 packets
(`Game.makeO2BroadcastMessage`). alttpo does not stop at an unknown message type: it logs it and keeps reading the
following bytes as more messages, which corrupts the rest of the packet.

## Format differences fixed in o2

- **Player name (0x0C):** o2 appended the run timer start and finish times after the 20 byte name, which alttpo would
  read as further messages. The name is now exactly 20 bytes and the run times moved to the o2-only `0x80` message.
- **SRAM (0x06):** o2 wrote two flag bytes before `start`; alttpo writes one (`in_sm`). o2 now matches alttpo and also
  ignores any SRAM data beyond its 0x500 byte shadow instead of panicking.
- **Sprites (0x03/0x04):** alttpo added unique tile indices (`uniq_absidx_0..3`) after the OAM bytes; o2 did not read
  them and would lose its place in the packet.

## Remaining discrepancies (not fixed)

- **alttpo players see only part of o2 players.** o2 sends Link's head, body and hands (tiles straight from ROM,
  which includes custom Link sprites), sword and shield, and Link's shadow. Other sprites (items in use, effects, held
  objects, followers) are not sent: their tiles only exist in VRAM, which o2 cannot read cheaply. Only sprite palettes
  5 (sword, shield) to 7 (Link) are sent. o2 also does not send sfx, ancillae, tilemaps, torches or PvP. o2 does not
  draw anyone.
- **Sword and shield tiles are rebuilt from ROM.** The game decompresses sprite sheets `$5E`/`$5F` and converts the
  current sword/shield to 4bpp in WRAM `$7E9000..$947F` (`DecompressSwordGraphics`, `DecompressShieldGraphics`); o2
  runs the same decompression and conversion on its ROM copy (verified byte for byte against the game's routines on
  US and JP ROMs) and sends them as unique tiles `$380..$3A3`, the indices alttpo reserves for them. alttpo itself
  does not send these (its code for this is disabled because it never invalidates cached tiles); o2 re-sends the
  changed tiles whenever its sword or shield level changes, which overwrites alttpo's cached copies. ROM hacks that
  load sword/shield graphics some other way will show the vanilla graphics for that level.
- **Sprite sampling is not frame-exact.** alttpo reads OAM and VRAM from the emulated PPU after the frame is done. o2
  reads Link's OAM buffer (`$0800`, `$0A20`) and graphics DMA source addresses (`$0ACC..$0AD7`) from WRAM over USB,
  which can catch the game mid-frame and occasionally send a mismatched pose for a frame. o2 locates Link's OAM entries
  from the previous read of `$0352` and skips a frame whenever they move.
- **Extra reads per frame.** Sampling sprites adds two small WRAM reads per frame, bringing a normal read cycle to 10
  reads, which takes two FX Pak Pro VGET commands instead of one.
- **Run timer:** alttpo does not report game start/finish times, so alttpo players have no times in o2's run timer.
- **Dungeon state WRAM:** o2 sends `$0400` (current dungeon supertile state), which alttpo discards. alttpo sends the
  crystal switch state `$C172`, which o2 stores but does not sync.
- **Small key timestamps:** both sync small keys as last-writer-wins `(timestamp u32, value u16)` entries for
  `$F37C..$F38B` in a 0x05 message, so the formats are compatible. alttpo timestamps come from the local wall clock
  (`chrono::realtime::millisecond`); o2's come from its estimate of the server clock. Clock skew between alttpo
  machines can misorder concurrent changes.
- **SRAM ranges and sync rules:** each project chooses which SRAM ranges to send per ROM type, and each applies its own
  rules (max value, bit OR, VT item swaps, bottles, etc.) to received data. Item sync from alttpo to o2 is covered by a
  test for one item; the rules were not compared item by item.
- **SMZ3 / Super Metroid:** o2 ignores all Super Metroid state and reports `in_sm = 0`.
- **alttpo stale-frame check:** `if (frame < this.frame && this.frame < 0xff)` does not handle wraparound, so alttpo
  drops a packet from any player (o2 or alttpo) each time that player's frame counter wraps from `$FF` to `$00`.
- **alttpo sprite size of disabled sprites:** alttpo's `Sprite::decodeOAMTableBytes` returns early when `b1 == $F0`
  without setting `size`. o2 assumes the default of 0 when parsing.
- **Group reset:** alttpo does not take part in o2's group reset. The initiating o2 player only waits for o2 players to
  acknowledge, and alttpo players keep broadcasting their state, which o2 players accept again once the reset
  finalizes. A mixed group should reset alttpo players by hand at the same time.
