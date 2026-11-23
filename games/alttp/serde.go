package alttp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
)

// SerializationVersion is the game ("script") protocol version shared with the alttpo emulator script project
// (see `script_protocol` in alttpo's GameState.as). Players send it in the info of their join-group (see joinInfo), and
// players with another version are shown as incompatible and their messages are dropped.
//
// Game protocol v2 (see o2-server's PROTOCOL05.md): a packet is `u8 frame`, then messages of `u8 type, length,
// body`; the length is 1 byte below 0x80, or 2 bytes (0x80 | low 7 bits, then the high 8 bits). Receivers skip
// message types they don't know or implement by their length.
//
// NOTE: coordinate with alttpo when the serialization code changes in an incompatible way
const SerializationVersion = 0x02

type MessageType uint8

// Message types shared with alttpo; values must match `GameState::deserialize` in alttpo's GameState.as. o2 doesn't
// implement the SM and enemy data messages and skips them by their length.
const (
	MsgLocation         MessageType = 0x01
	MsgLocationChanges  MessageType = 0x02 // changes since the last MsgLocation; see compact.go
	MsgSfx              MessageType = 0x03
	MsgSprites          MessageType = 0x04
	MsgSpriteChrs       MessageType = 0x05 // binds Link's chrs to tiles of the Link graphics fact; see sprites.go
	MsgSpritePalettes   MessageType = 0x06 // the sprite palettes the sprites use; see sprites.go
	MsgWRAM             MessageType = 0x07
	MsgSRAM             MessageType = 0x08
	MsgAncillae         MessageType = 0x09
	MsgTorches          MessageType = 0x0A
	MsgPvP              MessageType = 0x0B
	MsgMap16            MessageType = 0x0C // overworld map16 change log; see syncmap16.go
	MsgManip            MessageType = 0x0D // underworld manipulable objects; see syncmanip.go
	MsgStarSwitch       MessageType = 0x0E // star switch state of a room; see syncstar.go
	MsgRoomState        MessageType = 0x0F // trap doors, chest and key flags of a room; see syncroom.go
	MsgRoomDoors        MessageType = 0x10 // key doors and switches of a room; see syncroom.go
	MsgOwEvents         MessageType = 0x11 // overworld entrance animations and other events; see syncowevents.go
	MsgFlyingTiles      MessageType = 0x12 // tile room overlord state of a room; see synctiles.go
	MsgRoomKills        MessageType = 0x13 // kill triggers fired in a room; see syncroomkills.go
	MsgEnemiesKilled    MessageType = 0x14 // enemies killed in a room or area; see syncenemieskilled.go
	MsgRupees           MessageType = 0x15 // rupees of rupee floors deleted in a room; see syncrupees.go
	MsgEnemyData        MessageType = 0x16 // alttpo only
	MsgEnemySegmentData MessageType = 0x17 // alttpo only
	MsgOverlordData     MessageType = 0x18 // alttpo only
	MsgSmallKeys        MessageType = 0x19 // see SmallKeys.as
	MsgSmallKeysChanges MessageType = 0x1A // changes since the last MsgSmallKeys; see compact.go
	MsgHearts           MessageType = 0x1B // see Hearts.as
	MsgReset            MessageType = 0x1C // group reset; see reset.go
	MsgRunTiming        MessageType = 0x1D // see RUN_TIMING.md and runtiming.go
	MsgRunRestore       MessageType = 0x1E // gives a player back the run it lost; see runrestore.go
	MsgRunGap           MessageType = 0x1F // a player's lost connection; see runrestore.go
	MsgSMLocation       MessageType = 0x20 // alttpo only
	MsgSMEvents         MessageType = 0x21 // alttpo only
	MsgSMSRAM           MessageType = 0x22 // alttpo only
	MsgSMEnemies        MessageType = 0x23 // alttpo only
)

// maxMsgLen is the longest message body the 2-byte length can express:
const maxMsgLen = 0x7FFF

// appendMsgLen appends a message length: 1 byte below 0x80, else 2 bytes.
func appendMsgLen(b []byte, n int) []byte {
	if n < 0x80 {
		return append(b, uint8(n))
	}
	return append(b, 0x80|uint8(n&0x7F), uint8(n>>7))
}

