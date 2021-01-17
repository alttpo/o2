package main

import "fyne.io/fyne"

type Screen interface {
	Label() string
	View(w fyne.Window) fyne.CanvasObject
}

type GameScreen struct{}

func (s *GameScreen) Label() string    { return "Game" }


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
