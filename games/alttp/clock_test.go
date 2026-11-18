package alttp

import (
	"testing"
	"time"
)

// The offset comes from the echo with the shortest round trip, assuming the server stamped it half way.
func TestServerClock_ShortestRoundTrip(t *testing.T) {
	var c serverClock
	if _, ok := c.offset(); ok {
		t.Fatal("offset before any echo")
	}
	const skew = 3 * time.Second // the server's clock is ahead of ours by this
	t0 := time.Unix(1_700_000_000, 0)
	echo := func(sent time.Time, up, down time.Duration) {
		c.echo(sent.UnixNano(), sent.Add(skew+up).UnixNano(), sent.Add(up+down))
	}
	// asymmetric, slow round trips, then a fast symmetric one, then slow ones again:
	echo(t0, 80*time.Millisecond, 20*time.Millisecond)
	echo(t0.Add(500*time.Millisecond), 10*time.Millisecond, 10*time.Millisecond)
	for i := 2; i < 10; i++ {
		echo(t0.Add(time.Duration(i)*500*time.Millisecond), 30*time.Millisecond, 70*time.Millisecond)
	}
	offset, _ := c.offset()
	if offset != skew {
		t.Errorf("offset %v; expected %v", offset, skew)
	}
	if rtt, _ := c.rtt(); rtt != 20*time.Millisecond {
		t.Errorf("rtt %v; expected 20ms", rtt)
	}

	// the fast echo falls out of the window:
	for i := 10; i < 10+serverClockSamples; i++ {
		echo(t0.Add(time.Duration(i)*500*time.Millisecond), 40*time.Millisecond, 40*time.Millisecond)
	}
	if offset, _ = c.offset(); offset != skew {
		t.Errorf("offset %v after the window moved; expected %v", offset, skew)
	}

	// echoes from the future (the local clock stepped back) or too slow are ignored:
	c.echo(t0.UnixNano(), t0.UnixNano(), t0.Add(-time.Second))
	c.echo(t0.UnixNano(), t0.UnixNano(), t0.Add(serverClockMaxRTT+time.Second))
	if offset, _ = c.offset(); offset != skew {
		t.Errorf("offset %v after bad echoes; expected %v", offset, skew)
	}
}
