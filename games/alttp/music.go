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
// How the game manages music (NMI at $0080C9 and the overworld, underworld and entrance code in banks $02 and $09):
//
//   - $012C is the request. The game writes a track or command ($F1 fade, $F2 half volume, $F3 full volume) there.
//   - NMI sends a request to the APU unless it equals $0133 (the last track sent); then it sets $0133 to it, sets
//     $0130 (the track playing) to it unless it is $F2 or later, and clears $012C.
//   - $0132 is the track queued for the area or room being loaded; the loading code copies it to $012C.
//   - The game decides what to request by comparing the track an area or room wants with $0130: the overworld scroll
//     and mosaic transitions fade ($F1) when the area's track differs, and request the area's track once $0130 is
//     $F1. Several places also copy $0130 to $0133 to make NMI ignore a request equal to it.
//
// The z3randomizer (VT randomizer, DR) moves the request: the code after the module (CheckMusicLoadRequest) copies
// $012C to $012B and clears it, and NMI reads $012B instead (the operands of its LDA and STZ at $0080DD and $008101).
// So the request is the address NMI reads, found in the ROM (see musicRequestAddr). $0130 and $0133 mean the same.
//
// So that the game never picks a song, with the setting on the code at the start of NMI (see emitMusic, called by
// emitNMICount) swallows the requests of tracks that repeat: it sets $0130 and $0133 to the track requested as if it
// was sent, and clears the request. The game then believes the right track is playing, doesn't fade or request
// anything, and the APU hears nothing. When the setting turns on, a track that is playing is faded out once. Requests
// of tracks that play once (jingles, which the game can wait for, like the fanfares of the pendants and crystals in the
// randomizer), the mirror warp, nothing music and commands other than the volume commands are sent. Nothing is
// intercepted in the intro modules ($00 to $05), where it would break the game.
//
// musicWantedAddr remembers the last track the game requested that repeats, or the track that repeats that was playing
// when the setting turned on (so that it restarts after any number of toggles). Turning the setting off requests it once
// (and clears $0133, so that NMI doesn't skip it as the last track sent). Tracks that play once (short jingles) are
// never remembered, so they are never repeated.
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

// the operand of the LDA of the request in NMI, which is the request:
const musicRequestOperandAddr = uint32(0x0080DD)

