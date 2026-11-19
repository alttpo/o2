package alttp

import (
	"encoding/binary"
	"fmt"
	"log"
	"o2/games"
	"time"
)

type Module uint8

func (m Module) IsOverworld() bool {
	return m == 0x09 || m == 0x0B
}

func (m Module) IsDungeon() bool {
	return m == 0x07
}

type SyncableWRAM struct {
	g *Game

	Offset uint32
	Name   string
	Size   uint8

	Fresh *bool

	Value             uint16
	PreviousValue     uint16
	Timestamp         uint32
	PreviousTimestamp uint32

	// for tracking post-update confirmation:
	IsWriting         bool
	ValueExpected     uint16
	PendingTimestamp  uint32
	UpdatedFromPlayer *Player
}

func (s *SyncableWRAM) ConfirmAsmExecuted(index uint32, value uint8) {
	s.IsWriting = false
	if value != 1 {
		log.Printf("alttp: wram[%04x] %s update failed (check = %02x, expected 01)", s.Offset, s.Name, value)
		return
	}

	s.PreviousValue = s.Value
	s.PreviousTimestamp = s.Timestamp

	log.Printf("alttp: wram[%04x] %s update successful from ts=%08x,val=%04x to ts=%08x,val=%04x", s.Offset, s.Name, s.Timestamp, s.Value, s.PendingTimestamp, s.ValueExpected)

	notification := fmt.Sprintf("update %s to %d from %s", s.Name, s.ValueExpected, s.UpdatedFromPlayer.Name())
	s.g.PushNotification(notification)

	s.Timestamp = s.PendingTimestamp
	s.Value = s.ValueExpected
}

type SRAMShadow struct {
	data  *[0x500]byte
	fresh *[0x500]bool
}

func (r *SRAMShadow) IsFresh(offs uint32) bool {
	return r.fresh[offs]
}

func (r *SRAMShadow) BusAddress(offs uint32) uint32 {
	return 0x7EF000 + offs
}

func (r *SRAMShadow) ReadU8(offs uint32) uint8 {
	return r.data[offs]
}

func (r *SRAMShadow) ReadU16(offs uint32) uint16 {
	if offs >= 0x500 {
		return 0xFFFF
	}
	return binary.LittleEndian.Uint16(r.data[offs : offs+2])
}

type WRAMReadable map[uint16]*SyncableWRAM

func (r WRAMReadable) IsFresh(offs uint32) bool {
	if w, ok := r[uint16(offs)]; ok {
		return *w.Fresh
	}
	return false
}

func (r WRAMReadable) BusAddress(offs uint32) uint32 {
	return 0x7E0000 + offs
}

// ReadU8 returns 0 for values the player has not sent (e.g. alttpo clients do not send every WRAM value o2 syncs).
func (r WRAMReadable) ReadU8(offs uint32) uint8 {
	if w, ok := r[uint16(offs)]; ok && w != nil {
		return uint8(w.Value)
	}
	return 0
}

// ReadU16 returns 0 for values the player has not sent (e.g. alttpo clients do not send every WRAM value o2 syncs).
func (r WRAMReadable) ReadU16(offs uint32) uint16 {
	if w, ok := r[uint16(offs)]; ok && w != nil {
		return w.Value
	}
	return 0
}

type Player struct {
	IndexF int
	Ttl    int

	Team  uint8
	NameF string

	Frame uint8

	Module       Module
	PriorModule  Module
	SubModule    uint8
	SubSubModule uint8

	OverworldArea uint16
	DungeonRoom   uint16
	Location      uint32

	X uint16
	Y uint16

	Dungeon         uint16
	DungeonEntrance uint16

	LastOverworldX uint16
	LastOverworldY uint16

	XOffs int16
	YOffs int16

	PlayerColor uint16

	SRAM SRAMShadow
	WRAM WRAMReadable

	// sprites last received in message 0x03/0x04:
	Sprites remoteSprites
	// sound effects last received in message 0x02 and not yet played (see sfx.go):
	Sfx sfxRemote

	SmallKeys smallKeysRemote
	Hearts    heartsRemote

	// overworld map16 change log and underworld manipulable objects, sent to players in the same area or room:
	Map16 map16Remote
	Manip manipRemote
	// the torches the player lit in its room (see synctorches.go):
	Torches torchesRemote
	Star    starSwitchRemote
	Room    roomStateRemote
	Doors   roomDoorsRemote
	// the tile room overlord of the player's room (see synctiles.go):
	FlyingTiles flyingTilesRemote
	// overworld entrance animations and other events (see syncowevents.go):
	OwEvents owEventsRemote

	showJoinMessage bool

	GameStartTime  time.Time
	GameFinishTime time.Time
	// the player's run timing from message 0x84 (see runtiming.go):
	RunTiming runTimingRemote
	// what the local client keeps to verify it (see verify.go):
	RunVerify runTimingVerify
	// its frame pacing, from the same messages (see pacing.go):
	Pacing framePacing

	// the server's stamp of the last message received from the player:
	lastServerStamp time.Time
}

func (p *Player) Index() int {
	return p.IndexF
}

func (p *Player) Name() string {
	return p.NameF
}

func (p *Player) TTL() int {
	return p.Ttl
}

func (p *Player) ReadableMemory(kind games.MemoryKind) games.ReadableMemory {
	switch kind {
	case games.SRAM:
		return &p.SRAM
	case games.WRAM:
		return &p.WRAM
	}
	panic(fmt.Errorf("ReadableMemory kind %v not supported", kind))
}

