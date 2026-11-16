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
state, room state (trap doors, chests, keys, key doors, switches), overworld map16 changes and underworld manipulable objects (both), and sprites,
torches and enemies (alttpo).
Everything else is sent as broadcastAll.

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
| 0x05 | WRAM syncables | yes (crystal switch `$C172`) | yes (`$C172`) | `$C172` used; others stored but unused |
| 0x06 | SRAM | yes | yes | used |
| 0x07 | tilemaps | no | no | retired; replaced by 0x17 and 0x18 |
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
| 0x17 | overworld map16 changes | yes (per sector) | yes (per sector) | used |
| 0x18 | underworld manipulable objects | yes (per sector) | yes (per sector) | used |
| 0x19 | star switch state | yes (per sector) | yes (per sector) | used |
| 0x1A | room trap doors, chest and key flags | yes (per sector) | yes (per sector) | used |
| 0x1B | room key doors and switches | yes (per sector) | yes (per sector) | used |
| 0x80 | o2: run times | n/a | yes, **own packet** | used |
| 0x81 | o2: group reset state machine | n/a | yes, **own packet** | used |
| 0x82 | small keys (delta based) | yes (main packet) | yes, own packet | used |

Message types from `0x80` originated in o2 and are only sent in their own packets (`Game.makeO2BroadcastMessage`),
never together with message types alttpo uses. alttpo stops parsing a packet at the first unknown message type (it
cannot know its length) and does not log types `>= 0x80`; older alttpo versions kept reading the following bytes as
more messages, which corrupted the rest of the packet.

## Format differences fixed in o2

- **Location (0x01):** o2 sampled Link's coordinates and the screen scroll offsets (`xoffs`, `yoffs`) only in
  submodule 0, so they froze while the screen scrolled during room transitions. alttpo draws a remote player's OAM
  sprites shifted by `remote.xoffs - local.xoffs`, so o2 players drifted across alttpo players' screens when walking
  through a doorway. o2 now samples them every frame and updates the location only when alttpo would
  (`can_sample_location() && !is_in_screen_transition()`).
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
  one. The map16 change log adds two reads in the overworld; the manipulable objects add one read plus up to two
  one-byte attribute reads and the room state one read ($0641..$06EB) in the underworld (16 reads, still two VGET commands).
- **Run timer:** alttpo does not report game start/finish times, so alttpo players have no times in o2's run timer.
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
US, JP and VT ROMs; unknown for EU ROMs, where alttpo does not apply it). Every room load clears the overlay
(`Underworld_LoadRoom`) and most clear `$04BC` too (`ResetStarTileGraphics` in `Module07_02_01_LoadNextRoom` and
`Underworld_InitializeRoomFromSpecial`), but falling through a hole (`Module07_11_02_LoadAndPrepRoom`) keeps `$04BC`
and only re-toggles the graphics. During a room transition `$A0` already holds the new room while `$04BA` still holds
the previous room's overlay.

- **Message 0x19** (`room u16`, `timestamp u32`, `value u8` (`$04BC`), `overlay u8` (`$04BA`)) in a broadcastSector
  packet to the player's own sector, sent every frame in the dungeon module once the state was sampled in that room.
  The overlay is what the receiver needs to draw the same holes; the room says which room the state belongs to.
  (Earlier versions sent the state as a 0x05 message for `$04BC` without the room. A receiver then paired a player's
  new location with its last star state from the previous room, and alttpo sampled the previous room's overlay during
  the transition, so both players moving from a room with star tiles into one without could draw the old holes there.)
- **Local changes.** A change of `$04BC` in the dungeon module is timestamped only when it comes from stepping on a
  star tile: alttpo intercepts writes made in submodule `$03`; o2 polls during submodule 0 or `$03`. Room loads and
  any change of room (`$A0`) forget the state, and a new baseline with timestamp 0 is only taken in submodule 0 once
  the room is loaded. Leaving the dungeon module (except for the menus in `$0E`) forgets it too.
