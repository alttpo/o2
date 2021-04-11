package alttp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"log"
	"o2/client/protocol02"
	"o2/snes"
	"o2/snes/asm"
	"strings"
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
		copy(g.wram[(rsp.Address-0xF50000):], rsp.Data)
	}
}

// run in a separate goroutine
func (g *Game) run() {
	// for more consistent response times from fx pak pro, adjust firmware.im3 to patch bInterval from 2ms to 1ms.
	// 0x1EA5D = 01 (was 02)

	lastReadTime := time.Now()
	g.requestMainReads()

	fastbeat := time.NewTicker(250 * time.Millisecond)
	slowbeat := time.NewTicker(1000 * time.Millisecond)

	for g.running {
		select {
		// wait for SNES memory read completion:
		case rsps := <-g.readCompletionChannel:
			lastReadTime = time.Now()

			// allow further writes now:
			g.updateLock.Lock()
			if g.updateStage == 2 {
				g.updateStage = 0
			}
			g.updateLock.Unlock()

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
		case <-fastbeat.C:
			// make sure a read request is always in flight to keep our main loop running:
			if time.Now().Sub(lastReadTime).Milliseconds() >= 500 {
				g.requestMainReads()
			}
			if g.localIndex < 0 {
				// request our player index:
				m := protocol02.MakePacket(g.client.Group(), protocol02.RequestIndex, uint16(0))
				g.send(m)
				break
			}

		case <-slowbeat.C:
			if g.localIndex < 0 {
				break
			}
			// broadcast player name:
			m := g.makeGamePacket(protocol02.Broadcast)
			m.WriteByte(0x0C)
			var name [20]byte
			n := copy(name[:], g.local.Name)
			for ; n < 20; n++ {
				name[n] = ' '
			}
			m.Write(name[:])
			g.send(m)
			break
		}
	}
}

func (g *Game) requestMainReads() {
	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	g.readEnqueue(0xF50010, 0xF0) // $0010-$00FF
	g.readEnqueue(0xF50100, 0x36) // $0100-$0136
	g.readEnqueue(0xF50400, 0x20) // $0400-$041F
	// ALTTP's SRAM copy in WRAM:
	g.readEnqueue(0xF5F340, 0xF0) // $F340-$F42F
	// FX Pak Pro allows batches of 8 VGET requests to be submitted at a time:
	g.readSubmit()
}

// called when all reads are completed:
func (g *Game) readMainComplete() {
	// assign local variables from WRAM:
	local := g.local
	local.Module = Module(g.wram[0x10])
	local.SubModule = g.wram[0x11]
	local.SubSubModule = g.wram[0xB0]

	inDungeon := g.wram[0x1B]
	overworldArea := g.wram[0x8A]
	dungeonRoom := g.wram[0xA0]

	// TODO: fix this calculation to be compatible with alttpo
	inDarkWorld := uint32(0)
	if overworldArea&0x40 != 0 {
		inDarkWorld = 1 << 17
	}

	local.Location = inDarkWorld | (uint32(inDungeon&1) << 16)
	if inDungeon != 0 {
		local.Location |= uint32(dungeonRoom)
	} else {
		local.Location |= uint32(overworldArea)
	}

	if local.Module.IsOverworld() {
		local.LastOverworldX = local.X
		local.LastOverworldY = local.Y
	}

	local.X = binary.LittleEndian.Uint16(g.wram[0x22:])
	local.Y = binary.LittleEndian.Uint16(g.wram[0x20:])

	local.XOffs = int16(binary.LittleEndian.Uint16(g.wram[0xE2:])) - int16(binary.LittleEndian.Uint16(g.wram[0x11A:]))
	local.YOffs = int16(binary.LittleEndian.Uint16(g.wram[0xE8:])) - int16(binary.LittleEndian.Uint16(g.wram[0x11C:]))

	// copy $7EF000-4FF into `local.SRAM`:
	copy(local.SRAM[:], g.wram[0xF000:0xF500])

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

	for _, p := range g.ActivePlayers() {
		p.DecTTL()
	}

	// generate any WRAM update code and send it to the SNES:
	g.updateWRAM()

	{
		// send location packet every frame:
		m := g.makeGamePacket(protocol02.Broadcast)

		locStart := m.Len()
		if err := SerializeLocation(local, m); err != nil {
			panic(err)
		}

		// hash the location packet:
		locHash := hash64(m.Bytes()[locStart:])
		if g.locHashTTL > 0 {
			g.locHashTTL--
		}
		if locHash != g.locHash || g.locHashTTL <= 0 {
			// only send if different or TTL of last packet expired:
			g.send(m)
			g.locHashTTL = 60
			g.locHash = locHash
		}
	}

	if g.localFrame&15 == 0 {
		// Broadcast items and progress SRAM:
		m := g.makeGamePacket(protocol02.Broadcast)

		// items earned
		if err := SerializeSRAM(local, m, 0x340, 0x37C); err != nil {
			panic(err)
		}
		// progress made
		if err := SerializeSRAM(local, m, 0x3C5, 0x3CA); err != nil {
			panic(err)
		}

		// TODO: more ranges depending on ROM kind

		// VT randomizer:
		//serialize(r, 0x340, 0x390); // items earned
		//serialize(r, 0x390, 0x3C5); // item limit counters
		//serialize(r, 0x3C5, 0x43A); // progress made

		// Door randomizer:
		//serialize(r, 0x340, 0x390); // items earned
		//serialize(r, 0x390, 0x3C5); // item limit counters
		//serialize(r, 0x3C5, 0x43A); // progress made
		//serialize(r, 0x4C0, 0x4CD); // chests
		//serialize(r, 0x4E0, 0x4ED); // chest-keys

		g.send(m)
	}
	//if g.localFrame&31 == 0 {
	//	// Broadcast underworld SRAM:
	//	m := g.makeGamePacket(protocol02.Broadcast)
	//	if err := SerializeSRAM(local, m, 0, 0x250); err != nil {
	//		panic(err)
	//	}
	//	g.send(m)
	//}
	//if g.localFrame&31 == 16 {
	//	// Broadcast overworld SRAM:
	//	m := g.makeGamePacket(protocol02.Broadcast)
	//	if err := SerializeSRAM(local, m, 0x280, 0x340); err != nil {
	//		panic(err)
	//	}
	//	g.send(m)
	//}
}

