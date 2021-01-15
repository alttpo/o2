package main

import (
	"fyne.io/fyne"
	"fyne.io/fyne/app"
)

var (
	a fyne.App
	w fyne.Window
)

func main() {
	a = app.NewWithID("o2")
	w = a.NewWindow("O2")
	w.SetMaster()
	w.ShowAndRun()
}
