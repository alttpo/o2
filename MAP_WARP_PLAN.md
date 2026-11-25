# Plan: debug-mode warp to a point picked on the world map

In debug mode the player clicks the game view's world map to pick a point in an underworld room or an overworld
area. A zoomed-in popover follows the cursor so the point can be placed accurately. A new button in the Warp section
sends Link there, moved to the nearest spot he can stand on and walk away from.

## Status

- **Underworld: implemented** (steps 1–4 below). See "As built" for where it differs from the original plan.
- **Overworld: not started** (steps 5–6).

## What existed before

- `games/alttp/debugwarp.go`: `debugOpWarp` with `w = "entrance" | "exit"`. It waits for module $07/$09 sub 0
  (`debugCanWarp`), saves the dungeon keys, sets the world ($7EF3CA), then starts the iris (module $0F) with $010C =
  $06 (Module_PreDungeon, entrance $010E) or $08 (LoadOverworldFromDungeon, exit of room $A0).
- `webui/web/src/games/alttpmap.tsx`: `WorldMap`, which shows the light world, dark world and underworld images
  (`/rom/lightworld.png`, `/rom/darkworld.png`, `/rom/underworld.png?scale=4`) under an SVG overlay. It already
  converts game coordinates to map coordinates (`mapCoord`, `owBlock` for the large areas).
- `games/alttp/underworldmap`: runs the game's own room loader on an emulated SNES for each room, flood-filling from
  each entrance. `readRoomData` already copies each room's **tile attributes** ($7F2000, BG1 and BG2, 64×64 cells of
  8×8 px), its doors and `collision` ($046C), and it knows which **entrance** each room was drawn from (`Room.Entrance`).
- `games/alttp/worldmap`: draws only the small Mode 7 overview (1 map pixel = 16×16 game pixels). It has no
  collision data and is too coarse for a zoomed view.
- The web server keeps one generated underworld PNG (`serveUnderworldMap`, `underworldLock`).

## Design overview

Both maps are **streamed**: the backend generates rooms/areas one by one in the background, and the page shows
each one as soon as it is drawn instead of waiting for one big image of the whole map.

```
ROM ─┬─ underworldmap.Stream ──► per room, as drawn: full-res PNG, ¼-size PNG, walk cells, entrance state
     └─ overworldmap.Stream (new) ─► per area, as drawn: full-res PNG, small PNG, walk cells
                │
  shared per-ROM stream (used by the web server AND the game for warp validation)
                │
  /rom/underworld/events (SSE: map id, then each room as it's ready, then done/failed)
  /rom/underworld/room.png?id&room[&full=1]   /rom/underworld/walk.json?id&room
  (same three for /rom/overworld/… with area=)
                │
 WorldMap (debug mode): hover → popover (zoomed image + mask shading + snapped Link box)
                        click → pins target (shared state with the Warp section)
 Warp section: "Warp to map point" → debug op {t:"warp", w:"room"|"area", …}
                │
 Go: validate the point against the mask → emit asm (two strategies below)
```

## 1. Walkability data (Go)

### 1a. Underworld masks (extend `underworldmap`)

- Keep the attributes that `readRoomData` already reads: add `Map.Walk [RoomCount]*RoomWalk`, filled in for every
  room the flood fill draws.
- Classify each 8×8 cell per layer (BG1 = attrs $0000.., BG2 = $1000..; BG2 only when `collision` is 1..3, the same
  test as `walkwayLayers`). Use an allow-list, since an unknown attribute is safest treated as blocked:
  floor $00 and the other plain floor attributes, shallow water $09 and similar. Blocked: walls, pits $20/$62, deep
  water $08, spikes, warp tiles $4B, staircases and the inter-room stair triggers, doorways $80–$8D and the edge
  walkways, conveyors and ice (they move Link), chests, blocks, pots and torches ($70+slot, $58–$5F, …). Write the
  list down in `walk.go` next to the existing `attrFloor`/`attrPit` constants and look each one up in zelda3's
  `player.c` tile handlers.
- **Standable** cell: Link's collision box placed at that cell (16×16 px, anchored like $20/$22, with the collision
  rows at y+8..y+15) covers only walkable cells, plus a margin of one 8×8 cell, and stays ≥ 16 px from the room's
  outer edge so the warp can't trigger a room transition.
