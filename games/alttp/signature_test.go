package alttp

import (
	"io"
	"strings"
	"testing"
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
		{"unpatched ROM", 0, "not running an O2 patched ROM"},
		{"different patch version", 8, "different version of O2"},
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
				gs.g.locHashTTL = 0
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
	if !gs.e.RunUntil(testROMMainGameLoop, 0x1_000) {
		t.Fatalf("CPU did not reach the main loop after reset")
	}
	if got := patchSignature(gs.e.SRAM[o : o+patchSignatureSize]); got != gs.g.patchSignature {
		t.Errorf("SRAM signature % x after reset; expected % x", got[:], gs.g.patchSignature[:])
	}
}
