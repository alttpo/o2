package util

import (
	"io"
	"log"
	"os"
	"runtime/debug"
)

type PanicSafeLogger struct {
	f  *os.File
	mw io.Writer
}

var std *PanicSafeLogger

func NewPanicSafeLogger(f *os.File) *PanicSafeLogger {
	std = &PanicSafeLogger{
		f:  f,
		mw: io.MultiWriter(f, os.Stderr),
	}
	return std
}

func (l *PanicSafeLogger) Write(p []byte) (n int, err error) {
	return l.mw.Write(p)
}

func (l *PanicSafeLogger) Flush() error {
	return l.f.Sync()
}

func FlushLogger() error {
	if std == nil {
		return nil
	}
	return std.Flush()
}

func LogPanic(err any) {
	log.Printf("paniced with %v\n%s\n", err, string(debug.Stack()))
	_ = FlushLogger()
}
