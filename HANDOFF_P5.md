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
- o2 can't bind the flute bird's chrs ($0E-$0F, $1E-$1F): its patch copies only $0AC0-$0AD7 of the DMA sources to SRAM,
  and the bird's are at $0AF4-$0AF8. The fact has the bird's tiles anyway, for alttpo.

## Status

### o2-server

- [x] `protocol05.go` (encoding) and `p5server.go`: join/leave/move/migrate, grace, sectors, echo, who-is, relays,
      nak, expiry.
- [x] facts: fact-put, fact-status, fact-list, fact-get, fact-data, quotas, dedupe, group lifetime.
- [x] `protocol05_test.go`.
- [x] `main.go` dispatch and expiry.
- [ ] influx metrics for p5 (not started; p4's metrics take p4 types).

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
- [ ] Remote players' items in use (chrs $09-$0A, $19-$1A) aren't drawn by o2 (they weren't before either); the fact
      has them now, so they could be a fifth part.
- [ ] The flute bird: o2 doesn't send it at all. It is an ancilla (`Ancilla27_Duck`) drawn in ancilla OAM slots
      with chr $0E (and Link's chrs $00/$02 while it carries him), not in Link's OAM entries, and `captureChrs`
      doesn't capture it. Binding its chrs needs that first, plus $0AF4-$0AF9 in the Link OAM copy (one more DMA
      after the graphics DMA sources; `linkOAMCopyReadSize` would be 247 of 255).
- [x] Stop padding the group (`client.SetGroup`).

### alttpo

- [x] Envelope (`receive.as`, `Protocol.as`), join/sector/who-is/grace (`P5State`), leave on disconnect and unload.
- [x] Game protocol v2 framing (`msg_begin`/`msg_end`/`write_msg` in Protocol.as) and renumbering in
      `GameState::deserialize` and every serializer; each message is read from a copy of the packet that ends with it.
- [x] Fact store, uploads, downloads (`Facts.as`); the server's hashes are trusted (no hashing in AngelScript).
- [x] Fact 0x00 built from ROM (`LinkFact.as`), byte for byte the same as o2's on the US ROM (checked by running
      both and comparing an FNV-1a hash: 37,248 bytes, $5F919A6A).
- [x] Sprites 0x04 / 0x05 / 0x06 (`send_sprites`, `link_chr_tiles` in LocalGameState.as; `deserialize_sprite_chrs`,
      `resolve_sprite_chrs`, `deserialize_sprites` in GameState.as). Received chrs bound to a fact that hasn't
      downloaded yet aren't drawn (`chr_tile`); unbound chrs come from the local VRAM.
- [x] SM facts 0x10 and 0x11 (`SMFact.as`, built for SM and SMZ3 ROMs; the same bytes as the ROM file's on vanilla
      SM, and the same on SMZ3) and Samus' sprites by pose (`sm_chr_rows`); `send_sprites` takes its rows from
      `link_chr_rows` or `sm_chr_rows`. Checked only by compiling and by running the row builder over every pose.
- [x] Cleanup: the unique tile capture and drawing are gone.
- [ ] SMZ3: no Link graphics fact is built ("sprite sheet routines not found in ROM"): `LinkFactBuilder.find` only
      knows the US and JP addresses of `Decompress_sprite_arbitrary` etc., and SMZ3 moves ALTTP's code. Link's
      sprites aren't sent in SMZ3 until it finds them there.

### Open follow-ups (both clients)

- [ ] Followers' graphics as a fact (chrs $20-$23, $30-$33): alttpo used to send them inline; receivers now draw a
      remote follower with their own VRAM's tiles.
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
