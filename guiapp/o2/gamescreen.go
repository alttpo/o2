package main

import "fyne.io/fyne"

type GameScreen struct{}

func (s *GameScreen) Title() string { return "Game" }

func (s *GameScreen) Description() string { return "Shows information about the current game" }

func (s *GameScreen) View(w fyne.Window) fyne.CanvasObject {
	return nil
}
