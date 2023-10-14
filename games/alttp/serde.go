package alttp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
)

// NOTE: increment this when the serialization code changes in an incompatible way
const SerializationVersion = 0x13

type MessageType uint8

const (
	_                       = iota
	MsgLocation MessageType = iota
	MsgSfx
	MsgSprites1
	MsgSprites2
	MsgWRAM
	MsgSRAM
	MsgTilemaps
	MsgObjects
	MsgAncillae
	MsgTorches
	MsgPvP
	MsgPlayerName

	MsgMaxMessageType
)

type DeserializeFunc func(p *Player, r io.Reader) error

func (g *Game) initSerde() {
	g.deserTable = []DeserializeFunc{
		nil,
		g.DeserializeLocation,
		g.DeserializeSfx,
		g.DeserializeSprites1,
		g.DeserializeSprites2,
		g.DeserializeWRAM,
		g.DeserializeSRAM,
		g.DeserializeTilemaps,
		g.DeserializeObjects,
		g.DeserializeAncillae,
		g.DeserializeTorches,
		g.DeserializePvP,
		g.DeserializePlayerName,
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

func (g *Game) Deserialize(r io.Reader, p *Player) (err error) {
	var (
		serializationVersion uint8
		frame                uint8
	)

	if err = binary.Read(r, binary.LittleEndian, &serializationVersion); err != nil {
		panic(err)
	}

	if serializationVersion != SerializationVersion {
		panic(fmt.Errorf("serializationVersion mismatch"))
	}

	lastTeam := p.Team
	if err = binary.Read(r, binary.LittleEndian, &p.Team); err != nil {
		panic(err)
	}
	if p.Team != lastTeam {
		g.shouldUpdatePlayersList = true
	}

	if err = binary.Read(r, binary.LittleEndian, &frame); err != nil {
		panic(err)
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
			//log.Println(err)
			break
		}

		// check bounds for message type:
		if msgType == 0 || msgType >= MsgMaxMessageType {
			err = fmt.Errorf("alttp: msgType %#02x out of bounds", msgType)
			// no good recourse to be able to skip over the message
			break
		}

		// call deserializer for the message type:
		//log.Printf("deserializing message type %02x\n", msgType)
		if err = g.deserTable[msgType](p, r); err != nil {
			//log.Println(err)
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

func (g *Game) DeserializeSfx(p *Player, r io.Reader) (err error) {
	var dummy [2]byte
	_, err = r.Read(dummy[:])
	return
}

func (g *Game) DeserializeSprites1(p *Player, r io.Reader) (err error) {
	var (
		length uint8
	)
	if err = binary.Read(r, binary.LittleEndian, &length); err != nil {
		panic(fmt.Errorf("error deserializing sprites: %w", err))
	}

	for i := uint8(0); i < length; i++ {
		var spr [6]byte
		if _, err = r.Read(spr[:]); err != nil {
			panic(fmt.Errorf("error deserializing sprite %d: %w", i, err))
		}
		if spr[0]&0x80 != 0 {
			// sprite graphics data 4bpp:
			var gfx [32]byte
			if _, err = r.Read(gfx[:]); err != nil {
				panic(fmt.Errorf("error deserializing sprite %d gfx 0: %w", i, err))
			}
			size := (spr[5] >> 1) & 1
			if size != 0 {
				if _, err = r.Read(gfx[:]); err != nil {
					panic(fmt.Errorf("error deserializing sprite %d gfx 1: %w", i, err))
				}
				if _, err = r.Read(gfx[:]); err != nil {
					panic(fmt.Errorf("error deserializing sprite %d gfx 2: %w", i, err))
				}
				if _, err = r.Read(gfx[:]); err != nil {
					panic(fmt.Errorf("error deserializing sprite %d gfx 3: %w", i, err))
				}
			}
		}
		if spr[5]&0x80 != 0 {
			// palette data:
			var pal [32]byte
			if _, err = r.Read(pal[:]); err != nil {
				panic(fmt.Errorf("error deserializing sprite %d palette: %w", i, err))
			}
		}
	}

	return
}

func (g *Game) DeserializeSprites2(p *Player, r io.Reader) (err error) {
	var dummy [1]byte
	if _, err = r.Read(dummy[:]); err != nil {
		panic(fmt.Errorf("error deserializing sprite2: %w", err))
	}
	// TODO: pass in start flag
	return g.DeserializeSprites1(p, r)
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
			}
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
	// something about SM:
	var dummy [2]byte
	if _, err = r.Read(dummy[:]); err != nil {
		panic(fmt.Errorf("error deserializing sram: %w", err))
	}

	var (
		start uint16
		count uint16
	)
	if err = binary.Read(r, binary.LittleEndian, &start); err != nil {
		panic(fmt.Errorf("error deserializing sram: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
		panic(fmt.Errorf("error deserializing sram: %w", err))
	}

	if _, err = r.Read(p.SRAM.data[start : start+count]); err != nil {
		panic(fmt.Errorf("error deserializing sram: %w", err))
	}
	for j := start; j < start+count; j++ {
		p.SRAM.fresh[j] = true
	}
	return
}

func (g *Game) DeserializeTilemaps(p *Player, r io.Reader) (err error) {
	var (
		timestamp uint32
		location  uint32
		start     uint8
		length    uint8
	)
	if err = binary.Read(r, binary.LittleEndian, &timestamp); err != nil {
		panic(fmt.Errorf("error deserializing tilemaps: %w", err))
	}
	if location, err = readU24(r); err != nil {
		panic(fmt.Errorf("error deserializing tilemaps: %w", err))
	}
	_ = location

	if err = binary.Read(r, binary.LittleEndian, &start); err != nil {
		panic(fmt.Errorf("error deserializing tilemaps: %w", err))
	}
	if err = binary.Read(r, binary.LittleEndian, &length); err != nil {
		panic(fmt.Errorf("error deserializing tilemaps: %w", err))
	}

	for i := uint8(0); i < length; i++ {
		var (
			offs  uint16
			count uint8
		)
		if err = binary.Read(r, binary.LittleEndian, &offs); err != nil {
			panic(fmt.Errorf("error deserializing tilemaps: %w", err))
		}
		if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
			panic(fmt.Errorf("error deserializing tilemaps: %w", err))
		}

		same := (offs & 0x8000) != 0
		if same {
			var tile [3]byte
			if _, err = r.Read(tile[:]); err != nil {
				panic(fmt.Errorf("error deserializing tilemaps: %w", err))
			}
		} else {
			for j := uint8(0); j < count; j++ {
				var tile [3]byte
				if _, err = r.Read(tile[:]); err != nil {
					panic(fmt.Errorf("error deserializing tilemaps: %w", err))
				}
			}
		}
	}

	return
}

func (g *Game) DeserializeObjects(p *Player, r io.Reader) (err error) {
	panic(fmt.Errorf("not implemented"))
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

func (g *Game) DeserializeTorches(p *Player, r io.Reader) (err error) {
	var (
		count uint8
	)
	if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
		panic(fmt.Errorf("error deserializing torches: %w", err))
	}
	for i := uint8(0); i < count; i++ {
		var torch [2]byte
		if _, err = r.Read(torch[:]); err != nil {
			panic(fmt.Errorf("error deserializing torches: %w", err))
		}
	}
	return
}

func (g *Game) DeserializePvP(p *Player, r io.Reader) (err error) {
	panic(fmt.Errorf("not implemented"))
}

func (g *Game) DeserializePlayerName(p *Player, r io.Reader) (err error) {
	var name [20]byte
	if _, err = r.Read(name[:]); err != nil {
		panic(fmt.Errorf("error deserializing name: %w", err))
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

	var (
		startIsZero uint8 = 0
		inSM        uint8 = 0
	)
	if start == 0 {
		startIsZero = 1
	}

	if err = binary.Write(w, binary.LittleEndian, &startIsZero); err != nil {
		panic(fmt.Errorf("error serializing sram: %w", err))
	}
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
