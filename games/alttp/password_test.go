package alttp

import (
	"io"
	"testing"
	"time"

	"o2/client/protocol05"
	"o2/interfaces"
)

// A client's group password: a missing or wrong one is refused with a notification (repeated, and no more joins),
// and the right one joins; changing the password joins again.
func TestGroupPassword(t *testing.T) {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	g := gs.g
	var notes []string
	g.Notifications.Subscribe(interfaces.ObserverImpl(func(o interface{}) { notes = append(notes, o.(string)) }))

	srv := &testServer{Clients: []*testClient{gs.c}}
	srv.hub = protocol05.NewHub(srv.now)
	// another player created the group with the password "pw":
	creator, _ := protocol05.MakeJoin(&protocol05.Join{JoinSeq: 1, SessionID: 99, Group: "test", Name: "creator", Info: []byte{2, 0}, Auth: protocol05.AuthToken("pw")})
	srv.hub.Handle(99, creator)

	exchange := func() {
		srv.HandleAllClients(t)
		gameHandleNet(g)
	}
	now := time.Now()

	// no password: refused, told, and the join isn't resent:
	exchange()
	g.tickNet(now)
	if !g.net.authBlocked || len(notes) != 1 || notes[0] != "this group needs a password" {
		t.Fatalf("blocked=%v notes=%q", g.net.authBlocked, notes)
	}
	now = now.Add(time.Second)
	g.tickNet(now)
	if len(gs.c.Wr) != 0 || len(notes) != 1 {
		t.Fatalf("a refused join was sent again or told again: %d packets, notes %q", len(gs.c.Wr), notes)
	}
	now = now.Add(authNotifyEvery)
	g.tickNet(now)
	if len(notes) != 2 {
		t.Fatalf("not told again: %q", notes)
	}

	// a wrong password:
	gs.c.password = "nope"
	g.tickNet(now)
	if g.net.authBlocked {
		t.Fatalf("a new password didn't unblock")
	}
	exchange()
	g.tickNet(now)
	if !g.net.authBlocked || notes[len(notes)-1] != "wrong group password" {
		t.Fatalf("blocked=%v notes=%q", g.net.authBlocked, notes)
	}

	// the right one:
	gs.c.password = "pw"
	g.tickNet(now)
	exchange()
	if g.net.authBlocked || !g.net.joinAcked || g.local.Index() < 0 {
		t.Fatalf("not joined: blocked=%v acked=%v index=%d", g.net.authBlocked, g.net.joinAcked, g.local.Index())
	}
}
