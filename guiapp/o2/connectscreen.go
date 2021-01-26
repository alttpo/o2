package main

import (
	"fyne.io/fyne"
	"fyne.io/fyne/container"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/widget"
)

type ConnectScreen struct {
	view      *fyne.Container
	txtHost   *widget.Entry
	txtGroup  *widget.Entry
	txtPlayer *widget.Entry
	txtTeam   *widget.Entry
}

func (s *ConnectScreen) Title() string { return "Connect" }

func (s *ConnectScreen) Description() string { return "Connect to O2 server" }

func (s *ConnectScreen) View(w fyne.Window) fyne.CanvasObject {
	if s.view != nil {
		return s.view
	}

	a := fyne.CurrentApp()
	preferences := a.Preferences()

	s.txtHost = widget.NewEntry()
	s.txtHost.SetText(preferences.StringWithFallback("host", "alttp.online"))
	s.txtHost.OnChanged = func(s string) {
		preferences.SetString("host", s)
	}

	// allow users to hide group name e.g. for streaming
	s.txtGroup = widget.NewPasswordEntry()
	s.txtGroup.Password = preferences.BoolWithFallback("hideGroupName", false)
	s.txtGroup.SetText(preferences.StringWithFallback("groupName", ""))
	s.txtGroup.OnChanged = func(s string) {
		preferences.SetString("groupName", s)
	}

	s.txtPlayer = widget.NewEntry()
	s.txtPlayer.SetText(preferences.StringWithFallback("playerName", ""))
	s.txtPlayer.OnChanged = func(s string) {
		preferences.SetString("playerName", s)
	}

	s.txtTeam = widget.NewEntry()
	//s.txtTeam.Validator = numeric!
	s.txtTeam.SetText(preferences.StringWithFallback("teamNumber", "0"))
	s.txtTeam.OnChanged = func(s string) {
		preferences.SetString("teamNumber", s)
	}

	form := fyne.NewContainerWithLayout(
		layout.NewFormLayout(),
		widget.NewLabel("Host:"),
		s.txtHost,
		widget.NewLabel("Group:"),
		s.txtGroup,
		widget.NewLabel("Player Name:"),
		s.txtPlayer,
		widget.NewLabel("Team Number:"),
		s.txtTeam,
	)

	s.view = container.NewVBox(form)
	return s.view
}

// called immediately before screen view is destroyed:
func (s *ConnectScreen) Destroy(obj fyne.CanvasObject) {
	a := fyne.CurrentApp()
	preferences := a.Preferences()

	preferences.SetBool("hideGroupName", s.txtGroup.Password)
}
