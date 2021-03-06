package fxpakpro

import (
	"fmt"
	"log"
	"o2/snes"
	"strings"
)

func (c *Conn) MakeUploadROMCommands(name string, rom []byte) (path string, cmds snes.CommandSequence) {
	name = strings.ToLower(name)
	path = fmt.Sprintf("o2/%s", name)
	cmds = snes.CommandSequence{
		newMKDIR("o2"),
		newPUTFile(path, rom, func(sent, total int) {
			log.Printf("%d of %d\n", sent, total)
		}),
	}

	return
}

func (c *Conn) MakeBootROMCommands(path string) snes.CommandSequence {
	return snes.CommandSequence{
		newBOOT(path),
	}
}