func (g *Game) SetTTL(p *Player, ttl int) {
	joined := false
	if p.Ttl <= 0 && ttl > 0 {
		joined = true
	}

	p.Ttl = ttl
	if joined {
		g.PlayerJoined(p)
	}
}

func (g *Game) DecTTL(p *Player, amount int) {
	if p.Ttl <= 0 {
		return
	}

	p.Ttl -= amount
	if p.Ttl <= 0 {
		g.PlayerLeft(p)
	}
}

func (g *Game) PlayerJoined(p *Player) {
	// Activating new player:
	p.showJoinMessage = true
	g.activePlayersClean = false
	g.shouldUpdatePlayersList = true
}

func (g *Game) PlayerLeft(p *Player) {
	// Player left the game:
	p.Ttl = 0
	p.showJoinMessage = false

	log.Printf("alttp: player[%02x]: %s left\n", uint8(p.IndexF), p.NameF)
	g.PushNotification(fmt.Sprintf("%s left", p.NameF))

	// refresh the ActivePlayers():
	g.activePlayersClean = false

	// refresh the players list
	g.shouldUpdatePlayersList = true
}

// canSampleLocation reports whether the location (room or area) can be sampled in the current module. Same as alttpo's
// LocalGameState::can_sample_location.
func (p *Player) canSampleLocation() bool {
	switch p.Module {
	case 0x07:
		// climbing/descending stairs; keep the old location until the main climb animation finishes:
		if p.SubModule == 0x0E {
			return p.SubSubModule > 0x02
		}
		return true
	case 0x09:
		// overworld screen transition:
		if p.SubModule >= 0x01 && p.SubModule <= 0x08 {
			return false
		}
		// mirror (0x23) and failed mirror back to the dark world (0x2C); in the new world once sub-sub module is 3:
		if p.SubModule == 0x23 || p.SubModule == 0x2C {
			return p.SubSubModule >= 0x03
		}
		return true
	case 0x0E:
		// in-game mode 7 map:
		return p.SubModule != 0x07
	default:
		return true
	}
}

// isInScreenTransition reports whether the screen is scrolling to another overworld area or underworld room. Same as
// alttpo's GameState::is_in_screen_transition.
func (p *Player) isInScreenTransition() bool {
	switch {
	case p.Module == 0x09 && p.SubModule >= 0x01:
		// scrolling between overworld areas:
		return true
	case p.Module == 0x07 && p.SubModule == 0x02:
		// scrolling between underworld rooms:
		return true
	case p.Module == 0x07 && p.SubModule == 0x19:
		// mirroring in the underworld:
		return true
	}
	return false
}

// isGameOver reports whether the GAME OVER animation or menu is showing. Same as alttpo's GameState::is_game_over.
func (p *Player) isGameOver() bool {
	return p.Module == 0x12 && p.SubModule >= 0x06
}

func (p *Player) IsInDungeon() bool {
	if p.IsDungeon() {
		return true
	}
	return p.Location&(1<<16) != 0
}

func (p *Player) IsInGame() bool {
	return p.isModuleInGame(p.Module)
}

func (p *Player) isModuleInGame(m Module) bool {
	// bad modules to be in:
	// 0x00 - Triforce / Zelda startup screens
	// 0x01 - File Select screen
	// 0x02 - Copy Player Mode
	// 0x03 - Erase Player Mode
	// 0x04 - Name Player Mode
	// 0x05 - Loading Game Mode
	// 0x06 - Pre Dungeon Mode
	if m < 0x07 {
		return false
	}
	// 0x1B - Screen to select where to start from (House, sanctuary, etc.)
	if m > 0x1A {
		return false
	}
	// 0x14 - Attract Mode
	if m == 0x14 {
		return false
	}
	// 0x17 - Quitting mode (save and quit)
	if m == 0x17 {
		return false
	}

	// good modules:
	// 0x07 - Dungeon Mode
	// 0x08 - Pre Overworld Mode
	// 0x09 - Overworld Mode
	// 0x0A - Pre Overworld Mode (special overworld)
	// 0x0B - Overworld Mode (special overworld)
	// 0x0C - ???? I think we can declare this one unused, almost with complete certainty.
	// 0x0D - Blank Screen
	// 0x0E - Text Mode/Item Screen/Map
	if m == 0x0E {
		if p.PriorModule != 0x0E {
			return p.isModuleInGame(p.PriorModule)
		} else {
			return true
		}
	}
	// 0x0F - Closing Spotlight
	// 0x10 - Opening Spotlight
	// 0x11 - Happens when you fall into a hole from the OW.
	// 0x12 - Death Mode
	// 0x13 - Boss Victory Mode (refills stats)
	// 0x15 - Module for Magic Mirror
	// 0x16 - Module for refilling stats after boss.
	// 0x18 - Ganon exits from Agahnim's body. Chase Mode.
	// 0x19 - Triforce Room scene
	// 0x1A - End sequence
	return true
}

func (p *Player) IsDungeon() bool {
	return p.Module == 0x07
}

func (p *Player) FormatTimer(now time.Time) string {
	gameTimer := ""
	if !p.GameStartTime.IsZero() {
		var dur time.Duration

		var endTime time.Time
		if !p.GameFinishTime.IsZero() {
			endTime = p.GameFinishTime
		} else {
			endTime = now
		}

		dur = endTime.Sub(p.GameStartTime)
		gameTimer = fmt.Sprintf("%02d:%02d:%02d.%03d",
			(dur/time.Hour)%24,
			(dur/time.Minute)%60,
			(dur/time.Second)%60,
			(dur/time.Microsecond)%1000,
		)
	}
	return gameTimer
}
