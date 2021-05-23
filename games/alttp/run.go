package alttp

import (
	"encoding/binary"
	"fmt"
	"log"
	"o2/snes"
	"strings"
	"time"
)

func (g *Game) readEnqueue(q []snes.Read, addr uint32, size uint8, extra interface{}) []snes.Read {
	q = append(q, snes.Read{
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

	return q
}

func (g *Game) readSubmit(readQueue []snes.Read) {
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

	//log.Printf("alttp: readSubmit: enqueue start %d reads\n", len(readQueue))
	err := sequence.EnqueueTo(q)
	if err != nil {
		log.Printf("alttp: readSubmit: enqueue: %s\n", err)
		return
	}
	//log.Printf("alttp: readSubmit: enqueue complete\n")
}

const debugSprites = false

// run in a separate goroutine
func (g *Game) run() {
	// for more consistent response times from fx pak pro, adjust firmware.im3 to patch bInterval from 2ms to 1ms.
	// 0x1EA5D = 01 (was 02)

	q := make([]snes.Read, 0, 8)
	q = g.enqueueWRAMReads(q)
	// must always read module number LAST to validate the prior reads:
	q = g.enqueueMainRead(q, 0)
	g.readSubmit(q)

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

			// process the last read data:
			q := g.readMainComplete(rsps)
			g.lastReadCompleted = time.Now()

			g.readSubmit(q)
			break

		// wait for network message from server:
		case msg := <-g.client.Read():
			if msg == nil {
				// disconnected?
				for i := range g.players {
					p := &g.players[i]
					// reset Ttl for all players to make them inactive:
					g.DecTTL(p, 255)
					p.IndexF = -1
				}
				if g.shouldUpdatePlayersList {
					g.updatePlayersList()
				}
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
					q := make([]snes.Read, 0, 8)
					q = g.enqueueWRAMReads(q)
					// must always read module number LAST to validate the prior reads:
					q = g.enqueueMainRead(q, 0)
					g.readSubmit(q)
				} else {
					q := make([]snes.Read, 0, 8)
					q = g.enqueueSRAMRead(q, 1)

					if debugSprites {
						// DEBUG read sprite WRAM:
						q = g.readEnqueue(q, 0xF50D00, 0xF0, 1) // [$0D00..$0DEF]
						q = g.readEnqueue(q, 0xF50DF0, 0xF0, 1) // [$0DF0..$0EDF]
						q = g.readEnqueue(q, 0xF50EE0, 0xC0, 1) // [$0EE0..$0F9F]
					}

					// must always read module number LAST to validate the prior reads:
					q = g.enqueueMainRead(q, nil)
					g.readSubmit(q)
				}
			}

			if g.LocalPlayer().Index() < 0 && g.client != nil {
				// request our player index:
				m := g.makeJoinMessage()
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

			if g.LocalPlayer().Index() < 0 {
				break
			}

			// send an echo to the server to measure roundtrip time:
			g.lastServerSentTime = time.Now()
			g.send(&gameEchoMessage{g: g})

			// broadcast player name:
			m := g.makeBroadcastMessage()
			if m == nil {
				break
			}
			m.WriteByte(0x0C)
			var name [20]byte
			n := copy(name[:], g.LocalPlayer().Name())
			for ; n < 20; n++ {
				name[n] = ' '
			}
			m.Write(name[:])
			g.send(m)

			break
		}
	}
}

func (g *Game) isReadWRAM(rsp snes.Response) (start, end uint32, ok bool) {
	ok = rsp.Address >= 0xF50000 && rsp.Address < 0xF70000
	if !ok {
		return
	}

	start = rsp.Address - 0xF50000
	end = start + uint32(rsp.Size)
	return
}

func (g *Game) isReadSRAM(rsp snes.Response) (start, end uint32, ok bool) {
	ok = rsp.Address >= 0xE00000 && rsp.Address < 0xF00000
	if !ok {
		return
	}

	start = rsp.Address - 0xE00000
	end = start + uint32(rsp.Size)
	return
}

func (g *Game) enqueueSRAMRead(q []snes.Read, extra interface{}) []snes.Read {
	// read the SRAM copy for underworld and overworld:
	q = g.readEnqueue(q, 0xF5F000, 0xFE, extra) // [$F000..$F0FD]
	q = g.readEnqueue(q, 0xF5F0FE, 0xFE, extra) // [$F0FE..$F1FB]
	q = g.readEnqueue(q, 0xF5F1FC, 0x54, extra) // [$F1FC..$F24F]
	q = g.readEnqueue(q, 0xF5F280, 0xC0, extra) // [$F280..$F33F]
	return q
}