- **Reachable**: flood-fill the walkable cells (4-neighbourhood, per layer, with a layer change at the layer-switch
  attributes). Seed it from the room's ways in: the cells next to edge doorways and walkways, staircase landings,
  the entrance's start position when this room is an entrance room, and pit landing spots (rooms that are the pit
  destination of another room). Components with a seed are reachable. The rest are walkable but isolated (for
  example on top of a wall or behind a closed shutter).
- Mask byte per cell: `0` blocked, `1` standable but isolated, `2` standable and reachable (bit 7 is set on BG2
  cells). 64×64×2 layers = 8 KiB per room before compression.

### 1b. Overworld images and masks (new package `games/alttp/overworldmap`)

- Reuse the underworld emulator: move the generic parts of `underworldmap/emulator.go` (System, WRAM tracking,
  `call`/`runUntil`, scratch routines, decompress cache) into a shared package such as `games/alttp/romemu`, so that
  `underworldmap` and `overworldmap` both use it. `underworldmap` keeps its room-specific routines.
- For each parent area (owBlock parents of $00–$3F and $40–$7F), set the area/world and run the game's area load
  (the routines Module08 / Overworld_LoadAndBuildScreen run: they decompress map32→map16 into $7E2000, load the
  graphics, palettes and BG tilemaps). Then:
  - render the area at full resolution from VRAM + CGRAM + the tilemap (reuse `underworldmap/render.go`'s 4bpp
    renderer once it is shared);
  - compute the attributes per 8×8 cell from the map16 tilemap → Map16 definitions → the overworld tile-attribute
    table (the same lookup as `Overworld_GetTileAttrAtLocation`), and classify them with an overworld allow-list
    (ground $00, grass $40, diggable ground $48, shallow water $09, …; blocked: walls, cliffs, ledges $1C–$2F,
    deep water $08, holes $20, bushes/rocks/signs $50–$57, doors and entrances, warp tiles).
- Standable/reachable as above. The margin to the area's outer edge is 16 px. Seed reachability from the walkable
  edge openings and the exit-table positions (`UnderworldExitData` Link X/Y) in that area.
- Base state only: overlays that depend on events (bombed walls, the pyramid hole, the opened Thieves' Town
  entrance) aren't applied. That's acceptable for best effort; note it in the UI.

### 1c. Streaming and serving

- `underworldmap.Shared(rom)` returns the `Stream` of the last ROM asked for (one per process), starting its
  generation in the background. `Generate` takes `Options.OnRoom`, called from the draw goroutines with each room's
  full-size image as soon as it is drawn; the stream encodes it to a full-size and a ¼-size PNG (`png.BestSpeed`)
  and keeps the room's walk cells and entrance state. Full-size raw images are never kept (the whole map at full
  size is 318 MB of RGBA).
- The web server and the game share the stream: the game gets the unpatched ROM through a new optional
  `games.UnpatchedROMUser` interface that the engine calls after creating the game, and the web server through the
  existing `rom unpatched` command. Same bytes → same stream.
