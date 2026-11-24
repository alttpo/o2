# Handoff: protocol 0x05, facts and game protocol v2

**Read [../o2-server/PROTOCOL05.md](../o2-server/PROTOCOL05.md) first.** It is the contract between o2-server, o2 and
alttpo: the p5 envelope, facts, game protocol v2 (framing, info blob, message table) and the sprite redesign. This
file only tracks who has done what and the decisions taken while implementing.

Repos and branches (commit to the branch that is checked out; never create branches):

| Repo | Branch | Part |
|---|---|---|
| `../o2-server` | `main` | p5 server, facts store |
| `.` (o2) | `feature/alttpo-compat` | `client/protocol05`, game protocol v2, Link graphics fact, sprites |
| `../alttpo` | `esync` | AngelScript: envelope, game protocol v2, fact store, sprites |

No compatibility with p4 or game protocol v1 is kept anywhere. o2 and alttpo must ship together, after o2-server.

## Decisions made while implementing

- o2-server's working tree has an untracked, unfinished `bandwidth.go` that doesn't compile (it refers to
  `Client.Traffic`, `Client.JoinedAt`, `NetworkMetrics.ClientBandwidth`). It is not ours: don't commit, edit or
  delete it. Build and test the server by listing the tracked files:
  `go test $(git ls-files '*.go')`.
- p5 state in the server is separate from p1-p4's (`clientGroups`): `p5Server` with its own groups and address map.
  `main.go` only dispatches protocol 0x05 to it and calls its expiry from the 5 s ticker.
- The server's fact contents are deduplicated by hash across all groups (`p5Server.blobs`, reference counted); a
  session's quota counts the full size of its facts even when the bytes are shared.
- o2 keeps what player-info said (`netState.info`) outside `Player`, because a group reset (`SoftReset`) clears the
  players array; relays restore the name and team into the `Player` from it.
- o2's sector is "none" (leave-sector) while no game is loaded (`preGameModule`) and while resetting.
- o2 frames messages in `gameBroadcastMessage.add`/`addBytes`: every serializer still writes `type, body`, and `add`
  inserts the length. Every message must go through `add` or `addBytes`.
