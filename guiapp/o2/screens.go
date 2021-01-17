package main

import "fyne.io/fyne"

type Screen interface {
	Title() string
	Description() string
	View(w fyne.Window) fyne.CanvasObject
}

type ScreenDestroy interface {
	Destroy(obj fyne.CanvasObject)
}

var (
	Screens = []Screen{
		&ROMScreen{},
		&SNESScreen{},
		&ConnectScreen{},
		&GameScreen{},
	}
)
