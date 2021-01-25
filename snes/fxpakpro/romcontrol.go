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

	c.cq <- newMKDIR("o2")
	name = strings.ToLower(name)
	path = fmt.Sprintf("o2/%s", name)
	c.cq <- newPUTFile(path, rom, func(sent, total int) {
		log.Printf("%d of %d\n", sent, total)
	})

	c.cq <- &CallbackCommand{Callback: func() error {
		wg.Done()
		return nil
	}}

	// wait until last command is completed:
	wg.Wait()
	return
}

func (c *Conn) BootROM(path string) error {
	wg := sync.WaitGroup{}
	wg.Add(1)

	c.cq <- newBOOT(path)
	c.cq <- &CallbackCommand{Callback: func() error {
		wg.Done()
		return nil
	}}

	// wait until last command is completed:
	wg.Wait()

	return nil
}
