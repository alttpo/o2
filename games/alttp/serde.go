package alttp

import (
	"encoding/binary"
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

var deserTable = []DeserializeFunc{
	nil,
	DeserializeLocation,
	DeserializeSfx,
	DeserializeSprites1,
	DeserializeSprites2,
	DeserializeWRAM,
	DeserializeSRAM,
	DeserializeTilemaps,
	DeserializeObjects,
	DeserializeAncillae,
	DeserializeTorches,
	DeserializePvP,
	DeserializePlayerName,
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
	value = uint32(valueLo) | (uint32(valueHi) << 16)
	return
}

func (p *Player) Deserialize(r io.Reader) (err error) {
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

	if err = binary.Read(r, binary.LittleEndian, &p.Team); err != nil {
		panic(err)
	}

	if err = binary.Read(r, binary.LittleEndian, &frame); err != nil {
		panic(err)
	}

	// discard stale frame data:
	nextFrame := int(frame)
	lastFrame := int(p.Frame)
	if lastFrame - nextFrame >= 128 {
		lastFrame -= 256
	}
	if nextFrame < lastFrame {
		log.Println("discard stale frame data")
		return
	}
	p.Frame = frame

	for err != io.EOF {
		// read message type or expect an EOF:
		var msgType MessageType
		if err = binary.Read(r, binary.LittleEndian, &msgType); err != nil {
			log.Println(err)
			return
		}

		// check bounds for message type:
		if msgType == 0 || msgType >= MsgMaxMessageType {
			log.Println("msgType out of bounds")
			// no good recourse to be able to skip over the message
			return
		}

		// call deserializer for the message type:
		log.Printf("deserializing message type %02x\n", msgType)
		if err = deserTable[msgType](p, r); err != nil {
			log.Println(err)
			return
		}
	}

	err = nil
	return
}

func DeserializeLocation(p *Player, r io.Reader) (err error) {
	if err = binary.Read(r, binary.LittleEndian, &p.Module); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}
	if err = binary.Read(r, binary.LittleEndian, &p.SubModule); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}
	if err = binary.Read(r, binary.LittleEndian, &p.SubSubModule); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}
	if p.Location, err = readU24(r); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &p.X); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}
	if err = binary.Read(r, binary.LittleEndian, &p.Y); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &p.Dungeon); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}
	if err = binary.Read(r, binary.LittleEndian, &p.DungeonEntrance); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &p.LastOverworldX); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}
	if err = binary.Read(r, binary.LittleEndian, &p.LastOverworldY); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &p.XOffs); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}
	if err = binary.Read(r, binary.LittleEndian, &p.YOffs); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &p.PlayerColor); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}

	var inSM uint8
	if err = binary.Read(r, binary.LittleEndian, &inSM); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}

	log.Printf("%04x, %04x\n", p.X, p.Y)

	return
}

func DeserializeSfx(p *Player, r io.Reader) (err error) {
	var dummy [2]byte
	_, err = r.Read(dummy[:])
	return
}

func DeserializeSprites1(p *Player, r io.Reader) (err error) {
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

func DeserializeSprites2(p *Player, r io.Reader) (err error) {
	var dummy [1]byte
	if _, err = r.Read(dummy[:]); err != nil {
		panic(fmt.Errorf("error deserializing sprite2: %w", err))
	}
	// TODO: pass in start flag
	return DeserializeSprites1(p, r)
}

func DeserializeWRAM(p *Player, r io.Reader) (err error) {
	var dummy [3]byte
	if _, err = r.Read(dummy[:]); err != nil {
		panic(fmt.Errorf("error deserializing wram: %w", err))
	}
	for i := uint8(0); i < dummy[0]; i++ {
		var syncableByte [4 + 2]byte
		if _, err = r.Read(syncableByte[:]); err != nil {
			panic(fmt.Errorf("error deserializing wram syncableByte %d: %w", i, err))
		}
	}
	return
}

func DeserializeSRAM(p *Player, r io.Reader) (err error) {
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
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing sram: %w", err))
		return
	}
	if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing sram: %w", err))
		return
	}

	if _, err = r.Read(p.SRAM[start : start+count]); err != nil {
		panic(fmt.Errorf("error deserializing sram: %w", err))
	}
	return
}

func DeserializeTilemaps(p *Player, r io.Reader) (err error) {
	var (
		timestamp uint32
		location  uint32
		start     uint8
		length    uint8
	)
	if err = binary.Read(r, binary.LittleEndian, &timestamp); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing tilemaps: %w", err))
		return
	}
	if location, err = readU24(r); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing tilemaps: %w", err))
		return
	}
	_ = location

	if err = binary.Read(r, binary.LittleEndian, &start); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing tilemaps: %w", err))
		return
	}
	if err = binary.Read(r, binary.LittleEndian, &length); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing tilemaps: %w", err))
		return
	}

	for i := uint8(0); i < length; i++ {
		var (
			offs  uint16
			count uint8
		)
		if err = binary.Read(r, binary.LittleEndian, &offs); err != nil {
			// TODO: diagnostics
			panic(fmt.Errorf("error deserializing tilemaps: %w", err))
			return
		}
		if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
			// TODO: diagnostics
			panic(fmt.Errorf("error deserializing tilemaps: %w", err))
			return
		}

		same := (offs & 0x8000) != 0
		if same {
			var tile [3]byte
			if _, err = r.Read(tile[:]); err != nil {
				// TODO: diagnostics
				panic(fmt.Errorf("error deserializing tilemaps: %w", err))
				return
			}
		} else {
			for j := uint8(0); j < count; j++ {
				var tile [3]byte
				if _, err = r.Read(tile[:]); err != nil {
					// TODO: diagnostics
					panic(fmt.Errorf("error deserializing tilemaps: %w", err))
					return
				}
			}
		}
	}

	return
}

func DeserializeObjects(p *Player, r io.Reader) (err error) {
	panic(fmt.Errorf("not implemented"))
	return
}

func DeserializeAncillae(p *Player, r io.Reader) (err error) {
	var count uint8
	if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing ancillae: %w", err))
		return
	}

	for i := uint8(0); i < count; i++ {
		var index uint8
		if err = binary.Read(r, binary.LittleEndian, &index); err != nil {
			// TODO: diagnostics
			panic(fmt.Errorf("error deserializing ancillae: %w", err))
			return
		}
		index = index & 0x7F

		var facts [0x20]byte
		if index < 5 {
			if _, err = r.Read(facts[:0x20]); err != nil {
				// TODO: diagnostics
				panic(fmt.Errorf("error deserializing ancillae: %w", err))
				return
			}
		} else {
			if _, err = r.Read(facts[:0x16]); err != nil {
				// TODO: diagnostics
				panic(fmt.Errorf("error deserializing ancillae: %w", err))
				return
			}
		}
	}

	return
}

func DeserializeTorches(p *Player, r io.Reader) (err error) {
	var (
		count uint8
	)
	if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing torches: %w", err))
		return
	}
	for i := uint8(0); i < count; i++ {
		var torch [2]byte
		if _, err = r.Read(torch[:]); err != nil {
			// TODO: diagnostics
			panic(fmt.Errorf("error deserializing torches: %w", err))
			return
		}
	}
	return
}

func DeserializePvP(p *Player, r io.Reader) (err error) {
	panic(fmt.Errorf("not implemented"))
	return
}

func DeserializePlayerName(p *Player, r io.Reader) (err error) {
	var name [20]byte
	if _, err = r.Read(name[:]); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing name: %w", err))
		return
	}
	p.Name = strings.Trim(string(name[:]), " \t\n\r\000")
	return
}
