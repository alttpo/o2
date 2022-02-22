package util

type CommitLogger struct {
	Committer func(p []byte)
	buf       []byte
}

func (l *CommitLogger) Reserve(n int) {
	if cap(l.buf) >= n {
		return
	}

	newbuf := make([]byte, len(l.buf), n)
	copy(newbuf, l.buf)
	l.buf = newbuf
}

func (l *CommitLogger) Write(p []byte) (n int, err error) {
	l.buf = append(l.buf, p...)
	return len(p), nil
}

func (l *CommitLogger) Commit() {
	if l.Committer != nil {
		l.Committer(l.buf)
	}
	l.Reset()
}

func (l *CommitLogger) Reset() {
	l.buf = l.buf[:0]
}
