package alttp

import (
	"bytes"
	"context"
	"fmt"
	"github.com/alttpo/snes/emulator"
	"google.golang.org/protobuf/proto"
	"io"
	"log"
	"o2/client"
	"o2/client/protocol03"
	"o2/interfaces"
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
	Now     time.Time
}

func (s *testServer) AdvanceTime(duration time.Duration) {
	s.Now = s.Now.Add(duration)
}

func (s *testServer) HandleMessageFromClient(t testing.TB, i int, b []byte) (err error) {
	c := s.Clients[i]

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
	gm.ServerTime = s.Now.UnixNano()

	if gm.GetJoinGroup() != nil || gm.GetEcho() != nil || gm.GetBroadcastSector() != nil {
		pkt := client.MakePacket(protocol)
		var rspBytes []byte
		rspBytes, err = proto.Marshal(&gm)
		if err != nil {
			return
		}

		pkt.Write(rspBytes)

		pname := ""
		var pvalue any
		if gm.GetJoinGroup() != nil {
			pname = "joinGroup"
			pvalue = gm.GetJoinGroup()
		} else if gm.GetEcho() != nil {
			pname = "echo"
			pvalue = gm.GetEcho()
		} else if gm.GetBroadcastSector() != nil {
			pname = "broadcastSector"
			pvalue = gm.GetBroadcastSector()
		}
		if t != nil {
			log.Printf("server: c[%d] %s\n", i, pname)
			_ = pvalue
		}

		c.Rd <- pkt.Bytes()
	} else if ba := gm.GetBroadcastAll(); ba != nil {

		pkt := client.MakePacket(protocol)
		var rspBytes []byte
		rspBytes, err = proto.Marshal(&gm)
		if err != nil {
			return
		}

		pkt.Write(rspBytes)
		for j := range s.Clients {
			if j == i {
				continue
			}

			log.Printf("server: c[%d] -> c[%d] broadcastAll\n", i, j)
			s.Clients[j].Rd <- pkt.Bytes()
		}
	}
	// TODO: proper handling of GetBroadcastSector()

	return
}

func (s *testServer) Run(ctx context.Context) (err error) {
	cases := make([]reflect.SelectCase, 0, len(s.Clients)+1)
	for _, c := range s.Clients {
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
		if i == len(s.Clients) {
			// ctx.Done() was received from
			break
		}

		if err = s.HandleMessageFromClient(nil, i, rcv.Bytes()); err != nil {
			panic(err)
		}
	}

	return
}

func (s *testServer) HandleAllClients(t testing.TB) {
	for i := range s.Clients {
		for len(s.Clients[i].Wr) > 0 {
			b := <-s.Clients[i].Wr
			if err := s.HandleMessageFromClient(t, i, b); err != nil {
				panic(err)
			}
		}
	}
}

type testReadCommand []snes.Read

func (c testReadCommand) Execute(q snes.Queue, keepAlive snes.KeepAlive) error {
	tq := q.(*testQueue)
	for _, rd := range c {
		if rd.Address < 0xF5_0000 {
			panic("unsupported address for read in testQueue!")
		}

		d := make([]byte, rd.Size)
		copy(d, tq.E.WRAM[rd.Address-0xF5_0000:])
		if rd.Completion != nil {
			rd.Completion(snes.Response{
				IsWrite: false,
				Address: rd.Address,
				Size:    rd.Size,
				Data:    d,
				Extra:   rd.Extra,
			})
		}
	}
	return nil
}

type testWriteCommand []snes.Write

func (c testWriteCommand) Execute(q snes.Queue, keepAlive snes.KeepAlive) error {
	tq := q.(*testQueue)
	for _, wr := range c {
		if wr.Address < 0xE0_0000 || wr.Address >= 0xF5_0000 {
			panic("unsupported address for write in testQueue!")
		}

		copy(tq.E.SRAM[wr.Address-0xE0_0000:], wr.Data[0:wr.Size])
		if wr.Completion != nil {
			wr.Completion(snes.Response{
				IsWrite: true,
				Address: wr.Address,
				Size:    wr.Size,
				Data:    wr.Data,
				Extra:   wr.Extra,
			})
		}
	}
	return nil
}

