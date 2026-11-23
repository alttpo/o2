package alttp

import (
	"io"
	"log"
	"time"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Sound effects (message 0x02), shared with alttpo
//
// The game queues sound effects for the APU in $012E (SFX2) and $012F (SFX3) during the frame; NMI writes them to the
// APU ports and clears them. Bits 6-7 are the pan (0 = center, $40 = right, $80 = left). alttpo reads both right before
// NMI (pre_nmi.as), sends them in message 0x02 (`sfx2 u8`, `sfx3 u8`) and plays a remote player's sound effects by
// writing them there right before NMI, only while the local game left the slot empty: local sounds come first, and a
// remote sound waits for a free slot (up to 16 frames in alttpo).
//
// o2 can't read WRAM at a chosen point of the frame, and the values only live until the next NMI, so the frame
// trampoline does both (see emitSfx), right after the module ran:
//
//   - Local: a frame that queued SFX2 or SFX3 appends both to a ring of sfxLocalCount entries in the copy of Link's
//     OAM entries (linkOAMCopySfx), which o2 already reads every frame, and increments its count. o2 sends every entry
//     it has not seen yet, in its own packet each, since alttpo keeps only the last 0x02 of a packet.
//   - Remote: o2 queues the sound effects of players in the same location (with the pan adjusted to where the sound is
//     relative to the local player, like alttpo's adjust_sfx_pan) in a ring of sfxRemoteCount entries at sfxRemoteAddr
//     and increments its count. The trampoline writes an entry's SFX2 and SFX3 into $012E/$012F each only if the game
//     left that slot empty this frame, clears what it played and moves on to the next entry once both are played. Its
//     own count of entries played is in the copy o2 reads (linkOAMCopySfx + sfxLocalPlayed), so o2 knows which slots
//     are free again. Entries are dropped outside the modules where the game plays sound effects for the player
//     (the same modules o2's update routines run in), and o2 drops sound effects that waited too long to be queued.
//
// Remote sound effects are queued after the local ones are captured, so they are not sent back out.
const (
	// layout of the local sound effects at linkOAMCopyAddr + linkOAMCopySfx:
	sfxLocalCountOffs = 0 // u8: entries written by the game
	sfxLocalRingOffs  = 1 // sfxLocalCount entries of SFX2, SFX3
	sfxLocalCount     = 8
	sfxLocalPlayed    = sfxLocalRingOffs + sfxLocalCount*2 // u8: remote entries the game finished playing
	sfxLocalSize      = sfxLocalPlayed + 1

	// sfxRemoteAddr holds the remote sound effects o2 queues for the game: sfxRemoteCount entries of SFX2, SFX3, then
	// the number of entries o2 queued (u8). Zeroed on reset:
	sfxRemoteAddr     = linkColorAddr + linkColorSize
	sfxRemoteCount    = 4
	sfxRemoteQueued   = sfxRemoteCount * 2
	sfxRemoteSize     = sfxRemoteQueued + 1
	sfxRemoteMaxDelay = 16 * time.Second / 60
)

// the remote sound effects must fit between the player's color and the capture mask, in the area zeroed on reset:
const _ = captureMaskAddr - (sfxRemoteAddr + sfxRemoteSize)

// sfxRemote is the last sound effect pair received from a remote player and not yet queued for the game:
type sfxRemote struct {
	sfx      [2]uint8
	received time.Time
}

type sfxLocal struct {
	// the game's count of local entries already sent, and whether it was read since the last reset of the game:
	sent   uint8
	sentOK bool
	// entries queued for the game, and whether the count was written since the last reset of the game:
	queued   uint8
	queuedOK bool
}

// isSfxFiltered reports whether a sound effect is not sent, like alttpo's fetch_sfx: the low life warning beep
// (SFX2 $2B), the thunder of the rain (SFX2 $36, which runs into the rain's rumble; the game repeats it in every rainy
// area, so each player hears their own), and menu and map sounds (SFX3 $0C, $10, $11, $12, $20, $24). The sounds
// that loop (rain, wind, the rain's rumble) are ambient ($012D), which is never synced.
func isSfxFiltered(slot int, sfx uint8) bool {
	if slot == 0 {
		switch sfx & 0x3F {
		case 0x2B, 0x36:
			return true
		}
		return false
	}
	switch sfx & 0x3F {
	case 0x0C, 0x10, 0x11, 0x12, 0x20, 0x24:
		return true
	}
	return false
}

// readLocalSfx sends the local sound effects the game captured since the last read (see emitSfx). Called once per game
// frame after the copy of Link's OAM entries was read.
func (g *Game) readLocalSfx() {
	s := &g.sfx
	c := g.linkSprites.oamCopy[linkOAMCopySfx : linkOAMCopySfx+sfxLocalSize]
	count := c[sfxLocalCountOffs]
	if !s.sentOK || uint8(count-s.sent) > sfxLocalCount {
		// first read, or the game was reset (the count went back) or o2 fell too far behind: start from here:
		s.sent, s.sentOK = count, true
		return
	}

	local := g.local
	for ; s.sent != count; s.sent++ {
		if local == nil || g.local.Index() < 0 || !g.syncing || !g.SyncSfx || local.isBadTimeToDraw() {
			continue
		}
		o := sfxLocalRingOffs + int(s.sent%sfxLocalCount)*2
		var sfx [2]uint8
		for k := range sfx {
			if v := c[o+k]; v != 0 && !isSfxFiltered(k, v) {
				sfx[k] = v
			}
		}
		if sfx == [2]uint8{} {
			continue
		}
		// sent with the frame's packet to all players (see sendPackets):
		if len(g.sfxOut) < sfxLocalCount {
			g.sfxOut = append(g.sfxOut, sfx)
		}
	}
}

// sendSfx sends the sound effects read since the last frame sent.
func (g *Game) sendSfx() {
	for _, sfx := range g.sfxOut {
		m := g.makeBroadcastMessage()
		m.addBytes([]byte{byte(MsgSfx), sfx[0], sfx[1]})
		g.send(m)
	}
	g.sfxOut = g.sfxOut[:0]
}

func (g *Game) DeserializeSfx(p *Player, r io.Reader) (err error) {
	var b [2]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		return
	}
	// like alttpo, a new sound effect replaces one that was not played yet:
	for k := range b {
		if b[k] != 0 {
			p.Sfx.sfx[k] = b[k]
			p.Sfx.received = time.Now()
		}
	}
	return
}

