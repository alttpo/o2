package alttp

import (
	"o2/client/protocol02"
	"o2/snes"
	"time"
)

func (g *Game) readEnqueue(addr uint32, size uint8, complete func()) {
	g.readQueue = append(g.readQueue, snes.Read{
		Address:    addr,
		Size:       size,
		Extra:      complete,
		Completion: g.readCompletionChannel,
	})
}

func (g *Game) readSubmit() {
	sequence := g.queue.MakeReadCommands(g.readQueue...)
	g.queue.EnqueueMulti(sequence)

	// TODO: consider just clearing length instead to avoid realloc
	g.readQueue = nil
}

func (g *Game) handleSNESRead(rsp snes.Response) {
	//log.Printf("\n%s\n", hex.Dump(rsp.Data))

	// copy data read into our wram array:
	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	if rsp.Address >= 0xF50000 && rsp.Address < 0xF70000 {
		copy(g.wram[rsp.Address-0xF50000:], rsp.Data)
	}
}

// run in a separate goroutine
func (g *Game) run() {
	// for more consistent response times from fx pak pro, adjust firmware.im3 to patch bInterval from 2ms to 1ms.
	// 0x1EA5D = 01 (was 02)

	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	g.readEnqueue(0xF50010, 0xF0, g.readMainComplete)
	g.readSubmit()

	//readInventory: snes.Read{Address: 0xF5F340, Size: 0xF0, Extra: nil}
	heartbeat := time.NewTicker(250 * time.Millisecond)

	for {
		select {
		// wait for SNES memory read completion:
		case rsp := <-g.readCompletionChannel:
			if !g.IsRunning() {
				break
			}

			// copy the data into our wram shadow:
			g.handleSNESRead(rsp)

			complete := rsp.Extra.(func())
			if complete != nil {
				complete()
			}

			break

		// wait for network message from server:
		case msg := <-g.client.Read():
			if msg == nil {
				// disconnected from server; reset state:
				g.Reset()
				break
			}
			err := g.handleNetMessage(msg)
			if err != nil {
				break
			}
			break

		// periodically send basic messages to the server to maintain our connection:
		case <-heartbeat.C:
			if g.localIndex < 0 {
				// request our player index:
				m := protocol02.MakePacket(g.client.Group(), protocol02.RequestIndex, uint16(0))
				g.send(m)
				break
			}

			// broadcast player name:
			{
				m := g.makeGamePacket(protocol02.Broadcast)
				m.WriteByte(0x0C)
				var name [20]byte
				n := copy(name[:], g.local.Name)
				for ; n < 20; n++ {
					name[n] = ' '
				}
				m.Write(name[:])
				g.send(m)
			}
			break
		}
	}
}

func (g *Game) readMainComplete() {
	defer g.readSubmit()

	// requeue the main read:
	g.readEnqueue(0xF50010, 0xF0, g.readMainComplete)

	// did game frame change?
	if g.wram[0x1A] == g.lastGameFrame {
		return
	}

	// increment frame timer:
	lastFrame := uint64(g.lastGameFrame)
	nextFrame := uint64(g.wram[0x1A])
	if nextFrame < lastFrame {
		nextFrame += 256
	}
	g.localFrame += nextFrame - lastFrame
	g.lastGameFrame = g.wram[0x1A]

	//log.Printf("%08x\n", g.localFrame)

	if g.localFrame&31 == 0 {
		// TODO: send inventory update to server
	}
}
