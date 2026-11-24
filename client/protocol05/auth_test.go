package protocol05

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

// the same vector as o2-server's p5auth_test.go:
func TestAuthTokenVector(t *testing.T) {
	if got := hex.EncodeToString(AuthToken("hunter2")); got != "151ea7df512994f88ab9a1389a87f50c0bec6135cf0042fe1ad6ede592d03456" {
		t.Fatalf("token %s", got)
	}
	if AuthToken("") != nil {
		t.Fatal("empty password has a token")
	}
}

func TestJoinAuthRoundTrip(t *testing.T) {
	j := &Join{JoinSeq: 1, SessionID: 5, Group: "g", Name: "n", Info: []byte{2, 0}, Auth: AuthToken("pw")}
	pkt, err := MakeJoin(j)
	if err != nil {
		t.Fatal(err)
	}
	q, err := DecodeRequest(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if q.Join.Group != "g" || !bytes.Equal(q.Join.Auth, j.Auth) || !bytes.Equal(q.Join.Info, j.Info) {
		t.Fatalf("round trip %+v", q.Join)
	}
	// no password: no auth field
	j.Auth = nil
	pkt, _ = MakeJoin(j)
	if q, err = DecodeRequest(pkt); err != nil || q.Join.Auth != nil {
		t.Fatalf("%+v %v", q.Join, err)
	}
}

func TestHubPasswords(t *testing.T) {
	h := NewHub(time.Now)
	join := func(conn int, session uint64, password string) uint8 {
		pkt, err := MakeJoin(&Join{JoinSeq: 1, SessionID: session, Group: "g", Name: "n", Info: []byte{2, 0}, Auth: AuthToken(password)})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range h.Handle(conn, pkt) {
			if d.To != conn {
				continue
			}
			if m, err := Decode(d.Pkt); err == nil {
				switch m.Kind {
				case JoinAck:
					return 0xFF
				case JoinNak:
					return m.Reason
				}
			}
		}
		t.Fatal("no answer")
		return 0
	}
	if r := join(1, 1, "pw"); r != 0xFF {
		t.Fatalf("creator: %d", r)
	}
	if r := join(2, 2, ""); r != NakAuthRequired {
		t.Fatalf("no password: %d", r)
	}
	if r := join(2, 2, "nope"); r != NakAuthFailed {
		t.Fatalf("wrong password: %d", r)
	}
	if r := join(2, 2, "pw"); r != 0xFF {
		t.Fatalf("right password: %d", r)
	}
	if r := join(1, 1, "pw"); r != 0xFF {
		t.Fatalf("resend: %d", r)
	}

	open := NewHub(time.Now)
	h = open
	if r := join(1, 1, ""); r != 0xFF {
		t.Fatalf("open group: %d", r)
	}
	if r := join(2, 2, "pw"); r != NakAuthUnexpected {
		t.Fatalf("password on an open group: %d", r)
	}
}
