package alttp

import (
	"context"
	"github.com/alttpo/snes/emulator"
	"google.golang.org/protobuf/proto"
	"io"
	"log"
	"o2/client"
	"o2/client/protocol03"
	"o2/snes"
	"reflect"
	"testing"
	"time"
)

type testClient struct {
	Rd chan []byte
	Wr chan []byte
}

func (t *testClient) Group() []byte {
	return []byte("test")
}

func (t *testClient) IsConnected() bool {
	return true
}

func (t *testClient) Write() chan<- []byte {
	return t.Wr
}

func (t *testClient) Read() <-chan []byte {
	return t.Rd
}

type testServer struct {
	Clients []*testClient
}

func (t *testServer) Run(ctx context.Context) (err error) {
	cases := make([]reflect.SelectCase, 0, len(t.Clients)+1)
	for _, c := range t.Clients {
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(c.Wr),
			Send: reflect.Value{},
		})
	}
	cases = append(cases, reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
		Send: reflect.Value{},
	})

	for {
		i, rcv, ok := reflect.Select(cases)
		if !ok {
			continue
		}
		if i == len(t.Clients) {
			// ctx.Done() was received from
			break
		}

		// otherwise it was a client's Wr (to server, aka us) channel:
		c := t.Clients[i]
		b := rcv.Bytes()

		var protocolVersion uint8
		{
			var r io.Reader
			r, err = client.ParseHeader(b, &protocolVersion)
			if err != nil {
				return
			}
			b, err = io.ReadAll(r)
			if err != nil {
				return
			}
		}
		if protocolVersion != protocol {
			return
		}

		gm := protocol03.GroupMessage{}
		err = proto.Unmarshal(b, &gm)
		if err != nil {
			return
		}

		gm.PlayerIndex = uint32(i)
		gm.ServerTime = time.Now().UnixNano()

		if gm.GetJoinGroup() != nil || gm.GetEcho() != nil || gm.GetBroadcastSector() != nil {
			pkt := client.MakePacket(protocol)
			var rspBytes []byte
			rspBytes, err = proto.Marshal(&gm)
			if err != nil {
				return
			}

			pkt.Write(rspBytes)
			log.Printf("server: joinGroup -> c[%d]\n", i)
			c.Rd <- pkt.Bytes()
		} else if ba := gm.GetBroadcastAll(); ba != nil {

			pkt := client.MakePacket(protocol)
			var rspBytes []byte
			rspBytes, err = proto.Marshal(&gm)
			if err != nil {
				return
			}

			pkt.Write(rspBytes)
			for j := range t.Clients {
				if j == i {
					continue
				}

				log.Printf("server: broadcastAll c[%d] -> c[%d]\n", i, j)
				t.Clients[j].Rd <- pkt.Bytes()
			}
		}
		// TODO: proper handling of GetBroadcastSector()
	}

	return
}

func TestRun(t *testing.T) {
	g1, c1, e1 := createTestGame(t)
	g2, c2, e2 := createTestGame(t)

	// create a server and run it:
	s := &testServer{Clients: []*testClient{c1, c2}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if err := s.Run(ctx); err != nil {
			t.Error(err)
		}
	}()

	// set module to $07:
	e1.WRAM[0x10] = 0x07
	e2.WRAM[0x10] = 0x07
	for i := 0; i < 60; i++ {
		// run a single frame:
		gameRunFrame(g1, e1)
		gameRunFrame(g2, e2)

		// advance frame counters:
		log.Printf("frame advance")
		e1.WRAM[0x1a]++
		e2.WRAM[0x1a]++
	}

	_, _ = c1, c2
	_, _ = e1, e2
}

func createTestGame(t *testing.T) (g *Game, c *testClient, e *emulator.System) {
	var err error
	var rom *snes.ROM

	// ROM title must start with "VT " to indicate randomizer
	e, rom, err = CreateTestEmulator(t, "VT test")
	if err != nil {
		t.Fatal(err)
	}

	g = CreateTestGame(rom, e)
	c = &testClient{
		Rd: make(chan []byte, 100),
		Wr: make(chan []byte, 100),
	}
	g.ProvideClient(c)

	// request our player index:
	m := g.makeJoinMessage()
	g.send(m)

	return
}

func gameRunFrame(g *Game, e *emulator.System) {
	// process any incoming network messages:
	for len(g.client.Read()) > 0 {
		msg := <-g.client.Read()
		if err := g.handleNetMessage(msg); err != nil {
			panic(err)
		}
	}

	// do all WRAM + SRAM(shadow) reads:
	q := make([]snes.Read, 0, 20)
	q = g.enqueueSRAMRead(q)
	q = g.enqueueWRAMReads(q)
	q = g.enqueueMainRead(q)
	rsps := make([]snes.Response, 0, len(q))
	for i := range q {
		address := q[i].Address
		offs := address - 0xF50000
		rsps = append(rsps, snes.Response{
			IsWrite: false,
			Address: address,
			Size:    q[i].Size,
			Data:    e.WRAM[offs : offs+uint32(q[i].Size)],
			Extra:   nil,
		})
	}
	g.readMainComplete(rsps)
}
