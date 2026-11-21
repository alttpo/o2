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
// The game requests music by writing a track to $012C, which NMI sends to the APU. With the setting on, the trampoline
// (see emitMusic), right after the module ran, intercepts what the game requested in $012C before NMI sees it: it
// remembers the last requested track in musicWantedAddr and clears the request, so no track starts. A request to fade
// out is allowed, and a track that is currently playing is faded out. Nothing is intercepted in the intro modules ($00
// to $05), where it would break the game.
//
// Turning the setting off restarts the track that would be playing by requesting the remembered one once.
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

// emitMusic emits the trampoline's code, run right after the module, that disables the BG music when o2 says so, and
// restarts it when o2 says not to anymore. Called with A and X 8-bit.
func emitMusic(a *asm.Emitter) {
	a.AssumeSEP(0x30)
	a.Comment("BG music: intercept the track the game requests while o2 disables it, and restart it afterwards:")
	a.PHP()
	a.SEP(0x30)
	a.LDA_long(musicOffAddr)
	a.BEQ("musicOn")

	a.Comment("funny things happen when disabling music from the intro:")
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x06)
	a.BCC("musicExit")
	a.LDA_imm8_b(1)
	a.STA_long(musicWasOffAddr)
	a.LDA_long(0x7E012C)
	a.BEQ("musicNoCmd")
	a.Comment("special commands that control the volume are allowed:")
	a.CMP_imm8_b(0xF2)
	a.BCS("musicExit")
	a.STA_long(musicWantedAddr)
	a.Comment("allow fading out, nothing music and the mirror warp:")
	a.CMP_imm8_b(0xF1)
	a.BEQ("musicExit")
	a.CMP_imm8_b(0x20)
	a.BEQ("musicExit")
	a.CMP_imm8_b(0x08)
	a.BEQ("musicExit")
	a.Comment("changing from the mirror to some other track: fade out so that it can play again:")
	a.LDA_long(0x7E0130)
	a.CMP_imm8_b(0x08)
	a.BEQ("musicFade")
	a.Comment("drop the request:")
	a.LDA_imm8_b(0)
	a.STA_long(0x7E012C)
	a.BRA("musicExit")

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
	a.PLP()
}
