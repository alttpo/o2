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
	snesScreen    = &SNESScreen{}
	romScreen     = &ROMScreen{}
	connectScreen = &ConnectScreen{}
	gameScreen    = &GameScreen{}

	Screens = []Screen{
		snesScreen,
		romScreen,
		connectScreen,
		gameScreen,
	}
)
