package alttp

import (
	"encoding/binary"
	"o2/client/protocol02"
	"o2/snes"
	"time"
)

func (g *Game) readEnqueue(addr uint32, size uint8) {
	g.readQueue = append(g.readQueue, snes.Read{
		Address: addr,
		Size:    size,
		Extra:   nil,
		Completion: func(rsp snes.Response) {
			// append to response queue:
			g.readResponse = append(g.readResponse, rsp)
		},
	})
}

func (g *Game) readSubmit() {
	sequence := g.queue.MakeReadCommands(
		g.readQueue,
		func(cmd snes.Command, err error) {
			g.readCompletionChannel <- g.readResponse[:]
			// clear response queue:
			g.readResponse = g.readResponse[:0]
		},
	)
	sequence.EnqueueTo(g.queue)

	// clear the queue:
	g.readQueue = g.readQueue[:0]
}

func (g *Game) handleSNESRead(rsp snes.Response) {
	//log.Printf("\n%s\n", hex.Dump(rsp.Data))

	// copy data read into our wram array:
	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	if rsp.Address >= 0xF50000 && rsp.Address < 0xF70000 {
		o := rsp.Address - 0xF50000
		size := uint32(len(rsp.Data))
		for i := uint32(0); i < size; i++ {
			if g.wram[o] != rsp.Data[i] {
				g.wramDirty[o] = true
			}
			g.wram[o] = rsp.Data[i]
			o++
		}
	}
}

// run in a separate goroutine
func (g *Game) run() {
	// for more consistent response times from fx pak pro, adjust firmware.im3 to patch bInterval from 2ms to 1ms.
	// 0x1EA5D = 01 (was 02)

	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	g.readEnqueue(0xF50010, 0xF0)	// $0010-$00FF
	g.readEnqueue(0xF50100, 0x36)	// $0100-$0136
	g.readEnqueue(0xF50400, 0x20)	// $0400-$041F
	// ALTTP's SRAM copy in WRAM:
	g.readEnqueue(0xF5F340, 0xF0)	// $F340-$F42F
	// FX Pak Pro allows batches of 8 VGET requests to be submitted at a time:
	g.readSubmit()

	heartbeat := time.NewTicker(250 * time.Millisecond)

	for g.running {
		select {
		// wait for SNES memory read completion:
		case rsps := <-g.readCompletionChannel:
			if !g.IsRunning() {
				break
			}

			for _, rsp := range rsps {
				// copy the data into our wram shadow:
				g.handleSNESRead(rsp)
				g.readEnqueue(rsp.Address, rsp.Size)
			}
			g.readSubmit()

			g.readMainComplete()
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

// called when all reads are completed:
func (g *Game) readMainComplete() {
	// assign local variables from WRAM:
	p := g.local
	p.Module = Module(g.wram[0x10])
	p.SubModule = g.wram[0x11]
	p.SubSubModule = g.wram[0xB0]

	inDungeon := g.wram[0x1B]
	overworldArea := g.wram[0x8A]
	dungeonRoom := g.wram[0xA0]

	// TODO: fix this calculation to be compatible with alttpo
	inDarkWorld := uint32(0)
	if overworldArea&0x40 != 0 {
		inDarkWorld = 1 << 17
	}

	p.Location = inDarkWorld | (uint32(inDungeon&1) << 16)
	if inDungeon != 0 {
		p.Location |= uint32(dungeonRoom)
	} else {
		p.Location |= uint32(overworldArea)
	}

	if p.Module.IsOverworld() {
		p.LastOverworldX = p.X
		p.LastOverworldY = p.Y
	}

	p.X = binary.LittleEndian.Uint16(g.wram[0x22:])
	p.Y = binary.LittleEndian.Uint16(g.wram[0x20:])

	p.XOffs = int16(binary.LittleEndian.Uint16(g.wram[0xE2:])) - int16(binary.LittleEndian.Uint16(g.wram[0x11A:]))
	p.YOffs = int16(binary.LittleEndian.Uint16(g.wram[0xE8:])) - int16(binary.LittleEndian.Uint16(g.wram[0x11C:]))

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

	g.frameAdvanced()
}

// called when the local game frame advances:
func (g *Game) frameAdvanced() {
	//log.Printf("%08x\n", g.localFrame)

	// don't send out any updates until we're connected:
	if g.localIndex < 0 {
		return
	}

	local := g.local

	{
		// send location packet every frame:
		m := g.makeGamePacket(protocol02.Broadcast)
		if err := SerializeLocation(local, m); err != nil {
			panic(err)
		}
		g.send(m)
	}

	if g.localFrame&31 == 0 {
		// Broadcast underworld SRAM:
		m := g.makeGamePacket(protocol02.Broadcast)
		SerializeSRAM(local, m, 0, 0x250)
		g.send(m)
	}
	if g.localFrame&31 == 16 {
		// Broadcast overworld SRAM:
		m := g.makeGamePacket(protocol02.Broadcast)
		SerializeSRAM(local, m, 0x280, 0x340)
		g.send(m)
	}
}