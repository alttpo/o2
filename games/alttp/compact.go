package alttp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Compact messages
//
// The location (MsgLocation) and the small keys (MsgSmallKeys) change little from frame to frame, so clients send
// their changes instead (shared with alttpo, see its Compact.as):
//
//   - MsgLocationChanges: `base u8`, `mask u8`, then a u16 for each bit of mask: X ($01), Y ($02), X scroll offset
//     ($04), Y scroll offset ($08). The values differ from those of the full location sent in the packet whose
//     frame number (game header) is base; the other fields are that message's. A full location goes out when any other
//     field changes, every locationKeyRefresh frames, and when a player joins.
//   - MsgSmallKeysChanges: `base u8`, `count u8`, then `k u8`, `net i16`, `value u8`, `ts u32` for each dungeon k
//     that differs from the full small keys sent in the packet with frame number base (session included). Sent
//     when the keys change and every smallKeysRefresh frames, with no dungeons when nothing changed. A full message goes
//     out when more than smallKeysMaxChanges dungeons differ, every smallKeysKeyRefresh frames and when a player joins.
//
// A receiver that doesn't have the base of a change message ignores it until the next full message: since changes are
// relative to the full message, not to the previous change, a lost packet is repaired by the next one.

const (
	locationKeyRefresh  = 60
	smallKeysKeyRefresh = 256
	smallKeysMaxChanges = 2
)

const (
	locChangeX = 1 << iota
	locChangeY
	locChangeXOffs
	locChangeYOffs
)

// compactRemote is the full messages a remote player's change messages are relative to.
type compactRemote struct {
	locOK    bool
	locFrame uint8
	loc      [4]uint16

	skOK      bool
	skFrame   uint8
	skSession uint32
	skNet     [smallKeyCount]int16
	skValue   [smallKeyCount]uint8
	skTs      [smallKeyCount]uint32
}

func locFields(p *Player) [4]uint16 {
	return [4]uint16{p.X, p.Y, uint16(p.XOffs), uint16(p.YOffs)}
}

func (c *compactRemote) noteLocation(p *Player) {
	c.locOK, c.locFrame, c.loc = true, p.Frame, locFields(p)
}

func (c *compactRemote) noteSmallKeys(p *Player, session uint32, net [smallKeyCount]int16, value [smallKeyCount]uint8, ts [smallKeyCount]uint32) {
	c.skOK, c.skFrame, c.skSession = true, p.Frame, session
	c.skNet, c.skValue, c.skTs = net, value, ts
}

func (g *Game) DeserializeLocationChanges(p *Player, r io.Reader) (err error) {
	var hdr [2]uint8
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return fmt.Errorf("error deserializing location changes: %w", err)
	}
	base, mask := hdr[0], hdr[1]
	v := p.Compact.loc
	for i := 0; i < 4; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		if v[i], err = readU16(r); err != nil {
			return fmt.Errorf("error deserializing location changes: %w", err)
		}
	}
	if !p.Compact.locOK || p.Compact.locFrame != base {
		// relative to a full location we didn't get:
		return
	}
	p.X, p.Y, p.XOffs, p.YOffs = v[0], v[1], int16(v[2]), int16(v[3])
	return
}

func (g *Game) DeserializeSmallKeysChanges(p *Player, r io.Reader) (err error) {
	var hdr [2]uint8
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return fmt.Errorf("error deserializing small key changes: %w", err)
	}
	base, count := hdr[0], hdr[1]
	c := &p.Compact
	net, value, ts := c.skNet, c.skValue, c.skTs
	for i := 0; i < int(count); i++ {
		var e struct {
			K     uint8
			Net   int16
			Value uint8
			Ts    uint32
		}
		if err = binary.Read(r, binary.LittleEndian, &e); err != nil {
			return fmt.Errorf("error deserializing small key changes: %w", err)
		}
		if int(e.K) < smallKeyCount {
			net[e.K], value[e.K], ts[e.K] = e.Net, e.Value, e.Ts
		}
	}
	if !c.skOK || c.skFrame != base {
		// relative to full small keys we didn't get:
		return
	}
	g.applySmallKeys(p, c.skSession, net, value, ts)
	return
}