func hash64(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

func (g *Game) updateWRAM() {
	if !g.local.Module.IsInGame() {
		return
	}

	defer g.updateLock.Unlock()
	g.updateLock.Lock()

	if g.updateStage > 0 {
		return
	}

	// select target SRAM routine:
	var targetSNES uint32
	var oppositeSNES uint32
	if g.nextUpdateA {
		targetSNES, oppositeSNES = preMainUpdateAAddr, preMainUpdateBAddr
	} else {
		targetSNES, oppositeSNES = preMainUpdateBAddr, preMainUpdateAAddr
	}
	_ = oppositeSNES

	codeBuf := bytes.Buffer{}
	textBuf := strings.Builder{}

	var a asm.Emitter
	a.Text = &textBuf
	a.Code = &codeBuf
	a.SetBase(targetSNES)

	updated := false
	// assume 16-bit mode for accumulator
	a.AssumeREP(0x20)
	// generate update ASM code for any 16-bit values:
	for _, item := range g.syncableItems {
		if item.Size() != 2 {
			continue
		}
		if !item.IsEnabled() {
			continue
		}
		u := item.GenerateUpdate(&a)
		updated = updated || u
	}
	// use 8-bit mode for accumulator
	a.SEP(0x20)
	// generate update ASM code for any 8-bit values:
	for _, item := range g.syncableItems {
		if item.Size() != 1 {
			continue
		}
		if !item.IsEnabled() {
			continue
		}
		u := item.GenerateUpdate(&a)
		updated = updated || u
	}
	if !updated {
		return
	}

	// clear out our routine with an RTS instruction at the start:
	// MUST be in SEP(0x20) mode!
	a.LDA_imm8_b(0x60) // RTS
	a.STA_long(targetSNES)
	// back to 16-bit mode for accumulator:
	a.REP(0x20)
	a.RTS()

	// dump asm:
	log.Print(textBuf.String())

	if codeBuf.Len() > 255 {
		panic(fmt.Errorf("generated update ASM larger than 255 bytes: %d", codeBuf.Len()))
	}

	// prevent more updates until the upcoming write completes:
	g.updateStage = 1
	fmt.Println("write started")

	// calculate target address in FX Pak Pro address space:
	// SRAM starts at $E00000
	target := xlatSNEStoPak(targetSNES)
	g.nextUpdateA = !g.nextUpdateA

	// write generated asm routine to SRAM:
	g.queue.MakeWriteCommands(
		[]snes.Write{
			// TODO: might need multiple writes to cover full length if > 255:
			{
				Address: target,
				Size:    uint8(codeBuf.Len()),
				Data:    codeBuf.Bytes(),
			},
			// finally, update the JSR instruction to point to the updated routine:
			{
				// JSR $7C00 | JSR $7E00
				// update the $7C or $7E byte in the JSR instruction:
				Address: xlatSNEStoPak(preMainAddr + 2),
				Size:    1,
				Data:    []byte{uint8(targetSNES >> 8)},
			},
		},
		func(cmd snes.Command, err error) {
			fmt.Println("write completed")
			defer g.updateLock.Unlock()
			g.updateLock.Lock()
			// expect a read now to prevent double-write:
			if g.updateStage == 1 {
				g.updateStage = 2
			}
		},
	).EnqueueTo(g.queue)
}

func snesBankToLinear(addr uint32) uint32 {
	bank := addr >> 16
	linbank := ((bank & 1) << 15) + ((bank >> 1) << 16)
	linoffs := linbank + (addr & 0x7FFF)
	return linoffs
}

func xlatSNEStoPak(snes uint32) uint32 {
	if snes&0x8000 == 0 {
		if snes >= 0x700000 && snes < 0x7E0000 {
			sram := snesBankToLinear(snes-0x700000) + 0xE00000
			return sram
		} else if snes >= 0x7E0000 && snes < 0x800000 {
			wram := (snes - 0x7E0000) + 0xE50000
			return wram
		}
	}
	return snes
}
