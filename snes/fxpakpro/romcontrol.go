package fxpakpro

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

func (c *Conn) UploadROM(name string, rom []byte) (path string, err error) {
	wg := sync.WaitGroup{}
	wg.Add(1)

	c.submitCommand(newMKDIR("o2"))
	name = strings.ToLower(name)
	path = fmt.Sprintf("o2/%s", name)
	c.submitCommandWithCallback(
		newPUTFile(path, rom, func(sent, total int) {
			log.Printf("%d of %d\n", sent, total)
		}),
		func(error) { wg.Done() },
	)

	// wait until last command is completed:
	wg.Wait()
	return
}

func (c *Conn) BootROM(path string) error {
	wg := sync.WaitGroup{}
	wg.Add(1)

	c.submitCommandWithCallback(
		newBOOT(path),
		func(error) {
			wg.Done()
		},
	)

	// wait until last command is completed:
	wg.Wait()

	return nil
}
