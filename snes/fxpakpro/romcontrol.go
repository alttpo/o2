package fxpakpro

import (
	"fmt"
	"strings"
	"sync"
)

func (c *Conn) PlayROM(name string, rom []byte) {
	name = strings.ToLower(name)

	wg := sync.WaitGroup{}
	wg.Add(1)

	c.cq <- newMKDIR("o2")
	path := fmt.Sprintf("o2/%s", name)
	c.cq <- newPUTFile(path, rom)
	c.cq <- newBOOT(path)
	c.cq <- &CallbackCommand{Callback: func() error {
		wg.Done()
		return nil
	}}

	// wait until last command is completed:
	wg.Wait()
}
