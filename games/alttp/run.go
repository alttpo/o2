package alttp

import (
	"encoding/binary"
	"log"
	"o2/client/protocol02"
	"o2/snes"
	"time"
)

func (g *Game) readEnqueue(addr uint32, size uint8, extra interface{}) {
	defer func() {
		g.readQueueLock.Unlock()
	}()

	g.readQueueLock.Lock()

	g.readQueue = append(g.readQueue, snes.Read{
		Address: addr,
		Size:    size,
		Extra:   extra,
		Completion: func(rsp snes.Response) {
			defer g.readResponseLock.Unlock()
			g.readResponseLock.Lock()
			// append to response queue:
			g.readResponse = append(g.readResponse, rsp)
		},
	})
}

func (g *Game) readSubmit() {
	g.readQueueLock.Lock()
	// copy out the current queue contents:
	readQueue := g.readQueue[:]
	// clear the queue:
	g.readQueue = nil
	g.readQueueLock.Unlock()

	if len(readQueue) == 0 {
		return
	}

	q := g.queue
	if q == nil {
		return
	}

	sequence := q.MakeReadCommands(
		readQueue,
		func(cmd snes.Command, err error) {
			g.readResponseLock.Lock()
			// copy out read responses and clear that queue:
			rsps := g.readResponse[:]
			g.readResponse = nil
			g.readResponseLock.Unlock()

			if err != nil {
				log.Printf("alttp: readSubmit: complete: %s\n", err)
			}

			// inform the main loop:
			g.readComplete <- rsps
		},
	)

	err := sequence.EnqueueTo(q)
	if err != nil {
		log.Printf("alttp: readSubmit: enqueue: %s\n", err)
		return
	}
}

