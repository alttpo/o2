package protocol01

import (
	"encoding/binary"
	"io"
	"log"
)

type Header struct {
	Group      string
	Name       string
	ClientType uint8
}

func readTinyString(r io.Reader) (value string, err error) {
	var valueLength uint8
	if err = binary.Read(r, binary.LittleEndian, &valueLength); err != nil {
		return
	}

	valueBytes := make([]byte, valueLength)
	var n int
	n, err = r.Read(valueBytes)
	if err != nil {
		return
	}
	if n < int(valueLength) {
		return
	}

	value = string(valueBytes)
	return
}

func Parse(r io.Reader, header *Header) (err error) {
	header.Group, err = readTinyString(r)
	if err != nil {
		log.Print(err)
		return
	}

	header.Name, err = readTinyString(r)
	if err != nil {
		log.Print(err)
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &header.ClientType); err != nil {
		log.Print(err)
		return
	}

	return
}
