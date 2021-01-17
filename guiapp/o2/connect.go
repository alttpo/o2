package main

import (
	"fyne.io/fyne"
	"fyne.io/fyne/container"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/widget"
)

type ConnectScreen struct{
	txtServer *widget.Entry
}

func (s *ConnectScreen) Label() string { return "Connect" }

func (s *ConnectScreen) View(w fyne.Window) fyne.CanvasObject {
	s.txtServer = widget.NewEntry()
	s.txtServer.SetText("alttp.online")
	form := fyne.NewContainerWithLayout(
		layout.NewFormLayout(),
		widget.NewLabel("Server:"),
		s.txtServer,
	)
	return container.NewVBox(
		form,
	)
}
