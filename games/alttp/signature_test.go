package alttp

import (
	"io"
	"o2/interfaces"
	"strings"
	"testing"
	"time"
)

func TestPatchSignature_Verified(t *testing.T) {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if gs.g.ROMVerified {
		t.Fatal("ROM should not be verified before reading the SNES")
	}
	if !gs.g.patchSignatureOK {
		t.Fatal("patched ROM should contain the patch signature")
	}
	gs.runFrame(t)
	if !gs.g.ROMVerified {
		t.Fatalf("expected the patched ROM to be verified; status: %s", gs.g.ROMStatus)
	}
	if !strings.Contains(gs.g.ROMStatus, patchVersionString()) {
		t.Errorf("expected status to mention patch version %s; got %q", patchVersionString(), gs.g.ROMStatus)
	}
	if gs.g.rom.PatchVersion != patchVersionString() {
		t.Errorf("patched ROM PatchVersion = %q; expected %q", gs.g.rom.PatchVersion, patchVersionString())
	}

	// the signature in SRAM was copied there by the patched init code:
	o := patchSignatureAddr & 0x7FFF
	if got := patchSignature(gs.e.SRAM[o : o+patchSignatureSize]); got != gs.g.patchSignature {
		t.Errorf("SRAM signature % x != ROM signature % x", got[:], gs.g.patchSignature[:])
	}
}

func TestPatchSignature_Mismatch(t *testing.T) {
	tests := []struct {
		name   string
		offs   int
		status string
	}{
		{"unpatched ROM", 0, "could not verify the O2 patch version"},
		{"different patch version", 8, "patch version mismatch"},
		{"different ROM", 12, "different O2 patched ROM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs, err := createTestGameSync("VT test", "o2player", io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			// pretend the server assigned us an index so that we send packets:
			gs.g.local.IndexF = 2
			gs.g.players[2] = *gs.g.local
			gs.g.local = &gs.g.players[2]
			// in the overworld, where o2 syncs:
			gs.e.WRAM[0x10] = 0x09
			gs.e.WRAM[0x11] = 0x00
			gs.runFrame(t)
			gs.runFrame(t)
			if !gs.g.ROMVerified {
				t.Fatalf("expected the patched ROM to be verified; status: %s", gs.g.ROMStatus)
			}
			if !gs.g.syncing || len(gs.c.Wr) == 0 {
				t.Fatalf("expected a verified ROM to sync and send packets")
			}

			// the SNES now runs another ROM:
			gs.e.SRAM[patchSignatureAddr&0x7FFF+uint32(tt.offs)] ^= 0xFF

			// the signature is re-read at least every 8 read cycles:
			for f := 0; f < 9 && gs.g.ROMVerified; f++ {
				gs.runFrame(t)
			}
			if gs.g.ROMVerified {
				t.Fatal("expected the ROM to fail verification")
			}
			if !strings.Contains(gs.g.ROMStatus, tt.status) {
				t.Errorf("expected status to mention %q; got %q", tt.status, gs.g.ROMStatus)
			}

			// nothing is synced to the wrong ROM:
			for len(gs.c.Wr) > 0 {
				<-gs.c.Wr
			}
			for f := 0; f < 4; f++ {
				// the location packet would be sent again:
				gs.g.sendState.location.ok = false
				gs.runFrame(t)
				if gs.g.updateStage != 0 {
					t.Fatalf("update routine written to an unverified ROM")
				}
				if gs.g.syncing {
					t.Fatalf("syncing with an unverified ROM")
				}
			}
			if len(gs.c.Wr) != 0 {
				t.Errorf("expected no packets sent from an unverified ROM; got %d", len(gs.c.Wr))
			}

			// back to the right ROM:
			gs.e.SRAM[patchSignatureAddr&0x7FFF+uint32(tt.offs)] ^= 0xFF
			gs.runFrame(t)
			gs.runFrame(t)
			if !gs.g.ROMVerified {
				t.Fatalf("expected the ROM to be verified again; status: %s", gs.g.ROMStatus)
			}
		})
	}
}

func TestPatchSignature_Distinct(t *testing.T) {
	a := makePatchSignature([]byte("rom a"))
	b := makePatchSignature([]byte("rom b"))
	if a == b {
		t.Error("different ROMs should have different signatures")
	}
	if a != makePatchSignature([]byte("rom a")) {
		t.Error("signatures should be deterministic")
	}
	if string(a[0:8]) != "O2-PATCH" {
		t.Errorf("unexpected magic % x", a[0:8])
	}
}

// TestPatchSignature_InitOverwrites checks that the patched init code overwrites a signature left in SRAM by a
// previously loaded ROM on every reset.
func TestPatchSignature_InitOverwrites(t *testing.T) {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	o := patchSignatureAddr & 0x7FFF
	stale := makePatchSignature([]byte("another ROM"))
	copy(gs.e.SRAM[o:o+patchSignatureSize], stale[:])

	gs.e.CPU.Reset()
	if !gs.e.RunUntil(testROMMainGameLoop, 0x4_000) {
		t.Fatalf("CPU did not reach the main loop after reset")
	}
	if got := patchSignature(gs.e.SRAM[o : o+patchSignatureSize]); got != gs.g.patchSignature {
		t.Errorf("SRAM signature % x after reset; expected % x", got[:], gs.g.patchSignature[:])
	}
}

// TestPatchSignature_Timeout checks that the user is told when the patch version can't be read from the SNES.
func TestPatchSignature_Timeout(t *testing.T) {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var notifications []string
	h := gs.g.Notifications.Subscribe(interfaces.ObserverImpl(func(object interface{}) {
		notifications = append(notifications, object.(string))
	}))
	defer gs.g.Notifications.Unsubscribe(h)

	gs.g.resetPatchSignature()
	start := gs.g.romCheckStart

	// not yet:
	gs.g.checkPatchSignatureTimeout(start.Add(romCheckTimeout - time.Millisecond))
	if gs.g.ROMStatus != romStatusWaiting {
		t.Fatalf("expected to still be waiting; status: %q", gs.g.ROMStatus)
	}

	gs.g.checkPatchSignatureTimeout(start.Add(romCheckTimeout))
	if gs.g.ROMVerified {
		t.Fatal("ROM should not be verified without reading the SNES")
	}
	if !strings.Contains(gs.g.ROMStatus, "could not verify the O2 patch version") ||
		!strings.Contains(gs.g.ROMStatus, "Download") {
		t.Errorf("unexpected status: %q", gs.g.ROMStatus)
	}

	// a late signature still verifies the ROM:
	gs.runFrame(t)
	if !gs.g.ROMVerified {
		t.Fatalf("expected the patched ROM to be verified; status: %s", gs.g.ROMStatus)
	}

	// both the failure and the later success go to the notification history:
	if len(notifications) != 2 ||
		!strings.HasPrefix(notifications[0], "ROM check failed: ") ||
		!strings.HasPrefix(notifications[1], "ROM check passed: ") ||
		!strings.Contains(notifications[1], patchVersionString()) {
		t.Errorf("unexpected notifications: %q", notifications)
	}
}
