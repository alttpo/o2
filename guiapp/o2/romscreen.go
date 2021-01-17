package main

import (
	"fyne.io/fyne"
)

type ROMScreen struct {
}

func (s *ROMScreen) Title() string {
	return "ROM"
}

func (s *ROMScreen) Description() string {
	return "Load and patch an ALTTP ROM"
}

func (s *ROMScreen) View(w fyne.Window) fyne.CanvasObject {
	return nil
}