- `Game.strictMessages` (set by the tests' `createTestGameSync`) makes a message whose deserializer leaves bytes
  unread an error, so a body size mismatch with alttpo fails the tests; in production it is allowed (a newer client
  may append fields).
- o2's tests route through `protocol05.Hub` (`client/protocol05/hub.go`), an in-memory server without timeouts,
  grace or quotas. `alttpoPacket` in `alttpo_compat_test.go` builds framed v2 packets the way alttpo will (`m(type)`
  starts a message) and sends the sender's player-info first.
- `client.Client.Disconnect` sends leave-group twice before closing.
- alttpo is compile-checked headlessly (see the o2 memory "alttpo AngelScript compile check"); it has no tests, so
  o2's `alttpo_compat_test.go` model (`alttpoParseBody`) is the reference for the message bodies both must agree on.
- alttpo's compact small keys reads its own serialized message at fixed offsets; with the length byte they moved by one
  (`add_small_keys_compact`).
- Fact 0x00's item buffer is $D00 bytes ($7E9480-$7EA17F), not the $1200 first planned; PROTOCOL05.md is updated.
- Sprite entries carry no OAM index: their order is OAM order (front to back), and a receiver's `remoteSprite.index`
  is the position. Tests that need the sender's OAM index find it by the OAM bytes they set.
- The flute's bird (chrs $0E-$0F, $1E-$1F) isn't synced in either direction (decided 2026-11-23): it isn't in fact
  0x00 (now $8D80 = 36,224 bytes; o2 and alttpo build the same bytes, FNV-1a $63F9AEBD on the US ROM), and neither
  client binds its chrs, so sprites showing them are never sent.
- o2 draws the item in use as a part like the sword and shield (`remoteEquipBlocks`), within `remotePartMax` (4): the
  table, SRAM state and NMI budget are unchanged. With the head and body, sword, shield and item all shown, the item is
  left out (sword, shield, item order); in practice Link doesn't show his sword while using an item.
- Fact 0x01, the follower graphics (2026-11-24): for every follower $00-$0D ($7EF3CC), the $400-byte buffer
  LoadFollowerGraphics builds at $7EB940 (two rows of 16 tiles), at follower * $400 ($3800 = 14,336 bytes). Senders
  bind chr $20 (n = 2) to tile follower*32 + dma_var6/16 ($0AE8), chr $30 to 16 tiles after it, and chrs $22/$32 the
  same with dma_var7 ($0AEA), only while $7EF3CC is $01-$0D; MsgSpriteChrs is unchanged, it just binds chrs up to $3F
  now. NPCs that borrow the follower chrs with no follower (crystal maidens, Kiki, the old mountain man) stay unbound.
  o2 finds its follower's OAM entries by looking for chr $22 (name table 0, on screen) in the first 4 entries of the
  two OAM regions Follower_AnimateMovement_preserved uses for the sort setting ($0FB3); the copy of Link's OAM
  entries grew by 14 bytes to 255, the most one read takes. Worst case 478 cycles for the copy (was 235);
  linkOAMCopyCycles is 500.

## Status

### o2-server

- [x] `protocol05.go` (encoding) and `p5server.go`: join/leave/move/migrate, grace, sectors, echo, who-is, relays,
      nak, expiry.
- [x] facts: fact-put, fact-status, fact-list, fact-get, fact-data, quotas, dedupe, group lifetime.
- [x] `protocol05_test.go`.
- [x] `main.go` dispatch and expiry.
- [x] influx metrics for p5 (o2-server: `networkmetrics.go` sums per second; p5 reports kind bytes per group, no client tag).

### o2

- [x] `client/protocol05`: encoding shared by the game code and tests, and the test hub.
- [x] `games/alttp/net.go`: join/ack/nak, sectors, who-is, player-info/left, grace, server time from `serverTimeMs`,
      leave on disconnect, echo backoff (G3).
- [x] Game protocol v2: header `u8 frame`, `type, length, body` framing, renumbered messages, dropped messages, info
      blob `version, team`.
- [x] One packet per destination per frame (`frameBatch` in net.go): everything `sendPackets` sends, sfx included,
      merges into one packet to all and one to the own sector, split at `maxGamePacket`. Messages sent outside the
      frame (slowbeat run timing, run gaps, reset messages) and to another sector still go alone.
- [x] Players list: players whose info has another version are listed after the others as "incompatible version"
      (`appendIncompatiblePlayers`), until they leave or the group changes.
- [x] Fact store keyed by hash (`facts.go`); uploading fact 0x00 after every new join (first chunk alone until the
      server answers); downloading a remote player's fact on demand (`factData`), checked against its hash.
- [x] Fact 0x00 (Link graphics, `linkfact.go`): bank $10, sword levels 1-4, shield levels 1-3, item buffer, flute
      bird; `TestLinkFact_MatchesGame` compares it with the game's own routines on the US and JP ROMs.
- [x] Sprites 0x04 / SpriteChrs 0x05 / SpritePalettes 0x06 sent (`sprites.go`, `send.go` `addLinkSprites`) and
      received (`remotesprites.go`): sprites in OAM order, 5 bytes each; Link's rows ($00-$0A, $10-$1A) bound to
      fact 0x00 tiles, only the rows the sprites show; palettes when they change. Sprites showing a Link chr without a
      tile aren't sent. The receiver maps bindings onto the tiles it already draws with (`remoteChrTile`): LinkGraphics
      tiles from its own ROM, and its own sword/shield tiles only when the levels match.
- [x] Draw remote players with their own graphics from their fact 0x00 (custom Link sprites, other sword/shield
      levels): an SRAM buffer of 10 rows of 2 tiles at $706D80-$706FFF (`remoteTilesAddr`, `remoteTileRows`) that
      o2 fills from the fact before the table that uses it; part flag `partSRAM` (bit 13 of the NMI list's VRAM
      address) makes the NMI upload read bank $70. Writing rows clears `rsKept`. NMI upload measured 562 cycles
      (budget 600). Tests: `TestRemoteSpritesScenario_OwnGraphics`, `_SameGraphicsOtherSword`.
- [ ] Remote players' palettes: o2 still draws them with the local palettes (`DeserializeSpritePalettes` discards
      them), so a custom sprite with its own palette shows with the local one.
- [x] Remote players' items in use (chrs $09-$0A, $19-$1A) drawn as a part (`remoteEquipment`): from the local item
      buffer in WRAM while the graphics are the same or the fact isn't here, else from the fact through the SRAM
      buffer. Tests: `TestRemoteSpritesScenario_ItemInUse`, `_ItemPastPartMax`.
- [x] The flute bird: not synced (see decisions); removed from fact 0x00.
- [x] Stop padding the group (`client.SetGroup`).
- [x] Follower graphics fact (fact 0x01, `followerfact.go`) built from ROM and uploaded; `TestFollowerFact_MatchesGame`
      runs the game's LoadFollowerGraphics for every follower on the US and JP ROMs. o2 sends its follower's sprites
      with chrs `$20`-`$23`/`$30`-`$33` bound by pose (`emitFollowerCopy`, `followerChrRows`); test
      `TestLinkSprites_Follower`.
- [ ] o2 draws remote players' followers: not done (decided 2026-11-24). It needs two more parts per player through
      the NMI upload (562 of 600 cycles already), so it would cost the sword, shield or item, or a bigger budget.

### alttpo