// run in a separate goroutine
func (g *Game) run() {
	// for more consistent response times from fx pak pro, adjust firmware.im3 to patch bInterval from 2ms to 1ms.
	// 0x1EA5D = 01 (was 02)

	g.enqueueMainReads()
	go g.readSubmit()

	fastbeat := time.NewTicker(120 * time.Millisecond)
	slowbeat := time.NewTicker(500 * time.Millisecond)

	defer func() {
		fastbeat.Stop()
		slowbeat.Stop()
		log.Println("alttp: run loop exited")
	}()

	for g.running {
		select {
		// wait for reads to complete:
		case rsps := <-g.readComplete:
			if !g.IsRunning() {
				return
			}

			for _, rsp := range rsps {
				// copy the data into our wram shadow:
				g.handleSNESRead(rsp)
				// 0 indicates to re-enqueue the read every time:
				if rsp.Extra == 0 {
					g.readEnqueue(rsp.Address, rsp.Size, rsp.Extra)
				}
			}

			// process the last read data:
			g.readMainComplete()
			g.lastReadCompleted = time.Now()

			go g.readSubmit()
			break

		// wait for network message from server:
		case msg := <-g.client.Read():
			if msg == nil {
				// disconnected from server; reset state:
				g.Reset()
				break
			}
			if !g.IsRunning() {
				return
			}

			err := g.handleNetMessage(msg)
			if err != nil {
				break
			}
			break

		// periodically send basic messages to the server to maintain our connection:
		case <-fastbeat.C:
			if !g.IsRunning() {
				return
			}

			if g.queue != nil {
				// make sure a read request is always in flight to keep our main loop running:
				timeSinceRead := time.Now().Sub(g.lastReadCompleted)
				if timeSinceRead >= time.Millisecond*512 {
					log.Printf("alttp: fastbeat: enqueue main reads; %d msec since last read\n", timeSinceRead.Milliseconds())
					g.enqueueMainReads()
					go g.readSubmit()
				} else {
					// read the SRAM copy for underworld and overworld:
					g.readEnqueue(0xF5F000, 0xFE, 1) // [$F000..$F0FD]
					g.readEnqueue(0xF5F0FE, 0xFE, 1) // [$F0FE..$F1FB]
					g.readEnqueue(0xF5F1FC, 0x54, 1) // [$F1FC..$F24F]
					g.readEnqueue(0xF5F280, 0xC0, 1) // [$F280..$F33F]
					go g.readSubmit()
				}
			}

			if g.localIndex < 0 && g.client != nil {
				// request our player index:
				m := protocol02.MakePacket(g.client.Group(), protocol02.RequestIndex, uint16(0))
				if m == nil {
					break
				}
				g.send(m)
				break
			}

			break

		case <-slowbeat.C:
			if !g.IsRunning() {
				return
			}

			if g.localIndex < 0 {
				break
			}

			// broadcast player name:
			m := g.makeGamePacket(protocol02.Broadcast)
			if m == nil {
				break
			}
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

func (g *Game) handleSNESRead(rsp snes.Response) {
	//log.Printf("read completed: %06x size=%02x\n", rsp.Address, rsp.Size)

	// copy data read into our wram array:
	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	if rsp.Address >= 0xF50000 && rsp.Address < 0xF70000 {
		copy(g.wram[(rsp.Address-0xF50000):], rsp.Data)
	}
	if rsp.Address >= 0xE00000 && rsp.Address < 0xF00000 {
		copy(g.sram[(rsp.Address-0xE00000):], rsp.Data)
	}

	g.updateLock.Lock()
	if rsp.Address == g.lastUpdateTarget {
		log.Printf("alttp: update: check: $%06x [$%02x] == $60\n", rsp.Address, rsp.Data[0])
		if rsp.Data[0] == 0x60 {
			// allow next update:
			log.Printf("alttp: update: complete: $%06x [$%02x] == $60\n", rsp.Address, rsp.Data[0])
			if g.updateStage == 2 {
				g.updateStage = 0
				g.nextUpdateA = !g.nextUpdateA
				g.lastUpdateTarget = 0xFFFFFF
			}
		} else {
			// check again:
			g.enqueueUpdateCheckRead()
		}
	}
	g.updateLock.Unlock()
}

func (g *Game) enqueueMainReads() {
	// FX Pak Pro allows batches of 8 VGET requests to be submitted at a time:

	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	g.readEnqueue(0xF50010, 0xF0, 0) // [$0010..$00FF]
	g.readEnqueue(0xF50100, 0x36, 0) // [$0100..$0136]
	g.readEnqueue(0xF50400, 0x20, 0) // [$0400..$041F]
	// $068E[2] and $0690[2] for controlling doors
	g.readEnqueue(0xF5068E, 0x04, 0) // [$068E..$0691]
	g.readEnqueue(0xF51980, 0x6A, 0) // [$1980..$19E9]
	// ALTTP's SRAM copy in WRAM:
	g.readEnqueue(0xF5F340, 0xF0, 0) // [$F340..$F42F]
}

// called when all reads are completed:
func (g *Game) readMainComplete() {
	// assign local variables from WRAM:
	local := g.local

	newModule, newSubModule, newSubSubModule := Module(g.wram[0x10]), g.wram[0x11], g.wram[0xB0]
	if local.Module != newModule || local.SubModule != newSubModule {
		log.Printf(
			"alttp: module [%02x,%02x] -> [%02x,%02x]\n",
			local.Module,
			local.SubModule,
			newModule,
			newSubModule,
		)
	}
	local.Module, local.SubModule, local.SubSubModule = newModule, newSubModule, newSubSubModule

	// this is documented as a uint16, but we use it as a uint8
	local.PriorModule = Module(g.wram[0x010C])

	inDungeon := g.wram[0x1B]
	overworldArea := g.wramU16(0x8A)
	dungeonRoom := g.wramU16(0xA0)
	if local.OverworldArea != overworldArea || local.DungeonRoom != dungeonRoom {
		log.Printf(
			"alttp: supertile[overworld,underworld]: [%04x,%04x] -> [%04x,%04x]\n",
			local.OverworldArea,
			local.DungeonRoom,
			overworldArea,
			dungeonRoom,
		)
	}
	local.OverworldArea, local.DungeonRoom = overworldArea, dungeonRoom

	// TODO: fix this calculation to be compatible with alttpo
	inDarkWorld := uint32(0)
	if overworldArea&0x40 != 0 {
		inDarkWorld = 1 << 17
	}

	dungeon := g.wramU16(0x040C)
	if local.Dungeon != dungeon {
		log.Printf(
			"alttp: dungeon: %#04x -> %#04x\n",
			local.Dungeon,
			dungeon,
		)
	}
	local.Dungeon = dungeon

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

	local.X = g.wramU16(0x22)
	local.Y = g.wramU16(0x20)

	local.XOffs = int16(g.wramU16(0xE2)) - int16(g.wramU16(0x11A))
	local.YOffs = int16(g.wramU16(0xE8)) - int16(g.wramU16(0x11C))

	// copy $7EF000-4FF into `local.SRAM`:
	copy(local.SRAM[:], g.wram[0xF000:0xF500])

	// handle WRAM reads:
	g.readWRAM()
	g.notFirstWRAMRead = true

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

	// should wrap around 255 to 0:
	g.monotonicFrameTime++

	g.frameAdvanced()
}

func (g *Game) wramU8(addr uint32) uint8 {
	addr &= 0x01FFFF
	return g.wram[addr]
}

func (g *Game) wramU16(addr uint32) uint16 {
	addr &= 0x01FFFF
	return binary.LittleEndian.Uint16(g.wram[addr : addr+2])
}

// called when the local game frame advances:
func (g *Game) frameAdvanced() {
	// tick down TTLs of remote players:
	for _, p := range g.ActivePlayers() {
		p.DecTTL()
	}

	// update underworld supertile state sync bit masks based on sync toggles from front-end:
	g.setUnderworldSyncMasks()

	// generate any WRAM update code and send it to the SNES:
	g.updateWRAM()

	// send out any network updates:
	g.sendPackets()
}
