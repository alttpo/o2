package snes

// Queue interfaces may also implement this ROMControl interface if they allow for uploading a new ROM and booting a ROM
type ROMControl interface {
	// Uploads the ROM contents to a file called 'name' in a dedicated O2 folder
	// Returns the path to pass to BootROM.
	MakeUploadROMCommands(name string, rom []byte) (path string, cmds CommandSequence)

	// Boots the given ROM into the system and resets.
	MakeBootROMCommands(path string) CommandSequence
}
