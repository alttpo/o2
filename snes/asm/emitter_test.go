package asm

import (
	"log"
	"testing"
)

func TestEmitter_LabelBackwards(t *testing.T) {
	a := NewEmitter(make([]byte, 0x100), true)
	a.Label("loop")
	a.BNE("loop")
	if err := a.Finalize(); err != nil {
		t.Error(err)
	}
	if err := a.WriteTextTo(log.Writer()); err != nil {
		t.Error(err)
	}
}

func TestEmitter_LabelForwards_InRange(t *testing.T) {
	a := NewEmitter(make([]byte, 0x100), true)
	a.SEP(0x30)
	a.LDA_imm8_b(0x01)
	a.BNE("next")
	a.RTS()
	a.Label("next")
	a.CMP_imm8_b(0x02)
	a.RTS()
	if err := a.Finalize(); err != nil {
		t.Error(err)
	}
	if err := a.WriteTextTo(log.Writer()); err != nil {
		t.Error(err)
	}
}

func TestEmitter_LabelForwards_NoRef(t *testing.T) {
	a := NewEmitter(make([]byte, 0x100), true)
	a.SEP(0x30)
	a.LDA_imm8_b(0x01)
	a.BNE("next")
	a.RTS()
	a.Label("next")
	a.CMP_imm8_b(0x02)
	a.BNE("next2")
	a.RTS()
	err := a.Finalize()
	expectedErrStr := "could not resolve label 'next2'"
	if err == nil || err.Error() != expectedErrStr {
		t.Errorf("Finalize() error=%v, want=%v", err.Error(), expectedErrStr)
	}
	if err := a.WriteTextTo(log.Writer()); err != nil {
		t.Error(err)
	}
}

func TestEmitter_LabelForwards_OutOfRange(t *testing.T) {
	a := NewEmitter(make([]byte, 0x100), true)
	a.SEP(0x30)
	a.LDA_imm8_b(0x01)
	a.BNE("next")
	a.RTS()
	for i := 0; i < 127; i++ {
		a.NOP()
	}
	a.Label("next")
	a.CMP_imm8_b(0x02)
	a.RTS()
	err := a.Finalize()
	expectedErrStr := "branch from 0x000006 to 0x000086 too far for signed 8-bit; diff=128"
	if err == nil || err.Error() != expectedErrStr {
		t.Errorf("Finalize() error=%v, want=%v", err.Error(), expectedErrStr)
	}
	if err := a.WriteTextTo(log.Writer()); err != nil {
		t.Error(err)
	}
}
