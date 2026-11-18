package alttp

import (
	"sync"
	"time"
)

// Server clock
//
// The server stamps every message it relays with its clock. Taking that stamp plus the time since the message arrived
// as the server's current time runs late by the message's trip from the server, which differs between players. Instead,
// the offset of the server's clock from ours is estimated from the echoes o2 sends every 500ms, NTP style (see
// RUN_TIMING.md): an echo carries our clock when it was sent and the server's when it passed through, which is about
// half way through the round trip. The echo with the shortest round trip out of the last serverClockSamples gives the
// most accurate offset.
const (
	serverClockSamples = 30
	// echoes that took longer than this are ignored:
	serverClockMaxRTT = 5 * time.Second
)

type clockSample struct {
	rtt, offset time.Duration
}

type serverClock struct {
	// ignore the echoes and use the last message's stamp instead; for tests, whose server clock is simulated:
	lastMessageOnly bool

	mu      sync.Mutex
	samples [serverClockSamples]clockSample
	n, next int
	best    clockSample
}

// echo adds the round trip of an echo sent at sentNs (our clock, Unix ns), stamped by the server at serverNs and
// received at recv.
func (c *serverClock) echo(sentNs, serverNs int64, recv time.Time) {
	if c.lastMessageOnly || sentNs == 0 || serverNs == 0 {
		return
	}
	sent := time.Unix(0, sentNs)
	rtt := recv.Sub(sent)
	if rtt < 0 || rtt > serverClockMaxRTT {
		return
	}
	s := clockSample{rtt: rtt, offset: time.Duration(serverNs - sentNs - int64(rtt/2))}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.samples[c.next] = s
	c.next = (c.next + 1) % serverClockSamples
	if c.n < serverClockSamples {
		c.n++
	}
	c.best = c.samples[0]
	for _, x := range c.samples[1:c.n] {
		if x.rtt < c.best.rtt {
			c.best = x
		}
	}
}

// offset returns the server's clock minus ours, or false before the first echo came back.
func (c *serverClock) offset() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.best.offset, c.n > 0
}

// rtt returns the shortest round trip of the recent echoes.
func (c *serverClock) rtt() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.best.rtt, c.n > 0
}
