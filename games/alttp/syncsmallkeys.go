package alttp

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"io"
	"log"
	"math/rand"
	"time"
)

// Small key synchronization
//
// Small key counts can go up (key picked up) or down (key spent on a door) at any time by any player, so they cannot
// be synced like other items by taking the highest value, nor reliably by "last writer wins" of absolute values: two
// players changing the same dungeon's count at about the same time would lose one of the changes, and writing an
// absolute value to the SNES clobbers any change the local player made between when the value was computed and when
// the write executed.
//
// Instead, small keys are synced as per-player counters of *changes* (deltas):
//
//   - Each player tracks `net[d]`: the cumulative sum of key changes made by the local player in dungeon d during this
//     sync session. Only changes made by the local player are counted; changes made by sync writes are not.
//   - Each player broadcasts its (session, net[], value[], timestamp[]) every frame.
//   - When a receiver sees a remote player's net[d] change, it applies the difference as a relative adjustment to its
//     own SNES. The first message seen from a remote session only establishes a baseline; nothing is applied.
//   - Adjustments are written as relative add/subtract routines that execute atomically within a single game frame,
//     so they can never clobber a concurrent local change. The routine reports the key count it observed right before
//     applying the adjustment, which lets us exactly separate local changes from our own sync writes and avoids the
//     feedback effect of mistaking our own write for a key the local player acquired.
//   - A player with no history (fresh session, e.g. just joined, restarted or pressed "fix small keys") catches up once
//     by adopting the absolute key count of the remote player that most recently changed it (or the highest count if
//     nobody has changed it yet). After that, only deltas are exchanged.
//
// Where the current key count lives:
//
//   - While inside dungeon d, the game uses $7EF36F as the live key count; the per-dungeon slot $7EF37C+d is stale and
//     only gets written back when leaving the dungeon (SaveDungeonKeys). The slot must be ignored while inside the
//     dungeon, otherwise the stale value appears as a key change whenever the slot is sampled.
//   - Otherwise, the per-dungeon slot $7EF37C+d holds the key count.
//   - Sewers ($040C=$00) and Hyrule Castle ($040C=$02) share a single key count; vanilla stores it at $7EF37C and VT
//     randomizers store it in both $7EF37C and $7EF37D. Both are treated as one counter (index 0) and both slots are
//     written.

const smallKeyFirst = uint16(0xF37C)
const smallKeyLast = uint16(0xF38B)

const smallKeyCount = int(smallKeyLast-smallKeyFirst) + 1

// smallKeyMax is the highest small key count we will write:
const smallKeyMax = 99

// smallKeyUnknown is sent as the value of a key count not yet read from the SNES:
const smallKeyUnknown = 0xFF

// smallKeyMirror is the slot index for Hyrule Castle which mirrors the Sewers slot (index 0):
const smallKeyMirror = 1

// smallKeysLocal tracks the local player's small key state.
type smallKeysLocal struct {
	// session uniquely identifies this player's sync session; changes on reset so remote baselines are re-established
	session uint32

	// valid is true when observed holds a value read from the SNES while in a stable game state:
	valid [smallKeyCount]bool
	// observed is the last known key count, accounting for all local changes and sync writes:
	observed [smallKeyCount]uint8
	// net is the cumulative sum of changes made by the local player during this session:
	net [smallKeyCount]int16
	// ts is the server time (msec) of the last change to the key count; 0 means no change during this session:
	ts [smallKeyCount]uint32
	// caughtUp is true once we have either adopted the group's key count or had any key activity this session; after
	// that, only deltas are exchanged:
	caughtUp [smallKeyCount]bool

	// pending is the sum of remote changes not yet applied to our SNES:
	pending [smallKeyCount]int16
	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight [smallKeyCount]*smallKeyUpdate
}

// smallKeysRemote is the last small key state received from a remote player.
type smallKeysRemote struct {
	valid   bool
	session uint32
	net     [smallKeyCount]int16
	value   [smallKeyCount]uint8
	ts      [smallKeyCount]uint32

	// applied is the remote player's net[] already accounted for (applied to our SNES or taken as a baseline):
	applied [smallKeyCount]int16
}

