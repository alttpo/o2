# Bandwidth reduction plan (o2 and alttpo)

Goal: measure what each sync feature costs between a client and o2-server, then cut it without losing functionality
or adding sync latency. Changes to shared messages must land in both o2 and alttpo (and o2-server where noted).

## Current estimate (per player, from code, not measured)

Assumes `sendPackets()` runs at 60 Hz (once per observed change of `$1A`). Slower readers scale the per-frame rows
down; "every N frames" counts observed frames, so those rows scale too.

Fixed cost per packet: the p4 envelope is 55 B (broadcastAll) / 63 B (broadcastSector), because the group is always
padded to 20 bytes. The game header adds 3 B and IPv4+UDP adds 28 B, so a broadcastAll packet costs 86 B on the wire
before any payload.

| Feature (msg) | B/pkt | Rate | Wire KiB/s |
|---|---|---|---|
| Link sprites (0x03), every frame, broadcastAll | ~188 | 60/s | 12.7 |
| Small keys + hearts (0x82+0x83), every frame | 184 | 60/s | 12.4 |
| Room sector pkt: torches/star/room/doors/tiles/kills (dungeon) | 104 | 60/s | 7.7 |
| Location (0x01), on change or every 60 frames | 84 | 60/s moving | 6.6 |
| Crystal switch (0x05), every frame, overworld too | 68 | 60/s | 5.6 |
| Enemies killed (0x1F), every 4 frames (UW: + recent room) | 72 | 30/s UW, 15/s OW | 2.9 / 1.6 |
| Underworld SRAM $000-$24F, every 32 frames | 656 | 1.9/s | 1.3 |
| Items SRAM (vanilla; VT ~2x), every 16 frames | 135 | 3.75/s | 0.6 |
| Overworld SRAM $280-$33F, every 32 frames | 256 | 1.9/s | 0.5 |
| Name + run times + run timing + echo (500 ms) | - | 8/s | 0.9 |
| Sfx, manip, map16 resend, ow events | - | bursty | ~0.5-0.8 |
| **Total upstream** | | ~352 pkt/s dungeon, ~276 overworld | **~52 KiB/s dungeon, ~43 overworld** |

Downstream per player is about (N-1) x upstream when everyone is together (sector packets only reach the same
sector). Server egress grows as N(N-1).

The Link sprites size is a guess (~5 sprites, 2 palettes).

## Step 0: measure

- [x] o2: count bytes and packets per game message type in `send()` (split a packet's bytes by message), and show
      them on the debug page. o2-server's `bandwidth.go` only counts by p4 kind, so it can't tell features apart.
      Done in `bandwidth.go`; the /debug page's "Bandwidth sent" table shows the last second's rates.
- [ ] alttpo: same counters, if cheap, to compare. Not done; `enableNetReporting` still only logs the total.

## Changes

Status: 1-6 are done in o2 (`send.go`) and alttpo (`SendState.as`, `LocalGameState.as` `send`). 7 is not started.

Found with the debug page counters: o2's `sendPackets` ran on every SNES read, not once per game frame (the frame
check in `readMainComplete` only returns from a closure), so state went out every frame regardless and the items SRAM
several times per 16 frames. It now sends once per game frame.

Since then (compact messages, see `compact.go` and alttpo's `SendState.as`): new message 0x87 says a client reads 0x88
(small key changes) and 0x89 (location changes). These are changes relative to the last full 0x82 or 0x01, sent only
while every player in the group sent 0x87; otherwise the full messages go out as before. Torches only go out in rooms
with lightable torches, the crystal switch only in dungeons (receivers forget it when a player enters one), each room
message on its own change, the items SRAM only where it changed, and alttpo's name every 30 sends.

1. **Link sprites (0x03/0x04) to broadcastSector.** alttpo already sends them with
   `make_packet_broadcast_to_sector(broadcast_sector())` (LocalGameState.as `send_sprites`). o2 sends them
   broadcastAll (`sprites.go` `sendLinkSprites`), but only draws players where `locationsEqual(local, remote)`, which
   matches `broadcastSector()`. So other sectors' copies are wasted.
   - o2 only: use `makeSectorMessage()` for the 0x03 packet.
   - Keep uniq tiles (0x15) broadcastAll, as alttpo does. They are sent once per tile, so a player who enters the
     sector later still has them (and 0x16 NAK covers gaps).
   - Saves about (N-1) x 12 KiB/s downstream per player whenever players are apart. No upstream saving.
2. **Coalesce per-frame broadcastAll messages into one packet.** Location, crystal switch, sprites (if not
   sector), small keys + hearts each go in their own packet every frame. One packet per frame saves ~3 x 86 B x 60
   = ~15 KiB/s upstream. Constraints: alttpo stops parsing at the first unknown type, so o2-only types (>= 0x80
   that alttpo doesn't know) stay last or separate; 0x82/0x83 are known to alttpo. Sector and all packets stay
   separate. alttpo should coalesce the same way.
3. **Small keys + hearts (0x82/0x83): send on change + refresh every ~32 frames** instead of every frame. They are
   delta-based state, so change latency is unchanged. Saves ~12 KiB/s. Do in both o2 and alttpo (SmallKeys.as,
   Hearts.as); receivers must not treat a missing frame as a change.
4. **Link sprites: skip unchanged frames** with a hash, like location (send on change + every ~60 frames). Send each
   palette only when it changes (+ periodic refresh) instead of 32 B per palette per frame. Check first whether
   alttpo keeps palettes between packets; it currently draws only with palettes in the packet. Needs a matching
   alttpo change (receiver keeps last palettes per player). Saves ~3-10 KiB/s.
5. **Crystal switch, room sector packet, enemies killed: send on change + periodic refresh** (e.g. every 16-32
   frames) instead of every frame / every 4 frames. Stop sending the crystal switch outside module $07. ~10x cut on
   ~16 KiB/s. alttpo to match for the messages it sends (EnemiesKilled.as, RoomState.as, StarSwitch.as, torches.as,
   FlyingTiles.as).
6. **Underworld SRAM ($000-$24F, 592 B every 32 frames): send only dirty rooms** (plus a slow full refresh for
   joiners). Same idea could apply to overworld SRAM and items. alttpo's `serialize_sram_ranges` to match.
Refresh intervals as done: location 60 frames, crystal switch 32 (128 outside the dungeon), manipulables and small
keys and hearts 32, room state and enemies killed 16 (the other recent rooms one every 16, so each every 48, under
the receivers' 2 s TTL), sprites 8 (under the 250 ms stale time of o2 and alttpo receivers), palettes 32 or at once
when a player arrives in the location. The underworld and overworld SRAM are checked every 32 frames and sent whole
every 16 checks (~8.5 s) or when a player joins the group. Sprites are hashed with the scroll offsets, since
receivers place them with the offsets of the last location received.

7. **Protocol 05 envelope (o2, alttpo, o2-server).** The p4 envelope is 53 B per packet: a 20-byte group on every
   packet (the server keys clients on address), two int64 times, a u32 player index and a u64 sector. Send the group
   only on join, shrink the times and sector. Roughly halves the per-packet cost.

Rough result of 1-5: upstream from ~50 to ~10-15 KiB/s, downstream down by the same factor or more.
