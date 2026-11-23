package alttp

import (
	"hash/fnv"
	"io"
	"time"
)

// Sending game state
//
// Each frame, o2 sends at most one packet to all players and one to the players in its sector (see BANDWIDTH.md):
//
//   - To all players: the location, crystal switch (in the dungeon), manipulable objects, small keys and hearts. They are sent when they
//     change and refreshed every few frames (see changeSender). alttpo stops parsing a packet at the first message
//     type it doesn't know, so the newest types go last.
//   - To the sector: Link's sprites, the room's state in the dungeon (torches, star switch, manipulable objects, trap
//     doors, key doors and switches, flying tiles, kill triggers) and the enemies killed. Also sent when they change
//     and refreshed every few frames.
//
// The SRAM is sent in packets of its own, every 16 or 32 frames, only what changed since it was last sent (see
// sramSender). With players whose clients have them, the location and small keys are sent as changes to the last
// full message (see compact.go).

const (
	// how often an unchanged message is sent again, in frames:
	locationRefresh      = 60
	crystalRefresh       = 32
	manipRefresh         = 32
	smallKeysRefresh     = 32
	roomRefresh          = 16
	enemiesKilledRefresh = 16
	// remote sprites are drawn for remoteSpritesStale (250 ms) after they arrive, here and in alttpo:
	spritesRefresh = 8
	paletteRefresh = 32
	// the underworld and overworld SRAM are checked for changes every 32 frames and sent whole every this many checks
	// (about 8.5 s):
	sramFullRefresh = 16
)

// sendState is what was last sent of the messages that are only sent when they change.
type sendState struct {
	// the game frame last sent, since sendPackets runs on every read of the SNES:
	frame   uint8
	frameOK bool

	location, crystal, manip, smallKeys, hearts changeSender
	sprites, enemiesKilled                      changeSender
	// the room's messages to the sector:
	torches, star, roomManip, roomState, roomDoors, flyingTiles, roomKills, rupees changeSender
	// whether the crystal switch was last sent as known:
	crystalSentValid bool
	// the compact messages (see compact.go):
	compact compactLocal

	// the sprite palettes last sent, and frames of sprites since they were all sent:
	palettes      [8][32]byte
	paletteSent   [8]bool
	paletteFrames int

	// the remote players in the local player's location and in the group last frame, to catch them up at once:
	peersHere  [MaxPlayers / 64]uint64
	peersGroup [MaxPlayers / 64]uint64
	// newPeerHere and newPeerGroup are set on frames where a player arrived:
	newPeerHere, newPeerGroup bool

	// which recent room's enemies killed to send next:
	enemiesKilledRecent int

	items, underworld, overworld sramSender
}

// trackPeers notes the players that arrived in the local player's location or in the group since last frame.
func (g *Game) trackPeers() {
	st := &g.sendState
	var here, group [MaxPlayers / 64]uint64
	for _, p := range g.RemotePlayers() {
		i := p.Index()
		if i < 0 || i >= MaxPlayers {
			continue
		}
		group[i>>6] |= 1 << (i & 63)
		if locationsEqual(g.local.Location, p.Location) {
			here[i>>6] |= 1 << (i & 63)
		}
	}
	st.newPeerHere, st.newPeerGroup = false, false
	for k := range here {
		if here[k]&^st.peersHere[k] != 0 {
			st.newPeerHere = true
		}
		if group[k]&^st.peersGroup[k] != 0 {
			st.newPeerGroup = true
		}
	}
	st.peersHere, st.peersGroup = here, group
	if st.newPeerGroup {
		// the SRAM is sent every 16 or 32 frames; send all of it then:
		st.underworld.full, st.overworld.full, st.items.full = true, true, true
		// and the full messages that the compact ones change:
		st.compact.locOK, st.compact.skOK = false, false
	}
}