type testQueue struct {
	E *emulator.System
}

func (t *testQueue) Close() error { return nil }

func (t *testQueue) Closed() <-chan struct{} { return nil }

func (t *testQueue) Enqueue(cmd snes.CommandWithCompletion) error {
	// directly execute the command:
	err := cmd.Command.Execute(t, nil)
	if cmd.Completion != nil {
		cmd.Completion(cmd.Command, err)
	}
	return nil
}

func (t *testQueue) MakeReadCommands(reqs []snes.Read, batchComplete snes.Completion) snes.CommandSequence {
	return snes.CommandSequence{
		snes.CommandWithCompletion{
			Command:    testReadCommand(reqs),
			Completion: batchComplete,
		},
	}
}

func (t *testQueue) MakeWriteCommands(reqs []snes.Write, batchComplete snes.Completion) snes.CommandSequence {
	return snes.CommandSequence{
		snes.CommandWithCompletion{
			Command:    testWriteCommand(reqs),
			Completion: batchComplete,
		},
	}
}

func (t *testQueue) IsTerminalError(err error) bool {
	return false
}

func createTestGameSync(romTitle string, playerName string, logger io.Writer) (gs gameSync, err error) {
	var rom *snes.ROM

	// ROM title must start with "VT " to indicate randomizer
	gs.e, rom, err = createTestEmulator(romTitle, logger)
	if err != nil {
		return
	}

	gs.g = CreateTestGame(rom, gs.e)
	gs.g.local.NameF = playerName
	gs.c = &testClient{
		Rd: make(chan []byte, 100),
		Wr: make(chan []byte, 100),
	}
	gs.g.ProvideClient(gs.c)

	// request our player index:
	m := gs.g.makeJoinMessage()
	gs.g.send(m)
	gs.g.sendPlayerName()
	gs.g.sendEcho()

	// initialize reads:
	gs.g.priorityReads[0] = nil
	gs.g.priorityReads[1] = gs.g.enqueueMainRead(gs.g.enqueueSRAMRead(make([]snes.Read, 0, 20)))
	gs.g.priorityReads[2] = gs.g.enqueueMainRead(gs.g.enqueueWRAMReads(make([]snes.Read, 0, 20)))

	return
}

func gameHandleNet(g *Game) {
	// process any incoming network messages:
	for len(g.client.Read()) > 0 {
		msg := <-g.client.Read()
		if err := g.handleNetMessage(msg); err != nil {
			panic(err)
		}
	}
}

func (gs *gameSync) runFrame(t testing.TB) {
	g := gs.g
	e := gs.e

	log.Printf("%s ----------------------------------\n", g.LocalPlayer().Name())

	gameHandleNet(g)

	// do all WRAM + SRAM(shadow) reads:
	q := make([]snes.Read, 0, 20)
	for j := range g.priorityReads {
		reads := g.priorityReads[j]
		if reads == nil {
			continue
		}

		q = append(q, reads...)
		g.priorityReads[j] = nil
	}
	rsps := make([]snes.Response, 0, len(q))
	for i := range q {
		address := q[i].Address
		if address >= 0xF50000 {
			offs := address - 0xF50000
			rsps = append(rsps, snes.Response{
				IsWrite: false,
				Address: address,
				Size:    q[i].Size,
				Data:    e.WRAM[offs : offs+uint32(q[i].Size)],
				Extra:   nil,
			})
		} else if address >= 0xE00000 {
			offs := address - 0xE00000
			rsps = append(rsps, snes.Response{
				IsWrite: false,
				Address: address,
				Size:    q[i].Size,
				Data:    e.SRAM[offs : offs+uint32(q[i].Size)],
				Extra:   nil,
			})
		} else {
			panic(fmt.Errorf("unexpected read address %06x", address))
		}
	}
	g.readMainComplete(rsps)

	// emulate a main loop iteration:
	e.CPU.Reset()
	e.SetPC(testROMMainGameLoop)
	if !e.RunUntil(testROMBreakPoint, 0x1_000) {
		t.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x", testROMBreakPoint, e.CPU.PC)
		return
	}
}