// readMsgLen reads a message length from b, returning it and the bytes it took.
func readMsgLen(b []byte) (n int, w int, ok bool) {
	if len(b) < 1 {
		return 0, 0, false
	}
	if b[0] < 0x80 {
		return int(b[0]), 1, true
	}
	if len(b) < 2 {
		return 0, 0, false
	}
	return int(b[0]&0x7F) | int(b[1])<<7, 2, true
}

// frameMessage returns the message msg (type byte, then body) with its length inserted after the type.
func frameMessage(msg []byte) []byte {
	if len(msg) == 0 {
		return nil
	}
	b := append([]byte{msg[0]}, appendMsgLen(nil, len(msg)-1)...)
	return append(b, msg[1:]...)
}

type DeserializeFunc func(p *Player, r io.Reader) error

func (g *Game) initSerde() {
	g.deserTable = map[MessageType]DeserializeFunc{
		MsgLocation:         g.DeserializeLocation,
		MsgSfx:              g.DeserializeSfx,
		MsgSprites:          g.DeserializeSprites,
		MsgSpriteChrs:       g.DeserializeSpriteChrs,
		MsgSpritePalettes:   g.DeserializeSpritePalettes,
		MsgWRAM:             g.DeserializeWRAM,
		MsgSRAM:             g.DeserializeSRAM,
		MsgAncillae:         g.DeserializeAncillae,
		MsgTorches:          g.DeserializeTorches,
		MsgPvP:              g.DeserializePvP,
		MsgMap16:            g.DeserializeMap16,
		MsgManip:            g.DeserializeManip,
		MsgStarSwitch:       g.DeserializeStarSwitch,
		MsgRoomState:        g.DeserializeRoomState,
		MsgRoomDoors:        g.DeserializeRoomDoors,
		MsgOwEvents:         g.DeserializeOwEvents,
		MsgFlyingTiles:      g.DeserializeFlyingTiles,
		MsgRoomKills:        g.DeserializeRoomKills,
		MsgEnemiesKilled:    g.DeserializeEnemiesKilled,
		MsgRupees:           g.DeserializeRupees,
		MsgReset:            g.DeserializeReset,
		MsgSmallKeys:        g.DeserializeSmallKeys,
		MsgHearts:           g.DeserializeHearts,
		MsgRunTiming:        g.DeserializeRunTiming,
		MsgRunRestore:       g.DeserializeRunRestore,
		MsgRunGap:           g.DeserializeRunGap,
		MsgSmallKeysChanges: g.DeserializeSmallKeysChanges,
		MsgLocationChanges:  g.DeserializeLocationChanges,
	}
}

func readU24(r io.Reader) (value uint32, err error) {
	var valueLo uint8
	if err = binary.Read(r, binary.LittleEndian, &valueLo); err != nil {
		return
	}
	var valueHi uint16
	if err = binary.Read(r, binary.LittleEndian, &valueHi); err != nil {
		return
	}
	value = uint32(valueLo) | (uint32(valueHi) << 8)
	return
}

func writeU24(w io.Writer, value uint32) (err error) {
	var valueLo uint8 = uint8(value & 0xFF)
	if err = binary.Write(w, binary.LittleEndian, &valueLo); err != nil {
		return
	}
	var valueHi uint16 = uint16((value >> 8) & 0xFFFF)
	if err = binary.Write(w, binary.LittleEndian, &valueHi); err != nil {
		return
	}
	return
}

// Deserialize parses a game packet from another player (o2 or alttpo). Malformed or incompatible packets are
// rejected with an error rather than a panic.
func (g *Game) Deserialize(r io.Reader, p *Player) (err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return
	}
	if len(data) < 1 {
		return fmt.Errorf("alttp: deserialize: empty packet")
	}

	// discard stale frame data:
	frame := data[0]
	nextFrame := int(frame)
	lastFrame := int(p.Frame)
	if lastFrame-nextFrame >= 128 {
		lastFrame -= 256
	}
	if nextFrame < lastFrame {
		log.Printf("alttp: discard stale frame data (%d < %d)\n", nextFrame, lastFrame)
		return
	}
	p.Frame = frame

	for o := 1; o < len(data); {
		msgType := MessageType(data[o])
		o++
		n, w, ok := readMsgLen(data[o:])
		if !ok || o+w+n > len(data) {
			return fmt.Errorf("alttp: deserialize: message %#02x truncated", uint8(msgType))
		}
		o += w
		body := data[o : o+n]
		o += n

		deser, ok := g.deserTable[msgType]
		if !ok {
			// a message type o2 doesn't implement (SM, enemy data) or a newer one; skipped by its length:
			key := uint32(p.Index())<<8 | uint32(msgType)
			if _, logged := g.unknownMsgLogged[key]; !logged {
				if g.unknownMsgLogged == nil {
					g.unknownMsgLogged = make(map[uint32]struct{})
				}
				g.unknownMsgLogged[key] = struct{}{}
				log.Printf("alttp: player[%02x]: skipping message type %#02x\n", uint8(p.Index()), uint8(msgType))
			}
			continue
		}

		// while the reset state machine is active, ignore all game state from other players:
		if msgType != MsgReset && g.IsResetting() {
			continue
		}

		// a message that fails to deserialize doesn't take the rest of the packet with it:
		if merr := g.deserializeOne(deser, p, msgType, body); merr != nil {
			log.Printf("alttp: player[%02x]: message %#02x: %v\n", uint8(p.Index()), uint8(msgType), merr)
			if err == nil {
				err = merr
			}
		}
	}
	return
}