- **Applying.** In module `$07`, submodule 0, with no animation (`$0112` = 0), take the state of the remote player on
  the same team, in module `$07` and in the same room, sent for that room, with the latest nonzero timestamp (or, on a
  tie with our own timestamp, a higher value). A timestamp of 0 means the player did not step on a star tile during
  this room visit, so it never wins. A player's star state is dropped whenever its location changes, so a state from an
  earlier visit to the room is never used. If `$04BC` differs, do what stepping on the star tile does: set `$04BA` and
  `$04BC`, clear `$B0`/`$BA`/`$BB`, play the sound effect, switch to submodule `$03` and call `ToggleStarTileGraphics`.
- **Settings.** o2 uses "Sync underworld", alttpo "Sync tilemap" (the same settings as for crystal switches).
- **Timestamps** come from each client's estimate of the server clock, like small keys.

## Room state: trap doors, chests, keys, key doors and switches

o2 (`games/alttp/syncroom.go`) and alttpo (`RoomState.as`) share underworld state that only lives in WRAM while the
room is loaded. The SRAM room flags (`$7EF000`) only pick it up when a player leaves the room, so players in the same
room did not see each other open trap doors, chests or key doors, and a key or chest item taken by one player could
still be taken by the other.

- **Trap doors** (`$0468`, 0 = open, 1 = closed). Room tags open or close the shutters by changing it, clearing
  `$068E`/`$0690` and switching to submodule `$05` (`OperateShutterDoors`), e.g. `RoomTag_SwitchTrigger_ToggleDoor`
  for the switches hidden under pots in most rooms that have them, the pull switch door tag or the kill room tags. Room
  loads set it to 1, an inter-room transition closes the shutters behind Link and an intra-room transition (submodule
  `$01`) opens and then closes them.
