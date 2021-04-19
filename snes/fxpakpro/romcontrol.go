package fxpakpro

import (
	"fmt"
	"log"
	"o2/snes"
	"strings"
)

func (q *Queue) MakeUploadROMCommands(name string, rom []byte) (path string, cmds snes.CommandSequence) {
	name = strings.ToLower(name)
	path = fmt.Sprintf("o2/%s", name)
	cmds = snes.CommandSequence{
		snes.CommandWithCompletion{Command: newMKDIR("o2")},
		snes.CommandWithCompletion{Command: newPUTFile(path, rom, func(sent, total int) {
			log.Printf("fxpakpro: upload '%s': %#06x of %#06x\n", path, sent, total)
		})},
	}

	return
}

func (q *Queue) MakeBootROMCommands(path string) snes.CommandSequence {
	return snes.CommandSequence{
		snes.CommandWithCompletion{Command: newBOOT(path)},
	}
}
