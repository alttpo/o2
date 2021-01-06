package fxpakpro

import (
	"fmt"
	"log"
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

func newBOOT(path string) Command {
	return &CallbackCommand{Callback: func() error {
		log.Println("TODO")
		return nil
	}}
}

func newPUTFile(path string, rom []byte) Command {
	return &CallbackCommand{Callback: func() error {
		log.Println("TODO")
		return nil
	}}
}
