package alttp

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"log"
	"sync"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Patch signature
//
// o2 must know that the SNES is running the ROM it patched before it writes any update routines to SRAM; writing them
// to an unpatched ROM, a different ROM, or a ROM patched by another version of o2 with a different SRAM layout could
// corrupt the game or crash it. Not every SNES device can read ROM, but they can all read SRAM, so the patched init
// code copies a signature from ROM to SRAM at patchSignatureAddr on every reset, which o2 reads and compares with the
// signature it patched into its own copy of the ROM:
//
//	[0:8]   patchSignatureMagic
//	[8:12]  patchVersion: identifies the patch itself; see patchVersion
//	[12:16] CRC32 of the ROM before it was patched, to tell apart different ROMs patched by the same o2 version
//
// o2 doesn't sync anything until the signatures match. SRAM is battery-backed, so a stale signature could survive
// replacing a patched ROM with an unpatched one that uses the same SRAM file; the signature can't detect that.
const (
	patchSignatureSize = 16
	// patchSignatureAddr is the signature's SRAM address, past the end of the copy of Link's OAM entries:
	patchSignatureAddr = uint32(0x707FF0)
	// patchSignatureROMAddr is where the patcher writes the signature in ROM, at the end of the ROM area it uses:
	patchSignatureROMAddr = romGarbageEnd - patchSignatureSize

	// patchFormat must be incremented for any change to the patch that patchVersion does not hash, such as to the init
	// code or the frame hook in Patcher.Patch:
	patchFormat = 1
)

var patchSignatureMagic = [8]byte{'O', '2', '-', 'P', 'A', 'T', 'C', 'H'}

type patchSignature [patchSignatureSize]byte

var patchVersionOnce = sync.OnceValue(computePatchVersion)

// patchVersion identifies the patch: a hash of patchFormat, the SRAM and ROM layout, and the common code copied to
// SRAM (assembled with placeholder ROM addresses). Any change to the code in patcher.go that is copied to SRAM, or to
// the addresses that o2 relies on, changes the version.
func patchVersion() [4]byte {
	return patchVersionOnce()
}

func computePatchVersion() (v [4]byte) {
	h := sha256.New()
	_ = binary.Write(h, binary.LittleEndian, []uint32{
		patchFormat,
		updateBufAAddr,
		updateBufBAddr,
		updateBufSize,
		sramCodeAddr,
		sramCodeEnd,
		updateStubAddr,
		preMainAddr,
		preMainJSRAddr,
		map16ApplyAddr,
		linkOAMCopyAddr,
		linkOAMCopySize,
		patchSignatureAddr,
		patchSignatureROMAddr,
		romGarbageStart,
		romGarbageEnd,
	})

	// the common code, with placeholder ROM addresses:
	a := asm.NewEmitter(make([]byte, sramCodeEnd-sramCodeAddr), false)
	a.SetBase(sramCodeAddr)
	if err := (&Patcher{}).emitSRAMCode(a, []byte{0, 0, 0}, map[romFunction]uint32{}); err != nil {
		panic(fmt.Errorf("alttp: patch version: %w", err))
	}
	h.Write(a.Bytes())

	copy(v[:], h.Sum(nil))
	return
}

// makePatchSignature makes the signature for a ROM before it is patched.
func makePatchSignature(unpatched []byte) (s patchSignature) {
	copy(s[0:8], patchSignatureMagic[:])
	v := patchVersion()
	copy(s[8:12], v[:])
	binary.LittleEndian.PutUint32(s[12:16], crc32.ChecksumIEEE(unpatched))
	return
}

// readPatchSignature reads the signature the patcher wrote to a patched ROM.
func readPatchSignature(rom *snes.ROM) (s patchSignature, ok bool) {
	pak, err := lorom.BusAddressToPak(patchSignatureROMAddr)
	if err != nil || int(pak)+patchSignatureSize > len(rom.Contents) {
		return
	}
	copy(s[:], rom.Contents[pak:pak+patchSignatureSize])
	ok = bytes.Equal(s[0:8], patchSignatureMagic[:])
	return
}

// patchSignaturePak is the FX Pak Pro address of the signature in SRAM:
var patchSignaturePak = func() uint32 {
	pak, err := lorom.BusAddressToPak(patchSignatureAddr)
	if err != nil {
		panic(err)
	}
	return pak
}()

const romStatusWaiting = "waiting to read the patch signature from the SNES"

// enqueuePatchSignatureRead reads the signature from SRAM.
func (g *Game) enqueuePatchSignatureRead(q []snes.Read) []snes.Read {
	return g.readEnqueue(q, patchSignaturePak, patchSignatureSize, nil)
}

// resetPatchSignature forgets whether the SNES is running the patched ROM until the signature is read again.
func (g *Game) resetPatchSignature() {
	g.setROMStatus(false, romStatusWaiting)
}

// checkPatchSignature compares the signature read from SRAM with the one in the ROM o2 patched.
func (g *Game) checkPatchSignature(rsp snes.Response) {
	if rsp.Address != patchSignaturePak || len(rsp.Data) != patchSignatureSize {
		return
	}
	var s patchSignature
	copy(s[:], rsp.Data)

	expected := g.patchSignature
	var status string
	switch {
	case !g.patchSignatureOK:
		status = "the selected ROM was not patched by O2"
	case s == expected:
		g.setROMStatus(true, "the SNES is running the O2 patched ROM")
		return
	case !bytes.Equal(s[0:8], patchSignatureMagic[:]):
		status = "the SNES is not running an O2 patched ROM; Boot or load the patched ROM"
	case !bytes.Equal(s[8:12], expected[8:12]):
		status = "the SNES is running a ROM patched by a different version of O2; Boot or load the patched ROM again"
	default:
		status = "the SNES is running a different O2 patched ROM than the one selected; Boot or load the patched ROM"
	}
	if g.setROMStatus(false, status) {
		log.Printf("alttp: rom: signature % x != expected % x\n", s[:], expected[:])
		g.PushNotification("ROM check failed: " + status)
	}
}

// setROMStatus sets the ROM go/no-go status shown in the UI and reports whether it changed.
func (g *Game) setROMStatus(verified bool, status string) (changed bool) {
	if g.ROMVerified == verified && g.ROMStatus == status {
		return false
	}
	log.Printf("alttp: rom: %s\n", status)
	g.ROMVerified = verified
	g.ROMStatus = status
	g.clean = false
	g.NotifyView()
	return true
}
