package alttp

// legacy test framework; do not use

// sramTest represents a single byte of SRAM used for verifying sync logic
type sramTest struct {
	// offset from $7EF000 in WRAM, e.g. $340 for bow, $341 for boomerang, etc.
	offset uint16
	// value to set for the local player
	localValue uint8
	// value to set for the remote player syncing in
	remoteValue uint8
	// expected value to see for the local player after ASM code runs
	expectedValue uint8
}

type sramTestCase struct {
	name string
	// individual bytes of SRAM to be set and tested
	sram        []sramTest
	wantUpdated bool
	// expected front-end notification to be sent or "" if none expected
	wantNotification string
}