- Endpoints:
  - `GET /rom/underworld/events`: server-sent events. `map` (the stream's id, a CRC of the ROM) first, then
    `room` with each room's number as it is drawn (and all those drawn before), then `done`, or `failed` with the
    error. The page closes the `EventSource` on `done`/`failed` so it doesn't reconnect.
  - `GET /rom/underworld/room.png?id=…&room=…[&full=1]`: the ¼-size image (map tiles) or full size (the lens).
    Cacheable, as the id is in the URL. Only the stream the last `events` request started is served; another id
    gets 404.
  - `GET /rom/underworld/walk.json?id=…&room=…`: `{room, entrance, layers, cells}` with the cell flags as base64.
- The old `/rom/underworld.png` (one image of the whole map, waited for in full) is gone.
- The overworld does the same with `/rom/overworld/events`, `area.png`, `walk.json`, so its areas also appear one by
  one. The small light/dark world overview images can stay as the background until the areas arrive.

## 2. Picking the point (web UI)

- **Shared state**: lift a `warpTarget` state into `alttp.tsx` (the parent of both `DebugSections` and `WorldMap`)
  and pass it plus its setter to both. `WarpTarget = {kind: "room", room, x, y, layer} | {kind: "area", area, x, y}`
  with x/y in the room's/area's own pixels (room 0..511, area 0..511 or 0..1023 for large areas).
- **Streamed tiles**: the underworld screen is a grid of `<img>`s, one per room as its `room` event arrives
  (batched every 100 ms), placed by the room's row and column, with a small "Drawing the rooms… N" note until
  `done`. The overworld screen does the same per area.
- **WorldMap in debug mode** (`game.debugMode`; no handlers otherwise):
  - `pointermove` over the frame: map units → screen + room/area + local pixel. For the underworld: room =
    row*16 + col, local = coord mod 512, skipping the empty cells of the last row. For the overworld, invert
    `mapCoord` (×16, minus the offsets) to world pixels, then `owBlock` → parent area, and local = world − the
    parent's origin.
  - On entering a new room/area, fetch its full-res PNG and mask (memoized per ROM key; abort stale fetches).
  - **Popover**: an absolutely positioned `<canvas>` (~220×220 CSS px) next to the cursor, flipped to the other side
    near the frame edges. It draws a 128×128 game-px window of the full-res image around the cursor at 2× with
    `imageSmoothingEnabled = false`, shades the mask (red = blocked, amber = isolated, none = reachable), and draws a
    crosshair at the cursor and a 16×16 green box where Link would be placed after snapping. A caption gives the
    room/area id, the cursor's x/y and "→ snapped x/y (layer)". Hide the popover on `pointerleave`.
  - **Snap** (client side, for the instant preview): a BFS over the mask from the cursor's cell to the nearest cell
    with value 2 on either layer (on a tie prefer the cursor's layer, then BG1). With none within the room/area,
    fall back to value 1 and show a warning colour. With no standable cell at all, show "no safe spot".
  - `click`: set `warpTarget` to the snapped point. Draw a pin (ring + Link box) in the SVG overlay while it's on the
    displayed screen. `Escape` or right-click clears it.
  - While a target is pinned and Auto is on, the map may switch screens when Link moves. Keep showing the target's
    screen: picking a point turns Auto off, the same as choosing a screen by hand.
- **Warp section** (`debugsections.tsx` `Warp`): add a row below the existing selects that says
  "Map point: Underworld room $0A8 at ($0123, $01C0), BG1" or "Pick a point on the map", with a
  **Warp to map point** button (disabled unless `ready && warpTarget`) and a small "clear" link. It sends
  `{t:"warp", w:"room", r, x, y, l}` or `{t:"warp", w:"area", a, x, y}`. Extend `DebugOp` in `debuggfx.tsx`.

## 3. The warp itself (Go + asm)

### Op and validation (`debug.go`)

- `debugOp` gains `R uint16`, `X`, `Y uint16` and `L uint8` (the area goes in `V`, or a new `Area` field for
  clarity). `validate()` adds `w = "room"` (room < $128, x/y < 512) and `w = "area"` (parent area, x/y inside the
  area's size).
- In `Execute`, or when the op is emitted, look the point up in the mask and **reject** it unless it is standable
  (value ≥ 1). The client already snapped it, so the server only checks it and doesn't run a second nearest-point
  search. Log the rejection.

### Overworld target: no ROM patch, via the cached-exit path

`LoadOverworldFromDungeon` (module $08) takes the exit for rooms $0100–$017F (except $0104) from the WRAM cache
that `Dungeon_LoadEntrance` fills at $7EC140.. (`LoadCachedEntranceProperties`). The debug op writes that cache and
takes the same iris as the existing `exit` warp:

1. Same preamble as `emitDebugWarp`: wait for module $07/$09 sub 0, save the dungeon keys, set the world ($7EF3CA =
   area & $40).
2. Write the cache: area/screen index ($7EC140, $7EC14C), TM ($7EC142), BG2 scroll ($7EC144/46), Link Y/X
   ($7EC148/4A), map16_load_src_off ($7EC14E), camera scroll lows ($7EC150..), ow_scroll_vars0, the scroll
   targets, unk1/unk3, $0AA0 and the tile themes, all computed in Go by a new `overworldExitState(area, x, y)`.
3. $A0 = a house room ≥ $0124 so that `LoadCachedEntranceProperties` doesn't subtract $10 from Y. Check
   `PreOverworld_LoadProperties`' other uses of `dungeon_room_index` (songs, animated tiles) to choose one with no
   side effects. Set $0696 (ow_entrance_value) ≠ $FFFF.
4. $010C = $08, module $0F, the same as today.

`overworldExitState` has a free oracle: the 79 entries of `UnderworldExitData` (scroll Y/X, Link Y/X, map16 load
offset, camera scroll, unk1/unk3, screen index). A table test must reproduce every entry from (area, Link X, Link
Y) on the US and JP ROMs before the warp is wired up.

### Underworld target: two stages, no ROM patch

The room's graphics depend on the entrance (the dungeon's blockset $0AA1 and palace $040C), so:

1. **Stage 1 – get into the right dungeon.** Unless Link already stands in a room whose palace and blockset match
   the target's home entrance (`Room.Entrance` from the flood fill, read from the ROM's entrance tables), queue the
   existing `entrance` warp to that entrance. Then queue stage 2, which waits like every warp does for module $07
   sub 0. Raise its TTL past `debugOpTTL`, since the iris takes ~1–2 s.
2. **Stage 2 – drop into the target room like a pit fall.** Do what the pit-fall code does (zelda3 `player.c`
   ~1526–1550, before `Module07_07_FallingTransition`), with our room and position: save `dungeon_room_index_prev`,
   set $A0/$A1 = room, `tiledetect_which_y_pos[0]` ($51) = the target Y (landing), Link Y = above it the way the
   game does it (`y - screenOffset - $10`), Link X = target X, $EE/$0476 = the layer, the quadrant vars
   ($A9/$AA, $A6/$A7) and BG scrolls for the target's quadrant, then submodule $07, subsubmodule 0, and the
   falling Link state (visibility, `link_this_controls_sprite_oam`, `player_near_pit_state`). The transition
   (`Dungeon_InitializeRoomFromSpecial`, `Dungeon_AdjustForRoomLayout`, `HandleDungeonLandingFromPit`) then loads
   the room and lands Link on the target.
   - The fall decrements `dung_cur_floor`. Set $A4 to (target floor + 1) beforehand, with the floor from a new
     `Room.Floor` that the flood fill tracks (entrance floor, −1 per drop, ±1 per staircase). Where it can't tell,
     leave the floor as is (it's only the HUD/dungeon-map floor).
   - Special rooms in the fall code ($89, $4F, $A7, and $10/$07/$17 music) only change music and floor text, so
     they're fine for debug.

**Fallback if stage 2 proves fragile:** a new patch hook in `Module_PreDungeon` right after `JSR
Dungeon_LoadEntrance`. It checks a mailbox in o2's SRAM area and overrides the room, position, scroll, camera,
bounds and quadrant vars, giving a single iris and no fall. That needs those values computed in Go (oracle: the
$85 entrance table entries), a `patchFormat` bump and a free-ROM routine. More work, which is why it isn't first
choice.

## As built (underworld)

- Walk cells (`underworldmap/walk.go`): a flag byte per 8×8 cell per layer: `Standable`, `Reachable`,
  `Passable`, `Hazard`. The anchor convention: with Link at x = col·8, y = row·8 − 8, his feet (16×16) start at the
  cell. The attribute classes come from zelda3's `TileDetect_ExecuteInner`.
- Reachability seeds: exit tiles, door positions and the entrance's start position. 12 vanilla rooms (boss rooms
  and other drop-only rooms) have no reachable cell; the page then falls back to any standable cell and says
  "maybe cut off".
- Which layer Link is on: the game reads BG2's attributes when Link is on the lower level ($EE). In rooms with
  collision on both layers ($046C 1–3) either layer is valid anywhere except where it has $1C. In the others,
  **$1C in BG1 marks where Link is on the lower level (BG2)**, and BG1 is valid everywhere else. That covers rooms
  that are all lower level ($002, $077), all upper level (BG2 all $1C), and rooms with both, like $015: upper
  corridors with the doorways on BG1, lower area on BG2. `Walk.Layers` is a bitmask of the layers with standable
  cells.
- One flood over both layers. It crosses layers at the swap-layer stairs ($1E, $1F, $3D–$3F), and in one-layer rooms
  off ledges ($28–$2F, one way down). Door and exit seeds cover 4 cells, since a door's position is in the wall.
  Because a position belongs to one layer only in one-layer rooms, layer-toggle doors between rooms need no special
  handling: the cells next to the door decide the layer.
- The lower level ($0476) is tracked apart from the collision layer ($EE). They differ in rooms whose in-room
  staircases only switch $0476 (`kind_of_in_room_staircase` 2, Swamp Palace's water rooms like $034: the water
  channel is lower, the walkway around it upper, both with BG1's collision). The spiral and straight staircases to
  other rooms place Link by both (`byte_7E0492`), so a wrong $0476 left Link in a wall after the next staircase.
  The walk splits the room into regions separated by in-room staircases, each switching the level. It takes the
  level from where entrances put Link (their recorded $0476), then from the staircases from other rooms (the other
  room's staircase plane, read from its header via `RoomHeaderPointers`), then from the doors (the layer's level).
  Cells get `WalkLower`, the warp writes $0476 from it, and the page shows "BG1 lower" / "BG2 upper" where the
  level isn't the layer's.
- The hookshot: from a standable cell, up to 16 cells in each direction, to a tile it grabs ($27, liftables, chests,
  crystal pegs, pots and blocks $70–$7F, torches). It flies over gaps but stops at walls and stairs, except between
  two ledges it crosses, where it flies over everything (`ancilla_L`). In Swamp's $036 this reaches the side islands
  with the pots.
- The page snaps to the nearest standable point, preferring a reachable one if it is at most 24 px farther, so a
  cut-off area the flood doesn't know a way into can still be picked (shown as "maybe cut off").
- The server only checks the point is standable
  (`prepareRoomWarp`) and rejects it otherwise. No nearest-point search runs on the server.
- The generator also records each entrance's blockset ($0AA1), palace ($040C), floor ($A4) and start, and tracks
  each room's floor through drops (−1) and staircases (attribute bit 2: down).
- The warp op is `{t:"warp", w:"room", r, x, y, l}`. One routine checks at run time whether Link is indoors with
  the entrance's palace and blockset: if not, stage 1 (the entrance warp); if so, stage 2 (the fall). The op
  stays queued, its routine re-sent at most every 400 ms, until Link stands in the room ($A0 = room, module $07 sub
  0); TTL 20 s. `$0AA1` isn't in o2's WRAM reads, which is why the check is in the asm and not in Go.
- Stage 2 sets $A2 = $A0 = the room so the fall moves nothing (`Dungeon_AdjustAfterSpiralStairs` adds the
  distance between $A2 and $A0), and writes the quadrant's bounds/scroll/camera triggers itself. Those formulas
  are checked against the ROM's entrance table (`TestRoomFallFor`, entrance $02). The scroll is the quadrant's, so
  it is valid whether the room scrolls over its full size or not.
- Every entrance's state is recorded up front (`peekEntrance`, Dungeon_LoadEntrance alone). A warp to a room
  that an entrance starts in goes through that entrance (the one the map drew the room from, if it is one of them).
- The flood fill puts off a crossing through a dungeon toggle door ($98–$9F, $A0–$A5, $A8–$AF, which switch $040C
  between the sewers and Hyrule Castle) to the next round, like a drop, and tracks the palace flip. Before, the
  Sanctuary's entrance ($02) took all of Hyrule Castle through the door between the throne room ($051) and the
  passage ($041). Now HC's rooms come from HC's own entrances (palace $02).
- An edge door whose tiles are $8E/$8F (TileBehavior_Entrance) leads out to the overworld, whatever its door type, so
  it isn't a way to the room next to it on the map. Before this, the secret passage under Hyrule Castle ($055,
  entrance $32) "walked" through its door out into Thieves' Town's $065 and drew all of Thieves' Town's basement
  with a cave's state and graphics (the too-bright conveyors). Each cave in $100–$123 was drawn from the cave above
  it the same way, and is now drawn from its own entrance.
- In one-layer rooms, corridors of BG2 running off the edge under BG1's walls also count as walkways: runs of floor
  at most 16 cells wide between BG2 walls, since filler BG2 runs off every edge without walls. Thieves' Town's
  entrance hall ($0DB, $0DC) leads north this way.
- The screen centers Link like the entrances do (scroll = X − $78, Y − $6D), clamped to the room's bounds where its
  layout scrolls over the whole room in that quadrant (`Room.FullSize`, from $040E with `kLayoutQuadrantFlags`, the
  blast wall flags and dung_unk2), else to the quadrant. `TestRoomFallMatchesEntrances` checks this against all 133
  in-map entrances of the US ROM: all match to within a pixel except the two house starts ($00, $43).
- The warp is done only when Link is at the point ($22/$20), not merely in the room, since stage 1's entrance may
  start in that room.
- The lens darkens blocked cells, tints pits/deep water red and triggers (stairs, conveyors, spikes) yellow, and
  shows Link's 16×24 body with his feet filled.
- Ice ($0E, $0F) and spikes ($0D, $44) are floor: Link walks and stands on them (Ice Palace's $04F).
- The fall starts dark: the fall's fade out (Module07_07 steps 00 and 01) showed the room Link was in with the
  target's scroll already set, shifted. The warp sets the palettes as the fade out leaves them (its colors black,
  darkening_or_lightening_screen 2) and what step 00 resets, and starts the fall at step 02.
- Double-clicking a point of the map warps Link there at once (the clicks before it pin the point as the target). The
  lens and the Warp section name the room's dungeon (from the palace, $040C, the room is drawn with, which walk.json
  carries as `palace`), its number and its name (`roomnames.ts`, from ZScream's room names).
- Verified in an emulator by the user: a warp to the HC entrance room worked. That test found the off-center camera
  and the Sanctuary-first entrance, both fixed since.

## 4. Tests

- `underworldmap`: the walk masks of known rooms (for example the Hyrule Castle entrance room: its start position
  is reachable, its walls are blocked, a pit room has pits blocked) on the US and JP ROMs; plus a check that every
  entrance's start position is standable and reachable in its room.
- `overworldmap`: generation succeeds for all parent areas; every exit-table Link position is standable and
  reachable; render smoke test (no all-black areas).
- `overworldExitState`: the exit-table oracle test (above).
- `debugwarp_scenario_test.go`: validation (bad room/area, out of range, unstandable point rejected), the overworld
  op writes the cache and starts module $0F→$08, the underworld op from the overworld queues stage 1 then stage 2,
  stage 2 alone when already in the dungeon sets $A0/$51/$EE and submodule $07.
- `debug_scenario_test.go` style end-to-end against the real ROM where the harness allows (US/JP/randomizer list
  at line 268).
- Manual check in snes9x-emunwa and bsnes: warp to a few rooms (both layers, e.g. Swamp, Tower of Hera), to a large
  and a small area in both worlds, from the overworld and from a dungeon, and that the sync (location, keys) follows.

## 5. Order of work

1. Shared emulator package; `underworldmap` RoomSink + walk masks; their tests.
2. Web cache + `/rom/warp/room.*` endpoints; Go mask accessor for the game.
3. UI: debug-mode hover/popover/snap/pin on the underworld map; Warp section button.
4. Underworld warp (stage 1 + stage 2) with scenario tests; manual verification.
5. `overworldmap` generator + masks + endpoints; `overworldExitState` with the oracle test.
6. Overworld UI (inverse `mapCoord`, large areas) and the overworld warp op; manual verification.
7. HANDOFF/README note; log lines for each warp and rejection (the o2 logs are where reports start).

Steps 1–4 ship a usable underworld-only feature. The overworld (steps 5–6) is the larger piece because nothing
renders or classifies overworld areas at full resolution today.

## Risks / open questions

- **Stage-2 fall** relies on the fall transition handling any quadrant and scroll. If it doesn't, switch to the
  PreDungeon hook fallback.
- **Dynamic state** (shutters, water levels, moving floors, event overlays) isn't in the masks. Reachability is a
  best effort, and the popover legend says so.
- **Randomizers**: entrance shuffle changes which entrance reaches a room. The flood fill already follows the ROM's
  tables, so `Room.Entrance` stays correct. Door randomizer ROMs may change room connectivity, which the flood fill
  already handles in the same way.
- **Special overworld areas** ($80+: Master Sword grove, Zora's waterfall, under the bridge) aren't on the map grid.
  Leave them out in v1.
- **Memory/time**: the first hover after loading a ROM waits for generation (a few seconds), so the popover shows
  "generating…". The overworld generation adds a similar cost; run both in the background when a ROM loads in debug
  mode.
