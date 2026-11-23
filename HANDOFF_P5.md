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
- Fact 0x00's item buffer is $D00 bytes ($7E9480-$7EA17F), not the $1200 first planned; PROTOCOL05.md is updated.
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
- [ ] Players list: show "incompatible version" for players whose info has another version (only logged and
      notified now).
- [x] Fact store keyed by hash (`facts.go`); uploading fact 0x00 after every new join (first chunk alone until the
      server answers); downloading a remote player's fact on demand (`factData`), checked against its hash.
- [x] Fact 0x00 (Link graphics, `linkfact.go`): bank $10, sword levels 1-4, shield levels 1-3, item buffer, flute
      bird; `TestLinkFact_MatchesGame` compares it with the game's own routines on the US and JP ROMs.
- [ ] Sprites 0x04 / SpriteChrs 0x05 / SpritePalettes 0x06; remotesprites draws from the remote player's fact.
- [x] Stop padding the group (`client.SetGroup`).

### alttpo

- [ ] Envelope (`receive.as`, `make_server_envelope`), join/sector/who-is/grace, leave on unload.
- [ ] Game protocol v2 framing and renumbering in `GameState::deserialize` and every serializer.
- [ ] Fact store; fact 0x00 built from ROM (port o2's sheet decompressor and 3bpp → 4bpp expansion).
- [ ] Sprites 0x04 / 0x05 / 0x06 replacing unique tiles; SM facts 0x10, 0x11.

## Log

(newest last; one line per landed step with its commit)

- o2-server `89fda17`: p5 server and facts, with tests.
- o2 `6b333e4`: `client/protocol05` and the test hub.
- o2 `7daac7d`: p5 networking and game protocol v2 (framing, renumbering, dropped messages).
