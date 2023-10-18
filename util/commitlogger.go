package util

import "strings"

type CommitLogger struct {
	Committer func(p string)
	buf       strings.Builder
}

func (l *CommitLogger) Reserve(n int) {
	l.buf.Grow(n)
}

func (l *CommitLogger) Write(p []byte) (n int, err error) {
	return l.buf.Write(p)
}

func (l *CommitLogger) Commit() {
	if l.Committer != nil {
		l.Committer(l.buf.String())
	}
	l.Reset()
}

func (l *CommitLogger) Reset() {
	l.buf.Reset()
}
