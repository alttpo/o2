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
		m := g.makeBroadcastMessage()
		// current dungeon supertile door state:
		if err := g.SerializeWRAM(local, m, 0x0400, 1); err != nil {
			panic(err)
		}
		if g.SyncUnderworld {
			// crystal switch state (shared with alttpo):
			if err := g.SerializeCrystalSwitch(m); err != nil {
				panic(err)
			}
		}
		g.send(m)
	}

	if g.SyncUnderworld && local.Module == 0x07 {
		// star switch state is room specific; send it only to players in the same room:
		m := g.makeSectorMessage()
		if err := g.SerializeStarSwitch(m); err != nil {
			panic(err)
		}
		g.send(m)
	}

	{
		// small keys (delta based; shared with alttpo's SmallKeys.as), in their own packet:
		m := g.makeO2BroadcastMessage()
		if err := g.SerializeSmallKeys(m); err != nil {
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

	if g.SyncOverworld && g.monotonicFrameTime&31 == 16 {
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
