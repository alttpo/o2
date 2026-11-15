package protocol04

import (
	"bytes"
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	cases := []GroupMessage{
		{Group: "mygroup", PlayerTime: -12345, ServerTime: 987654321, PlayerIndex: 3, PlayerInSector: 0x1122, Kind: JoinGroup},
		{Group: "g", PlayerTime: 1, Kind: BroadcastAll, Data: []byte{1, 2, 3}},
		{Group: "", Kind: BroadcastSector, TargetSector: 0xDEADBEEFCAFE, Data: []byte{}},
		{Group: "echo", Kind: Echo, Data: bytes.Repeat([]byte{0xAB}, 300)},
	}
	for _, want := range cases {
		b, err := Append(nil, &want)
		if err != nil {
			t.Fatalf("%v: marshal: %v", want.Kind, err)
		}
		var got GroupMessage
		if err := Unmarshal(b, &got); err != nil {
			t.Fatalf("%v: unmarshal: %v", want.Kind, err)
		}
		if len(want.Data) == 0 {
			want.Data, got.Data = nil, nil
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("%v: mismatch:\nwant %+v\n got %+v", want.Kind, want, got)
		}

		// every strict prefix must fail:
		for n := 0; n < len(b); n++ {
			if err := Unmarshal(b[:n], &got); err == nil {
				t.Fatalf("%v: expected error for %d/%d bytes", want.Kind, n, len(b))
			}
		}

		// trailing bytes are ignored:
		if err := Unmarshal(append(b, 0xFF, 0xFF), &got); err != nil {
			t.Fatalf("%v: trailing bytes: %v", want.Kind, err)
		}
	}
}

// TestWireFormat matches o2-server's TestP04WireFormat byte for byte.
func TestWireFormat(t *testing.T) {
	gm := GroupMessage{
		Group:          "ab",
		PlayerTime:     0x0102030405060708,
		ServerTime:     0x1112131415161718,
		PlayerIndex:    0x21222324,
		PlayerInSector: 0x3132333435363738,
		Kind:           BroadcastSector,
		TargetSector:   0x4142434445464748,
		Data:           []byte{0xEE, 0xFF},
	}
	got, err := MakePacket(&gm)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x1F, 0x65, 0x04, // header + protocol
		0x02, 'a', 'b',
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
		0x18, 0x17, 0x16, 0x15, 0x14, 0x13, 0x12, 0x11,
		0x24, 0x23, 0x22, 0x21,
		0x38, 0x37, 0x36, 0x35, 0x34, 0x33, 0x32, 0x31,
		0x03,
		0x48, 0x47, 0x46, 0x45, 0x44, 0x43, 0x42, 0x41,
		0x02, 0x00, 0xEE, 0xFF,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("wire mismatch:\nwant % x\n got % x", want, got)
	}
}

func TestUnknownKind(t *testing.T) {
	b, _ := Append(nil, &GroupMessage{Kind: JoinGroup})
	b[len(b)-1] = 0x7F
	if err := Unmarshal(b, &GroupMessage{}); err != ErrUnknownKind {
		t.Fatalf("expected ErrUnknownKind, got %v", err)
	}
}