func smallKeyName(k int) string {
	if k == 0 {
		return "Sewers/Hyrule Castle"
	}
	return dungeonNames[k]
}

func clampSmallKeys(v int) (clamped uint8, underflow bool) {
	if v < 0 {
		return 0, true
	}
	if v > smallKeyMax {
		return smallKeyMax, false
	}
	return uint8(v), false
}

// initSmallKeysSync called from Reset() and FixSmallKeys(); starts a new small key sync session.
func (g *Game) initSmallKeysSync() {
	sk := &g.smallKeys
	for k := range sk.inflight {
		if sk.inflight[k] != nil {
			sk.inflight[k].cancelled = true
		}
	}

	*sk = smallKeysLocal{}
	for sk.session == 0 {
		sk.session = rand.Uint32()
	}
}

// invalidateSmallKeys forgets the observed key counts so they are re-read from the SNES without counting any difference
// as a local change. Used when the SNES state may have changed out from under us (device reconnect, game reloaded).
func (g *Game) invalidateSmallKeys(adopt bool) {
	sk := &g.smallKeys
	for k := range sk.valid {
		sk.valid[k] = false
		if sk.inflight[k] != nil {
			sk.inflight[k].cancelled = true
			sk.inflight[k] = nil
		}
		if adopt {
			// catch up to the group's key counts:
			sk.ts[k] = 0
			sk.pending[k] = 0
			sk.caughtUp[k] = false
		}
	}
}

func timestampFromTime(t time.Time) uint32 {
	// convert to milliseconds
	return uint32(t.UnixNano() / 1e6)
}

// FixSmallKeys is only invoked from the view model by the player:
func (g *Game) FixSmallKeys() {
	g.stateLock.Lock()
	defer g.stateLock.Unlock()

	// start a new session which re-baselines all remote players and adopts the group's current key counts:
	g.initSmallKeysSync()
}

// currentSmallKeyIndex returns the small key counter index for the dungeon the player is in, or -1 if not in a dungeon.
func currentSmallKeyIndex(dungeon uint8) int {
	if dungeon == 0xFF || dungeon >= uint8(smallKeyCount<<1) {
		return -1
	}
	k := int(dungeon >> 1)
	if k == smallKeyMirror {
		k = 0
	}
	return k
}

// readSmallKeys samples the small key counts from WRAM and records any changes made by the local player.
// Called when WRAM is read from the SNES.
func (g *Game) readSmallKeys() {
	local := g.local
	sk := &g.smallKeys

	// only sample during stable game states where the key count locations are consistent:
	if !(local.Module == 0x07 || local.Module == 0x09 || local.Module == 0x0B) || local.SubModule != 0 {
		return
	}
	if !g.wramFresh[0x040C] || !g.wramFresh[0xF36F] || !g.wramFresh[smallKeyFirst] || !g.wramFresh[smallKeyLast] {
		return
	}

	dungeon := g.wram[0x040C]
	current := currentSmallKeyIndex(dungeon)
	currentKeys := g.wram[0xF36F]
	if dungeon != 0xFF && current < 0 {
		// unknown dungeon:
		return
	}
	if current >= 0 && currentKeys == 0xFF {
		// in transition between overworld and dungeon:
		return
	}

	nowTs := uint32(0)
	for k := 0; k < smallKeyCount; k++ {
		if k == smallKeyMirror {
			continue
		}
		if sk.inflight[k] != nil {
			// wait for the update to confirm; it will account for any local changes made in the meantime:
			continue
		}

		var v uint8
		if k == current {
			v = currentKeys
		} else {
			v = g.wram[smallKeyFirst+uint16(k)]
		}

		if !sk.valid[k] {
			// first observation; nothing to attribute:
			sk.valid[k] = true
			sk.observed[k] = v
			continue
		}

		if v == sk.observed[k] {
			continue
		}

		if nowTs == 0 {
			nowTs = timestampFromTime(g.ServerSNESTimestamp())
		}
		g.recordLocalSmallKeyChange(k, int(v)-int(sk.observed[k]), nowTs)
		sk.observed[k] = v
	}
}

