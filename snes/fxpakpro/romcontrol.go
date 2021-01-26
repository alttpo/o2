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
	c.submitCommand(newPUTFile(path, rom, func(sent, total int) {
		log.Printf("%d of %d\n", sent, total)
	}))

	c.submitCommand(&CallbackCommand{Callback: func() error {
		wg.Done()
		return nil
	}})

	// wait until last command is completed:
	wg.Wait()
	return
}

func (c *Conn) BootROM(path string) error {
	wg := sync.WaitGroup{}
	wg.Add(1)

	c.submitCommand(newBOOT(path))
	c.submitCommand(&CallbackCommand{Callback: func() error {
		wg.Done()
		return nil
	}})

	// wait until last command is completed:
	wg.Wait()

	return nil
}
