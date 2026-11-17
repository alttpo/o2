package alttp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"
)

// SerializationVersion is the game ("script") protocol version shared with the alttpo emulator script project
// (see `script_protocol` in alttpo's GameState.as). o2 and alttpo clients can only share game state in the same group
// when this matches.
//
// NOTE: coordinate with alttpo when the serialization code changes in an incompatible way
const SerializationVersion = 0x01

type MessageType uint8

// Message types shared with alttpo; values must match `GameState::deserialize` in alttpo's GameState.as.
const (
	MsgLocation         MessageType = 0x01
	MsgSfx              MessageType = 0x02
	MsgSprites1         MessageType = 0x03
	MsgSprites2         MessageType = 0x04
	MsgWRAM             MessageType = 0x05
	MsgSRAM             MessageType = 0x06
	MsgTilemaps         MessageType = 0x07 // retired; replaced by MsgMap16 and MsgManip, neither project sends it
	MsgObjects          MessageType = 0x08 // reserved; not implemented by either project
	MsgAncillae         MessageType = 0x09
	MsgTorches          MessageType = 0x0A
	MsgPvP              MessageType = 0x0B
	MsgPlayerName       MessageType = 0x0C
	MsgSMEvents         MessageType = 0x0D
	MsgSMSRAM           MessageType = 0x0E
	MsgSMLocation       MessageType = 0x0F
	MsgSMSprite         MessageType = 0x10 // reserved; not implemented by alttpo
	MsgSMEnemies        MessageType = 0x11
	MsgEnemyData        MessageType = 0x12
	MsgEnemySegmentData MessageType = 0x13
	MsgOverlordData     MessageType = 0x14
	MsgUniqTiles        MessageType = 0x15
	MsgNakUniqTiles     MessageType = 0x16
	MsgMap16            MessageType = 0x17 // overworld map16 change log, broadcastSector; see syncmap16.go
	MsgManip            MessageType = 0x18 // underworld manipulable objects, broadcastSector; see syncmanip.go
	MsgStarSwitch       MessageType = 0x19 // star switch state of a room, broadcastSector; see syncstar.go
	MsgRoomState        MessageType = 0x1A // trap doors, chest and key flags of a room, broadcastSector; see syncroom.go
	MsgRoomDoors        MessageType = 0x1B // key doors and switches of a room, broadcastSector; see syncroom.go
	MsgOwEvents         MessageType = 0x1C // overworld entrance animations and other events, broadcastSector; see syncowevents.go
)

// o2-originated message types. alttpo stops parsing a packet at the first message type it does not understand, so
// these are sent in their own packets, separate from message types alttpo uses; see makeO2BroadcastMessage.
const (
	MsgO2RunTimes MessageType = 0x80 // o2-only
	MsgReset      MessageType = 0x81 // o2-only
	MsgSmallKeys  MessageType = 0x82 // also understood and sent by alttpo (SmallKeys.as)
	MsgHearts     MessageType = 0x83 // also understood and sent by alttpo (Hearts.as)
)

type DeserializeFunc func(p *Player, r io.Reader) error