func (g *Game) sendPackets() {
	// don't send out any network updates until we're connected:
	if g.local.Index() < 0 {
		return
	}
	// keep the join and the sector up to date (leave-sector while resetting):
	g.tickNet(time.Now())
	// don't send any state while the reset state machine is active:
	if g.IsResetting() {
		return
	}
	// don't send state read from a ROM other than the one we patched:
	if !g.ROMVerified {
		return
	}

	local := g.local
	st := &g.sendState
	// sendPackets runs on every read of the SNES; send once per game frame:
	if st.frameOK && st.frame == g.monotonicFrameTime {
		return
	}
	st.frame, st.frameOK = g.monotonicFrameTime, true
	g.trackPeers()
	// the messages to all players, in one packet:
	all := g.makeBroadcastMessage()
	g.addLocationCompact(all)

	// don't send any game state while the local player is not in a module safe for syncing (e.g. the startup
	// screens, file select, copy/erase/name player and loading game modules $00-$05). In those modules the WRAM copy
	// of SRAM can contain stale data from a previous game or a partially loaded save file which other players would
	// otherwise pick up:
	if !g.syncing {
		g.sendIfAny(all)
		return
	}

	if g.SyncUnderworldLocal && (g.crystalSwitch.valid || st.crystalSentValid) {
		// crystal switch state (shared with alttpo), only in the dungeon; once more as unknown when leaving it, though
		// receivers also forget it when a player enters a dungeon:
		g.addOnChange(all, &st.crystal, crystalRefresh, g.SerializeCrystalSwitch)
		st.crystalSentValid = g.crystalSwitch.valid
	}

	if g.SyncUnderworldLocal && g.manip.valid && (local.Module == 0x07 || local.Module == 0x0E) {
		// the manipulable objects, to all players too, so that players entering the room have them while it loads
		// (see manipstage.go):
		g.addOnChange(all, &st.manip, manipRefresh, g.SerializeManip)
	}

	// small keys and heart pieces (delta based; shared with alttpo's SmallKeys.as and Hearts.as), last since they are
	// the newest:
	g.addSmallKeysCompact(all)
	g.addOnChange(all, &st.hearts, smallKeysRefresh, g.SerializeHearts)

	g.sendIfAny(all)

	// the messages to the players in the same sector, in one packet; Link's sprites first, since they are the
	// oldest:
	sector := g.makeSectorMessage()
	g.addLinkSprites(&sector.gameBroadcastMessage)
	if local.Module == 0x07 {
		g.addRoom(&sector.gameBroadcastMessage)
	}
	if g.SyncEnemiesKilled {
		g.addOnChange(&sector.gameBroadcastMessage, &st.enemiesKilled, enemiesKilledRefresh, g.SerializeEnemiesKilled)
	}
	g.sendIfAny(sector)

	if g.SyncEnemiesKilled {
		// the enemies killed of the other rooms the game remembers, each to its room's sector:
		g.sendEnemiesKilledRecent()
	}

	if g.SyncOverworldLocal {
		// the overworld map16 change log, to all players so that it is applied while they load the area:
		g.sendMap16()
		// overworld entrance animations and other events, in their own packet:
		g.sendOwEvents()
	}

	if g.monotonicFrameTime&15 == 0 {
		// items and progress SRAM, where they changed:
		ranges := [][2]uint16{
			// items earned and progress made:
			{0x340, 0x37B + 1}, {0x3C5, 0x3C9 + 1},
		}
		if g.isVTRandomizer() {
			ranges = [][2]uint16{
				// vanilla:
				{0x340, 0x390 + 16 + 1}, {0x3C5, 0x3C9 + 1},
				// randomizer specifics:
				{0x3FF, 0x429 + 1}, {0x46e, 0x476 + 1 + 1},
				// Door randomizer = VT + these:
				{0x4C0, 0x4CC + 1},
				//{0x4E0, 0x4ED}, // chest-keys
			}
		}
		g.sendSRAMChanges(&st.items, ranges, 1)
	}

	if g.SyncUnderworld && g.monotonicFrameTime&31 == 0 {
		// dungeon rooms
		g.sendSRAMChanges(&st.underworld, [][2]uint16{{0x000, 0x250}}, 2)
	}

	if g.SyncOverworld && (g.monotonicFrameTime&31 == 16 || g.owFlagsSend) {
		g.owFlagsSend = false
		// overworld events; heart containers, overlays
		g.sendSRAMChanges(&st.overworld, [][2]uint16{{0x280, 0x340}}, 1)
	}
}

// sendIfAny sends m if it has any message.
func (g *Game) sendIfAny(m interface {
	gameMessage
	empty() bool
}) {
	if !m.empty() {
		g.send(m)
	}
}

// addRoom adds the room's state the dungeon module shares with the players in the room to m, each message when it
// changes and every roomRefresh frames. Older alttpo versions stop parsing at the first message type they don't
// know, so the newer types go last.
func (g *Game) addRoom(m *gameBroadcastMessage) {
	st := &g.sendState
	if g.SyncTorches && g.torches.valid && g.torches.n > 0 {
		// the torches the local player lit (shared with alttpo's torches.as), in rooms with lightable torches:
		g.addOnChange(m, &st.torches, roomRefresh, g.SerializeTorches)
	}
	if !g.SyncUnderworldLocal {
		return
	}
	g.addOnChange(m, &st.star, roomRefresh, g.SerializeStarSwitch)
	if g.manip.valid {
		g.addOnChange(m, &st.roomManip, roomRefresh, g.SerializeManip)
	}
	g.addOnChange(m, &st.roomState, roomRefresh, g.SerializeRoomState)
	g.addOnChange(m, &st.roomDoors, roomRefresh, g.SerializeRoomDoors)
	g.addOnChange(m, &st.flyingTiles, roomRefresh, g.SerializeFlyingTiles)
	g.addOnChange(m, &st.roomKills, roomRefresh, g.SerializeRoomKills)
	g.addOnChange(m, &st.rupees, roomRefresh, g.SerializeRupees)
}

