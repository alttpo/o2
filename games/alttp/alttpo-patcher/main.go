package main

import (
	"fmt"
	"io/ioutil"
	"o2/games/alttp"
	"o2/snes"
	"os"
	"path/filepath"
)

func main() {
	var err error

	args := os.Args[1:]
	if len(args) == 0 {
		panic(fmt.Errorf("missing filename argument"))
	}

	var contents []byte
	contents, err = ioutil.ReadFile(args[0])
	if err != nil {
		panic(err)
	}
	_, name := filepath.Split(args[0])

	var rom *snes.ROM
	rom, err = snes.NewROM(name, contents)
	if err != nil {
		panic(err)
	}

	// patch the ROM:
	patcher := alttp.NewPatcher(rom)
	err = patcher.Patch()
	if err != nil {
		panic(err)
	}

	// write it out to a file:
	err = ioutil.WriteFile("patched.smc", rom.Contents, 0644)
	if err != nil {
		panic(err)
	}
}