func (g *Game) initSerde() {
	g.deserTable = map[MessageType]DeserializeFunc{
		MsgLocation:         g.DeserializeLocation,
		MsgSfx:              g.DeserializeSfx,
		MsgSprites1:         g.DeserializeSprites1,
		MsgSprites2:         g.DeserializeSprites2,
		MsgWRAM:             g.DeserializeWRAM,
		MsgSRAM:             g.DeserializeSRAM,
		MsgAncillae:         g.DeserializeAncillae,
		MsgTorches:          g.DeserializeTorches,
		MsgPvP:              g.DeserializePvP,
		MsgPlayerName:       g.DeserializePlayerName,
		MsgSMEvents:         skipBytes(0x54 + 2),
		MsgSMSRAM:           skipBytes(0x40),
		MsgSMLocation:       skipBytes(15),
		MsgSMEnemies:        skipSMEnemies,
		MsgEnemyData:        skipEnemyData,
		MsgEnemySegmentData: skipEnemySegmentData,
		MsgOverlordData:     skipOverlordData,
		MsgUniqTiles:        skipUniqTiles,
		MsgNakUniqTiles:     g.DeserializeNakUniqTiles,
		MsgMap16:            g.DeserializeMap16,
		MsgManip:            g.DeserializeManip,
		MsgStarSwitch:       g.DeserializeStarSwitch,
		MsgRoomState:        g.DeserializeRoomState,
		MsgRoomDoors:        g.DeserializeRoomDoors,
		MsgOwEvents:         g.DeserializeOwEvents,
		MsgO2RunTimes:       g.DeserializeRunTimes,
		MsgReset:            g.DeserializeReset,
		MsgSmallKeys:        g.DeserializeSmallKeys,
		MsgHearts:           g.DeserializeHearts,
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
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("alttp: deserialize: %v", rec)
		}
	}()

	var (
		serializationVersion uint8
		frame                uint8
	)

	if err = binary.Read(r, binary.LittleEndian, &serializationVersion); err != nil {
		return
	}

	if serializationVersion != SerializationVersion {
		// ignore players using an incompatible protocol version:
		if g.badVersionLogLimit&0x7F == 0 {
			log.Printf("alttp: ignoring player[%02x] with serialization version %#02x; expected %#02x\n", uint8(p.Index()), serializationVersion, SerializationVersion)
		}
		g.badVersionLogLimit++
		return fmt.Errorf("alttp: serialization version mismatch %#02x != %#02x", serializationVersion, SerializationVersion)
	}

	lastTeam := p.Team
	if err = binary.Read(r, binary.LittleEndian, &p.Team); err != nil {
		return
	}
	if p.Team != lastTeam {
		g.shouldUpdatePlayersList = true
	}

	if err = binary.Read(r, binary.LittleEndian, &frame); err != nil {
		return
	}

	// discard stale frame data:
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

	for err != io.EOF {
		// read message type or expect an EOF:
		var msgType MessageType
		if err = binary.Read(r, binary.LittleEndian, &msgType); err != nil {
			break
		}

		deser, ok := g.deserTable[msgType]
		if !ok {
			// no way to know the length of an unknown message, so the rest of the packet is lost. Newer clients add
			// message types at the end of their packets, so keep what was parsed so far and only log it once per player
			// and message type:
			key := uint32(p.Index())<<8 | uint32(msgType)
			if _, logged := g.unknownMsgLogged[key]; !logged {
				if g.unknownMsgLogged == nil {
					g.unknownMsgLogged = make(map[uint32]struct{})
				}
				g.unknownMsgLogged[key] = struct{}{}
				log.Printf("alttp: player[%02x]: ignoring unknown message type %#02x and the rest of its packet; is it a newer version?\n", uint8(p.Index()), uint8(msgType))
			}
			err = nil
			break
		}

		// while the reset state machine is active, ignore all game state from other players:
		if msgType != MsgReset && g.IsResetting() {
			break
		}

		// call deserializer for the message type:
		if err = deser(p, r); err != nil {
			break
		}
	}

	if errors.Is(err, io.EOF) {
		err = nil
	}
	return
}

