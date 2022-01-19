package asm

import (
	"log"
	"testing"
)

func TestEmitter_LabelBackwards(t *testing.T) {
	a := NewEmitter(make([]byte, 0x100), true)
	a.Label("loop")
	a.BNE("loop")
	a.Finalize()
	if err := a.WriteTextTo(log.Writer()); err != nil {
		t.Error(err)
	}
}
