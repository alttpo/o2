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
- The server's fact contents are deduplicated by hash across all groups (`p5Blobs`, reference counted); a session's
  quota counts the full size of its facts even when the bytes are shared.

## Status

### o2-server

- [ ] `protocol05.go`: encoding, join/leave/move/migrate, grace, sectors, echo, who-is, relays, nak, expiry.
- [ ] facts: fact-put, fact-status, fact-list, fact-get, fact-data, quotas, dedupe, group lifetime.
- [ ] `protocol05_test.go`.
- [ ] `main.go` dispatch and expiry.
- [ ] influx metrics for p5 (not started; p4's metrics take p4 types).

### o2

- [ ] `client/protocol05`: encoding shared by the game code and tests.
- [ ] `games/alttp/net.go`: join/ack/nak, sectors, who-is, player-info/left, grace, server time from `serverTimeMs`,
      leave on disconnect, echo backoff (G3).
- [ ] Game protocol v2: header `u8 frame`, `type, length, body` framing, renumbered messages, dropped messages,
      one packet per destination per frame, info blob `version, team`.
- [ ] Fact store keyed by hash; uploading fact 0x00; downloading others' facts.
- [ ] Fact 0x00 (Link graphics): bank $10, sword levels 1-4, shield levels 1-3, item buffer, flute bird.
- [ ] Sprites 0x04 / SpriteChrs 0x05 / SpritePalettes 0x06; remotesprites draws from the remote player's fact.
- [ ] Stop padding the group (`client.SetGroup`).

### alttpo

- [ ] Envelope (`receive.as`, `make_server_envelope`), join/sector/who-is/grace, leave on unload.
- [ ] Game protocol v2 framing and renumbering in `GameState::deserialize` and every serializer.
- [ ] Fact store; fact 0x00 built from ROM (port o2's sheet decompressor and 3bpp → 4bpp expansion).
- [ ] Sprites 0x04 / 0x05 / 0x06 replacing unique tiles; SM facts 0x10, 0x11.

## Log

(newest last; one line per landed step with its commit)
