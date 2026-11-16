package alttp

import (
	"errors"
	"testing"
	"time"
)

func TestReadBackoff(t *testing.T) {
	g := &Game{}
	if g.inReadBackoff(time.Now()) {
		t.Fatal("no backoff before any read fails")
	}

	g.readFinished(errors.New("no game loaded"))
	now := time.Now()
	if !g.inReadBackoff(now) {
		t.Fatal("expected backoff after a failed read")
	}
	if g.inReadBackoff(now.Add(readBackoff)) {
		t.Fatal("expected backoff to expire")
	}

	// identical errors are counted, not logged again:
	g.readFinished(errors.New("no game loaded"))
	g.readFinished(errors.New("no game loaded"))
	if g.readErrRepeats != 2 {
		t.Errorf("readErrRepeats = %d; expected 2", g.readErrRepeats)
	}

	// a different error resets the count:
	g.readFinished(errors.New("other"))
	if g.readErrRepeats != 0 || g.readErr != "other" {
		t.Errorf("unexpected state: %q x%d", g.readErr, g.readErrRepeats)
	}

	g.readFinished(nil)
	if g.readErr != "" || g.readErrRepeats != 0 {
		t.Errorf("expected error state cleared after a successful read")
	}
}
