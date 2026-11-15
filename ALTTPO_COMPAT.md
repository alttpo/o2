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
world bit cleared in the underworld), so they agree on sectors. Both send room-specific state as broadcastSector to their own sector: the star switch
state (both), and sprites, tilemaps, torches and enemies (alttpo). Everything else is sent as broadcastAll.

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
| 0x05 | WRAM syncables | yes (crystal switch `$C172`; star switch `$04BC` per sector) | yes (`$0400`, `$C172`; `$04BC` per sector) | `$C172`, `$04BC` used; others stored but unused |
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
| 0x82 | small keys (delta based) | yes (main packet) | yes, own packet | used |

Message types from `0x80` originated in o2 and are only sent in their own packets (`Game.makeO2BroadcastMessage`),
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
- **Extra reads per frame.** Sampling sprites adds two small WRAM reads per frame, and the crystal and star switch
  states one more each, bringing a normal read cycle to 12 reads, which takes two FX Pak Pro VGET commands instead of
  one.
- **Run timer:** alttpo does not report game start/finish times, so alttpo players have no times in o2's run timer.
- **Dungeon state WRAM:** o2 sends `$0400` (current dungeon supertile state), which alttpo discards.
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

## Small keys

o2 (`games/alttp/syncsmallkeys.go`) and alttpo (`SmallKeys.as`) share small keys with the same delta-based scheme
in message 0x82; alttpo no longer sends small keys as last-writer-wins entries in a 0x05 message.

- **Message format (0x82).** `session u32`, `count u8` (16), then per dungeon slot `$F37C+i`: `net i16` (cumulative
  change made by that player this session), `value u8` (current count, `$FF` if unknown), `timestamp u32` (server
  msec of the last change, 0 if none). o2 sends it in its own packet; alttpo sends it in its main packet (only when
  "Sync small keys" is enabled).
- **Receiving.** Each remote `(player, session)` is tracked; the first message only records `net` as a baseline. After
  that, the difference in `net` is added to a pending adjustment for that dungeon. Remote absolute values are only used
  once when catching up (no history this session): adopt the value of the player with the latest timestamp, else the
  highest value. alttpo only accepts changes from players on the same team.
- **Local changes.** The live count is `$7EF36F` while in that dungeon (`$040C`, with Sewers `$00` and Hyrule Castle
  `$02` sharing slot `$F37C`/`$F37D`), otherwise the slot; the slot is stale inside the dungeon. Both sample only in
  modules `$07`/`$09`/`$0B` with submodule 0.
- **Applying.** o2 applies pending adjustments with a generated read-modify-write routine that reports the value it
  saw, confirmed asynchronously. alttpo runs between frames (`on_main_alttp`), so it applies the adjustment directly
  with bus reads and writes, clamped to 0..99, and takes the written value as the new observed value.
- **Sessions.** A new session (new id, zero `net`) starts on game reset or "fix small keys" (o2), and on connect,
  disconnect or enabling "Sync small keys" (alttpo). Returning to the title/file select screens re-reads key counts
  and catches up again without starting a new session.
- **Timestamps** come from each client's estimate of the server clock (alttpo's `server_now()`, falling back to the
  local clock until a server message has been received).

## Crystal switches

o2 (`games/alttp/synccrystal.go`) and alttpo (`LocalGameState.as`, `crystal`) share the dungeon crystal switch state
`$7EC172` as a last-writer-wins value in a 0x05 message (`count 1`, `offs $C172`, `timestamp u32`, `value u16`). o2
sends and applies it when "Sync underworld" is enabled; alttpo when "Sync tilemap" is enabled.

- **Local changes.** `$C172` is overworld scratch space, so it is only sampled in the dungeon module `$07`. A change
  there is timestamped. Leaving the dungeon module (except for the menus in `$0E`) forgets the timestamp, since the
  dungeon load resets `$C172`; the first value read back is a baseline. alttpo intercepts writes instead of polling,
  so it only captures changes in module `$07` and forgets the timestamp in modules `$05`/`$06`.
- **Applying.** In module `$07`, submodule 0, with no animation (`$0112` = 0), take the value of the remote player on
  the same team, in module `$07` and the same dungeon (`$040C`) with the latest timestamp (or, on a tie with our own
  timestamp, a higher value). If it differs, write `$C172` and switch to submodule `$16` to redraw the pegs. o2 also
  plays the crystal switch sound effect, which alttpo does not.
- **Timestamps.** alttpo uses its local clock (`chrono::realtime::millisecond`), o2 its estimate of the server clock,
  both in msec truncated to 32 bits. Clock skew between alttpo players' machines can make an older change win.

## Star switches

o2 (`games/alttp/syncstar.go`) and alttpo (`StarSwitch.as`) share the star tile state `$7E04BC` of the current room as
a last-writer-wins value. Stepping on a star tile (`RoomTag_TriggerHoles`) picks a hole overlay (`$04BA`), switches to
submodule `$03` to draw it, toggles `$04BC` and swaps the star tile graphics (`ToggleStarTileGraphics`, `$00FDA7` in
US, JP and VT ROMs; unknown for EU ROMs, where alttpo does not apply it). Every room load resets the state.

- **Message.** A 0x05 message (`count 1`, `offs $04BC`, `timestamp u32`, `value u16`) in a broadcastSector packet to
  the player's own sector, sent every frame in the dungeon module. The value's low byte is `$04BC` and its high byte is
  the overlay `$04BA`, which the receiver needs to draw the same holes. Older alttpo versions skip it.
- **Local changes.** A change of `$04BC` in the dungeon module is timestamped only when it comes from stepping on a
  star tile: alttpo intercepts writes made in submodule `$03`; o2 polls during submodule 0 or `$03`. Room loads and
  any change of room (`$A0`) only record a baseline, so a freshly loaded room never overwrites the group's state.
  Leaving the dungeon module (except for the menus in `$0E`) forgets the timestamp.
- **Applying.** In module `$07`, submodule 0, with no animation (`$0112` = 0), take the state of the remote player on
  the same team, in module `$07` and in the same room with the latest timestamp (or, on a tie with our own timestamp,
  a higher value). If `$04BC` differs, do what stepping on the star tile does: set `$04BA` and `$04BC`, clear
  `$B0`/`$BA`/`$BB`, play the sound effect, switch to submodule `$03` and call `ToggleStarTileGraphics`.
- **Settings.** o2 uses "Sync underworld", alttpo "Sync tilemap" (the same settings as for crystal switches).
- **Timestamps** come from each client's estimate of the server clock, like small keys.