// deserializeOne calls a message's deserializer on its body.
func (g *Game) deserializeOne(deser DeserializeFunc, p *Player, msgType MessageType, body []byte) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%v", rec)
		}
	}()
	r := bytes.NewReader(body)
	err = deser(p, r)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		// the deserializer wanted more than the message's length:
		err = fmt.Errorf("read past the end of the message (%d bytes): %w", len(body), err)
	} else if err == nil && g.strictMessages && r.Len() > 0 {
		// a newer client may append fields to a message, so this is only an error in tests:
		err = fmt.Errorf("%d of %d bytes not read", r.Len(), len(body))
	}
	return
}

func (g *Game) DeserializeLocation(p *Player, r io.Reader) (err error) {
	defer func() {
		if err == nil {
			// the base of the location changes that follow (see compact.go):
			p.Compact.noteLocation(p)
		}
	}()
	lastModule := p.Module
	if err = binary.Read(r, binary.LittleEndian, &p.Module); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &p.SubModule); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &p.SubSubModule); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	p.RunVerify.observeLocation(p)
	if p.Module == 0x07 && lastModule != 0x07 && lastModule != 0x0E {
		// entering a dungeon resets its crystal switches; the state last received is of an earlier visit, and players
		// only send it while in the dungeon:
		delete(p.WRAM, crystalSwitchOffs)
	}

	lastLocation := p.Location
	if p.Location, err = readU24(r); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}

	// decode location and assign DungeonRoom or OverworldArea:
	if p.Location&(1<<16) != 0 {
		p.DungeonRoom = uint16(p.Location & 0xFFFF)
	} else {
		p.OverworldArea = uint16(p.Location & 0xFFFF)
	}

	if p.Location != lastLocation {
		g.shouldUpdatePlayersList = true
		if p.Location&(1<<16) != 0 {
			log.Printf("alttp: player '%s': entered room $%03x\n", p.Name(), p.DungeonRoom)
		} else {
			log.Printf("alttp: player '%s': entered area $%02x\n", p.Name(), p.OverworldArea)
		}
		// room and area specific state received so far belongs to the previous location:
		p.Star.valid = false
		p.FlyingTiles.valid = false
		p.Rupees.valid = false
		p.Room.valid = false
		p.Doors.valid = false
		p.Kills.valid = false
		p.Manip.reset()
		p.Torches.valid = false
		p.Map16.reset(0)
		p.OwEvents.valid = false
	}

	if err = binary.Read(r, binary.LittleEndian, &p.X); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &p.Y); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}

	lastDungeon := p.Dungeon
	if err = binary.Read(r, binary.LittleEndian, &p.Dungeon); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &p.DungeonEntrance); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	if p.Dungeon != lastDungeon {
		g.shouldUpdatePlayersList = true
		log.Printf("alttp: player '%s': entered %s\n", p.Name(), describeDungeon(p.Dungeon, p.DungeonEntrance))
	}

	if err = binary.Read(r, binary.LittleEndian, &p.LastOverworldX); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &p.LastOverworldY); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}

	if err = binary.Read(r, binary.LittleEndian, &p.XOffs); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &p.YOffs); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}

	lastColor := p.PlayerColor
	if err = binary.Read(r, binary.LittleEndian, &p.PlayerColor); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	if p.PlayerColor != lastColor {
		g.shouldUpdatePlayersList = true
	}

	var inSM uint8
	if err = binary.Read(r, binary.LittleEndian, &inSM); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}

	//log.Printf("[%02x]: %04x, %04x\n", uint8(p.Index), p.X, p.Y)

	return
}