func (g *Game) enqueueWRAMReads(q []snes.Read) []snes.Read {
	// FX Pak Pro allows batches of 8 VGET requests to be submitted at a time:

	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	q = g.readEnqueue(q, 0xF50100, 0x36, 0) // [$0100..$0136]
	q = g.readEnqueue(q, 0xF50400, 0x20, 0) // [$0400..$041F]
	// $1980..19E9 for reading underworld door state
	q = g.readEnqueue(q, 0xF51980, 0x6A, 0) // [$1980..$19E9]
	// ALTTP's SRAM copy in WRAM:
	q = g.readEnqueue(q, 0xF5F340, 0xFF, 0) // [$F340..$F43E]
	// Link's palette:
	q = g.readEnqueue(q, 0xF5C6E0, 0x20, 0)
	return q
}

func (g *Game) enqueueMainRead(q []snes.Read, extra interface{}) []snes.Read {
	// NOTE: order matters! must read the module number LAST to make sure all reads prior are valid.
	q = g.readEnqueue(q, 0xF50010, 0xF0, extra) // [$0010..$00FF]
	return q
}

// called when all reads are completed:
func (g *Game) readMainComplete(rsps []snes.Response) []snes.Read {
	q := make([]snes.Read, 0, 8)

	// assume module is invalid until we read it:
	moduleStaging := uint8(0xFF)
	for _, rsp := range rsps {
		// check WRAM reads:
		if start, end, ok := g.isReadWRAM(rsp); ok {
			copy(g.wramStaging[start:end], rsp.Data)
			// did we read the module number?
			if start <= 0x10 && 0x10 <= end {
				moduleStaging = g.wramStaging[0x10]
			}
		}
		// ignore SRAM for staging.

		// handle update routine check:
		g.updateLock.Lock()
		if rsp.Address == g.lastUpdateTarget {
			log.Printf("alttp: update: check: $%06x [$%02x] == $60\n", rsp.Address, rsp.Data[0])
			// when executed, the routine replaces its first instruction with RTS ($60):
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
				q = g.enqueueUpdateCheckRead(q)
				// TODO: this may or may not be redundant
				q = g.enqueueMainRead(q, nil)
			}
		}
		g.updateLock.Unlock()

		// 0 indicates to re-enqueue the read every time:
		if rsp.Extra == 0 {
			q = g.readEnqueue(q, rsp.Address, rsp.Size, rsp.Extra)
		}
	}

	// validate new reads in staging area before copying to wram/sram:
	if moduleStaging <= 0x06 || moduleStaging >= 0x1B {
		if !g.invalid {
			log.Println("alttp: game now in invalid state")
		}
		g.invalid = true
		return q
	}

	if g.invalid {
		log.Println("alttp: game now in valid state")
		g.invalid = false
	}

	// copy the read data into our view of memory:
	for _, rsp := range rsps {
		// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
		if start, end, ok := g.isReadWRAM(rsp); ok {
			copy(g.wram[start:end], rsp.Data)
		}
		// $E0-EF:xxxx is SRAM, aka $70-7D:xxxx
		if start, end, ok := g.isReadSRAM(rsp); ok {
			copy(g.sram[start:end], rsp.Data)
		}
	}

	//log.Printf("alttp: read %d responses\n", len(rsps))

	// assign local variables from WRAM:
	local := g.LocalPlayer()

	g.SetTTL(local, 255)

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
		g.shouldUpdatePlayersList = true
	}
	local.Dungeon = dungeon

	lastLocation := local.Location
	local.Location = inDarkWorld | (uint32(inDungeon&1) << 16)
	if inDungeon != 0 {
		local.Location |= uint32(dungeonRoom)
	} else {
		local.Location |= uint32(overworldArea)
	}
	if local.Location != lastLocation {
		g.shouldUpdatePlayersList = true
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

	if debugSprites {
		// display sprite data:
		sb := strings.Builder{}
		// reset 41 rows up
		sb.WriteString("\033[42A\033[80D")
		// [$0D00..$0DEF]
		// [$0E20..$0E8F]
		// [$0EF0..$0F9F]
		for i := 0; i < 0x2A; i++ {
			// clear to end of line:
			sb.WriteString(fmt.Sprintf("\033[K$%04x: ", 0xD00+(i<<4)))
			for j := 0; j < 16; j++ {
				sb.WriteString(fmt.Sprintf(" %02x", g.wram[0x0D00+(i<<4)+j]))
			}
			sb.WriteByte('\n')
		}
		fmt.Printf(sb.String())
	}

	// handle WRAM reads:
	g.readWRAM()
	g.notFirstWRAMRead = true

	if g.shouldUpdatePlayersList {
		g.updatePlayersList()
	}

	// did game frame change?
	if g.wram[0x1A] == g.lastGameFrame {
		return q
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

	return q
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
	//log.Printf("server now(): %v\n", g.ServerNow())

	// tick down TTLs of remote players:
	for _, p := range g.ActivePlayers() {
		g.DecTTL(p, 1)
	}

	// update underworld supertile state sync bit masks based on sync toggles from front-end:
	g.setUnderworldSyncMasks()

	// generate any WRAM update code and send it to the SNES:
	g.updateWRAM()

	// send out any network updates:
	g.sendPackets()
}