- **Chest and key flags** (`$0403`, the room's chest/key/boss flags). Opening chest i (`OpenItemChest`) sets bit i
  (chests 0-5, positions in `$06E0`). Picking up a small or big key sprite (`$E4`/`$E5`) or a bonk key (sprite `$3B`
  with `$0DC0` = 1, like the one on a Desert Palace torch) sets `$40` (key index 0) or `$20` (key index 1, also chest
  5's bit), the index being the sprite's `$0CBA`. Key sprites remove themselves as soon as their flag is set; bonk keys
  only check it when the room loads.
- **Key doors** (`$0401`, the high byte of `$0400`). Unlocking door slot k (`UnlockKeyDoor_Main`, submodule `$04`)
  sets `$80 >> (k & 7)`; bombed and dashed walls set their bits the same way. Only slots 0-3 are kept in `$7EF000`.
- **Pull switches** (`$0642`). A pulled wall switch (and Swamp Palace's water switches) sets it; the pull switch door
  (`$14`), pull switch exploding wall (`$28`) and water tags (`$18`-`$1A`) act on it once.
- **Hold switches.** The hold switch door tag (`$16`) recomputes `$0468` every frame: open while Link stands on a floor
  switch or a star tile, `$0646` is set (a Cane of Somaria block on a floor switch) or `$0642` is set (a statue on a
  floor switch), closed otherwise.
- **Message 0x1A**, broadcastSector: `room u16`, `flags u8` (`$0403`), `doorTs u32`, `door u8` (`$0468`). **Message
  0x1B** follows it: `room u16`, `doors u8` (`$0401`), `switches u8` (bit 0: pressing a hold switch, bit 1: pulled a
  switch), `pulledTs u32`. Both are last in the same packet as the star switch state (dropped whenever the sender's
  location changes); older versions stop parsing at 0x1B.
- **Sampling.** In the dungeon module while `$A0` stays the same. The baseline is taken in submodule 0 (after the room
  load), recording the time it was taken. A change of `$0468` seen in submodule 0, `$05` or `$17` (pressure plate) is
  timestamped, as is the first pull (`$0642` set, or a water tag switching to submodule `$0B`-`$0D`, since water tags
  take `$0642` back right away). A player holds a hold switch while its `$0468` is open in a hold switch room and its
  `$0646` is not `$80`. A change of room, an intra-room transition or leaving the dungeon module (except for the menus)
  forgets the state. o2 reads `$0641..$06EB` once per frame in the underworld for this (and reads one fewer
  manipulable attribute per frame to keep the read count).
- **Applying.** In submodule 0 of the dungeon module, from remote players on the same team in the same room whose
  state was sampled there:
  - Key flags `$40` (and, when chests are synced, `$20` and the chest bits) are OR'ed into `$0403`, and bonk keys of the
    new flags are removed (`$0DD0` cleared). Small chests still closed here are drawn open like `OpenItemChest` does
    (opened chest tiles in `$7E2000`, attributes `$2727` so they cannot be opened again, four VRAM stripes in `$1000`,
    `$14` = 1), without giving their items; big chests and big key locks only get the flag. Chest bits wait while a tag
    that hides the room's chests until it is solved is active (`$27`, `$29`-`$32`, `$3C`, `$3E`), and bit 3 is not
    taken in water rooms, whose tags set it to remember the water was let out.
  - A remote door bit whose slot here is a small or big key door (`$1980` type `$1C`-`$2B` except `$28`, `$2A`) that is
    still locked (attribute `$F0 + slot` at `$7F2041 + $19A0/2`) is unlocked like the game does once the key was
    used: `$068E` = that attribute index, `$0690` = 0, SFX3 `$14`, submodule `$04`. No key is used; the small key sync
    takes care of the key count. One door per update; the trap doors wait for the next one.
  - Trap doors are last writer wins among changes made after our own baseline, so only changes made while both players
    are in the room count; the winner's state is written like a room tag does (`$0468`, clear `$068E`/`$0690`, SFX3
    `$25`, submodule `$05`). Rooms with a hold switch (`$16`), push block (`$0B`-`$13`) or torch puzzle (`$33`) door tag
    are skipped, since those tags recompute `$0468` every frame.
  - Push block doors (`$0B`-`$13`) only open while Link is in the tag's quadrant. When another player is in the room,
    `$0641` says the block was pushed (it may have been moved by sync while the doors were busy, e.g. closing behind a
    player entering through them) and the doors are closed, they are opened like `RoomTag_OperateBlockDoor` does.
  - A pull made after our baseline sets `$0642` = 1 once, so the room's tag opens the door, explodes the wall or lets
    the water out here too.
  - While any remote player holds a hold switch, `$0646` is set to `$80` (a somaria block only ever sets it to 1), which
    holds the doors open here; it is cleared again once nobody does. Since a player whose `$0646` is `$80` does not
    count as holding the switch, players do not keep each other's doors open forever. A local somaria block
    recomputes `$0646` every frame and wins while it exists.
- **Settings.** o2 uses "Sync underworld" (and "Sync chests" for chest bits and `$20`), alttpo "Sync tilemap" (and
  "Sync chests").
- **Not synced.** Heart pieces in caves share the `$40`/`$20` flags but their sprites are not removed. The pulled
  lever sprite is not drawn pulled for other players. Enemies are not synced by o2, so a kill room opened by another
  player stays open even if its enemies are alive here, and keys dropped by enemies get their index in the order they
  spawn, which can differ between players in rooms with two. Moving wall tags (`$1E`/`$1F`) are not synced, since they
  also trigger on lit torches.
- **o2 only:** o2 used to send `$0400` in message 0x05 and unlock doors from it; that is replaced by message 0x1B. Door
  bits received through the SRAM room flags of the current room (another player left it) unlock key doors the same
  way.

## Overworld map16 changes

o2 (`games/alttp/syncmap16.go`) and alttpo (`Map16Changes.as`) share the game's own log of persistent map16 changes
in the current overworld area. They replace alttpo's old tilemap sync (message 0x07), which intercepted every write to
`$7E2000`/`$7F2000` and sent compressed runs of the whole changed tilemap every frame.

- **The log.** `$7E04AC` is the byte index of the next free entry, `$7EF800` a `[$A0]uint16` array of tilemap
  offsets (`$7E2000,X`) and `$7EFA00` a `[$A0]uint16` array of the map16 tiles written there. The game appends to it in
  `Overworld_MemorizeMap16Change` (`$0EDD40` US, `$04E780` JP) and `Overworld_DoMapUpdate32x32` (four entries at once),
  always before advancing `$04AC`, and resets `$04AC` on every area load. Its only use in the game is restoring the
  changes after a mirror bonk. Cut grass and dirt patches are never logged, so they are not synced.
- **Message 0x17**, broadcastSector: `area u16` (`$8A`, which is the parent screen in large areas), `count u8`,
  `start u8`, `len u8`, then `len` × (`pos u16`, `tile u16`) for log indices `[start, start+len)`. Entries are sent as
  soon as they are seen and the whole log (in chunks of 64) every 16 frames so players entering the area catch up.
- **Sampling.** Only in overworld modules `$09`/`$0B` during submodule 0, since `$8A` and `$04AC` change at different
  times during an area load. A change of area, leaving the overworld modules (except for the menus in `$0E`) or `$04AC`
  going down resets the log. o2 reads `$04AC` along with the star switch state and a 16-entry window of both arrays
  starting at the first entry it has not read yet.
- **Applying.** Each receiver remembers how much of each remote player's log it applied and only applies new entries,
  keeping the last tile for each position. In submodule 0 of the overworld module, in the same area, an entry whose
  tile `$7E2000,X` already has is skipped; otherwise it is appended to the local log (while it has room), which passes
  it on to players who enter the area later and keeps it through a mirror bonk, and drawn with
  `Overworld_DrawMap16_Persist` (`$1BC97C`) if VRAM holds it; other tiles are only written to `$7E2000` and the game
  draws them as it scrolls. Small areas (`$0712` = 0) are entirely in VRAM since the area load and the game never draws
  tiles while scrolling there, so changes are always drawn. Large areas keep a 32x32 map16 ring in VRAM around `$84`:
  the area load draws rows -7..+24 and columns -8..+23 of `$84`, and scrolling draws the next row or column (-8 or +24)
  from `$7E2000` before moving `$84`, even if it was in VRAM already, so rows and columns -7..+23 are always in VRAM and
  are drawn. (Earlier versions drew within 12 map16 rows/columns before or 15 after `$84`, which left changes in small
  areas and below or right of the screen undrawn until scrolled away and back, and could overwrite ring slots of rows
  in VRAM.) Draws stop when the VRAM stripe buffer (`$1000`) passes `$80` bytes and continue later. A local
  area load makes all remote entries apply again. o2 applies them from its update routine (a table of up to 8 entries
  per routine, reporting how many were applied); alttpo checks between frames and puts the draw calls in its patch
  buffer.
- **Menus.** A player in the item menu or a text box (`$0E`) keeps its log, keeps sending it and is still taken as
  being in its area, so players entering the area meanwhile catch up.
- **Teams.** Only players on the same team share changes; game worlds are team specific. (alttpo's old tilemap sync
  shared overworld changes across teams.)
- **Regions.** Map16 tile numbers differ between US and JP ROMs (the cut grass tile is `$0DC5` in US and `$0DBF` in
  JP), so US and JP based players in the same area would draw each other's changes with the wrong tiles. The old sync
  had the same problem.

## Underworld manipulable objects

o2 (`games/alttp/syncmanip.go`) and alttpo (`Manipulables.as`) share the state of the current room's manipulable
objects: pots, big gray rocks, hammer pegs and push blocks. The underworld has no change log like the overworld.

- **The slots.** The room loader registers up to 16 objects: `$0500` type and state (`$0000`-`$0005`/`$FFFF` push
  block states, `$1010`-`$1212` pots, `$2020`-`$2323` the quarters of a big gray rock, `$3030`-`$3333` the quarters of a
  bombable floor, `$4040` hammer pegs), `$0520` the object's offset in the room data (for push blocks the index into the
  dungeon's push block table at `$7EF940`: `room u16`, `pos u16` per block, `$18C` bytes, loaded from ROM at the
  dungeon entrance and overlapping the overworld log arrays), `$0540` the tilemap position (`$2000` set for the upper
  layer) and `$0560`/`$0580`/`$05A0`/`$05C0` the tiles drawn when the object goes away. Slots `[0, $0478/2)` are
  manipulables; the room's torches follow (synced by alttpo's torch messages).
