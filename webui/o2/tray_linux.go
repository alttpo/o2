package main

import (
	"os"
)

func createSystray() {
	// just open the browser UI on startup:
	openWebUI()
	// sleep the main goroutine so the process does not exit immediately:
	select{}
}

func quitSystray() {
	os.Exit(0)
}