type gameSync struct {
	e *emulator.System
	g *Game
	c *testClient
	n []string
}

type gameSyncTestUpdateFunc func(t testing.TB, gs [2]gameSync)

type gameSyncTestFrame struct {
	preFrame  gameSyncTestUpdateFunc
	postFrame gameSyncTestUpdateFunc
}

type gameSyncTestCase struct {
	gs       [2]gameSync
	s        *testServer
	f        []gameSyncTestFrame
	romTitle string
}

func (tc *gameSyncTestCase) runFrame(t testing.TB, f *gameSyncTestFrame, duration time.Duration) bool {
	// clear notifications:
	for i := range tc.gs {
		tc.gs[i].n = tc.gs[i].n[:0]
	}

	// pre-frame setup and/or test assumption verification:
	if f.preFrame != nil {
		f.preFrame(t, tc.gs)
	}
	if t.Failed() {
		return false
	}

	// run a single frame for each client:
	for i := range tc.gs {
		tc.gs[i].runFrame(t)
		tc.s.HandleAllClients(t)
	}

	// post-frame test validation:
	if f.postFrame != nil {
		f.postFrame(t, tc.gs)
	}
	if t.Failed() {
		return false
	}

	// advance server time:
	tc.s.AdvanceTime(duration)
	return true
}

func newGameSyncTestCase(romTitle string, f []gameSyncTestFrame) (tc *gameSyncTestCase) {
	tc = &gameSyncTestCase{
		romTitle: romTitle,
		f:        f,
	}

	return
}

type testLogger struct {
	u io.Writer
	b bytes.Buffer
}

func (l *testLogger) Write(p []byte) (n int, err error) {
	return l.b.Write(p)
}

func (l *testLogger) WriteTo(w io.Writer) (n int64, err error) {
	n, err = l.b.WriteTo(w)
	l.b.Reset()
	return
}

func (tc *gameSyncTestCase) runGameSyncTest(t *testing.T) {
	const duration = time.Millisecond * 17

	var err error

	logger := log.Writer().(*testLogger)

	// create two independent clients and their respective emulators:
	tc.gs[0], err = createTestGameSync(tc.romTitle, "g1", logger)
	if err != nil {
		t.Error(err)
		return
	}

	tc.gs[1], err = createTestGameSync(tc.romTitle, "g2", logger)
	if err != nil {
		t.Error(err)
		return
	}

	// create a mock server to facilitate network comms between the clients:
	tc.s = &testServer{
		Clients: []*testClient{tc.gs[0].c, tc.gs[1].c},
		Now:     time.Now(),
	}

	// issue join group messages:
	tc.s.HandleAllClients(t)
	tc.s.AdvanceTime(duration)

	// handle join group messages for each client now to avoid them showing up in notification subscriptions:
	for _, gs := range tc.gs {
		gameHandleNet(gs.g)
	}

	for i := range tc.gs {
		i := i

		// subscribe to front-end Notifications from the game:
		tc.gs[i].n = make([]string, 0, 10)
		observerHandle := tc.gs[i].g.Notifications.Subscribe(interfaces.ObserverImpl(func(object interface{}) {
			notification := object.(string)
			tc.gs[i].n = append(tc.gs[i].n, notification)
			log.Printf("notification[%d]: '%s'\n", len(tc.gs[i].n)-1, notification)
		}))

		//goland:noinspection GoDeferInLoop
		defer func() {
			tc.gs[i].g.Notifications.Unsubscribe(observerHandle)
		}()
	}

	// execute test frames:
	for i := range tc.f {
		if !tc.runFrame(t, &tc.f[i], duration) {
			break
		}
	}
}
