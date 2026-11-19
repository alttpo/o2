package alttp

import (
	"hash/fnv"
)

func (g *Game) sendPackets() {
	// don't send out any network updates until we're connected:
	if g.local.Index() < 0 {
		return
	}
	// don't send any state while the reset state machine is active:
	if g.IsResetting() {
		return
	}
	// don't send state read from a ROM other than the one we patched:
	if !g.ROMVerified {
		return
	}

	local := g.local

	{
		// send location packet every frame:
		m := g.makeBroadcastMessage()

		locStart := m.Len()
		if err := g.SerializeLocation(local, m); err != nil {
			panic(err)
		}

		// hash the location packet:
		locHash := hash64(m.Bytes()[locStart:])
		if g.locHashTTL > 0 {
			g.locHashTTL--
		}
		if locHash != g.locHash || g.locHashTTL <= 0 {
			// only send if different or Ttl of last packet expired:
			g.send(m)
			g.locHashTTL = 60
			g.locHash = locHash
		}
	}

	// don't send any game state while the local player is not in a module safe for syncing (e.g. the startup
	// screens, file select, copy/erase/name player and loading game modules $00-$05). In those modules the WRAM copy
	// of SRAM can contain stale data from a previous game or a partially loaded save file which other players would
	// otherwise pick up:
	if !g.syncing {
		return
	}

	{
		if g.SyncUnderworldLocal {
			m := g.makeBroadcastMessage()
			// crystal switch state (shared with alttpo):
			if err := g.SerializeCrystalSwitch(m); err != nil {
				panic(err)
			}
			g.send(m)
		}
	}

	if (g.SyncUnderworldLocal || g.SyncTorches) && local.Module == 0x07 {
		// star switch state and manipulable objects are room specific; send them only to players in the same room:
		m := g.makeSectorMessage()
		if g.SyncTorches {
			// the torches the local player lit (shared with alttpo's torches.as):
			if err := g.SerializeTorches(m); err != nil {
				panic(err)
			}
		}
		if g.SyncUnderworldLocal {
			if err := g.SerializeStarSwitch(m); err != nil {
				panic(err)
			}
			if g.manip.valid {
				if err := g.SerializeManip(m); err != nil {
					panic(err)
				}
			}
			// last, since older alttpo versions stop parsing at the first message type they don't know:
			if err := g.SerializeRoomState(m); err != nil {
				panic(err)
			}
			if err := g.SerializeRoomDoors(m); err != nil {
				panic(err)
			}
			if err := g.SerializeFlyingTiles(m); err != nil {
				panic(err)
			}
			if err := g.SerializeRoomKills(m); err != nil {
				panic(err)
			}
		}
		g.send(m)
	}

	if g.SyncUnderworldLocal && g.manip.valid && (local.Module == 0x07 || local.Module == 0x0E) {
		// the manipulable objects again, to all players whenever they change and every 32 frames, so that players
		// entering the room have them while it loads (see manipstage.go):
		m := g.makeBroadcastMessage()
		start := m.Len()
		if err := g.SerializeManip(m); err != nil {
			panic(err)
		}
		if h := hash64(m.Bytes()[start:]); h != g.manipSentHash || g.monotonicFrameTime&31 == 0 {
			g.manipSentHash = h
			g.send(m)
		}
	}

	if g.SyncOverworldLocal {
		// the overworld map16 change log, to all players so that it is applied while they load the area:
		g.sendMap16()
		// overworld entrance animations and other events, in their own packet:
		g.sendOwEvents()
	}

	{
		// small keys (delta based; shared with alttpo's SmallKeys.as), in their own packet:
		m := g.makeO2BroadcastMessage()
		if err := g.SerializeSmallKeys(m); err != nil {
			panic(err)
		}
		// heart pieces (delta based; shared with alttpo's Hearts.as):
		if err := g.SerializeHearts(m); err != nil {
			panic(err)
		}
		g.send(m)
	}

	if g.monotonicFrameTime&15 == 0 {
		// Broadcast items and progress SRAM:
		m := g.makeBroadcastMessage()
		if m != nil {
			if g.isVTRandomizer() {
				// VT randomizer:

				// vanilla:
				if err := g.SerializeSRAM(local, m, 0x340, 0x390+16+1); err != nil {
					panic(err)
				}
				if err := g.SerializeSRAM(local, m, 0x3C5, 0x3C9+1); err != nil {
					panic(err)
				}
				// randomizer specifics:
				if err := g.SerializeSRAM(local, m, 0x3FF, 0x429+1); err != nil {
					panic(err)
				}
				if err := g.SerializeSRAM(local, m, 0x46e, 0x476+1+1); err != nil {
					panic(err)
				}

				// Door randomizer = VT + these:
				if err := g.SerializeSRAM(local, m, 0x4C0, 0x4CC+1); err != nil {
					panic(err)
				}

				//serialize(r, 0x4E0, 0x4ED); // chest-keys
			} else {
				// assume vanilla game:

				// items earned
				if err := g.SerializeSRAM(local, m, 0x340, 0x37B+1); err != nil {
					panic(err)
				}
				// progress made
				if err := g.SerializeSRAM(local, m, 0x3C5, 0x3C9+1); err != nil {
					panic(err)
				}
			}

			g.send(m)
		}
	}

	if g.SyncUnderworld && g.monotonicFrameTime&31 == 0 {
		// dungeon rooms
		m := g.makeBroadcastMessage()
		err := g.SerializeSRAM(g.local, m, 0x000, 0x250)
		if err != nil {
			panic(err)
		}
		g.send(m)
	}

	// Link's sprites for alttpo clients:
	g.sendLinkSprites()

	if g.SyncOverworld && (g.monotonicFrameTime&31 == 16 || g.owFlagsSend) {
		g.owFlagsSend = false
		// overworld events; heart containers, overlays
		m := g.makeBroadcastMessage()
		err := g.SerializeSRAM(g.local, m, 0x280, 0x340)
		if err != nil {
			panic(err)
		}
		g.send(m)
	}
}

func hash64(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}