// compactLocal is what was sent of the compact messages.
type compactLocal struct {
	// the full location last sent, the frame of its packet, frames since, and the fields last sent:
	locOK    bool
	locFrame uint8
	locAge   int
	loc      [4]uint16
	locLast  [4]uint16
	locOther []byte

	skOK      bool
	skFrame   uint8
	skAge     int
	skSession uint32
	skNet     [smallKeyCount]int16
	skValue   [smallKeyCount]uint8
	skTs      [smallKeyCount]uint32
	// the changes last sent, and frames since:
	skLast    []byte
	skLastAge int
}

// smallKeysState returns the small key state as sent in message 0x82.
func (g *Game) smallKeysState() (net [smallKeyCount]int16, value [smallKeyCount]uint8, ts [smallKeyCount]uint32) {
	sk := &g.smallKeys
	for k := 0; k < smallKeyCount; k++ {
		net[k] = sk.net[k]
		kv := k
		if k == smallKeyMirror {
			kv = 0
		}
		value[k], ts[k] = sk.observed[kv], sk.ts[kv]
		if !sk.valid[kv] {
			value[k], ts[k] = smallKeyUnknown, 0
		}
	}
	return
}

// addLocationCompact adds the full location or its changes to m, when changed (see the top of this file).
func (g *Game) addLocationCompact(m *gameBroadcastMessage) {
	c := &g.sendState.compact
	local := g.local

	var full, other bytes.Buffer
	_ = g.SerializeLocation(local, &full)
	b := full.Bytes()
	cur := locFields(local)
	// the location message without the fields that changes carry:
	local.X, local.Y, local.XOffs, local.YOffs = 0, 0, 0, 0
	_ = g.SerializeLocation(local, &other)
	local.X, local.Y, local.XOffs, local.YOffs = cur[0], cur[1], int16(cur[2]), int16(cur[3])

	c.locAge++
	if !c.locOK || c.locAge >= locationKeyRefresh || string(other.Bytes()) != string(c.locOther) {
		m.addBytes(b)
		c.locOK, c.locFrame, c.locAge = true, g.lastGameFrame, 0
		c.loc, c.locLast = cur, cur
		c.locOther = append(c.locOther[:0], other.Bytes()...)
		return
	}
	if cur == c.locLast {
		return
	}
	c.locLast = cur
	msg := []byte{uint8(MsgLocationChanges), c.locFrame, 0}
	for i := 0; i < 4; i++ {
		if cur[i] != c.loc[i] {
			msg[2] |= 1 << i
			msg = binary.LittleEndian.AppendUint16(msg, cur[i])
		}
	}
	m.addBytes(msg)
}

// addSmallKeysCompact adds the full small keys or their changes to m, when changed or due (see the top of this file).
func (g *Game) addSmallKeysCompact(m *gameBroadcastMessage) {
	c := &g.sendState.compact
	net, value, ts := g.smallKeysState()

	var changes []byte
	n := 0
	for k := 0; k < smallKeyCount; k++ {
		if net[k] == c.skNet[k] && value[k] == c.skValue[k] && ts[k] == c.skTs[k] {
			continue
		}
		n++
		changes = append(changes, uint8(k), uint8(net[k]), uint8(uint16(net[k])>>8), value[k])
		changes = binary.LittleEndian.AppendUint32(changes, ts[k])
	}

	c.skAge++
	c.skLastAge++
	if !c.skOK || c.skAge >= smallKeysKeyRefresh || n > smallKeysMaxChanges || c.skSession != g.smallKeys.session {
		_ = m.add(g.SerializeSmallKeys)
		c.skOK, c.skFrame, c.skAge, c.skLastAge = true, g.lastGameFrame, 0, 0
		c.skSession, c.skNet, c.skValue, c.skTs = g.smallKeys.session, net, value, ts
		c.skLast = c.skLast[:0]
		return
	}
	if string(changes) == string(c.skLast) && c.skLastAge < smallKeysRefresh {
		return
	}
	c.skLast = append(c.skLast[:0], changes...)
	c.skLastAge = 0
	m.addBytes(append([]byte{uint8(MsgSmallKeysChanges), c.skFrame, uint8(n)}, changes...))
}
