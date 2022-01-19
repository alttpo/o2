package asm

import (
	"strings"
	"testing"
)

func TestEmitter_Label(t *testing.T) {
	a := NewEmitter(make([]byte, 0x100), &strings.Builder{})
	a.Label("loop")
	a.BNE("loop")
	a.Finalize()
	t.Log(a.Text.String())
}
