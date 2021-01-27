package main

import (
	"fyne.io/fyne"
	"fyne.io/fyne/container"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	"o2/snes"
)

type GameScreen struct {
	view       *fyne.Container
	btnSendROM *widget.Button
	btnStart   *widget.Button
}

func (s *GameScreen) Title() string { return "Game" }

func (s *GameScreen) Description() string { return "Shows information about the current game" }

func (s *GameScreen) View(w fyne.Window) fyne.CanvasObject {
	if s.view != nil {
		return s.view
	}

	s.view = container.NewVBox()

	s.btnSendROM = widget.NewButtonWithIcon("Send ROM to SNES", theme.MoveUpIcon(), controller.loadROM)
	s.btnSendROM.Disable()
	s.view.Add(s.btnSendROM)

	s.btnStart = widget.NewButtonWithIcon("Start Game", theme.MediaPlayIcon(), controller.startGame)
	s.btnStart.Enable()
	s.view.Add(s.btnStart)

	s.Refresh()

	return s.view
}

func (s *GameScreen) Refresh() {
	if s.view == nil {
		return
	}

	sendRomEnabled := false
	s.btnSendROM.Disable()
	if controller.IsConnected() {
		_, ok := controller.dev.(snes.ROMControl)
		sendRomEnabled = ok
	}
	setEnabled(s.btnSendROM, sendRomEnabled)

	setEnabled(s.btnStart, controller.gameInst != nil && !controller.gameInst.IsRunning())

	s.view.Refresh()
}