// readWRAM called when WRAM is read from SNES:
func (g *Game) readWRAM() {
	local := g.local

	// don't sample updates when not in game:
	if !local.IsInGame() {
		return
	}

	g.readSmallKeys()
	g.readHearts()
	g.readOverworldFlags()
	g.readCrystalSwitch()
	g.readStarSwitch()
	g.readRoomState()
	g.readMap16()
	g.readOwEvents()
	g.readManip()
	g.readTorches()
	g.readBossPrize()
	g.readFlyingTiles()
	g.readRupeesRoom()
	g.readEnemiesKilled()

	now := g.ServerSNESTimestamp()
	nowTs := timestampFromTime(now)

	// read in all other WRAM syncables:
	for offs, w := range local.WRAM {
		if w.IsWriting {
			continue
		}
		if !*w.Fresh {
			continue
		}

		var v uint16
		if w.Size == 2 {
			v = g.wramU16(uint32(offs))
		} else {
			v = uint16(g.wramU8(uint32(offs)))
		}

		if v != w.Value {
			w.PreviousValue = w.Value
			w.PreviousTimestamp = w.Timestamp
			if g.notFirstWRAMRead {
				w.Timestamp = nowTs
			}
			w.Value = v
			w.UpdatedFromPlayer = local
		}
	}
}

func (g *Game) recordLocalSmallKeyChange(k int, delta int, ts uint32) {
	sk := &g.smallKeys
	if delta == 0 {
		return
	}
	sk.net[k] += int16(delta)
	if ts == 0 {
		ts = 1
	}
	sk.ts[k] = ts
	sk.caughtUp[k] = true
	log.Printf("alttp: smallkeys: local: %s %+d (net %+d)\n", smallKeyName(k), delta, sk.net[k])
}

// adoptSmallKeys makes a player with no small key history catch up to the group's current small key counts. This only
// happens once per session (or after the game is reloaded) so that it never discards deltas during normal play.
func (g *Game) adoptSmallKeys() {
	sk := &g.smallKeys
	remotePlayers := g.RemotePlayers()

	for k := 0; k < smallKeyCount; k++ {
		if k == smallKeyMirror {
			continue
		}
		if !sk.valid[k] || sk.caughtUp[k] || sk.inflight[k] != nil {
			continue
		}

		// find the remote player that changed the key count most recently, else the one with the highest count:
		var latest, highest *Player
		for _, p := range remotePlayers {
			rk := &p.SmallKeys
			if !rk.valid || rk.value[k] == smallKeyUnknown {
				continue
			}
			if rk.ts[k] != 0 && (latest == nil || rk.ts[k] > latest.SmallKeys.ts[k]) {
				latest = p
			}
			if highest == nil || rk.value[k] > highest.SmallKeys.value[k] {
				highest = p
			}
		}
		if highest == nil {
			// no remote players with known key counts yet:
			continue
		}

		sk.caughtUp[k] = true

		winner := latest
		if winner == nil {
			if highest.SmallKeys.value[k] <= sk.observed[k] {
				continue
			}
			winner = highest
		} else {
			sk.ts[k] = winner.SmallKeys.ts[k]
		}

		value := winner.SmallKeys.value[k]
		// replace any pending changes since the adopted value already includes them:
		sk.pending[k] = int16(value) - int16(sk.observed[k])
		if sk.pending[k] != 0 {
			log.Printf("alttp: smallkeys: catch up %s = %d from player '%s'\n", smallKeyName(k), value, winner.Name())
		}
	}
}

// smallKeyUpdate is a generated update routine for a single small key counter awaiting execution confirmation.
type smallKeyUpdate struct {
	g         *Game
	k         int
	delta     int16
	cancelled bool
}

func (u *smallKeyUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	sk := &g.smallKeys
	if u.cancelled || sk.inflight[u.k] != u {
		return
	}
	sk.inflight[u.k] = nil

	k := u.k
	name := smallKeyName(k)
	nowTs := timestampFromTime(g.ServerSNESTimestamp())

	// value is the key count the routine observed right before applying the delta. Any difference from what we last
	// observed was made by the local player in the meantime:
	before := value
	if sk.valid[k] {
		g.recordLocalSmallKeyChange(k, int(before)-int(sk.observed[k]), nowTs)
	}

	after, underflow := clampSmallKeys(int(before) + int(u.delta))
	sk.valid[k] = true
	sk.observed[k] = after
	if nowTs == 0 {
		nowTs = 1
	}
	sk.ts[k] = nowTs
	sk.caughtUp[k] = true

	log.Printf("alttp: smallkeys: update: %s %d %+d -> %d\n", name, before, u.delta, after)
	if underflow {
		notification := fmt.Sprintf("%s small key double-spend detected", name)
		log.Printf("alttp: smallkeys: %s\n", notification)
		g.PushNotification(notification)
	}
	g.PushNotification(fmt.Sprintf("update %s small keys to %d", name, after))
}

