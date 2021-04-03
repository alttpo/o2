package alttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
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
	if frame < p.Frame {
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

	var locationLo uint8
	if err = binary.Read(r, binary.LittleEndian, &locationLo); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}
	var locationHi uint16
	if err = binary.Read(r, binary.LittleEndian, &locationHi); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error deserializing location: %w", err))
		return
	}
	p.Location = uint32(locationLo) | (uint32(locationHi) << 16)

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

	return
}

func DeserializeSprites1(p *Player, r io.Reader) (err error) {

	return
}

func DeserializeSprites2(p *Player, r io.Reader) (err error) {

	return
}

func DeserializeWRAM(p *Player, r io.Reader) (err error) {

	return
}

func DeserializeSRAM(p *Player, r io.Reader) (err error) {

	return
}

func DeserializeTilemaps(p *Player, r io.Reader) (err error) {

	return
}

func DeserializeObjects(p *Player, r io.Reader) (err error) {

	return
}

func DeserializeAncillae(p *Player, r io.Reader) (err error) {

	return
}

func DeserializeTorches(p *Player, r io.Reader) (err error) {

	return
}

func DeserializePvP(p *Player, r io.Reader) (err error) {

	return
}

func DeserializePlayerName(p *Player, r io.Reader) (err error) {

	return
}
