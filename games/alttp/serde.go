package alttp

import (
	"encoding/binary"
	"io"
)

// NOTE: increment this when the serialization code changes in an incompatible way
const SerializationVersion = 0x13

type MessageType uint8
const (
	_                    = iota
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
		return
	}

	if serializationVersion != SerializationVersion {
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &p.Team); err != nil {
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &frame); err != nil {
		return
	}
	// discard stale frame data:
	if frame < p.Frame {
		return
	}
	p.Frame = frame

	for err != io.EOF {
		// read message type or expect an EOF:
		var msgType MessageType
		if err = binary.Read(r, binary.LittleEndian, &msgType); err != nil {
			return
		}

		// check bounds for message type:
		if msgType == 0 || msgType >= MsgMaxMessageType {
			// no good recourse to be able to skip over the message
			return
		}

		// call deserializer for the message type:
		if err = deserTable[msgType](p, r); err != nil {
			return
		}
	}

	err = nil
	return
}

func DeserializeLocation(p *Player, r io.Reader) (err error) {

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
