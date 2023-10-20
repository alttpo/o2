package util

import (
	"bytes"
)

type CommitLogger struct {
	Committer func(p []byte)
	buf       bytes.Buffer
}

func (l *CommitLogger) Reserve(n int) {
	l.buf.Grow(n)
}

func (l *CommitLogger) Write(p []byte) (n int, err error) {
	return l.buf.Write(p)
}

func (l *CommitLogger) Commit() {
	if l.Committer != nil {
		l.buf.WriteByte('\n')
		l.Committer(l.buf.Bytes())
	}
	l.Reset()
}

func (l *CommitLogger) Reset() {
	l.buf.Reset()
}
