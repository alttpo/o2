package snes

import (
	"encoding/hex"
	"testing"
)

func TestNewROM(t *testing.T) {
	contents := make([]byte, 0x8000)
	_, err := hex.Decode(
		contents[0x7FB0:],
		[]byte("018d2401e2306bffffffffffffffffff544845204c4547454e44204f46205a454c4441202020020a03010100f2500dafffffffff2c82ffff2c82c9800080d882"),
	)
	if err != nil {
		t.Fatal(err)
	}

	gotR, err := NewROM(contents)
	if err != nil {
		t.Fatal(err)
	}

	// check:
	if gotR.Header.MakerCode != 0x8D01 {
		t.Fatal("MakerCode")
	}
	if gotR.Header.GameCode != 0x30E20124 {
		t.Fatal("GameCode")
	}
}
