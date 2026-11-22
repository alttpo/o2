package alttp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"o2/snes"
	"o2/util"
	"strings"
	"sync/atomic"
	"time"

	"github.com/alttpo/snes/timing"
)

func (g *Game) readEnqueue(q []snes.Read, addr uint32, size uint8, extra interface{}) []snes.Read {
	q = append(q, snes.Read{
		Address: addr,
		Size:    size,
		Extra:   extra,
		Completion: func(rsp snes.Response) {
			g.readResponseLock.Lock()
			defer g.readResponseLock.Unlock()
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

	q := g.getQueue()
	if q == nil {
		return
	}

	// a sequence may be split into several commands (e.g. FX Pak Pro VGETs are limited to 8 reads each) which each call
	// the completion; only process the responses once all commands have completed (or one fails):
	var remaining int32
	sequence := q.MakeReadCommands(
		readQueue,
		func(cmd snes.Command, err error) {
			if n := atomic.AddInt32(&remaining, -1); err == nil && n > 0 {
				return
			} else if n < 0 {
				// an earlier command failed and already completed the sequence:
				return
			} else if err != nil {
				// complete the sequence now; ignore completions of any remaining commands:
				atomic.StoreInt32(&remaining, -1<<30)
			}

			g.readResponseLock.Lock()
			// copy out read responses and clear that queue:
			rsps := g.readResponse[:]
			g.readResponse = nil
			g.readResponseLock.Unlock()

			g.readFinished(err)

			// inform the main loop:
			g.readComplete <- rsps
		},
	)

	atomic.StoreInt32(&remaining, int32(len(sequence)))

	//log.Printf("alttp: readSubmit: enqueue start %d reads\n", len(readQueue))
	err := sequence.EnqueueTo(q)
	if err != nil {
		log.Printf("alttp: readSubmit: enqueue: %s\n", err)
		var termErr *snes.TerminalError
		if errors.As(err, &termErr) {
			log.Println("alttp: readSubmit: terminal error encountered; disconnecting from queue")
			_ = termErr
			g.clearQueue(q)
		}
		return
	}
	//log.Printf("alttp: readSubmit: enqueue complete\n")
}

const debugSprites = false

// readBackoff is how long to wait before reading again after a read fails:
const readBackoff = time.Second

// readFinished logs a read failure and backs off further reads; identical repeated failures are logged once, with a
// count when they stop.
func (g *Game) readFinished(err error) {
	g.readErrLock.Lock()
	defer g.readErrLock.Unlock()

	if err == nil {
		if g.readErr != "" {
			log.Printf("alttp: readSubmit: reads recovered after %d repeated errors\n", g.readErrRepeats)
		}
		g.readErr = ""
		g.readErrRepeats = 0
		return
	}

	g.readBackoffUntil = time.Now().Add(readBackoff)

	msg := err.Error()
	if msg == g.readErr {
		g.readErrRepeats++
		return
	}
	if g.readErrRepeats > 0 {
		log.Printf("alttp: readSubmit: previous error repeated %d times\n", g.readErrRepeats)
	}
	log.Printf("alttp: readSubmit: complete: %s (retrying every %s)\n", msg, readBackoff)
	g.readErr = msg
	g.readErrRepeats = 0
}

// inReadBackoff reports whether reads are paused after a failed read.
func (g *Game) inReadBackoff(now time.Time) bool {
	g.readErrLock.Lock()
	defer g.readErrLock.Unlock()
	return now.Before(g.readBackoffUntil)
}

func (g *Game) sendReads() {
	defer func() {
		if err := recover(); err != nil {
			util.LogPanic(err)
		}
	}()

	// every 8 msec prepare to send a batch of read requests:
	t := time.NewTicker(time.Millisecond * 8)

sendloop:
	for {
		select {
		case <-t.C:
			// leave the reads queued while backing off after a failed read:
			if g.inReadBackoff(time.Now()) {
				break
			}

			var reads []snes.Read

			// submit and clear the highest priority read:
			g.priorityReadsMu.Lock()
			for p := range g.priorityReads {
				reads = g.priorityReads[p]
				if len(reads) == 0 {
					continue
				}

				// clear the read so it doesn't repeat on the next tick:
				g.priorityReads[p] = nil

				// we have `reads` assigned and need to submit it to the queue:
				break
			}
			g.priorityReadsMu.Unlock()

			if reads != nil {
				// NOTE: very important to not do this while under g.priorityReadsMu.Lock():
				g.readSubmit(reads)
			}
			break
		case <-g.stopped:
			break sendloop
		}
	}
}

// run in a separate goroutine
func (g *Game) run() {
	q := make([]snes.Read, 0, 12)

	// kick off initial WRAM read request:
	g.priorityReadsMu.Lock()
	q = g.queueReads(q)
	// must always read module number LAST to validate the prior reads:
	q = g.enqueueMainRead(q)
	g.priorityReads[2] = q
	g.priorityReadsMu.Unlock()

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
			g.lastReadCompleted = time.Now()
			g.readMainComplete(rsps)
			break

		// wait for network message from server:
		case msg := <-g.client.Read():
			if msg == nil {
				// disconnected?
				g.noteServerLost(g.local != nil && g.local.Index() >= 0)
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

			// handle any reset state machine messages received:
			g.processResetMessages(time.Now())

			if err != nil {
				break
			}
			break

		// local player requested a reset:
		case <-g.resetRequests:
			if !g.IsRunning() {
				return
			}

			g.initiateReset(time.Now())
			break

		// periodically send basic messages to the server to maintain our connection:
		case <-fastbeat.C:
			if !g.IsRunning() {
				return
			}

			if g.LocalPlayer().Index() < 0 && g.client != nil {
				// request our player index:
				m := g.makeJoinMessage()
				g.send(m)
			}

			if g.getQueue() != nil {
				// only issue a new read if there's not an update-check in progress:
				g.updateLock.Lock()
				stg := g.updateStage
				g.updateLock.Unlock()
				if stg == 0 {
					// make sure a read request is always in flight to keep our main loop running:
					// (unless reads are backing off after a failure; they are already queued for when it ends)
					now := time.Now()
					timeSinceRead := now.Sub(g.lastReadCompleted)
					if timeSinceRead > time.Millisecond*250 && !g.inReadBackoff(now) {
						g.priorityReadsMu.Lock()
						log.Printf("alttp: fastbeat: enqueue main reads; %d msec since last read\n", timeSinceRead.Milliseconds())
						q := make([]snes.Read, 0, 12)
						q = g.queueReads(q)
						q = g.enqueueMainRead(q)
						g.priorityReads[2] = q
						g.priorityReadsMu.Unlock()
					}
				}
			}

			// tell the user if the patch version can't be read from the SNES:
			g.checkPatchSignatureTimeout(time.Now())

			// drive the reset state machine:
			g.tickReset(time.Now())

			// update run timer:
			g.updateRunTimer()

			// update the debug page's view of the game:
			g.notifyDebugState()
			g.notifyBandwidth()
			break

		case <-slowbeat.C:
			if !g.IsRunning() {
				return
			}

			if g.LocalPlayer().Index() < 0 {
				break
			}

			g.sendEcho()

			g.sendPlayerName()

			// tell the others how long we were disconnected:
			g.announceGaps(time.Now())

			break
		}
	}
}

func (g *Game) sendEcho() {
	// send an echo to the server to measure roundtrip time:
	g.lastServerSentTime = time.Now()
	g.send(&gameEchoMessage{g: g})
}

func (g *Game) sendPlayerName() {
	// don't send any state (including run timer start/finish times) while resetting:
	if g.IsResetting() {
		return
	}

	p := g.LocalPlayer()

	// broadcast player name to all clients (alttpo format; exactly 20 bytes):
	m := g.makeBroadcastMessage()
	m.WriteByte(byte(MsgPlayerName))
	var name [20]byte
	n := copy(name[:], p.Name())
	for ; n < 20; n++ {
		name[n] = ' '
	}
	m.Write(name[:])
	g.send(m)

	// broadcast run timer to o2 clients only (older o2 versions read only this):
	o := g.makeO2BroadcastMessage()
	if err := SerializeRunTimes(o, p.GameStartTime, p.GameFinishTime); err != nil {
		log.Printf("alttp: serialize run times: %v\n", err)
		return
	}
	g.send(o)

	// and the run timing, shared with alttpo:
	g.stateLock.Lock()
	g.sendRunTiming()
	g.stateLock.Unlock()
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

func (g *Game) extractWRAMByte(rsp snes.Response, addr uint32) (val uint8, ok bool) {
	// not in WRAM?
	if rsp.Address < 0xF50000 || rsp.Address >= 0xF70000 {
		return 0, false
	}

	// check if address in read range:
	i := addr - rsp.Address
	if i >= uint32(len(rsp.Data)) {
		return 0, false
	}

	return rsp.Data[i], true
}

func (g *Game) enqueueSRAMRead(q []snes.Read) []snes.Read {
	// read the SRAM copy for underworld and overworld:
	q = g.readEnqueue(q, 0xF5F000, 0xFE, nil) // [$F000..$F0FD]
	q = g.readEnqueue(q, 0xF5F0FE, 0xFE, nil) // [$F0FE..$F1FB]
	q = g.readEnqueue(q, 0xF5F1FC, 0x54, nil) // [$F1FC..$F24F]
	q = g.readEnqueue(q, 0xF5F280, 0xC0, nil) // [$F280..$F33F]
	// the patch signature, to make sure the SNES is still running the ROM we patched:
	q = g.enqueuePatchSignatureRead(q)
	// the player colors the patch applies to Link's palette:
	q = g.enqueueLinkColorRead(q)
	// the overlords, to find a tile room overlord in the underworld:
	q = g.enqueueFlyingTilesRead(q, true)
	return q
}

func (g *Game) enqueueWRAMReads(q []snes.Read) []snes.Read {
	// FX Pak Pro allows batches of 8 VGET requests to be submitted at a time:

	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	q = g.readEnqueue(q, 0xF50100, 0x36, nil) // [$0100..$0135]
	q = g.readEnqueue(q, 0xF502E0, 0x11, nil) // [$02E0..$02F0]; $02F0 = desert statues released
	q = g.readEnqueue(q, 0xF50352, 0xCE, nil) // [$0352..$041F]; $0352 = Link's OAM buffer offset
	if isOverworldModule(Module(g.wram[0x10])) {
		// the world Link is in, to make sure overworld changes are only applied in the area of the same world:
		q = g.readEnqueue(q, 0xF50FFF, 0x01, nil) // [$0FFF]
	} else {
		// $1980..19E9 for reading underworld door state (19A0)
		q = g.readEnqueue(q, 0xF51980, 0x6A, nil) // [$1980..$19E9]
	}
	// ALTTP's SRAM copy in WRAM:
	q = g.readEnqueue(q, 0xF5F340, 0xFF, nil) // [$F340..$F43E]
	q = g.readEnqueue(q, 0xF5F43F, 0xC1, nil) // [$F43E..$F4FF]
	// crystal switch state:
	q = g.readEnqueue(q, 0xF5C172, 0x01, nil) // [$C172]
	// the current overworld area's flags, to send them as soon as a heart piece is collected:
	if area := g.wram[0x8A]; g.wram[0x1B] == 0 && area < 0xC0 {
		q = g.readEnqueue(q, 0xF5F280+uint32(area), 0x01, nil) // [$F280+$8A]
	}
	// trap doors, manipulable object count, map16 change log index, star switch state and hole overlay, the
	// overworld entrance animation and the torch timers:
	q = g.readEnqueue(q, 0xF50466, 0x9A, nil) // [$0466..$04FF]
	// pull switch ($0642), hold switch ($0646) and chest positions ($06E0..$06EB) in the underworld:
	q = g.enqueueRoomReads(q)
	// overworld map16 change log and underworld manipulable objects:
	q = g.enqueueMap16Reads(q)
	q = g.enqueueOwEventReads(q)
	q = g.enqueueManipReads(q)
	// the overlords while the room has a tile room overlord:
	q = g.enqueueFlyingTilesRead(q, false)
	// the enemies killed in the room or area:
	q = g.enqueueEnemiesKilledReads(q)

	// sprite palettes 0..7 (Link, sword and shield, sparkles, dust, shadow); color 0 of palette 0 is transparent and
	// not read, so this fits in a single read:
	q = g.readEnqueue(q, 0xF5C601, 0xFF, nil)

	// Link's sprites; this pushes the read cycle beyond a single FX Pak Pro batch of 8 reads:
	q = g.enqueueSpriteReads(q)
	// the frame counters, the run's start and finish latches and the features the patch runs:
	q = g.enqueueRunTimingRead(q)

	return q
}

func (g *Game) enqueueMainRead(q []snes.Read) []snes.Read {
	// NOTE: order matters! must read the module number LAST to make sure all reads prior are valid.
	q = g.readEnqueue(q, 0xF50010, 0xF0, nil) // [$0010..$00FF]
	return q
}

func (g *Game) queueReads(q []snes.Read) []snes.Read {
	if g.monotonicFrameTime&7 == 7 {
		// read SRAM data less frequently:
		q = g.enqueueSRAMRead(q)

		if debugSprites {
			// DEBUG read sprite WRAM:
			q = g.readEnqueue(q, 0xF50D00, 0xF0, 1) // [$0D00..$0DEF]
			q = g.readEnqueue(q, 0xF50DF0, 0xF0, 1) // [$0DF0..$0EDF]
			q = g.readEnqueue(q, 0xF50EE0, 0xC0, 1) // [$0EE0..$0F9F]
		}
	} else {
		// normally read just WRAM data:
		q = g.enqueueWRAMReads(q)

		// the SRAM reads include the patch signature, but they only happen while syncing; read the signature until
		// it matches, and then periodically in case a different ROM was loaded:
		g.readCycle++
		if !g.ROMVerified || g.readCycle&7 == 0 {
			q = g.enqueuePatchSignatureRead(q)
		}
	}

	return q
}

// enqueueUnlocked runs enqueue with g.priorityReadsMu temporarily released (it must be held by the caller) so that
// the enqueue can't deadlock with sendReads. The mutex is re-acquired even if enqueue panics, so the caller's deferred
// Unlock never unlocks an unlocked mutex (a fatal, unrecoverable error).
func (g *Game) enqueueUnlocked(enqueue func() error) error {
	g.priorityReadsMu.Unlock()
	defer g.priorityReadsMu.Lock()
	return enqueue()
}

// clearQueue disconnects from the queue after a terminal error, unless it was already replaced by a new one:
func (g *Game) clearQueue(q snes.Queue) {
	g.queueLock.Lock()
	defer g.queueLock.Unlock()
	if g.queue == q {
		g.queue = nil
	}
}

// called when all reads are completed:
func (g *Game) readMainComplete(rsps []snes.Response) {
	g.stateLock.Lock()
	defer g.stateLock.Unlock()

	// disallow any reads until we figure out what we need:
	g.priorityReadsMu.Lock()
	defer g.priorityReadsMu.Unlock()

	g.priorityReads[0] = nil
	g.priorityReads[1] = nil
	g.priorityReads[2] = nil

	q := make([]snes.Read, 0, 16)

	// assume module is invalid until we read it:
	moduleStaging := -1
	submoduleStaging := -1

	needUpdateCheck := false
	now := time.Now()
	g.updateLock.Lock()
	for _, rsp := range rsps {
		// check WRAM reads:
		if val, ok := g.extractWRAMByte(rsp, 0xF50010); ok {
			// did we read the module number?
			moduleStaging = int(val)
		}
		if val, ok := g.extractWRAMByte(rsp, 0xF50011); ok {
			submoduleStaging = int(val)
		}
		// ignore SRAM for staging.
		g.checkPatchSignature(rsp)

		// handle update routine check:
		if g.updateStage > 0 {
			// escape mechanism for long-running updates:
			if now.Sub(g.lastUpdateTime) > timing.Frame*600 {
				log.Printf("alttp: update: wait time elapsed with no confirmation of asm execution; aborting\n")
				g.abortUpdateGenerators()
				g.updateStage = 0
				g.nextUpdateA = !g.nextUpdateA
				g.lastUpdateTarget = 0xFFFFFF
				g.lastUpdateFrame ^= 0xFF
				g.cooldownTime = now
			} else if rsp.Address == g.lastUpdateTarget {
				// check the "update" read:
				ins0 := rsp.Data[0]
				updateFrameCounter := rsp.Data[1]
				log.Printf("alttp: update: check: $%06x [$%02x,$%02x] ?= [$60,$%02x]\n", rsp.Address, ins0, updateFrameCounter, g.lastUpdateFrame)
				// when executed, the routine replaces its first instruction with RTS ($60):
				if ins0 == 0x60 && updateFrameCounter == g.lastUpdateFrame {
					// allow next update:
					log.Printf("alttp: update: complete: $%06x [$%02x,$%02x] == [$60,$%02x]\n", rsp.Address, ins0, updateFrameCounter, g.lastUpdateFrame)
					if g.updateStage == 2 {
						// confirm ASM execution:
						log.Printf("alttp: update: states = %v\n", rsp.Data[2:2+len(g.updateGenerators)])
						for i, generator := range g.updateGenerators {
							generator.ConfirmAsmExecuted(uint32(i), rsp.Data[i+2])
						}

						g.updateStage = 0
						g.nextUpdateA = !g.nextUpdateA
						g.lastUpdateTarget = 0xFFFFFF
						g.lastUpdateFrame ^= 0xFF
						g.cooldownTime = now
					}
				} else {
					needUpdateCheck = true
				}
			}
		}
	}

	// we're not sure if an update is coming soon so prevent any background tickers from requesting a read:
	if g.updateStage == 0 {
		g.updateStage = -1
	}
	g.updateLock.Unlock()

	func() {
		if moduleStaging == -1 {
			return
		}
		if submoduleStaging == -1 {
			return
		}

		// backup the current WRAM:
		copy(g.wramLastFrame[:], g.wram[:])
		for i := range g.wramFresh {
			g.wramFresh[i] = false
		}
		g.linkSprites.oamCopyFresh = false

		// the server time the reads completed at, for the run timing; unknown until a message from the server came:
		var readAt time.Time
		if !g.lastServerTime.IsZero() {
			readAt = g.ServerNow()
		}

		// copy the read data into our view of memory:
		for _, rsp := range rsps {
			// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
			if start, end, ok := g.isReadWRAM(rsp); ok {
				// copy in new data:
				copy(g.wram[start:end], rsp.Data)
				for i := start; i <= end; i++ {
					g.wramFresh[i] = true
				}
			}
			// $E0-EF:xxxx is SRAM, aka $70-7D:xxxx
			if _, _, ok := g.isReadSRAM(rsp); ok {
				g.readSpriteResponse(rsp)
				g.readLinkColorResponse(rsp)
				g.readRunTimingResponse(rsp, readAt)
			}
		}

		// assign local variables from WRAM:
		local := g.LocalPlayer()

		g.SetTTL(local, 255)

		// log module changes regardless of syncing:
		if g.lastModule != moduleStaging || g.lastSubModule != submoduleStaging {
			log.Printf("alttp: local: module [$%02x,$%02x]\n", moduleStaging, submoduleStaging)
			if moduleStaging <= 0x05 && g.lastModule > 0x05 {
				// back at the title, file select or loading screens; a different game may be loaded so re-read small
				// key counts without attributing any changes to the local player and re-adopt the group's counts:
				g.invalidateSmallKeys(true)
				g.invalidateHearts(true)
			}
		}
		g.lastModule = moduleStaging
		g.lastSubModule = submoduleStaging

		// always track the current module, even when not syncing, so that IsInGame() and the location packet
		// reflect when the local player is in a menu (e.g. file select) rather than the last in-game module:
		newModule, newSubModule, newSubSubModule := Module(g.wram[0x10]), g.wram[0x11], g.wram[0xB0]
		if local.Module != newModule || local.SubModule != newSubModule {
			log.Printf(
				"alttp: local: module [$%02x,$%02x] -> [$%02x,$%02x]\n",
				local.Module,
				local.SubModule,
				newModule,
				newSubModule,
			)
		}
		local.Module, local.SubModule, local.SubSubModule = newModule, newSubModule, newSubSubModule

		// this is documented as a uint16, but we use it as a uint8
		local.PriorModule = Module(g.wram[0x010C])

		doSync := true
		if _, ok := modulesOKForSync[uint8(moduleStaging)]; !ok {
			// bad module:
			doSync = false
		}
		if !g.ROMVerified {
			// the SNES is not running the ROM we patched:
			doSync = false
		}

		if !doSync {
			if g.syncing {
				log.Printf("alttp: DISABLED syncing [$%02x,$%02x]", moduleStaging, submoduleStaging)
			}
			g.syncing = false
			return
		}

		if !g.syncing {
			log.Printf("alttp:  ENABLED syncing [$%02x,$%02x]", moduleStaging, submoduleStaging)
			g.syncing = true
		}

		// sample location the same way alttpo does (LocalGameState::fetch_basics): don't update it during screen
		// transitions so that it stays on the room or area the player's screen is still showing:
		if local.canSampleLocation() && !local.isInScreenTransition() {
			inDungeon := g.wram[0x1B]
			overworldArea := g.wramU16(0x8A)
			dungeonRoom := g.wramU16(0xA0)
			if local.OverworldArea != overworldArea {
				log.Printf(
					"alttp: local: entered area $%02x (was $%02x) ; %s\n",
					overworldArea,
					local.OverworldArea,
					overworldNames[overworldArea],
				)
			}
			if local.DungeonRoom != dungeonRoom {
				log.Printf(
					"alttp: local: entered room $%03x (was $%03x) ; %s\n",
					dungeonRoom,
					local.DungeonRoom,
					underworldNames[dungeonRoom],
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
					"alttp: local: entered %s (was $%04x)\n",
					describeDungeon(dungeon, g.wramU16(0x010E)),
					local.Dungeon,
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

		}

		if local.Module.IsOverworld() {
			local.LastOverworldX = local.X
			local.LastOverworldY = local.Y
		}

		// Link's coordinates and the screen scroll offsets are sampled every frame, including during screen
		// transitions, like alttpo does. alttpo draws a remote player's OAM sprites (which are relative to that
		// player's screen) offset by the difference between both players' scroll offsets; if these went stale while
		// the screen scrolls during a room transition, the sprites would drift across the other player's screen:
		if !(local.Module == 0x0E && local.SubModule == 0x07) {
			local.X = g.wramU16(0x22)
			local.Y = g.wramU16(0x20)

			// BG2 scroll offsets minus the screen shake offsets:
			local.XOffs = int16(g.wramU16(0xE2)) - int16(g.wramU16(0x11A))
			local.YOffs = int16(g.wramU16(0xE8)) - int16(g.wramU16(0x11C))
		}

		// copy $7EF000-4FF into `local.SRAM`:
		//copy(local.SRAM[:], g.wram[0xF000:0xF500])

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
			fmt.Print(sb.String())
		}

		if g.shouldUpdatePlayersList {
			g.updatePlayersList()
		}

		// did game frame change?
		newFrame := g.wram[0x1A]
		if newFrame == g.lastGameFrame {
			return
		}

		// update frame as early as possible:
		g.lastGameFrame = newFrame

		// should wrap around 255 to 0:
		g.monotonicFrameTime++

		//log.Printf("server now(): %v\n", g.ServerNow())

		// handle WRAM reads:
		g.readWRAM()
		g.notFirstWRAMRead = true

		// sample Link's sprites to send to alttpo clients:
		g.readLinkSprites()
		// send the sound effects the game played:
		g.readLocalSfx()

		// update underworld supertile state sync bit masks based on sync toggles from front-end:
		g.setUnderworldSyncMasks()

		// generate notifications about locally picked up items:
		g.localChecks()
	}()

	// don't generate new ASM updates until confirmation of last update; don't sync anything while resetting:
	if !needUpdateCheck && !g.IsResetting() {
		// generate any WRAM update code and send it to the SNES. Snapshot the queue once; it is cleared concurrently
		// when the SNES disconnects:
		q := g.getQueue()
		if writes, ok := g.updateWRAM(q); ok {
			if err := g.enqueueUnlocked(func() error {
				return q.MakeWriteCommands(
					writes,
					func(cmd snes.Command, err error) {
						log.Println("alttp: update: write completed")

						g.updateLock.Lock()
						if g.updateStage != 1 {
							log.Printf("alttp: update: write complete but updateStage = %d (should be 1)\n", g.updateStage)
							g.abortUpdateGenerators()
							g.updateStage = 0
							g.updateLock.Unlock()
							return
						}

						g.updateStage = 2
						g.lastUpdateTime = time.Now()
						g.updateLock.Unlock()

						g.priorityReadsMu.Lock()
						q := make([]snes.Read, 0, 8)
						q = g.enqueueUpdateCheckRead(q)
						// must always read module number LAST to validate the prior reads:
						q = g.enqueueMainRead(q)

						// we must only allow for check-for-update:
						g.priorityReads[0] = q
						g.priorityReads[1] = nil
						g.priorityReads[2] = nil
						g.priorityReadsMu.Unlock()
					},
				).EnqueueTo(q)
			}); err != nil {
				log.Println(fmt.Errorf("alttp: update: error enqueuing snes write for update routine: %w", err))
				g.updateLock.Lock()
				g.abortUpdateGenerators()
				g.updateStage = 0
				g.updateLock.Unlock()
				var termErr *snes.TerminalError
				if errors.As(err, &termErr) {
					log.Println("alttp: update: terminal error encountered; disconnecting from queue")
					_ = termErr
					g.clearQueue(q)
				}
				return
			}
		}
	}

	// the features the patch runs, and clearing the run's latches:
	g.writeRunTiming(now)
	// update the remote players the game draws:
	g.writeRemoteSprites(now)
	// queue remote players' sound effects for the game to play:
	g.writeRemoteSfx(now)
	// tell the game whether to disable the BG music:
	g.writeMusicSetting(now)
	// stage remote map16 changes for the game to apply when it loads their areas:
	g.writeMap16Stage(now)
	// stage remote manipulable objects for the game to apply when it loads their rooms:
	g.writeManipStage(now)

	// send out any network updates:
	g.sendPackets()

	// tick down TTLs of remote players:
	for _, p := range g.ActivePlayers() {
		g.DecTTL(p, 1)
	}

	g.notFirstFrame = true

	// now determine what to read next:
	if needUpdateCheck {
		// check for asm/update completion:
		q = g.enqueueUpdateCheckRead(q)
		q = g.enqueueMainRead(q)
		g.priorityReads[0] = q
		return
	}

	g.updateLock.Lock()
	if g.updateStage == -1 {
		// no update was made so we're okay now:
		g.updateStage = 0
	}
	g.updateLock.Unlock()

	// don't issue a normal read if an update is in progress:
	if g.updateStage != 0 {
		return
	}

	q = g.queueReads(q)
	q = g.enqueueMainRead(q)
	g.priorityReads[2] = q

	return
}

func (g *Game) wramU8(addr uint32) uint8 {
	addr &= 0x01FFFF
	return g.wram[addr]
}

func (g *Game) wramU16(addr uint32) uint16 {
	addr &= 0x01FFFF
	return binary.LittleEndian.Uint16(g.wram[addr : addr+2])
}
