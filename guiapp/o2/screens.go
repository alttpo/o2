package main

import "fyne.io/fyne"

type Screen interface {
	Label() string
	View(w fyne.Window) fyne.CanvasObject
}

type SNESScreen struct{}
type ConnectScreen struct{}
type GameScreen struct{}

func (s *SNESScreen) Label() string    { return "SNES" }
func (s *ConnectScreen) Label() string { return "Connect" }
func (s *GameScreen) Label() string    { return "Game" }

func (s *SNESScreen) View(w fyne.Window) fyne.CanvasObject {
	return nil
}

func (s *ConnectScreen) View(w fyne.Window) fyne.CanvasObject {
	return nil
}

func (s *GameScreen) View(w fyne.Window) fyne.CanvasObject {
	return nil
}

var (
	Screens = []Screen{
		&SNESScreen{},
		&ConnectScreen{},
		&GameScreen{},
	}
)
