package snes

type ROMControl interface {
	// Uploads the ROM contents to a file called 'name' in a dedicated O2 folder
	// Returns the path to pass to BootROM.
	MakeUploadROMCommands(name string, rom []byte) (path string, cmds CommandSequence)

	// Boots the given ROM into the system and resets.
	MakeBootROMCommands(path string) CommandSequence
}
