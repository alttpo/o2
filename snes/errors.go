package snes

import (
	"errors"
	"fmt"
)

var ErrDeviceDisconnected = errors.New("device disconnected")

type TerminalError struct {
	wrapped error
}

func (e *TerminalError) Unwrap() error { return e.wrapped }
func (e *TerminalError) Error() string {
	if e.wrapped == nil {
		return "snes device terminal error"
	}
	return fmt.Sprintf("snes device terminal error: %v", e.wrapped)
}
