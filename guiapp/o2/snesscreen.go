package main

import (
	"fmt"
	"fyne.io/fyne"
	"fyne.io/fyne/container"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	"log"
	"o2/snes"
)

type SNESScreen struct {
	txtServer *widget.Entry
}

func (s *SNESScreen) Title() string { return "SNES" }

func (s *SNESScreen) Description() string { return "Connect to a SNES" }

func (s *SNESScreen) View(w fyne.Window) fyne.CanvasObject {
	vb := container.NewVBox()
	for _, name := range snes.Drivers() {
		d, ok := snes.DriverByName(name)
		if !ok {
			continue
		}

		var title, subtitle string
		if dd, ok := d.(snes.DriverDescriptor); ok {
			title = dd.DisplayName()
			subtitle = dd.DisplayDescription()
		} else {
			title = name
			subtitle = fmt.Sprintf("Driver for %s", name)
		}

		form := fyne.NewContainerWithLayout(layout.NewFormLayout(), []fyne.CanvasObject{}...)
		card := widget.NewCard(title, subtitle, form)
		vb.Add(card)

		var err error
		var devSelect *widget.Select
		options := make([]string, 0)
		devices := make([]snes.DeviceDescriptor, 0, 4)
		devSelected := func(id string) {
			i := devSelect.SelectedIndex()
			if i == 0 {
				return
			}

			// send device to main loop:
			snesC <- snes.DriverDevicePair{Driver: d, Device: devices[i-1]}
		}
		devSelect = widget.NewSelect(options, devSelected)
		devSelect.PlaceHolder = "(No Devices Detected)"

		doDetect := func() {
			devices, err = d.Detect()
			if err != nil {
				log.Println(err)
				return
			}

			options = make([]string, 0, len(devices) + 1)
			i := 0
			if len(devices) == 0 {
				options = append(options, "(No Devices Detected)")
			} else {
				options = append(options, "(Select Device)")
				i = 1
			}
			for _, dev := range devices {
				options = append(options, dev.DisplayName())
			}
			devSelect.Options = options

			devSelect.SetSelectedIndex(i)
		}

		doDetect()

		form.Objects = []fyne.CanvasObject{
			widget.NewLabel("Device:"),
			devSelect,
			widget.NewLabel(""),
			widget.NewButtonWithIcon("Detect", theme.ViewRefreshIcon(), doDetect),
		}
		form.Refresh()
	}

	return vb
}