func (g *Game) DeserializeWRAM(p *Player, r io.Reader) (err error) {
	var count uint8
	var offsStart uint16

	if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
		panic(fmt.Errorf("error deserializing wram: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &offsStart); err != nil {
		panic(fmt.Errorf("error deserializing wram: %w", err))
	}

	if count > 0 && p.WRAM == nil {
		p.WRAM = make(map[uint16]*SyncableWRAM)
	}

	for i := uint8(0); i < count; i++ {
		var timestamp uint32
		var value uint16
		if err = binary.Read(r, binary.LittleEndian, &timestamp); err != nil {
			panic(fmt.Errorf("error deserializing wram: %w", err))
		}
		if err = binary.Read(r, binary.LittleEndian, &value); err != nil {
			panic(fmt.Errorf("error deserializing wram: %w", err))
		}

		offs := offsStart + uint16(i)
		w, ok := p.WRAM[offs]
		if !ok {
			w = &SyncableWRAM{
				g:         g,
				Offset:    uint32(offs),
				Name:      fmt.Sprintf("wram[$%04x]", offs),
				Size:      2,
				Timestamp: timestamp,
				Value:     value,
				Fresh:     new(bool),
			}
			*w.Fresh = true
			p.WRAM[offs] = w
		} else {
			w.PreviousValue = w.Value
			w.PreviousTimestamp = w.Timestamp
			w.Timestamp = timestamp
			w.Value = value
		}
	}

	return
}

func (g *Game) DeserializeSRAM(p *Player, r io.Reader) (err error) {
	var (
		inSM  uint8
		start uint16
		count uint16
	)
	if err = binary.Read(r, binary.LittleEndian, &inSM); err != nil {
		return fmt.Errorf("error deserializing sram: %w", err)
	}
	if err = binary.Read(r, binary.LittleEndian, &start); err != nil {
		return fmt.Errorf("error deserializing sram: %w", err)
	}
	if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
		return fmt.Errorf("error deserializing sram: %w", err)
	}

	data := make([]byte, count)
	if _, err = io.ReadFull(r, data); err != nil {
		return fmt.Errorf("error deserializing sram: %w", err)
	}

	// only keep what fits in our SRAM shadow:
	for i, b := range data {
		j := int(start) + i
		if j >= len(p.SRAM.data) {
			break
		}
		p.SRAM.data[j] = b
		p.SRAM.fresh[j] = true
	}
	return
}

func (g *Game) DeserializeObjects(p *Player, r io.Reader) (err error) {
	return fmt.Errorf("objects message not implemented")
}

func (g *Game) DeserializeAncillae(p *Player, r io.Reader) (err error) {
	var count uint8
	if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
		panic(fmt.Errorf("error deserializing ancillae: %w", err))
	}

	for i := uint8(0); i < count; i++ {
		var index uint8
		if err = binary.Read(r, binary.LittleEndian, &index); err != nil {
			panic(fmt.Errorf("error deserializing ancillae: %w", err))
		}
		index = index & 0x7F

		var facts [0x20]byte
		if index < 5 {
			if _, err = r.Read(facts[:0x20]); err != nil {
				panic(fmt.Errorf("error deserializing ancillae: %w", err))
			}
		} else {
			if _, err = r.Read(facts[:0x16]); err != nil {
				panic(fmt.Errorf("error deserializing ancillae: %w", err))
			}
		}
	}

	return
}

// DeserializePvP skips over alttpo's PvP state which o2 does not implement.
func (g *Game) DeserializePvP(p *Player, r io.Reader) (err error) {
	var active uint8
	if active, err = readU8(r); err != nil {
		return
	}
	if active == 1 {
		// hitbox x, y (u16), w, h, sword time, item used:
		if err = discard(r, 8); err != nil {
			return
		}
	}
	// sword type, room level:
	if err = discard(r, 2); err != nil {
		return
	}
	var count uint8
	if count, err = readU8(r); err != nil {
		return
	}
	// attacks: player index (u16), sword time, melee item, ancilla mode, damage, recoil dx, dy, dz:
	return discard(r, int(count)*9)
}