// sramSender is what was last sent of an SRAM range that is sent only where it changed.
type sramSender struct {
	last   []byte
	frames int
	// full is set to send all of it next time, e.g. since a player joined:
	full bool
}

// sendSRAMChanges sends the parts of the SRAM ranges that changed since they were last sent, in units of unit bytes
// (a room's flags are 2 bytes). All of it is sent the first time, every sramFullRefresh calls, and when a player
// joined the group: remote players keep what they received, so that is all they miss.
func (g *Game) sendSRAMChanges(ss *sramSender, ranges [][2]uint16, unit int) {
	local := g.local
	ss.frames++
	full := ss.last == nil || ss.frames >= sramFullRefresh || ss.full
	ss.full = false
	if full {
		ss.frames = 0
	}
	m := g.makeBroadcastMessage()
	var last []byte
	o := 0
	for _, r := range ranges {
		start, end := r[0], r[1]
		cur := local.SRAM.data[start:end]
		if full || o+len(cur) > len(ss.last) {
			if err := m.add(func(w io.Writer) error { return g.SerializeSRAM(local, w, start, end) }); err != nil {
				panic(err)
			}
		} else {
			prev := ss.last[o : o+len(cur)]
			// a message costs 6 bytes, so changes up to that far apart go in one:
			const gap = 6
			for i := 0; i < len(cur); {
				if cur[i] == prev[i] {
					i++
					continue
				}
				lo := i - i%unit
				hi := lo + unit
				for j := hi; j < len(cur) && j < hi+gap+unit; j++ {
					if cur[j] != prev[j] {
						hi = j - j%unit + unit
					}
				}
				if hi > len(cur) {
					hi = len(cur)
				}
				a, b := start+uint16(lo), start+uint16(hi)
				if err := m.add(func(w io.Writer) error { return g.SerializeSRAM(local, w, a, b) }); err != nil {
					panic(err)
				}
				i = hi
			}
		}
		last = append(last, cur...)
		o += len(cur)
	}
	ss.last = last
	g.sendIfAny(m)
}

// addLinkSprites adds the sprites sampled this frame to the sector packet m when they, the scroll offsets they were
// drawn with or their palettes changed, or are due for a refresh. Each palette is sent when it changes and every
// paletteRefresh frames, and at once when a player arrived: alttpo keeps the palettes it received.
func (g *Game) addLinkSprites(m *gameBroadcastMessage) {
	s := &g.linkSprites
	st := &g.sendState
	if !s.fresh {
		return
	}
	s.fresh = false

	// debounce frames where Link has no sprites, e.g. when the copy was read while being written
	if len(s.sprites) == 0 {
		s.linkMissingFrames++
		if s.linkMissingFrames < 2 {
			return
		}
	} else {
		s.linkMissingFrames = 0
	}

	st.paletteFrames++
	h := fnv.New64a()
	var used [8]bool
	for _, sp := range s.sprites {
		_, _ = h.Write([]byte{sp.index, sp.b[0], sp.b[1], sp.b[2], sp.b[3], sp.b[4]})
		for _, idx := range sp.absidx {
			_, _ = h.Write([]byte{uint8(idx), uint8(idx >> 8)})
		}
		used[sp.pal] = true
	}
	// receivers place the sprites with the offsets of the last location they received:
	_, _ = h.Write([]byte{uint8(g.local.XOffs), uint8(g.local.XOffs >> 8), uint8(g.local.YOffs), uint8(g.local.YOffs >> 8)})
	for k := range used {
		if used[k] {
			_, _ = h.Write(s.palettes[k][:])
		}
	}
	if !g.changeDue(&st.sprites, h.Sum64(), spritesRefresh) && !st.newPeerHere {
		return
	}

	refresh := st.paletteFrames >= paletteRefresh || st.newPeerHere
	if refresh {
		st.paletteFrames = 0
	}
	var sendPalette [8]bool
	for k := range used {
		if used[k] && (refresh || !st.paletteSent[k] || st.palettes[k] != s.palettes[k]) {
			sendPalette[k] = true
			st.palettes[k], st.paletteSent[k] = s.palettes[k], true
		}
	}

	_ = m.add(func(w io.Writer) error {
		g.SerializeLinkSprites(w, s.sprites, sendPalette)
		return nil
	})
}

func hash64(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}