// AbortAsm is called when the update routine could not be confirmed to have executed.
func (u *smallKeyUpdate) AbortAsm() {
	g := u.g
	sk := &g.smallKeys
	if u.cancelled || sk.inflight[u.k] != u {
		return
	}
	sk.inflight[u.k] = nil

	// we cannot know whether the routine executed or not so re-read the key count without attributing any change
	// to the local player and re-adopt the group's key count:
	log.Printf("alttp: smallkeys: update for %s aborted; re-syncing\n", smallKeyName(u.k))
	sk.valid[u.k] = false
	sk.ts[u.k] = 0
	sk.pending[u.k] = 0
	sk.caughtUp[u.k] = false
}

// generateSmallKeyUpdate generates an update routine to apply pending remote changes for small key counter k.
// It has no side effects; the caller must call begin() if the routine is emitted.
func (g *Game) generateSmallKeyUpdate(k int, newEmitter func() *asm.Emitter, index uint32) (u *smallKeyUpdate, a *asm.Emitter) {
	sk := &g.smallKeys
	if k == smallKeyMirror {
		return
	}
	if !sk.valid[k] || sk.inflight[k] != nil || sk.pending[k] == 0 {
		return
	}

	delta := sk.pending[k]
	if delta > smallKeyMax {
		delta = smallKeyMax
	} else if delta < -smallKeyMax {
		delta = -smallKeyMax
	}

	u = &smallKeyUpdate{g: g, k: k, delta: delta}

	slot := 0x7E0000 + uint32(smallKeyFirst) + uint32(k)
	lblSlot := fmt.Sprintf("sk%02x_slot", k)
	lblAdj := fmt.Sprintf("sk%02x_adj", k)
	lblDone := fmt.Sprintf("sk%02x_done", k)

	a = newEmitter()
	a.Comment(fmt.Sprintf("adjust %s small keys by %+d:", smallKeyName(k), delta))

	a.LDA_abs(0x040C)
	a.CMP_imm8_b(0xFF)
	a.BEQ(lblSlot)
	if k == 0 {
		a.Comment("in Sewers ($00) or Hyrule Castle ($02)?")
		a.CMP_imm8_b(0x04)
		a.BCS(lblSlot)
	} else {
		a.Comment(fmt.Sprintf("in %s ($%02x)?", dungeonNames[k], k<<1))
		a.CMP_imm8_b(uint8(k << 1))
		a.BNE(lblSlot)
	}
	a.Comment("in the dungeon; $7EF36F is the live key count:")
	a.LDA_long(0x7EF36F)
	a.CMP_imm8_b(0xFF)
	a.BEQ(lblSlot)
	a.STA_long(a.GetBase() + 0x02 + index)
	g.emitSmallKeyAdjust(a, delta, lblAdj+"_d")
	a.STA_long(0x7EF36F)
	a.BRA(lblDone)

	a.Label(lblSlot)
	a.Comment("not in the dungeon; the dungeon's slot is the key count:")
	a.LDA_long(slot)
	a.Comment("report key count before adjustment for confirmation:")
	a.STA_long(a.GetBase() + 0x02 + index)
	g.emitSmallKeyAdjust(a, delta, lblAdj+"_s")

	a.Label(lblDone)
	a.STA_long(slot)
	if k == 0 {
		a.STA_long(slot + smallKeyMirror)
	}

	return
}