- **What changes.** Each manipulable except bombable floors gets the tile attribute `$70+slot` in `$7F2000`. Lifting a
  pot, pounding a peg or lifting a big rock (all four quarters) calls `RoomDraw_16x16Single` (`$01D828` US, `$01D826`
  JP), which draws the replacement tiles and their attributes and leaves `$0500` alone; a pot may first replace its
  tiles with a hole or a switch it hid (`RevealPotItem`). A pushed block draws its replacement tiles, moves 16 pixels,
  then either is drawn at the new position (tiles `$0922`/`$0932`/`$0923`/`$0933`, attribute `$27`, state `$0000` once `PushBlock_Main` increments the
  `$FFFF` `PushBlock_CheckForPit` leaves),
  toggles the switch (`$0466`) under it, or falls into a pit, recording the room below (`$7EC000`) and position in
  the push block table.
- **Message 0x18**, broadcastSector, in the same packet as the star switch state (dropped, like 0x17, whenever the
  sender's location changes): `room u16`, `count u8`, then per
  slot `type u16`, `objx u16`, `pos u16`, `flags u8` (bit 0: revealed, bit 1: four replacement tiles `u16` follow).
- **Sampling.** In the dungeon module during submodule 0. A change of room, of the slot count or of any `$0520` starts
  a new baseline; leaving the dungeon module (except for the menus) forgets the room. An object counts as revealed once
  its attribute was seen as `$70+slot` and then no longer is. o2 reads `$0500..$05DF` every frame and the attributes of
  three not yet revealed objects per frame in turn; alttpo reads everything every frame.
- **Applying.** In submodule 0 of the dungeon module, same room, same team: slots are matched by `$0520` (and position
  and type, except for push blocks), since their order can differ between players. A remote revealed object whose
  attribute is still `$70+slot` here gets the remote's replacement tiles and `RoomDraw_16x16Single`, called through
  the RTL right before it (`PHK`, `PEA` return, `PEA` RTL-1, `JML`), since it ends with RTS. A remote push block that
  finished moving one step from where ours still stands untouched is moved here in one step: draw its replacement
  tiles, move it, then do what `PushBlock_CheckForPit` does at the new position (toggle `$0641` for a block on the upper layer, which the
  push block door tags `$0B`-`$13` and the chest tag `$3C` turn into opened trap doors or a revealed chest. The block door
  tags only run while Link is in their quadrant, so the shutters are also operated right away like
  `RoomTag_OperateBlockDoor` does, if no other submodule is running; pit: record it in the push block
  table; switch: set `$0466` and state `$0005`, which the hold switch door tag looks for; otherwise draw the block
  with attribute `$27` and state `$0000`). A remote block in state `$0000`, `$0005` or `$FFFF` (older versions) is done
  moving. A block is not moved onto a spot still taken by another object here (attribute `$70`-`$7F`), e.g. a hammer
  peg in Ganon's Tower room `$8B` that the remote player pounded but that is still up here; it waits for the peg. Draws stop when the
  VRAM stripe buffer passes `$68` (o2) or `$80` (alttpo) bytes.
- **Not synced.** Items hidden under pots are not spawned for other players (only the player who lifts the pot gets
  them, so keys are not duplicated), the push animation is not shown, and bombable floors are left to the room flags in
  `$0400`. The push block table itself is not sent; each player records a fall when its own copy of the block falls,
  whether it was pushed there locally or by sync.
- **Settings.** o2 uses "Sync underworld" and "Sync overworld", alttpo "Sync tilemap".
