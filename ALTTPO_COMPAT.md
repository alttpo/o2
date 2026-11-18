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
state, room state (trap doors, chests, keys, key doors, switches), overworld events, underworld manipulable objects
and torches, the flying floor tiles (both), and sprites and enemies (alttpo).
Everything else is sent as broadcastAll, including the overworld map16 changes, which players need before they enter
the area (see [Overworld map16 changes](#overworld-map16-changes)).

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
| 0x02 | sfx | yes | yes (own packet per sound) | played, local sounds first (see [Sound effects](#sound-effects)) |
| 0x03/0x04 | sprites / continuation | yes | 0x03: Link's body, sword, shield, shadow, sword sparkles and dash dust | head, body, shadow, sparkles and dust used to draw one remote player |
| 0x05 | WRAM syncables | yes (crystal switch `$C172`) | yes (`$C172`) | `$C172` used; others stored but unused |
| 0x06 | SRAM | yes | yes | used |
| 0x07 | tilemaps | no | no | retired; replaced by 0x17 and 0x18 |
| 0x08 | objects | no | no | reserved; not parsed by either |
| 0x09 | ancillae | yes | no | parsed, ignored |
| 0x0A | torches | yes (per sector) | yes (per sector) | used |
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
| 0x17 | overworld map16 changes | yes (to all) | yes (to all) | used |
| 0x18 | underworld manipulable objects | yes (per sector, and to all on change) | yes (per sector, and to all on change) | used |
| 0x19 | star switch state | yes (per sector) | yes (per sector) | used |
| 0x1A | room trap doors, chest and key flags | yes (per sector) | yes (per sector) | used |
| 0x1B | room key doors and switches | yes (per sector) | yes (per sector) | used |
| 0x1C | overworld entrance animations and other events | yes (per sector, own packet) | yes (per sector, own packet) | used |
| 0x1D | flying floor tiles (tile room overlord) | yes (per sector, last) | yes (per sector, last) | used |
| 0x80 | o2: run times | n/a | yes, **own packet** | used |
| 0x81 | o2: group reset state machine | n/a | yes, **own packet** | used |
| 0x82 | small keys (delta based) | yes (main packet) | yes, own packet | used |
| 0x83 | hearts (delta based) | yes (main packet) | yes, own packet (after 0x82) | used |
| 0x84 | run timing (see [RUN_TIMING.md](RUN_TIMING.md)) | planned | planned, own packet | planned |

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
- **Scroll offsets sent with the sprites:** o2 read `xoffs`/`yoffs` from WRAM, at another point of the frame than the
  copy of Link's OAM entries (see "Sprite sampling" below), so while the screen scrolled (room transitions) the
  offsets could be a frame off from the sprites they were sent with, which shifted o2's Link on other players'
  screens. The trampoline's capture routine now also stores `$E2 - $011A` and `$E8 - $011C` with the copy, and o2
  sends those. Receivers (o2 and alttpo) also keep the offsets that arrived with each 0x03 (the sender's location
  comes first in the same frame) and place the sprites with them, not with a later location packet's offsets.

## Remaining discrepancies (not fixed)

- **alttpo players see only part of o2 players.** o2 sends Link's head, body and hands (tiles straight from ROM,
  which includes custom Link sprites), sword and shield, and Link's shadow (chr `$6C` on the ground, `$28`/`$38`
  while recoiling or jumping off a ledge). It also sends up to 16 OAM entries of:
  - sword sparkles (charging and doing a spin attack, poking walls, stabbing) and dash dust;
  - projectiles: arrows, boomerang, bombs and their explosions, fire and ice rod shots and the fire rod's flame where
    it hits, sword beams and the four sparks flying apart diagonally when they hit (chrs `$D5`/`$D6`;
    `Ancilla04_BeamHit` adds `$82` to the `$53`/`$54` in its table, which o2 used to capture instead), Cane of
    Somaria blocks and blasts, and their impacts;
  - the lamp's flame (ancilla `$2F`);
  - carried and thrown objects (pots, bushes, rocks, signs) and their shadows and debris.

  The water ripples (`$D8`..`$DA`, in shallow water and swamp puddles) and tall grass (`$C8`..`$CA`) that the game
  draws under Link instead of his shadow are in Link's own OAM entries; o2 sends them without tile data like the
  shadow (it used to drop them, since they are not LinkGraphics tiles).

  Only chrs in sprite page 0 that no NMI DMA overwrites are captured (not `$00..$25`, `$30..$35`, which hold Link's,
  his items', the bird's and followers' tiles every frame), so they are sent without tile data and receivers draw
  them from their own VRAM, like alttpo does for chrs `$80` and up. The trampoline finds them by scanning the OAM
  buffer against a chr whitelist (`captureChrs`) after excluding Link's own entries, only on frames where one of
  the ancillae (`captureAncillae`), a carried sprite (state `$0A`), thrown scenery (sprite `$EC`) or thrown object
  debris (garnish `$16`) exists, and only while another player is in the same location. Matching chrs from
  enemies on screen at the same time are sent too, as alttpo does, except shadows (`$6C`, `$28`, `$38`), which
  every enemy and NPC has: they are only captured while a bomb, Cane of Somaria block or carried or thrown object
  exists, and o2 only sends the exact shadow entries those objects draw themselves (as the game draws them): a
  bomb's 8x8 halves at its X and X+8 (or `$38` at X+4 in the air) within two OAM entries of it; a bush, pot or rock's
  16x16 `$6C` at its X in the next OAM entry; a large rock's three 16x16 `$6C` with attributes `$24` at X -6, 0 and
  +6 from its center (enemy shadows use palette 4). Enemy shadows next to them are not sent. Checking for those objects costs a few hundred
  cycles per frame while someone is in the same location; the scan of the OAM buffer (up to about 5,000 cycles)
  only runs while one exists. alttpo draws remote sprites only with palettes the remote player sent, so o2 sends
  every OBJ palette its sprites use (it used to send only palettes 5-7, which made sparkles and dust black), read
  from `$7EC601..$7EC6FF` (color 0 of palette 0 is transparent and not read). Other sprites (items in use, held
  items like the lamp, followers) are not sent. o2 also does not send ancillae, tilemaps or PvP.
- **Enemy deaths are not sent as player sprites.** Enemies are synced on their own (alttpo), so every player in the
  room draws an enemy's death poof (`SpriteDeath_DrawPoof`), explosion (`SpriteModule_Explode`) or poof
  (`SpriteModule_Poof`) already. Both used to also send them along with their player's sprites, since some of their
  chrs (`$A9`, `$9B`, `$B3`, ...) are also used by the cape, dash dust and lamp flame. Both now skip those chrs within
  about 24 pixels of an enemy in sprite state `$02`, `$04` or `$06`: alttpo reads OAM after the frame and also checks
  the enemies of the frame before; o2's capture routine lists the enemies' screen positions (`$706A70`) of the same
  frame, which suffices since those routines only change the state on frames where they draw nothing.
- **No sprites in GAME OVER.** From module `$12` submodule `$06` on (alttpo's `is_game_over`), the GAME OVER animation
  and menu draw their own sprites into Link's OAM slots, which alttpo players saw as garbled tiles. o2 sends an empty
  sprite list there instead, so receivers stop drawing the player. Link's death spin (submodules `$00..$05`) is still
  sent.
- **o2 players see much less of other players than alttpo players do.** See "Drawing remote players" below.
- **Sword and shield tiles are rebuilt from ROM.** The game decompresses sprite sheets `$5E`/`$5F` and converts the
  current sword/shield to 4bpp in WRAM `$7E9000..$947F` (`DecompressSwordGraphics`, `DecompressShieldGraphics`); o2
  runs the same decompression and conversion on its ROM copy (verified byte for byte against the game's routines on
  US and JP ROMs) and sends them as unique tiles `$380..$3A3`, the indices alttpo reserves for them. alttpo itself
  does not send these (its code for this is disabled because it never invalidates cached tiles); o2 re-sends the
  changed tiles whenever its sword or shield level changes, which overwrites alttpo's cached copies. ROM hacks that
  load sword/shield graphics some other way will show the vanilla graphics for that level.
- **Sprite sampling is not frame-exact.** alttpo reads OAM and VRAM from the emulated PPU after the frame is done. o2
  can't read WRAM at a chosen point in the frame, and reading the OAM buffer (`$0800`) directly races with the game's
  `ClearOAMBuffer` at the start of every frame, which sets every entry's Y to `$F0` and made o2's Link disappear for
  alttpo players. Instead, the patched ROM's frame trampoline copies `$0352`, Link's 12 OAM entries (`$0800`), their
  extended attributes (`$0A20`) and the graphics DMA source addresses (`$0AC0..$0AD7`) to SRAM (`$706140`) right after
  `Module_MainRouting` returns, and o2 reads that copy. A read can still overlap the copy being written, which at worst
  mixes two consecutive frames' entries for one frame.
- **Extra reads per frame.** Sampling sprites adds one small SRAM read per frame, and the crystal and star switch
  states one more each, bringing a normal read cycle to 11 reads, which takes two FX Pak Pro VGET commands instead of
  one. The 16-byte patch signature (`$707FF0`, see `games/alttp/signature.go`) is read with the SRAM reads and every
  8th WRAM read cycle, and every cycle until it matches. The map16 change log adds two reads in the overworld, and the current area's flags (`$7EF280+$8A`) one more (and the sprite tables `$0D80..$0E2F` one more in the lumberjacks' area `$02`, for the overworld events); the manipulable objects add one read plus up to two
  one-byte attribute reads and the room state one read ($0641..$06EB) in the underworld (16 reads, still two VGET commands).
  The overlord tables (`$0B00..$0B37`) are read with the SRAM reads every 8 frames in the underworld, and with every
  read cycle only while the room has a tile room overlord (17 reads there, three VGET commands).
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

## Sound effects

o2 (`games/alttp/sfx.go`) and alttpo (`pre_nmi.as`, `GameState::play_sfx`) play the sound effects of players in the
same location, panned to where they are relative to the local player.

- **The game** queues sound effects for the APU in `$012E` (SFX2) and `$012F` (SFX3) during the frame; NMI writes them
  to the APU ports and clears them. Bits 6-7 are the pan (`$40` right, `$80` left).
- **Message 0x02** (`sfx2 u8`, `sfx3 u8`), broadcastAll. alttpo reads both right before NMI and sends them in its
  main packet every frame. o2 can't read them at that point, so the frame trampoline appends the values of every
  frame that queued one to a ring of 8 entries right after the module ran, in the copy o2 reads every frame (no extra
  read), and o2 sends each in its own packet (alttpo keeps only the last 0x02 of a packet). Neither sends the low
  life warning beep (SFX2 `$2B`) or menu and map sounds (SFX3 `$0C`, `$10`-`$12`, `$20`, `$24`), nor anything while
  the player is in a bad module.
- **Playing.** A remote player's sound is played while that player is in the same location, panned like alttpo's
  `adjust_sfx_pan` (the sender's pan places it 40 pixels left or right of the sender; it is panned left or right
  when that is 40 pixels or more from the local player). **Local sounds come first:** a remote sound is only written
  to `$012E`/`$012F` when the game left that slot empty this frame, and waits otherwise (a new sound from the same
  player replaces it). alttpo does this right before NMI and drops sounds that waited 16 frames. o2 queues them in a
  ring of 4 entries in SRAM (`$7062F4`) that the trampoline plays after the module ran, one entry per frame, each
  slot only if empty; o2 drops sounds that waited 16 frames before they were queued, and the trampoline drops the
  queue outside the modules o2's update routines run in. Remote sounds are queued after the local ones are captured,
  so they are not sent back out.

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

## Hearts

o2 (`games/alttp/synchearts.go`) and alttpo (`Hearts.as`) share heart containers and heart pieces as deltas in message
0x83, like small keys. They used to take the highest value of `$7EF36C` (health capacity) and `$7EF36B` (heart pieces),
so two players collecting different heart pieces at about the same time ended up with only one of them.

- **Message format (0x83).** `session u32`, `net i16` (heart pieces collected by that player this session, a heart
  container counting as four), `value u8` (current count, `$FF` if unknown). o2 sends it in its own packet after 0x82;
  alttpo sends it in its main packet after 0x82 (only when "Sync hearts" is enabled).
- **The count** is `($7EF36C & $F8) / 2 + ($7EF36B & 3)`, at most 83 (20 hearts and 3 pieces).
- **Local changes.** Sampled in modules `$07`/`$09`/`$0B` with submodule 0 while Link is not frozen (`$02E4` = 0).
  The count is a high-water mark: collecting the 4th piece sets `$7EF36B` to 0 right away, but `$7EF36C` only goes
  up by 8 once Link stops holding up the heart container, so decreases are ignored and only counts above the mark are
  local changes. Sync writes raise the mark by the amount they added.
- **Receiving.** Each remote `(player, session)` is tracked; the first message only records `net` as a baseline. After
  that, increases of `net` from players on the same team are added to a pending amount. A player with no history this
  session (new session, or back from the title/file select screens) catches up once by adopting the highest remote
  value.
- **Applying.** o2 adds the pending amount with a generated routine that reports the count it saw (confirmed
  asynchronously); alttpo adds it between frames. Both clamp to 83 and leave the current health alone, like before.
- **Not handled.** Two players collecting the *same* heart piece in the same moment both count it. The heart piece
  removal below makes that unlikely.

## Overworld heart pieces

Picking up a heart piece (`$EB`) or heart container (`$EA`) on the overworld sets bit `$40` of the area's flags
`$7EF280+$8A`. `SpritePrep_HeartContainer` checks it only when the area loads, so another player in the area kept seeing
the heart piece (and could collect it again) until they left and came back.

- **Sending.** Both send the overworld flags `$280..$33F` as soon as they change, besides every 32 frames. o2 reads
  the current area's flags every frame for this; the rest only come with its SRAM reads every 8 frames.
- **Applying.** alttpo applies received overworld flags right away instead of every 32 frames. Once bit `$40` of the
  current area's flags is set, the area's `$EA`/`$EB` sprites are removed (`$0DD0` = 0): o2 does this in the update
  routine that sets the bit, when the local player is in that area (checking `$1B` and `$8A` when it runs); alttpo
  checks every frame in modules `$09`/`$0B`.
- **Settings.** "Sync overworld" in both.

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
  only check it when the room loads. Heart pieces (`$EB`) and heart containers (`$EA`) on the floor set the same bits
  (`HeartUpgrade_SetObtainedFlag`), the index being bit 0 of their X high byte `$0D30`, and only check them when they
  spawn. A boss's heart container (`$EA` with `$0D90` != 0) sets `$80` instead, which also makes the boss room drop its
  prize (`RoomTag_GetHeartForPrize`) and keeps the boss from coming back.
- **Key doors** (`$0401`, the high byte of `$0400`). Unlocking door slot k (`UnlockKeyDoor_Main`, submodule `$04`)
  sets `$80 >> (k & 7)`; bombed and dashed walls set their bits the same way. Only slots 0-3 are kept in `$7EF000`.
- **Pull switches** (`$0642`). A pulled wall switch (and Swamp Palace's water switches) sets it; the pull switch door
  (`$14`), pull switch exploding wall (`$28`) and water tags (`$18`-`$1A`) act on it once.
- **Hold switches.** The hold switch door tag (`$16`) recomputes `$0468` every frame: open while Link stands on a floor
  switch or a star tile, `$0646` is set (a Cane of Somaria block on a floor switch) or `$0642` is set (a statue on a
  floor switch), closed otherwise.
- **Message 0x1A**, broadcastSector: `room u16`, `flags u8` (`$0403`), `doorTs u32`, `door u8` (`$0468`). **Message
  0x1B** follows it: `room u16`, `doors u8` (`$0401`), `switches u8` (bit 0: pressing a hold switch, bit 1: pulled a
  switch, bits 2/3: it was pulled in the east/south quadrant), `pulledTs u32`. Both are last in the same packet as the star switch state (dropped whenever the sender's
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
    new flags are removed (`$0DD0` cleared), and so are heart pieces and heart containers of the new flags. Small chests still closed here are drawn open like `OpenItemChest` does
    (opened chest tiles in `$7E2000`, attributes `$2727` so they cannot be opened again, four VRAM stripes in `$1000`,
    `$14` = 1), without giving their items; big chests and big key locks only get the flag. Chest bits wait while a tag
    that hides the room's chests until it is solved is active (`$27`, `$29`-`$32`, `$3C`, `$3E`), and bit 3 is not
    taken in water gate rooms (`$1A`/`$1B`), whose tags check it to know whether the gate was opened. The fill and drain
    tags (`$18`/`$19`) set it too but only act on `$0642`; sharing it there makes the room load filled or drained
    (`RoomDraw_WaterOverlay*`, `RoomDraw_EmptyWaterFace`) when another player enters it next.
  - The boss flag `$80` is taken only while the boss's heart container lies on the floor here (o2 reads the sprite
    tables `$0D90..$0E2F` for this while a remote player in the room has the flag and we do not), and that heart
    container is removed. A boss still alive here keeps its fight; once it is beaten here, its heart container is
    removed as soon as it drops, so nobody collects the same heart container twice.
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
    An intra-room transition through the trap door clears `$0641` (`UnderworldTransition_Intraroom_TriggerShutters`
    calls `ResetThenCacheRoomEntryProperties`) and closes the shutters, while the block stays pushed and cannot be
    pushed again, which locks Link in (e.g. Ganon's Tower room `$8B` after another player pushed the block from the
    other quadrant). When `$0641` was odd in the room before the transition and is 0 after it, both set it back to 1
    and open the doors the same way, also when no other player is in the room.
  - A pull made after our baseline sets `$0642` = 1 once, so the room's tag opens the door, explodes the wall or lets
    the water out here too.
    In water rooms (`$18`-`$1A`) only while Link is in the quadrant the switch was pulled in (not where that player is now), which the game's
    flood/drain routines assume: `Underworld_FloodSwampWater_CoverFloor` fills until `$068A` reaches
    `$0688 - $E8 - $24`, which with the camera in another quadrant (`$E8` 256 pixels off) is never reached, hanging the
    game in submodule `$0C` (e.g. Swamp Palace room `$37` with one player south of the switch). The pull stays pending
    until Link walks into that quadrant. A player who stays in another quadrant gets the water bit (see above) and sees the room filled
    or drained once it reloads; the water is drawn while the room loads, so it cannot be shown live from there.
  - While any remote player holds a hold switch, `$0646` is set to `$80` (a somaria block only ever sets it to 1), which
    holds the doors open here; it is cleared again once nobody does. Since a player whose `$0646` is `$80` does not
    count as holding the switch, players do not keep each other's doors open forever. A local somaria block
    recomputes `$0646` every frame and wins while it exists.
- **Settings.** o2 uses "Sync underworld" (and "Sync chests" for chest bits and `$20`), alttpo "Sync tilemap" (and
  "Sync chests").
- **Not synced.** The pulled
  lever sprite is not drawn pulled for other players. Enemies are not synced by o2, so a kill room opened by another
  player stays open even if its enemies are alive here, and keys dropped by enemies get their index in the order they
  spawn, which can differ between players in rooms with two. Moving wall tags (`$1E`/`$1F`) are not synced, since they
  also trigger on lit torches, which are synced on their own (see [Torches](#torches)).
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
- **Message 0x17**, broadcastAll: `location u24`, `count u8`, `start u8`, `len u8`, then `len` × (`pos u16`,
  `tile u16`) for log indices `[start, start+len)`. Entries are sent as soon as they are seen and the whole log (in
  chunks of 64) every 32 frames. It used to be sent as broadcastSector every 16 frames, so a player only received an
  area's changes after entering it (the sector only switches once `$8A` changes, and o2's only once the transition
  ended); now every player knows every other player's current log before entering the area. Older versions still
  send it per sector; their changes appear once the area is loaded, as before. The location is the one the log was read in,
  encoded like the location in message 0x01: bit 17 the dark world (`$0FFF & 1`), bit 16 the underworld (0) and bits
  0-15 the area (`$8A`, which is the parent screen in large areas). (Earlier versions sent `area u16` instead; they
  cannot share map16 changes with these.)
- **Same world.** The log is only read, and changes are only applied, while `$0FFF` agrees with the area's world (the
  dark world areas are `$40`-`$7F`), which it does not while a warp switches worlds. o2 reads `$0FFF` in the
  overworld instead of the underworld door table (`$1980..$19E9`), which is only used in the underworld, so the read
  count stays the same.
- **Sampling.** Only in overworld modules `$09`/`$0B` during submodule 0, since `$8A` and `$04AC` change at different
  times during an area load. Not while Link lands from a mirror or portal warp (`$5D` = `$14`): the warp switches the area and
  returns to submodule 0 but keeps the previous area's log until `LinkState_CrossingWorlds` resets it once Link lands
  (it keeps it for a mirror bonk), so the light world's Turtle Rock pegs were sent as changes of the dark world area.
  Changes are not applied then either. A change of area, leaving the overworld modules (except for the menus in `$0E`) or `$04AC`
  going down resets the log. o2 reads `$04AC` along with the star switch state and a 16-entry window of both arrays
  starting at the first entry it has not read yet.
- **While the area loads.** Every overworld area load (scroll transitions, coming out of the underworld or a special
  area before the spotlight opens, warps, the flute, the reload after a mirror bonk) decompresses the area's tilemap
  into `$7E2000` and calls `Overworld_HandleOverlaysAndBombDoors` (`$02EF29` US, `$02EC8D` JP), which applies the
  area's overlay and bombed doors. Both hook its first instructions: the remote changes for the area being loaded
  (`$8A`) are written to `$7E2000` first, so the load draws them (or the scroll transition streams them in) and they
  appear seamlessly. The changes are those of the remote players on the same team who are in the area their log is
  for. alttpo does this in a PC interceptor. o2 stages up to 64 of them (area, position, tile) in one of two SRAM
  tables (`$706302`, `$706484`) and then switches a selector byte (`$706300`) to it, and the patch replaces the
  hooked `LDA.w #$020F : LDX.b $8A` with a JSL to a routine that applies them and runs those instructions;
  randomizers already replace them with a JSL to their own routine (`HardcodedRocks`), which o2's routine jumps to
  instead. The changes are not added to the local log there (scroll transitions reset `$04AC` afterwards); that
  happens when they are applied as below, which finds the tiles already there and only memorizes them.
- **Applying.** Each receiver remembers how much of each remote player's log it applied and only applies new entries,
  keeping the last tile for each position. Only a log of exactly the local location (area and world) is applied, and
  only while its sender's current location (message 0x01) is that location too; o2's update routine checks `$8A` and
  `$0FFF` again when it runs. In submodule 0 of the overworld module, in the same area, an entry is appended to the
  local log (while it has room), which passes it on to players who enter the area later and keeps it through a mirror
  bonk. If `$7E2000,X` already has its tile (applied while the area loaded), that is all; otherwise it is drawn with
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
- **Underworld.** Manipulable objects get the same treatment; see [While the room loads](#underworld-manipulable-objects).
- **Teams.** Only players on the same team share changes; game worlds are team specific. (alttpo's old tilemap sync
  shared overworld changes across teams.)
- **Regions.** Map16 tile numbers differ between US and JP ROMs (the cut grass tile is `$0DC5` in US and `$0DBF` in
  JP), so US and JP based players in the same area would draw each other's changes with the wrong tiles. The old sync
  had the same problem.

## Overworld events

o2 (`games/alttp/syncowevents.go`, `sync.go`) and alttpo (`OverworldEvents.as`) replay overworld changes made while
another player is in the same area. The area flags (`$7EF280+area`, bit `$20` for overlays) are synced with the SRAM
messages, but the game only loads an overlay with the area, and draws these changes with `Overworld_DrawMap16_Persist`
or with sprites, which the map16 change log does not record, so players already in the area saw nothing until they
left and came back.

- **Entrance animations.** `$04C6` (1: Palace of Darkness (Kiki), 2: Skull Woods (fire rod), 3: Misery Mire, 4: Turtle
  Rock (medallions), 5: Ganon's Tower (crystals)) makes `Module09_00_PlayerControl` run `Overworld_AnimateEntrance`
  every frame until the animation clears it; it freezes Link, draws the entrance step by step and sets bit `$20` of the
  area's flags (Turtle Rock and Ganon's Tower on the first frame, Misery Mire only after its rain, about 5 seconds in).
  A receiver starts the same animation with `$04C6` = the sender's value and `$B0` = `$C8` = 0, except Skull Woods,
  which it starts by calling `FireRodShot_BecomeSkullWoodsFire` (`$099C4F` US, `$099C3B` JP) so the fire is lit too
  (it clears the receiver's ancillae 0-5, like the fire rod shot does for the sender). The medallion, Kiki and crystal
  effects that come before are not replayed.
- **Desert Palace statues.** Reading the tablet sets `$02F0`, which makes the statues (sprite `$57`) move apart until the
  area reloads (`Sprite_ResetAll` clears it); nothing is saved. A receiver sets `$02F0` too.
- **The lumberjacks' tree** (sprite `$3B` with `$0DC0` = 2, area `$02`) hops and falls apart in leaves when Link dashes
  into it (`$0D80` = 1, `$0F80` = 20), revealing the hole under it until the area reloads. A receiver does the same to its
  tree top if it still stands.
- **Message 0x1C**, broadcastSector, in its own packet: `location u24` (like message 0x17; only applied in exactly that
  location, area and world, and only while it is the sender's current location), `entrance u8` (`$04C6`), `flags u8` (bit 0:
  desert statues released, bit 1: tree top dashed into, during this area visit). Sent every frame in overworld modules
  `$09`/`$0B` (and `$0E`) while something is set and for 16 frames after, so receivers see it clear; receivers ignore
  it once not refreshed for 500ms, and drop it when the sender's location changes.
- **Sampling.** In submodule 0 of the overworld modules. The first sample in an area records whether bit `$20` of its
  flags was set (the overlay was loaded with the area); a local entrance animation (`$04C6` set) counts as shown too.
  o2 reads `$04C6` and `$02F0` with its other reads, and the sprite tables in area `$02` only.
- **Applying.** In the overworld module `$09`, submodule 0, from remote players on the same team in the same area: the
  entrance animation only if the overlay was not shown here, no animation runs and Link stands on the ground (`$5D`,
  `$4D` and `$02E4` = 0), since the animation keeps him frozen where he is; it waits until he does. The statues and the
  tree only once per area visit.
- **Overlays without animation.** When the area flags received set bit `$20` of the local player's area while it is in
  module `$09`, submodule 0, the overlay is drawn with the routine that draws it for the player who opened it:
  `Overworld_AlterWeathervane` (`$1BC21D`, Kakariko weathervane, area `$18`), `OpenGargoylesDomain` (`$1BC264`,
  Thieves' Town, area `$58`, also removing the grate sprite `$14`) and `CreatePyramidHole` (`$1BC2A7`, area `$5B`; o2
  used to call it without checking the module). Otherwise the overlay shows the next time the area loads. The
  weathervane explosion and the bird are not replayed (the bird activates the flute of whoever sees it).
- **Agahnim's barrier** (sprite `$40`, area `$1B`) sets bit `$40` of the area's flags when destroyed, like heart pieces;
  receivers in the area set its `$0DC0` to 4, which draws it destroyed and makes it harmless, like
  `SpritePrep_AgahnimsBarrier` does when the area loads.
- **Already synced.** Changes the game logs in the map16 change log are synced as map16 changes: the Turtle Rock pegs
  (area `$07`), the hammer pegs (area `$62`), gravestones, rocks and bombable walls.
- **Notifications.** o2 names the areas' overlay bits (e.g. "got Turtle Rock entrance from ...").
- **Settings.** "Sync overworld" in both.

## Underworld manipulable objects

o2 (`games/alttp/syncmanip.go`) and alttpo (`Manipulables.as`) share the state of the current room's manipulable
objects: pots, big gray rocks, hammer pegs and push blocks. The underworld has no change log like the overworld.

- **The slots.** The room loader registers up to 16 objects: `$0500` type and state (`$0000`-`$0005`/`$FFFF` push
  block states, `$1010`-`$1212` pots, `$2020`-`$2323` the quarters of a big gray rock, `$3030`-`$3333` the quarters of a
  bombable floor, `$4040` hammer pegs), `$0520` the object's offset in the room data (for push blocks the index into the
  dungeon's push block table at `$7EF940`: `room u16`, `pos u16` per block, `$18C` bytes, loaded from ROM at the
  dungeon entrance and overlapping the overworld log arrays), `$0540` the tilemap position (`$2000` set for the upper
  layer) and `$0560`/`$0580`/`$05A0`/`$05C0` the tiles drawn when the object goes away. Slots `[0, $0478/2)` are
  manipulables; the room's torches follow (see [Torches](#torches)).
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
- **Also to all players.** Both also send message 0x18 in its own broadcastAll packet whenever it changes and every 32
  frames, so players entering the room have it while the room loads (see below). The per-frame sector copy stays, since
  the item reveal below compares consecutive messages.
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
- **While the room loads.** Every room load (`Dungeon_LoadRoom`: room transitions, staircases, falling through holes,
  entering from the overworld before the spotlight opens) draws the room into the tilemaps at `$7E2000`/`$7E4000` and
  registers the manipulable objects; the tilemaps are uploaded to VRAM afterwards. Both hook the code right after the
  push blocks are drawn (`$01889A`, `REP #$20 : LDA.w $042C`, the same in US, JP and randomizer ROMs): for the room
  being loaded (`$A0`), objects remote players on the same team in that room revealed get the remote replacement tiles
  in the tilemap and in the slot, and push blocks they left in state `$0000` one step from where the block stands here
  are moved: the slot's replacement tiles where it was, the block's tiles (`$0922`/`$0932`/`$0923`/`$0933`) where it
  is now, and `$0540`. The attribute table built after the load marks every slot at its position with `$70+slot`, so
  these are recorded in a pending list (`$706A20`) and fixed in the first frame of submodule 0 in that room:
  `RoomDraw_16x16Single` for revealed objects still marked (it draws the same tiles again and sets the attributes), and
  attributes `$27` for moved blocks; for a block on the upper layer (no `$4000`) `$0641` is toggled like
  `PushBlock_CheckForPit` does and the push block door shutters operated like below. Blocks on switches (`$0005`) or in
  pits are left to the update in submodule 0, which checks the attributes. o2 stages up to 32 objects in two SRAM
  tables (`$706612`, `$706814`, selector `$706610`) for its patch's hook and does the fix-up in the frame trampoline;
  alttpo uses a PC interceptor and does the fix-up between frames. A revealed object applied there counts as revealed
  locally (o2 marks it present at the baseline), so it is passed on.
- **Items under pots.** A pot another player lifts while we are in the room reveals its item here too; see
  [Secrets under lifted objects](#secrets-under-lifted-objects).
- **Not synced.** The push animation is not shown, and bombable floors are left to the room flags in
  `$0400`. The push block table itself is not sent; each player records a fall when its own copy of the block falls,
  whether it was pushed there locally or by sync.
- **Settings.** o2 uses "Sync underworld" and "Sync overworld", alttpo "Sync tilemap".

## Flying floor tiles

o2 (`games/alttp/synctiles.go`) and alttpo (`FlyingTiles.as`) share the state of the tile room overlord, so players in
the same room see the same floor tiles fly at the same time.

- **The overlord.** Overlord `$14` (`Overlord14_TileRoom`) only ticks while its position is on screen (in its
  quadrant). 128 frames after the room loads and then every 96 frames it spawns the next of 22 tile sprites (`$94`) at
  a fixed position (`TileRoom_SpawnTile`); the tile erases its floor tile right away (`UpdateTilemapWithCommonObject`
  with object 6, `$01E7A9` US, `$01E7A7` JP and randomizers), rises and flies at Link. After the 22nd tile the overlord
  frees its slot. Its state is `$0B28+k` (tiles spawned) and the timer `$0B30+k`, counting down from 0 (first tile)
  or `$E0` to `$80`. A player entering the room or the quadrant later used to start over from the first tile.
- **Message 0x1D**, broadcastSector: `room u16`, `count u8` (tiles spawned, 22 once the overlord is done),
  `timer u8`. Both send it in the dungeon module once the room's tile room overlord was seen in this room visit, last
  in the room state packet. A receiver drops a player's state whenever that player's location changes.
- **Applying.** In submodule 0 of the dungeon module, while the local overlord is on screen, take the state of the
  remote player in the same room (and team) that is furthest along, if it is more than 8 frames further along (more
  tiles spawned, or as many and more time elapsed; the remote state is a few frames old, which must not make players
  keep taking each other's state): erase the floor tiles its overlord already lifted (at most 4 per frame, since each
  queues a VRAM upload), take its count and timer, or free the overlord when the remote one is done. The tiles already
  flying for the remote player are not spawned here.
- **Settings.** o2 uses "Sync underworld", alttpo "Sync tilemap". alttpo players with "Sync enemies" also copy the
  overlords of the player owning the room's enemies, which agrees with this.

## Uncle's sword and shield

Talking to Link's uncle in the secret passage (`Uncle_GrantEquipment`) gives the sword and shield (or, in randomizers,
the uncle's item) and sets bit 0 of `$7EF3C6`, which `SpritePrep_UncleAndPriest` only checks when the room loads. A
player already in the passage could get them a second time from their own uncle after another player received them.
When bit 0 arrives from another player (progress sync, "Sync progress" in o2), both o2 (`takeUncleGear` in
`games/alttp/sync.go`) and alttpo (`take_uncle_gear` in `SyncableItem.as`) switch an uncle that was not talked to yet
(sprite `$73` with `$0E90` = 0, `$0E80` = 1, AI state `$0D80` = 0) to AI state 2 and graphics `$0DC0` = 1: lying
there without his sword and shield, as after granting them. The telepathic Zelda follower is removed as before.

## Torches

o2 (`games/alttp/synctorches.go`) and alttpo (`torches.as`) share the lit torches of the current room and their
timers, so a torch another player lights is lit here too and goes out at the same time.

- **The torches.** The room loader registers the room's lightable torches (`RoomDraw_LightableTorch`) in the
  manipulable object slots after the manipulables: torch t is slot `$0478/2 + t`, with its tilemap position in `$0540`
  (bit 15 set while lit). `Underworld_LightTorch` (`$0333` = `$C0 + t`; `$01F3EC` US, `$01F3EA` JP) sets bit 15, draws
  the lit torch and sets its timer `$04F0+t` to `$C0`; `Underworld_ProcessTorchesAndDoors` decrements the timers every
  4 frames and extinguishes a torch when its timer reaches 0, about 13 seconds later. Torches the room loader lights
  have a timer of 0 and stay lit.
- **Doors and chests follow.** `RoomTag_TorchPuzzleDoor` (`$33`) counts the lit torches every frame and opens the trap
  doors while at least 4 are lit, closing them again as soon as fewer are (e.g. Turtle Rock room `$C7`, where 4 torches
  open the north trap door); `RoomTag_TorchPuzzleChest` (`$3E`) reveals a chest and the moving wall tags (`$1E`/`$1F`)
  move a wall the same way. With the torches and their timers shared, every player in the room gets the same result,
  including the doors closing again when the first torch goes out. The room state sync skips `$33` rooms' trap doors
  for this reason (see above).
- **Message 0x0A**, broadcastSector: `count u8`, then per torch `t u8`, `timer u8` for the torches the sender owns.
  alttpo sends it every other frame, o2 every frame in the dungeon module (first in the room state packet). A receiver
  drops a player's torches whenever that player's location changes.
- **Owners.** A player owns a torch it lit. alttpo intercepts `Underworld_LightTorch` and `Underworld_ExtinguishTorch`;
  o2 cannot, so it reads `$0540` and `$04F0..$04FF` in submodule 0 and takes a torch that turns lit without o2
  lighting it as lit by the local player. A torch stops being owned when it goes out, and both disown their torches
  when their location changes. A torch lit from a remote player belongs to that player; when that player leaves the
  room while it still burns here, it becomes ours and is sent to the others, and when that player stops sending it
  (it went out there) it burns out by itself here.
- **Applying.** In submodule 0 of the dungeon module, not in Ganon's room (`$A0` = 0), from remote players in the same
  room (o2: and team): a remote torch that is unlit here is lit like the game does (`$0333` = `$C0 + t`, `JSL
  Underworld_LightTorch`, which may switch to submodule `$0A` to brighten a dark room), one per frame since they all
  draw through the tilemap buffer at `$1100`, and its timer set to the owner's. A torch is only lit while the owner's
  timer is at least 8: one that just went out here may still be lit for its owner with a timer of 1 or 2, and lighting
  it again would only put it out a second time a few frames later. Timers of 0 and timers of torches that stay unlit
  are not written; the game would extinguish an unlit torch whose timer runs out, lowering a dark room's light level
  once more. alttpo writes the owner's timer every frame; o2 only when it differs by more than 2.
- **Settings.** o2 uses "Sync underworld"; alttpo always syncs torches.

## Secrets under lifted objects

o2 (`games/alttp/syncsecrets.go`) and alttpo (`Secrets.as`) reveal the item under a bush, rock, sign or pot that
another player lifts while the local player is in the same overworld area or room, so every player there gets one to
pick up. Nothing is sent for this; each receiver finds out from the map16 changes (message 0x17) and manipulable
objects (message 0x18) it already gets, and runs the game's own lookup, so a hidden item configured at that spot is
revealed and anything else gets the game's usual random prize.

- **What the game does.** Lifting a small overworld object (`Overworld_HandleLiftableTiles`) calls
  `Overworld_RevealSecret` (`$1BC8A4`), which looks the tilemap offset up in the area's hidden items and sets the low
  byte of `$0B9C` to the secret index (0 if none, `$FF` for holes and stairs); `Sprite_SpawnThrowableTerrain` then
  spawns the carried object (sprite `$EC`) and the secret under it: for bushes (thrown object types 2 and 3) without
  one `Overworld_SubstituteAlternateSecret` may pick a green rupee, guard, landmine, stal or bee, and in the overworld
  `Sprite_SpawnSecret` only spawns anything half of the time and picks a random prize for secret 4. Lifting a pot calls
  `RevealPotItem` (`$01E6B2` US, `$01E6B0` JP) with the pot's `$0540` position, which looks it up in the room's pot
  items and marks it taken in `$7EF580` (except keys), or replaces its replacement tiles with a hole or a switch.
- **Small objects.** The tiles that reveal secrets are the operands of `Overworld_HandleLiftableTiles`' CMPs at
  `$1BBFE7` (bush `$0036`), `$1BBFEF` (dark bush, `$072A` US, `$0727` JP), `$1BBFF7` (light rock `$020F`), `$1BBFFC`
  (dark rock `$0239`) and `$1BC001` (sign `$0101`), read from the ROM. Their thrown object types (2, 4, 1, 5, 0) are
  found like `Overworld_GetLiftableTileType` does: the tile type (`OverworldTileTypes`, operand at `$1BC04F`) of the top
  left 8x8 tile (`Map16Definitions`, operand at `$1BC041`), looked up in `LinkToss_liftable_tiles`
  (`$54 $52 $50 $FF $51 $53 $55 $56 $57`). Big rocks and rock piles are not included.
- **Live changes only.** A map16 change entry is live if it was first received while the local player had been in the
  area for 48 frames or more (changes made before the local player arrived were received before, since the log is
  sent to all players, or come in within 32 frames, when senders resend their whole log); an area (re)load clears all
  live marks. A remote pot is live if the sender's previous
  message, received in the same room while the local player was there, had it still there. Entering an area or room
  where objects were lifted earlier reveals nothing.
- **Revealing.** In the update that applies the change, before the tile changes, and only if the local tile is still a
  small object (overworld; the remote's new tile must not be one) or the pot's attribute is still `$70+slot`: save
  `$0B9C`, `$0314` and `$0FB2`; call `Overworld_RevealSecret` with X = the tilemap offset (through the RTL at
  `$1BBD79`), or `RevealPotItem` with A = the pot's position and `$06` = its attribute index (through the bank `$01`
  RTL); set `$00`/`$02` to the object's X/Y (overworld: `((pos & $7E) + $070C) << 3` and `((pos & $1F80) >> 3) +
  $0708`, like `Overworld_GetLinkMap16Coords`; underworld: `(room & $0F) << 9 + (pos & $7E) << 2` and `(room & $F0) << 5
  + (pos & $1F80) >> 4`); call `Sprite_SpawnThrowableTerrain_silently` (`$068156`) with A = the thrown object type (0
  for pots), clear the carried object sprite's `$0DD0,X` it returns (X = `$FF` if no slot was free), and restore what
  was saved. o2 marks the entries revealed once the update routine reports it ran; alttpo once it wrote the patch
  buffer code.
- **Keys.** A small key under a pot gets its key index from `$0B9B` like any spawned key, so with two keys in a room
  the receiver's index can differ from the lifter's. Key sprites remove themselves once their key flag is set, which
  the room state sync shares, so a key taken by one player disappears for the others.
- **Other ways of removing objects.** Any logged map16 change that replaces a small object reveals its secret, so
  bushes cut with the sword (`HandleItemTileAction_Overworld`, which logs the stump) count too, and pots smashed with
  the hammer like lifted ones. A small object
  the local player already removed reveals nothing.
- **Settings.** The map16 and manipulable object settings: o2 "Sync overworld" / "Sync underworld", alttpo "Sync
  tilemap".

## Drawing remote players

alttpo now shows and hides remote players with the same rules as o2 (`can_draw_remote_players` and the checks in
`pre_frame.as`): the local module and "screen still shows the sampled location" checks, players in a screen
transition or warp, sprites older than 250ms, and players without Link's head or body (their label stays). Before,
alttpo kept drawing the last sprites it received until the remote location changed, so players lingered in place
during warps, and it drew both rooms during its own screen transitions. alttpo also stops sending sprites in GAME
OVER, like o2.

alttpo draws every sprite a remote player sends, with their own graphics and palettes, on top of the emulated
picture. o2 can only use the game's own OAM buffer and VRAM, so it draws a simplified version
(`games/alttp/remotesprites.go`):

- **Only players in the same room or overworld area** (alttpo's `locations_equal`) and not in a bad module
  (alttpo's `is_it_a_bad_time`; o2 used to also require its own "in game" check, which depends on the module before
  a dialog or menu that remote players don't send, so remote players disappeared during their dialogs) are drawn, and only while the local game draws the underworld or overworld sprites: modules `$07`, `$09`,
  `$0B`, spotlight open/close (`$0F`/`$10`), boss victories (`$13`/`$16`), and dialogs and menus (`$0E`) except
  while the dungeon map, overworld map or flute menu is shown (the same condition as `Module0E_Interface`).
- **Switched off** with "Sync Sprites" in the web UI (on by default).
- **Players leaving the room or area are not drawn**, although their reported location only changes once their
  screen transition or warp finishes: not while they are in a screen transition (alttpo's `is_in_screen_transition`,
  which includes warps; a warp switches the area on its first frame), and not once their sprites stop arriving for
  250ms (alttpo sends them to the sector it is actually in, and only draws remote players from received sprites).
- **One player gets Link's head and body:** the two 16x16 sprites of message 0x03/0x04 whose unique tiles are all
  LinkGraphics tiles (`$000..$37F`). Unique tile indices are offsets into LinkGraphics, so the tiles are uploaded
  from the local ROM: the remote pose is shown with the local Link graphics and palette, never the remote player's
  custom sprite, and without items.
- **The same player's sword and shield** are drawn in their pose with the local player's sword and shield tiles,
  which the game keeps in WRAM `$7E9000..$7E947F`. o2 sends the pose as unique tiles `$380..$3A3` (offsets into that
  buffer); alttpo sends the 4bpp tiles themselves along with the sprites (once per chr and packet), and o2 finds them
  in its own copy of the local player's tiles (see "Sword and shield tiles are rebuilt from ROM" above). A sword or
  shield the local player doesn't have tiles for is not drawn, nor is an alttpo player's whose tiles differ from the
  local player's (a different sword or shield, or a custom sprite); an o2 player's different sword or shield is drawn
  as the local player's.
- **Shadows, ripples and grass under Link, sparkles, dust, projectiles and carried and thrown objects** of every drawn player with recent
  sprites (the chrs o2 captures itself, see above) are drawn from the local static and common sprite sheets with
  the local palettes, up to 14 sprites (fewer with many players: the table holds 31 sprites in all). Those the body's
  player draws with a lower OAM index than Link are drawn in front of the body, the others behind it.
- **VRAM:** the head, body, sword and shield are each 2x2 chrs (a "part") that NMI hooks upload into a VRAM slot:
  chrs that `NMI_DoUpdates` uploads itself on every frame with core updates enabled, so that overwriting one that
  nothing shows is harmless. The slots, in the order they are handed out, are the follower and NPC chrs `$20`, `$22`
  (`dma_var6/7`, used by followers, Kiki, the crystal maidens, the old mountain man, the returning smithy and Blind's
  maiden form) and `$24` (from `$7EBD40`), and Link's own item chrs `$0C` and `$09`, sword chrs `$05` and shield chrs
  `$07`. After the module ran, the trampoline scans the OAM buffer for the slots any sprite shows (8x8, or 16x16
  covering them) and hands the others out to the parts, head and body first. If those two don't both get a slot,
  the player is drawn as a blob; a sword or shield without a slot is not drawn. o2 used to upload only the head and
  body, over chrs `$20..$23` only, and drew a blob whenever a follower or NPC visibly used them. Kiki keeps setting
  `dma_var6/7` after it fled out of view on the Palace of Darkness area (`Kiki_Flee` calls `SpriteDraw_Kiki` every
  frame while the sprite is alive), which is why the slots go by what the OAM buffer shows rather than by what was
  written to `dma_var6/7`. The scan costs up to about 3,300 cycles per frame, only while a body is drawn.
- **Uploads only as needed:** a part is only uploaded when one of its sprites is on screen this frame, and only the
  rows of chrs (top, bottom) its sprites show: 64 bytes per row, at most 512 bytes for all four parts (the game's own
  uploads are up to `$9C0` bytes per frame). The late NMI hook (`$008B50`) uploads right after the game's own uploads
  of those chrs. While the game disables its own sprite uploads (`$0710` != 0: a dialog rendering text,
  `Text_Render`, room and area loads, warps), the early NMI hook (`$0089E2`) uploads instead, and skips rows that are
  still in VRAM from the last upload (the game doesn't overwrite the slots on those frames); Link's own chrs aren't
  handed out then, since the game won't upload them again before Link may show them. The body is drawn in every
  submodule (room transitions, doors, staircases, spotlights, menus).
- **Players without Link's head or body** in their sprites (Link is invisible, e.g. flickering after a warp; alttpo
  still sends Link's shadow then, o2 sends nothing) are not drawn at all, not even as a blob or a shadow, so the
  flicker doesn't alternate between Link and a blob. The "drawing N players" log line is only written once the counts
  hold for half a second.
- **Everyone else** (up to 8 players) is a single 16x16 blob (chr `$64`/`$66`, alternating every 8 frames, palette 4
  like the blob magic powder turns enemies into) at their Link coordinates, with a 16x16 shadow (chr `$6C`) 10 pixels
  below it, as `SpriteDraw_Shadow` draws under enemies.
- **OAM:** sprites go into unused OAM entries (Y = `$F0`) searched from the end of the buffer, behind all of the
  game's own sprites. Positions use alttpo's convention (OAM coordinate + `BG2 scroll - shake offset` of the sender)
  and are converted with the local scroll offsets by the game every frame.
- **Updates:** o2 writes a table of at most 255 bytes (a single FX Pak Pro VPUT, so the game never draws half of an
  update) to SRAM (`$706AC0`) when it changes and at least every 250ms. The game counts
  its TTL (60 frames) down and stops drawing remote players when it runs out, e.g. when o2 disconnects.
