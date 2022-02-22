package util

import (
	"testing"
	"unsafe"
)

func NewTestingLogger(tb testing.TB) *CommitLogger {
	return &CommitLogger{
		Committer: func(p []byte) {
			line := *(*string)(unsafe.Pointer(&p))
			tb.Log(line)
		},
		buf: nil,
	}
}