// musicRequestAddr returns the address of the music request that NMI reads, or 0 if the ROM's NMI is not one that
// o2 knows how to intercept the music of.
func musicRequestAddr(rom *snes.ROM) uint32 {
	pak, err := lorom.BusAddressToPak(musicRequestOperandAddr - 1)
	if err != nil || int(pak)+3 > len(rom.Contents) {
		return 0
	}
	c := rom.Contents[pak : pak+3]
	// LDA.w $xxxx:
	if c[0] != 0xAD || c[2] != 0x01 || (c[1] != 0x2C && c[1] != 0x2B) {
		return 0
	}
	return 0x7E0100 | uint32(c[1])
}

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
// o2 says not to anymore. req is the address NMI reads the request from. Called with A 8-bit; uses long addressing for
// any DB and D.
func emitMusic(a *asm.Emitter, req uint32) {
	a.Comment("BG music: swallow the requests of tracks that repeat while o2 disables it, and restart one afterwards:")
	a.LDA_long(musicOffAddr)
	a.BNE("musicOff")

	a.LDA_long(musicWasOffAddr)
	a.BNE("musicRestart")
	a.Label("musicExitNear")
	a.JMP_abs("musicExit")

	a.Label("musicRestart")
	a.LDA_imm8_b(0)
	a.STA_long(musicWasOffAddr)
	a.Comment("request the track that repeats the game last requested, once; NMI skips a request equal to $0133:")
	a.STA_long(0x7E0133)
	a.LDA_long(req)
	a.BNE("musicRestartDone")
	a.LDA_long(musicWantedAddr)
	a.STA_long(req)
	a.Label("musicRestartDone")
	a.LDA_imm8_b(0)
	a.STA_long(musicWantedAddr)
	a.BRA("musicExitNear")

	a.Label("musicOff")
	a.Comment("funny things happen when disabling music from the intro:")
	a.LDA_long(0x7E0010)
	a.CMP_imm8_b(0x06)
	a.BCC("musicExitNear")

	a.BRA("musicStart")
	a.Label("musicClear")
	a.LDA_imm8_b(0)
	a.STA_long(req)
	a.BRA("musicExitNear")

	a.Label("musicFadeReq")
	a.LDA_long(musicWasOffAddr)
	a.BEQ("musicFadeSend")
	a.JMP_abs("musicSwallow")
	a.Label("musicFadeSend")
	a.Comment("the game's own fade out is sent the first time:")
	a.LDA_imm8_b(1)
	a.STA_long(musicWasOffAddr)
	a.BRA("musicExitNear")

	a.Label("musicStart")
	a.LDA_long(req)
	a.BEQ("musicIdle")
	a.Comment("the volume commands are swallowed; fade out is sent the first time, other commands are sent:")
	a.CMP_imm8_b(0xF1)
	a.BEQ("musicFadeReq")
	a.CMP_imm8_b(0xF2)
	a.BEQ("musicClear")
	a.CMP_imm8_b(0xF3)
	a.BEQ("musicClear")
	a.CMP_imm8_b(musicTrackMax)
	a.BCS("musicExitNear")
	a.Comment("the mirror warp, nothing music and tracks that play once are sent:")
	a.CMP_imm8_b(0x08)
	a.BEQ("musicExitNear")
	a.CMP_imm8_b(0x20)
	a.BEQ("musicExitNear")
	for _, t := range musicOneShots {
		a.CMP_imm8_b(t)
		a.BEQ("musicExitNear")
	}
	a.Comment("a track that repeats: remember it, and fade out the track that is playing if this is the first one:")
	a.STA_long(musicWantedAddr)
	a.LDA_long(musicWasOffAddr)
	a.BNE("musicSwallow")
	a.BRA("musicFirst")

	a.Label("musicIdle")
	a.LDA_long(musicWasOffAddr)
	a.BNE("musicExit")
	a.Comment("remember the track that repeats that is playing, so that turning the setting off restarts it:")
	a.LDA_long(0x7E0130)
	a.BEQ("musicFirst")
	a.CMP_imm8_b(musicTrackMax)
	a.BCS("musicFirst")
	a.CMP_imm8_b(0x08)
	a.BEQ("musicFirst")
	a.CMP_imm8_b(0x20)
	a.BEQ("musicFirst")
	for _, t := range musicOneShots {
		a.CMP_imm8_b(t)
		a.BEQ("musicFirst")
	}
	a.STA_long(musicWantedAddr)
	a.Label("musicFirst")
	a.LDA_imm8_b(1)
	a.STA_long(musicWasOffAddr)
	a.LDA_long(0x7E0130)
	a.BEQ("musicAfterFade")
	a.CMP_imm8_b(0x08)
	a.BEQ("musicAfterFade")
	a.CMP_imm8_b(0xF1)
	a.BCS("musicAfterFade")
	a.LDA_imm8_b(0xF1)
	a.STA_long(req)
	a.STA_long(0x7E0130)
	a.LDA_imm8_b(0)
	a.STA_long(0x7E0133)
	a.BRA("musicExit")
	a.Label("musicAfterFade")
	a.LDA_long(req)
	a.BEQ("musicExit")

	a.Label("musicSwallow")
	a.Comment("act as if NMI sent the track: the game believes it is playing:")
	a.LDA_long(req)
	a.STA_long(0x7E0130)
	a.STA_long(0x7E0133)
	a.LDA_imm8_b(0)
	a.STA_long(req)
	a.BRA("musicExit")

	a.Label("musicExit")
}