func (g *Game) DeserializeLocation(p *Player, r io.Reader) (err error) {
	if err = binary.Read(r, binary.LittleEndian, &p.Module); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &p.SubModule); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &p.SubSubModule); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
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
		// room and area specific state received so far belongs to the previous location:
		p.Star.valid = false
		p.Room.valid = false
		p.Doors.valid = false
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
	if p.Dungeon != lastDungeon {
		g.shouldUpdatePlayersList = true
	}

	if err = binary.Read(r, binary.LittleEndian, &p.DungeonEntrance); err != nil {
		panic(fmt.Errorf("error deserializing location: %w", err))
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

func (g *Game) DeserializeSprites1(p *Player, r io.Reader) (err error) {
	return g.deserializeRemoteSprites(p, r, 0)
}

func (g *Game) DeserializeSprites2(p *Player, r io.Reader) (err error) {
	var start uint8
	if start, err = readU8(r); err != nil {
		return fmt.Errorf("error deserializing sprites continuation: %w", err)
	}
	return g.deserializeRemoteSprites(p, r, int(start))
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

func (g *Game) DeserializePlayerName(p *Player, r io.Reader) (err error) {
	var name [20]byte
	if _, err = io.ReadFull(r, name[:]); err != nil {
		return fmt.Errorf("error deserializing name: %w", err)
	}
	lastName := p.NameF
	p.NameF = strings.Trim(string(name[:]), " \t\n\r\000")
	if lastName != p.NameF {
		p.showJoinMessage = true
		// refresh the players list
		g.shouldUpdatePlayersList = true
	}

	return
}

// DeserializeRunTimes reads the o2-only game start and finish times used for the run timer.
func (g *Game) DeserializeRunTimes(p *Player, r io.Reader) (err error) {
	var isZero bool

	if err = binary.Read(r, binary.LittleEndian, &isZero); err != nil {
		return
	}
	var startTime time.Time
	if !isZero {
		var startNano int64
		if err = binary.Read(r, binary.LittleEndian, &startNano); err != nil {
			return
		}
		startTime = time.Unix(startNano/1e9, startNano%1e9)
	}

	if err = binary.Read(r, binary.LittleEndian, &isZero); err != nil {
		return
	}
	var finishTime time.Time
	if !isZero {
		var finishNano int64
		if err = binary.Read(r, binary.LittleEndian, &finishNano); err != nil {
			return
		}
		finishTime = time.Unix(finishNano/1e9, finishNano%1e9)
	}

	if startTime != p.GameStartTime {
		p.GameStartTime = startTime
		g.shouldUpdatePlayersList = true
	}
	if finishTime != p.GameFinishTime {
		p.GameFinishTime = finishTime
		g.shouldUpdatePlayersList = true
	}

	return
}

// SerializeRunTimes writes the o2-only game start and finish times used for the run timer.
func SerializeRunTimes(w io.Writer, start, finish time.Time) (err error) {
	if err = binary.Write(w, binary.LittleEndian, uint8(MsgO2RunTimes)); err != nil {
		return
	}
	for _, t := range []time.Time{start, finish} {
		if err = binary.Write(w, binary.LittleEndian, t.IsZero()); err != nil {
			return
		}
		if !t.IsZero() {
			if err = binary.Write(w, binary.LittleEndian, t.UnixNano()); err != nil {
				return
			}
		}
	}
	return
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

func skipBytes(n int) DeserializeFunc {
	return func(p *Player, r io.Reader) error {
		return discard(r, n)
	}
}

func skipSMEnemies(p *Player, r io.Reader) (err error) {
	var isEnemy, flag uint8
	if isEnemy, err = readU8(r); err != nil {
		return
	}
	if isEnemy == 34 {
		return
	}
	// enemy index:
	if err = discard(r, 1); err != nil {
		return
	}
	if flag, err = readU8(r); err != nil {
		return
	}
	if flag == 0 {
		return
	}
	// 32 u16 enemy properties, host index, flag:
	return discard(r, 32*2+2)
}

// alttpo: len(enemy_data_ptrs); one byte per property per sprite (two for OWDEATH spanning two properties):
const alttpoEnemyDataProperties = 55

func skipEnemyData(p *Player, r io.Reader) (err error) {
	var mask uint16
	if mask, err = readU16(r); err != nil {
		return
	}
	// in_dungeon:
	if err = discard(r, 1); err != nil {
		return
	}
	for s := 0; s < 16; s++ {
		if mask&(1<<s) == 0 {
			continue
		}
		if err = discard(r, alttpoEnemyDataProperties); err != nil {
			return
		}
	}
	return
}

func skipEnemySegmentData(p *Player, r io.Reader) (err error) {
	var mask uint16
	if mask, err = readU16(r); err != nil {
		return
	}
	for s := 0; s < 16; s++ {
		if mask&(1<<s) == 0 {
			continue
		}
		var id uint8
		if id, err = readU8(r); err != nil {
			return
		}
		n := 0
		switch id {
		case 0xCF: // swamola
			if s < 6 {
				n = 4 * 0x20
			}
		case 0x09: // moldorm
			n = 4 * 0x80
		case 0x18: // mini moldorm
			if s < 4 {
				n = 4 * 0x20
			}
		case 0x54: // lanmolas
			if s < 4 {
				n = 4 * 0x40
			}
		default:
			if s < 8 {
				n = 7 * 0x10
			}
		}
		if err = discard(r, n); err != nil {
			return
		}
	}
	return
}

func skipOverlordData(p *Player, r io.Reader) (err error) {
	var mask uint8
	if mask, err = readU8(r); err != nil {
		return
	}
	for s := 0; s < 8; s++ {
		if mask&(1<<s) == 0 {
			continue
		}
		// 9 u8 properties and 1 u16 property:
		if err = discard(r, 9+2); err != nil {
			return
		}
	}
	return
}

func skipUniqTiles(p *Player, r io.Reader) (err error) {
	var count uint8
	if count, err = readU8(r); err != nil {
		return
	}
	// absidx (u16) and 16 u16 tile words:
	return discard(r, int(count)*(2+32))
}