// adjustSfxPan pans a remote player's sound effect to where it is relative to the local player, like alttpo's
// adjust_sfx_pan: the sender's pan places it 40 pixels left or right of the sender.
func adjustSfxPan(sfx uint8, remoteX, localX uint16) uint8 {
	sx := int(remoteX)
	if sfx&0x80 != 0 {
		sx -= 40
	} else if sfx&0x40 != 0 {
		sx += 40
	}
	sfx &= 0x3F
	if d := sx - int(localX); d <= -40 {
		sfx |= 0x80
	} else if d >= 40 {
		sfx |= 0x40
	}
	return sfx
}

// sfxRemotePak is the FX Pak Pro address of the remote sound effects:
var sfxRemotePak = func() uint32 {
	pak, err := lorom.BusAddressToPak(sfxRemoteAddr)
	if err != nil {
		panic(err)
	}
	return pak
}()

// writeRemoteSfx queues the sound effects of remote players in the same location for the game to play (see emitSfx).
func (g *Game) writeRemoteSfx(now time.Time) {
	s := &g.sfx
	local := g.local
	if !g.ROMVerified || g.IsResetting() || local == nil || !g.linkSprites.oamCopyFresh {
		return
	}
	q := g.getQueue()
	if q == nil {
		return
	}

	played := g.linkSprites.oamCopy[linkOAMCopySfx+sfxLocalPlayed]
	if !s.queuedOK || uint8(s.queued-played) > sfxRemoteCount {
		// the game was reset (its count went back) or o2 lost track; the game plays the entries between its count and
		// ours, so start from its count:
		s.queued = played
		if err := g.enqueueUnlocked(func() error {
			return q.MakeWriteCommands([]snes.Write{{Address: sfxRemotePak + sfxRemoteQueued, Size: 1, Data: []byte{played}}}, nil).EnqueueTo(q)
		}); err != nil {
			log.Printf("alttp: sfx: error enqueuing snes write: %v\n", err)
			return
		}
		s.queuedOK = true
	}

	var writes []snes.Write
	for _, p := range g.RemotePlayers() {
		if p == local || p.Sfx.sfx == [2]uint8{} {
			continue
		}
		if !g.SyncSfx || p.TTL() <= 0 || now.Sub(p.Sfx.received) > sfxRemoteMaxDelay || local.isBadTimeToDraw() ||
			!locationsEqual(local.Location, p.Location) {
			p.Sfx.sfx = [2]uint8{}
			continue
		}
		// wait for a free slot; the game frees one per frame at most:
		if uint8(s.queued-played) >= sfxRemoteCount {
			break
		}
		var e [2]byte
		for k, v := range p.Sfx.sfx {
			if v != 0 {
				e[k] = adjustSfxPan(v, p.X, local.X)
			}
		}
		p.Sfx.sfx = [2]uint8{}
		writes = append(writes, snes.Write{
			Address: sfxRemotePak + uint32(s.queued%sfxRemoteCount)*2,
			Size:    2,
			Data:    e[:],
		})
		s.queued++
	}
	if len(writes) == 0 {
		return
	}
	// the count last, after the entries it makes the game play:
	writes = append(writes, snes.Write{Address: sfxRemotePak + sfxRemoteQueued, Size: 1, Data: []byte{s.queued}})
	if err := g.enqueueUnlocked(func() error {
		return q.MakeWriteCommands(writes, nil).EnqueueTo(q)
	}); err != nil {
		log.Printf("alttp: sfx: error enqueuing snes write: %v\n", err)
	}
}

