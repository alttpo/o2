package main

import (
	"fyne.io/fyne"
	"fyne.io/fyne/container"
)

type GameScreen struct{
	view *fyne.Container
}

func (s *GameScreen) Title() string { return "Game" }

func (s *GameScreen) Description() string { return "Shows information about the current game" }

func (s *GameScreen) View(w fyne.Window) fyne.CanvasObject {
	if s.view != nil {
		return s.view
	}

	s.view = container.NewHBox()
	return s.view
}
