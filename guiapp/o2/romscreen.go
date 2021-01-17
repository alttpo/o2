package main

import (
	"fyne.io/fyne"
	"fyne.io/fyne/container"
	"fyne.io/fyne/dialog"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	"io/ioutil"
	"log"
)

type ROMScreen struct {
	IsLoaded    bool
	ROMURI      fyne.URI
	ROMContents []byte

	txtURL   *widget.Entry
	txtTitle *widget.Entry
}

func (s *ROMScreen) Title() string {
	return "ROM"
}

func (s *ROMScreen) Description() string {
	return "Load and patch an ALTTP ROM"
}

func (s *ROMScreen) View(w fyne.Window) fyne.CanvasObject {
	s.txtURL = widget.NewEntry()
	s.txtTitle = widget.NewEntry()
	romContent := fyne.NewContainerWithLayout(
		layout.NewFormLayout(),
		widget.NewLabel("URL:"),
		s.txtURL,
		widget.NewLabel("Title:"),
		s.txtTitle,
	)
	cardROM := widget.NewCard("ROM Details", "Details found in the loaded ROM", romContent)
	cardROM.Hide()

	localContent := fyne.NewContainerWithLayout(
		layout.NewFormLayout(),
		widget.NewLabel("ROM:"),
		widget.NewButtonWithIcon("Open...", theme.FolderOpenIcon(), func() {
			dialog.ShowFileOpen(func(rc fyne.URIReadCloser, err error) {
				if err != nil {
					log.Println(err)
					return
				}

				s.ROMContents, err = ioutil.ReadAll(rc)
				if err != nil {
					log.Println(err)
					return
				}

				s.ROMURI = rc.URI()
				s.IsLoaded = true
				rc.Close()
			}, w)
		}),
	)
	cardLocal := widget.NewCard("Local file", "Load a ROM from your local filesystem", localContent)

	return container.NewVBox(cardLocal, cardROM)
}