- [x] Envelope (`receive.as`, `Protocol.as`), join/sector/who-is/grace (`P5State`), leave on disconnect and unload.
- [x] Game protocol v2 framing (`msg_begin`/`msg_end`/`write_msg` in Protocol.as) and renumbering in
      `GameState::deserialize` and every serializer; each message is read from a copy of the packet that ends with it.
- [x] Fact store, uploads, downloads (`Facts.as`); the server's hashes are trusted (no hashing in AngelScript).
- [x] Fact 0x00 built from ROM (`LinkFact.as`), byte for byte the same as o2's on the US ROM (checked by running
      both and comparing an FNV-1a hash: 36,224 bytes, $63F9AEBD since the bird was dropped).
- [x] Sprites 0x04 / 0x05 / 0x06 (`send_sprites`, `link_chr_tiles` in LocalGameState.as; `deserialize_sprite_chrs`,
      `resolve_sprite_chrs`, `deserialize_sprites` in GameState.as). Received chrs bound to a fact that hasn't
      downloaded yet aren't drawn (`chr_tile`); unbound chrs come from the local VRAM.
- [x] SM facts 0x10 and 0x11 (`SMFact.as`, built for SM and SMZ3 ROMs; the same bytes as the ROM file's on vanilla
      SM, and the same on SMZ3) and Samus' sprites by pose (`sm_chr_rows`); `send_sprites` takes its rows from
      `link_chr_rows` or `sm_chr_rows`. Checked only by compiling and by running the row builder over every pose.
- [x] Cleanup: the unique tile capture and drawing are gone.
- [x] Follower graphics fact (fact 0x01, `FollowerFact.as`), byte for byte o2's (FNV-1a $06AC8A41 on the US and JP
      ROMs); `send_sprites` binds the follower's chrs by pose (`follower_chr_rows`) and receivers draw bound chrs up to
      $3F from the sender's fact.
- [ ] SMZ3: no Link graphics fact is built ("sprite sheet routines not found in ROM"): `LinkFactBuilder.find` only
      knows the US and JP addresses of `Decompress_sprite_arbitrary` etc., and SMZ3 moves ALTTP's code. Link's
      sprites aren't sent in SMZ3 until it finds them there.

### Open follow-ups (both clients)

- [x] Followers' graphics as a fact (chrs $20-$23, $30-$33): fact 0x01, bound by pose (see decisions).
- [ ] End-to-end check with o2-server, o2 and alttpo running together (not done in this session: only unit tests for
      o2-server and o2, and compile checks for alttpo).

## Log

(newest last; one line per landed step with its commit)

- o2-server `89fda17`: p5 server and facts, with tests.
- o2 `6b333e4`: `client/protocol05` and the test hub.
- o2 `7daac7d`: p5 networking and game protocol v2 (framing, renumbering, dropped messages).
- o2 `7e77624`: one packet per destination per frame.
- o2 `cc9dfe0`: facts and the Link graphics fact.
- o2 `4ae8c98`: sprites by pose (MsgSprites, MsgSpriteChrs, MsgSpritePalettes).
- alttpo `b697255`: p5, game protocol v2, facts store; sprite sync off.
- alttpo `1f21357`: the Link graphics fact.
- alttpo `c3796ab`: sprites by pose.
- o2 `9a584bc`: incompatible players in the players list.
- o2 `cfc8070`: remote players drawn with their own graphics from fact 0x00.
- alttpo `791a4b2`: the bird's flag read as a byte.
- alttpo `443af9c`: SM facts and Samus' sprites by pose; unique tiles removed.
- o2-server `f6b307c`: PROTOCOL05.md: the bird isn't synced; fact 0x00 is 36,224 bytes.
- alttpo `88d31d5`: the bird out of fact 0x00 and its chrs unbound.
- o2 `90c959a`: the bird out of fact 0x00.
- o2 `ad629aa`: remote players' item in use drawn.
- o2 `829c0b2`: follower graphics fact 0x01; o2's follower sent, bound by pose.
- alttpo `c6ff726`: follower graphics fact 0x01; followers bound by pose and drawn from the sender's fact.

### Group passwords

- [x] Optional, set by the first player of a group; keeps uninvited players out, not meant against sniffers. join-group's
      optional trailing auth field is `u8 length, token` with token = SHA-256("o2-p5-group:" || password). The group's
      first join sets it; later joins must match (join-nak 2 no token, 3 wrong token, 4 group has no password or bad
      field; no body). Scheme in o2-server `p5auth.go`; o2: `client/protocol05/auth.go`, `games/alttp/net.go`
      (`netJoinRefused`), password field in the Server view (kept in the config file in plain text). The test vector
      is shared by both repos. alttpo: `Protocol.as` `p5_auth_token`, `receive.as`, Password box in `SettingsWindow.as`; it needs bsnes-as's `sha256(string)` global (script-string.cpp).
- [ ] Not in PROTOCOL05.md (the file is only in `backup/protocol05-before-squash`).
