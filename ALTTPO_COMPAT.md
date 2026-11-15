# Compatibility with alttpo

o2 and the [alttpo](../alttpo/alttpo/) emulator scripts can join the same group on the alttp.online server and share
game state. This note records what was changed to make that work and the discrepancies that remain.

## Transport

Both o2 and alttpo use server protocol 04 exclusively (`client/protocol04`, alttpo's `receive.as` and
`LocalGameState::make_server_envelope`). Protocol 04 carries the same `GroupMessage` as protocol 03 but in a simple
little-endian binary encoding instead of protobuf, which AngelScript can read and write easily:

| Kind | Name | Body | Server response |
|---|---|---|---|
| 0x01 | joinGroup | (empty) | sent back to the sender with its player index |
| 0x02 | broadcastAll | `u16 len`, data | relayed to all other clients in the group |
| 0x03 | broadcastSector | `u64 targetSector`, `u16 len`, data | relayed to other clients whose `playerInSector` matches |
| 0x04 | echo | `u16 len`, data | sent back to the sender |

Every message the server sends carries the sender's `playerIndex`, the sender's own clock (`playerTime`, Unix
nanoseconds) and the server's clock (`serverTime`, Unix nanoseconds, stamped by the server). Both clients keep an
estimate of the server clock from the last message received and send an echo every 500ms to keep it fresh even when
alone in a group. alttpo stores `player_time` / `server_time` per remote player and exposes `server_now()`.

Both clients set `playerInSector` on every message to alttpo's `broadcast_sector()` value (the location, with the dark
world bit cleared in the underworld), so they agree on sectors. o2 only sends broadcastAll; alttpo sends some state
(sprites, tilemaps, torches, enemies) as broadcastSector.

Older o2 and alttpo versions used protocol 02 (alttpo) or a mix of 02 and 03 (o2). The server does not translate
between protocols and the game `script_protocol` was reset, so those versions cannot share a group with the current
ones.

## Game packet header

Both: `script protocol (u8)`, `team (u8)`, `frame (u8)`, then messages of `type (u8)` + payload.

- `SerializationVersion` and alttpo's `script_protocol` are both `0x01` (reset when moving to server protocol 04).
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
| 0x80 | o2: run times | n/a | yes, **own packet** | used |
| 0x81 | o2: group reset state machine | n/a | yes, **own packet** | used |
| 0x82 | o2: small keys (delta based) | n/a | yes, **own packet** | used |

o2-only message types start at `0x80` and must only be sent in their own packets (`Game.makeO2BroadcastMessage`),
never together with message types alttpo uses. alttpo stops parsing a packet at the first unknown message type (it
cannot know its length) and does not log types `>= 0x80`; older alttpo versions kept reading the following bytes as
more messages, which corrupted the rest of the packet.

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

## Bringing alttpo to delta small keys

To share small keys between o2 and alttpo players, alttpo would need to adopt o2's delta-based scheme:

1. **Transport.** Done: alttpo now receives o2's 0x82 packets over protocol 04 (and currently ignores them) and has the
   server clock (`server_now()`) needed for timestamps. Adding 0x82 to alttpo's `GameState::deserialize` is enough to
   receive it.
2. **Message format (0x82).** `session u32`, `count u8` (16), then per dungeon slot `$F37C+i`: `net i16` (cumulative
   change made by that player this session), `value u8` (current count, `$FF` if unknown), `timestamp u32` (server
   msec of the last change, 0 if none).
3. **Receiving.** Track each remote `(player, session)`; the first message only records `net` as a baseline. After
   that, add the difference in `net` to a pending adjustment for that dungeon. Never copy remote absolute values except
   once when catching up (no history this session): adopt the value of the player with the latest timestamp, else
   the highest value.
4. **Local changes.** Read the live count from `$7EF36F` while in that dungeon (`$040C`, with Sewers `$00` and Hyrule
   Castle `$02` sharing slot `$F37C`/`$F37D`), otherwise from the slot; the slot is stale inside the dungeon. Count only
   changes not caused by sync writes. alttpo's bus write interceptors make this simpler than in o2: apply pending
   adjustments as a read-modify-write during the frame (clamped to 0..99) and exclude those writes from capture.
5. **Resets.** Start a new session (new random id, zero `net`) on reset or game reload.