// emitSfx emits the trampoline's code, run right after the module, that appends the sound effects the game queued this
// frame to the ring o2 reads, and then plays one queued remote entry in whichever of $012E/$012F the game left empty.
// Called with A and X 8-bit.
//
// Cycles (cycles_test.go): 75 with no sound effects queued; the paths that queue and play them are not measured yet.
func emitSfx(a *asm.Emitter) {
	a.AssumeSEP(0x30)
	local := linkOAMCopyAddr + linkOAMCopySfx
	emitTimeCheckLong(a, mustLateLineFor(sfxCycles, "sfx"), "sfx_time", "sfxSkip")
	a.Comment("sound effects: append the ones the game queued this frame for o2 to send:")
	a.PHP()
	a.PHB()
	a.SEP(0x30)
	a.PEA(0x7070)
	a.PLB()
	a.PLB()
	localCount := uint16((local + sfxLocalCountOffs) & 0xFFFF)
	localRing := uint16((local + sfxLocalRingOffs) & 0xFFFF)
	a.LDA_long(0x7E012E)
	a.ORA_long(0x7E012F)
	a.BEQ("sfxLocalDone")
	a.LDA_abs(localCount)
	a.AND_imm8_b(sfxLocalCount - 1)
	a.ASL()
	a.TAX()
	a.LDA_long(0x7E012E)
	a.STA_abs_x(localRing)
	a.LDA_long(0x7E012F)
	a.STA_abs_x(localRing + 1)
	a.INC_abs(localCount)
	a.Label("sfxLocalDone")

	played := uint16((local + sfxLocalPlayed) & 0xFFFF)
	remote := uint16(sfxRemoteAddr & 0xFFFF)
	queued := uint16((sfxRemoteAddr + sfxRemoteQueued) & 0xFFFF)
	a.Comment("drop the queued remote sound effects outside the modules the player's sounds are played in:")
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BCC("sfxDrop")
	a.CMP_imm8_b(0x19)
	a.BCS("sfxDrop")
	a.CMP_imm8_b(0x08)
	a.BEQ("sfxDrop")
	a.CMP_imm8_b(0x0A)
	a.BEQ("sfxDrop")
	a.CMP_imm8_b(0x14)
	a.BNE("sfxPlay")
	a.Label("sfxDrop")
	a.LDA_abs(queued)
	a.STA_abs(played)
	a.BRA("sfxExit")

	a.Label("sfxPlay")
	a.Comment("play the next queued remote entry in the slots the game left empty (local sounds come first):")
	a.LDA_abs(played)
	a.CMP_abs(queued)
	a.BEQ("sfxExit")
	a.AND_imm8_b(sfxRemoteCount - 1)
	a.ASL()
	a.TAX()
	a.LDA_abs_x(remote)
	a.BEQ("sfxPlay3")
	a.LDA_long(0x7E012E)
	a.BNE("sfxPlay3")
	a.LDA_abs_x(remote)
	a.STA_long(0x7E012E)
	a.STZ_abs_x(remote)
	a.Label("sfxPlay3")
	a.LDA_abs_x(remote + 1)
	a.BEQ("sfxPlayed")
	a.LDA_long(0x7E012F)
	a.BNE("sfxExit")
	a.LDA_abs_x(remote + 1)
	a.STA_long(0x7E012F)
	a.STZ_abs_x(remote + 1)
	a.Label("sfxPlayed")
	a.Comment("the next entry once both are played:")
	a.LDA_abs_x(remote)
	a.BNE("sfxExit")
	a.INC_abs(played)
	a.Label("sfxExit")
	a.PLB()
	a.PLP()
	a.Label("sfxSkip")
}
