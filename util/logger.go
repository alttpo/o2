package util

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
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

// LogPath is the path of the log file, or empty when o2 logs only to stderr (e.g. in tests).
var LogPath string

// WriteLogSibling writes contents to a file next to the log file, named after it with the given suffix in place of
// its extension, and returns its path. It writes nothing and returns "" when there is no log file.
func WriteLogSibling(suffix string, contents []byte) (path string, err error) {
	if LogPath == "" {
		return "", nil
	}
	path = strings.TrimSuffix(LogPath, filepath.Ext(LogPath)) + suffix
	if err = os.WriteFile(path, contents, 0644); err != nil {
		return "", err
	}
	return path, nil
}