// emitSmallKeyAdjust emits code to add delta to A, clamped to [0, smallKeyMax].
func (g *Game) emitSmallKeyAdjust(a *asm.Emitter, delta int16, lbl string) {
	a.CLC()
	if delta > 0 {
		a.ADC_imm8_b(uint8(delta))
		a.CMP_imm8_b(smallKeyMax + 1)
		a.BCC(lbl)
		a.LDA_imm8_b(smallKeyMax)
	} else {
		// adding the two's complement sets carry when there is no borrow:
		a.ADC_imm8_b(uint8(256 + int(delta)))
		a.BCS(lbl)
		a.LDA_imm8_b(0)
	}
	a.Label(lbl)
}

// begin marks the update as in flight once its routine has been emitted.
func (u *smallKeyUpdate) begin() {
	sk := &u.g.smallKeys
	sk.inflight[u.k] = u
	sk.pending[u.k] -= u.delta
	log.Printf("alttp: smallkeys: update: %s %+d\n", smallKeyName(u.k), u.delta)
}

func (g *Game) SerializeSmallKeys(w io.Writer) (err error) {
	sk := &g.smallKeys
	if err = binary.Write(w, binary.LittleEndian, uint8(MsgSmallKeys)); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, sk.session); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, uint8(smallKeyCount)); err != nil {
		return
	}
	for k := 0; k < smallKeyCount; k++ {
		if err = binary.Write(w, binary.LittleEndian, sk.net[k]); err != nil {
			return
		}
		kv := k
		if k == smallKeyMirror {
			kv = 0
		}
		value, ts := sk.observed[kv], sk.ts[kv]
		if !sk.valid[kv] {
			// don't let anyone adopt a value we haven't read yet:
			value, ts = smallKeyUnknown, 0
		}
		if err = binary.Write(w, binary.LittleEndian, value); err != nil {
			return
		}
		if err = binary.Write(w, binary.LittleEndian, ts); err != nil {
			return
		}
	}
	return
}

func (g *Game) DeserializeSmallKeys(p *Player, r io.Reader) (err error) {
	var session uint32
	var count uint8
	if err = binary.Read(r, binary.LittleEndian, &session); err != nil {
		return fmt.Errorf("error deserializing small keys: %w", err)
	}
	if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
		return fmt.Errorf("error deserializing small keys: %w", err)
	}

	var (
		net   [smallKeyCount]int16
		value [smallKeyCount]uint8
		ts    [smallKeyCount]uint32
	)
	for k := 0; k < int(count); k++ {
		var n int16
		var v uint8
		var t uint32
		if err = binary.Read(r, binary.LittleEndian, &n); err != nil {
			return fmt.Errorf("error deserializing small keys: %w", err)
		}
		if err = binary.Read(r, binary.LittleEndian, &v); err != nil {
			return fmt.Errorf("error deserializing small keys: %w", err)
		}
		if err = binary.Read(r, binary.LittleEndian, &t); err != nil {
			return fmt.Errorf("error deserializing small keys: %w", err)
		}
		if k < smallKeyCount {
			net[k], value[k], ts[k] = n, v, t
		}
	}

	// the base of the small key changes that follow (see compact.go):
	p.Compact.noteSmallKeys(p, session, net, value, ts)
	g.applySmallKeys(p, session, net, value, ts)
	return
}

// applySmallKeys takes a remote player's small key state, from message 0x82 or 0x88.
func (g *Game) applySmallKeys(p *Player, session uint32, net [smallKeyCount]int16, value [smallKeyCount]uint8, ts [smallKeyCount]uint32) {
	rk := &p.SmallKeys
	if p == g.local {
		return
	}

	if !rk.valid || rk.session != session {
		// first message from this session; only establish a baseline:
		*rk = smallKeysRemote{valid: true, session: session, applied: net}
		log.Printf("alttp: smallkeys: player '%s' session %08x baseline\n", p.Name(), session)
	} else {
		sk := &g.smallKeys
		for k := 0; k < smallKeyCount; k++ {
			d := net[k] - rk.applied[k]
			if d == 0 {
				continue
			}
			rk.applied[k] = net[k]
			if k == smallKeyMirror || !g.SyncSmallKeys {
				continue
			}
			sk.pending[k] += d
			log.Printf("alttp: smallkeys: remote: player '%s' %s %+d\n", p.Name(), smallKeyName(k), d)
		}
	}

	rk.net = net
	rk.value = value
	rk.ts = ts
}
