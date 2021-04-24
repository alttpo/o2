package alttp

import (
	"hash/fnv"
	"o2/client/protocol02"
)

func (g *Game) sendPackets() {
	// don't send out any network updates until we're connected:
	if g.local.Index < 0 {
		return
	}

	local := g.local

	{
		// send location packet every frame:
		m := g.makeGamePacket(protocol02.Broadcast)

		locStart := m.Len()
		if err := SerializeLocation(local, m); err != nil {
			panic(err)
		}

		// hash the location packet:
		locHash := hash64(m.Bytes()[locStart:])
		if g.locHashTTL > 0 {
			g.locHashTTL--
		}
		if locHash != g.locHash || g.locHashTTL <= 0 {
			// only send if different or TTL of last packet expired:
			g.send(m)
			g.locHashTTL = 60
			g.locHash = locHash
		}
	}

	{
		m := g.makeGamePacket(protocol02.Broadcast)
		// small keys:
		if err := SerializeWRAM(local, m, smallKeyFirst, 0x10); err != nil {
			panic(err)
		}
		// current dungeon supertile door state:
		if err := SerializeWRAM(local, m, 0x0400, 1); err != nil {
			panic(err)
		}
		g.send(m)
	}

	if g.monotonicFrameTime&15 == 0 {
		// Broadcast items and progress SRAM:
		m := g.makeGamePacket(protocol02.Broadcast)
		if m != nil {
			// items earned
			if err := SerializeSRAM(local, m, 0x340, 0x37C); err != nil {
				panic(err)
			}
			// progress made
			if err := SerializeSRAM(local, m, 0x3C5, 0x3CA); err != nil {
				panic(err)
			}

			// TODO: more ranges depending on ROM kind

			// VT randomizer:
			//serialize(r, 0x340, 0x390); // items earned
			//serialize(r, 0x390, 0x3C5); // item limit counters
			//serialize(r, 0x3C5, 0x43A); // progress made

			// Door randomizer:
			//serialize(r, 0x340, 0x390); // items earned
			//serialize(r, 0x390, 0x3C5); // item limit counters
			//serialize(r, 0x3C5, 0x43A); // progress made
			//serialize(r, 0x4C0, 0x4CD); // chests
			//serialize(r, 0x4E0, 0x4ED); // chest-keys

			g.send(m)
		}
	}

	if g.SyncUnderworld && g.monotonicFrameTime&31 == 0 {
		// dungeon rooms
		m := g.makeGamePacket(protocol02.Broadcast)
		err := SerializeSRAM(g.local, m, 0x000, 0x250)
		if err != nil {
			panic(err)
		}
		g.send(m)
	}

	if g.SyncOverworld && g.monotonicFrameTime&31 == 16 {
		// overworld events; heart containers, overlays
		m := g.makeGamePacket(protocol02.Broadcast)
		err := SerializeSRAM(g.local, m, 0x280, 0x340)
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
