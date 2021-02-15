package main

import (
	"fyne.io/fyne"
	"fyne.io/fyne/container"
	"fyne.io/fyne/dialog"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/storage"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	"io/ioutil"
	"log"
	"o2/snes"
)

type ROMScreen struct {
	view *fyne.Container

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
	if s.view != nil {
		return s.view
	}

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

	fileOpenCallback := func(rc fyne.URIReadCloser, err error) {
		if err != nil {
			log.Println(err)
			return
		}
		if rc == nil {
			return
		}
		defer rc.Close()

		// save last location:
		dir, _ := storage.Parent(rc.URI())
		a.Preferences().SetString("lastLocation", dir.String())

		// load contents:
		contents, err := ioutil.ReadAll(rc)
		if err != nil {
			log.Println(err)
			notify("Error reading ROM file")
			return
		}

		rom, err := snes.NewROM(rc.URI().Name(), contents)
		if err != nil {
			log.Println(err)
			notify("Error parsing ROM contents")
			return
		}

		controller.ROMSelected(rom)
	}

	localContent := fyne.NewContainerWithLayout(
		layout.NewFormLayout(),
		widget.NewLabel("ROM:"),
		widget.NewButtonWithIcon("Open...", theme.FolderOpenIcon(), func() {
			var fo *dialog.FileDialog

			fo = dialog.NewFileOpen(
				fileOpenCallback,
				w,
			)

			sz := w.Content().Size()
			fo.Resize(fyne.NewSize(int(float64(sz.Width)*0.85), int(float64(sz.Height)*0.85)))

			if lastLocation := a.Preferences().String("lastLocation"); lastLocation != "" {
				l, err := storage.ListerForURI(storage.NewURI(lastLocation))
				if err != nil {
					log.Println(err)
				} else {
					fo.SetLocation(l)
				}
			}

			fo.Show()
		}),
	)
	cardLocal := widget.NewCard("Local file", "Load a ROM from your local filesystem", localContent)

	s.view = container.NewVBox(cardLocal, cardROM)
	return s.view
}
