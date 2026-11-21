package alttp

import (
	"log"
	"time"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Disable BG Music, like alttpo's disable_bg_music (pre_nmi.as)
//
// The game requests music by writing a track to $012C, which NMI sends to the APU. With the setting on, the code at
// the start of NMI (see emitMusic, called by emitNMICount; the game can request tracks at any point of the frame, so
// only there it sees all of them, like alttpo's pre_nmi) intercepts what the game requested in $012C before NMI sends
// it: it clears the request, so no track starts. A request to fade out is allowed, and a track that is currently
// playing is faded out. Nothing is intercepted in the intro modules ($00 to $05), where it would break the game.
//
// musicWantedAddr remembers the last track the game requested that repeats. Turning the setting off restarts it by
// requesting it once. Tracks that play once (short jingles) are never remembered, so they are never repeated.
//
// o2 writes the setting in musicOffAddr, and rewrites it periodically since a reset of the console zeroes it.
const (
	// musicWasOffAddr is set by the trampoline while it intercepts, so that it requests the remembered track once
	// when the setting turns off. Zeroed on reset:
	musicWasOffAddr = sfxRemoteAddr + sfxRemoteSize
	// musicOffAddr is the setting, written by o2; musicWantedAddr is the last track the game requested, kept by the
	// trampoline. They are in the unused capture mask, zeroed on reset:
	musicOffAddr    = captureMaskAddr
	musicWantedAddr = captureMaskAddr + 1

	// o2 rewrites the setting this often:
	musicRewriteInterval = 2 * time.Second
)

const _ = musicWasOffAddr - (sfxRemoteAddr + sfxRemoteSize)
const _ = captureMaskAddr - musicWasOffAddr - 1
const _ = sramZeroEnd - (musicWantedAddr + 1)

type musicState struct {
	// what o2 wrote to the game last, and when:
	written   bool
	value     bool
	writtenAt time.Time
}

var musicOffPak = func() uint32 {
	pak, err := lorom.BusAddressToPak(musicOffAddr)
	if err != nil {
		panic(err)
	}
	return pak
}()

// writeMusicSetting tells the game whether to disable the BG music (see emitMusic).
func (g *Game) writeMusicSetting(now time.Time) {
	m := &g.music
	if !g.ROMVerified {
		m.written = false
		return
	}
	if m.written && m.value == g.DisableBgMusic && now.Sub(m.writtenAt) < musicRewriteInterval {
		return
	}
	q := g.getQueue()
	if q == nil {
		return
	}
	var b byte
	if g.DisableBgMusic {
		b = 1
	}
	if err := g.enqueueUnlocked(func() error {
		return q.MakeWriteCommands([]snes.Write{{Address: musicOffPak, Size: 1, Data: []byte{b}}}, nil).EnqueueTo(q)
	}); err != nil {
		log.Printf("alttp: music: error enqueuing snes write: %v\n", err)
		return
	}
	m.written, m.value, m.writtenAt = true, g.DisableBgMusic, now
}

// musicOneShots are the tracks that play once and don't repeat (kMsuTracksWithRepeat in zelda3); musicTrackMax is the
// first number past the tracks:
var musicOneShots = []uint8{0x01, 0x0A, 0x0F, 0x13, 0x1D, 0x21, 0x22}

const musicTrackMax = 0x30

// emitMusic emits the code, run at the start of NMI, that disables the BG music when o2 says so, and restarts it when
// o2 says not to anymore. Called with A 8-bit; uses long addressing for any DB and D.
func emitMusic(a *asm.Emitter) {
	a.Comment("BG music: intercept the track the game requests while o2 disables it, and restart it afterwards:")
	a.LDA_long(musicOffAddr)
	a.BEQ("musicOn")

	a.Comment("funny things happen when disabling music from the intro:")
	a.LDA_long(0x7E0010)
	a.CMP_imm8_b(0x06)
	a.BCC("musicExitNear")
	a.LDA_imm8_b(1)
	a.STA_long(musicWasOffAddr)
	a.LDA_long(0x7E012C)
	a.BEQ("musicNoCmd")
	a.Comment("special commands that control the volume are allowed:")
	a.CMP_imm8_b(0xF2)
	a.BCS("musicExitNear")
	a.Comment("allow fading out, nothing music and the mirror warp:")
	a.CMP_imm8_b(0xF1)
	a.BEQ("musicExitNear")
	a.CMP_imm8_b(0x20)
	a.BEQ("musicExitNear")
	a.CMP_imm8_b(0x08)
	a.BEQ("musicExitNear")
	a.Comment("remember tracks that repeat, not the ones that play once:")
	a.CMP_imm8_b(musicTrackMax)
	a.BCS("musicDrop")
	for _, t := range musicOneShots {
		a.CMP_imm8_b(t)
		a.BEQ("musicDrop")
	}
	a.STA_long(musicWantedAddr)
	a.Label("musicDrop")
	a.Comment("changing from the mirror to some other track: fade out so that it can play again:")
	a.LDA_long(0x7E0130)
	a.CMP_imm8_b(0x08)
	a.BEQ("musicFade")
	a.Comment("drop the request:")
	a.LDA_imm8_b(0)
	a.STA_long(0x7E012C)

	a.Label("musicExitNear")
	a.JMP_abs("musicExit")

	a.Label("musicNoCmd")
	a.Comment("no request: fade out any track that is playing:")
	a.LDA_long(0x7E0130)
	a.BEQ("musicExit")
	a.CMP_imm8_b(0x08)
	a.BEQ("musicExit")
	a.CMP_imm8_b(0xF1)
	a.BCS("musicExit")
	a.Label("musicFade")
	a.LDA_imm8_b(0xF1)
	a.STA_long(0x7E012C)
	a.STA_long(0x7E0130)
	a.BRA("musicExit")

	a.Label("musicOn")
	a.LDA_long(musicWasOffAddr)
	a.BEQ("musicExit")
	a.LDA_imm8_b(0)
	a.STA_long(musicWasOffAddr)
	a.LDA_long(musicWantedAddr)
	a.BEQ("musicExit")
	a.Comment("request the track the game last requested, once:")
	a.STA_long(0x7E012C)
	a.LDA_imm8_b(0)
	a.STA_long(musicWantedAddr)
	a.STA_long(0x7E0130)

	a.Label("musicExit")
}