func (g *Game) SerializeLocation(p *Player, w io.Writer) (err error) {
	if err = binary.Write(w, binary.LittleEndian, uint8(MsgLocation)); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}

	if err = binary.Write(w, binary.LittleEndian, &p.Module); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}
	if err = binary.Write(w, binary.LittleEndian, &p.SubModule); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}
	if err = binary.Write(w, binary.LittleEndian, &p.SubSubModule); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}
	if err = writeU24(w, p.Location); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}

	if err = binary.Write(w, binary.LittleEndian, &p.X); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}
	if err = binary.Write(w, binary.LittleEndian, &p.Y); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}

	if err = binary.Write(w, binary.LittleEndian, &p.Dungeon); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}
	if err = binary.Write(w, binary.LittleEndian, &p.DungeonEntrance); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}

	if err = binary.Write(w, binary.LittleEndian, &p.LastOverworldX); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}
	if err = binary.Write(w, binary.LittleEndian, &p.LastOverworldY); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}

	if err = binary.Write(w, binary.LittleEndian, &p.XOffs); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}
	if err = binary.Write(w, binary.LittleEndian, &p.YOffs); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}

	if err = binary.Write(w, binary.LittleEndian, &p.PlayerColor); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}

	var inSM uint8 = 0
	if err = binary.Write(w, binary.LittleEndian, &inSM); err != nil {
		panic(fmt.Errorf("error serializing location: %w", err))
	}

	return
}

func (g *Game) SerializeSRAM(p *Player, w io.Writer, start, endExclusive uint16) (err error) {
	if err = binary.Write(w, binary.LittleEndian, uint8(MsgSRAM)); err != nil {
		panic(fmt.Errorf("error serializing sram: %w", err))
	}

	// alttpo format: in_sm flag (always 0 for o2):
	var inSM uint8 = 0
	if err = binary.Write(w, binary.LittleEndian, &inSM); err != nil {
		panic(fmt.Errorf("error serializing sram: %w", err))
	}

	if err = binary.Write(w, binary.LittleEndian, &start); err != nil {
		panic(fmt.Errorf("error serializing sram: %w", err))
	}
	count := endExclusive - start
	if err = binary.Write(w, binary.LittleEndian, &count); err != nil {
		panic(fmt.Errorf("error serializing sram: %w", err))
	}

	if _, err = w.Write(p.SRAM.data[start:endExclusive]); err != nil {
		panic(fmt.Errorf("error serializing sram: %w", err))
	}
	return
}

func (g *Game) SerializeWRAM(p *Player, w io.Writer, start uint16, count uint8) (err error) {
	if err = binary.Write(w, binary.LittleEndian, uint8(MsgWRAM)); err != nil {
		panic(fmt.Errorf("error serializing wram: %w", err))
	}

	if err = binary.Write(w, binary.LittleEndian, &count); err != nil {
		panic(fmt.Errorf("error serializing wram: %w", err))
	}
	if err = binary.Write(w, binary.LittleEndian, &start); err != nil {
		panic(fmt.Errorf("error serializing wram: %w", err))
	}

	for offs := start; offs < start+uint16(count); offs++ {
		wv, ok := p.WRAM[offs]
		var timestamp uint32 = 0
		var value uint16 = 0
		if ok {
			timestamp = wv.Timestamp
			value = wv.Value
		}

		if err = binary.Write(w, binary.LittleEndian, &timestamp); err != nil {
			panic(fmt.Errorf("error serializing wram: %w", err))
		}
		if err = binary.Write(w, binary.LittleEndian, &value); err != nil {
			panic(fmt.Errorf("error serializing wram: %w", err))
		}
	}

	return
}

func readU8(r io.Reader) (v uint8, err error) {
	err = binary.Read(r, binary.LittleEndian, &v)
	return
}

func discard(r io.Reader, n int) (err error) {
	if n <= 0 {
		return
	}
	_, err = io.CopyN(io.Discard, r, int64(n))
	return
}

func readU16(r io.Reader) (v uint16, err error) {
	err = binary.Read(r, binary.LittleEndian, &v)
	return
}

// The following skip over alttpo-only messages (Super Metroid / SMZ3 state, enemy sync and unique sprite tiles) so
// that the rest of the packet can still be read. Formats follow alttpo's GameState.as deserialize_* functions.

// alttpo: len(enemy_data_ptrs); one byte per property per sprite (two for OWDEATH spanning two properties):
const alttpoEnemyDataProperties = 55
