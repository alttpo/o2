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
	drivers map[string]*SNESDriverView
}

type SNESDriverView struct {
	DriverName string
	Driver     snes.Driver

	devices       []snes.DeviceDescriptor
	ddlDevice     *widget.Select
	btnConnect    *widget.Button
	btnDisconnect *widget.Button
}

func (s *SNESScreen) Title() string { return "SNES" }

func (s *SNESScreen) Description() string { return "Connect to a SNES" }

func (v *SNESDriverView) onDeviceChanged(id string) {
	i := v.ddlDevice.SelectedIndex()
	if i == 0 {
		v.btnConnect.Disable()
		return
	}
	v.btnConnect.Enable()
}

func (v *SNESDriverView) doConnect() {
	i := v.ddlDevice.SelectedIndex()
	if i == 0 {
		return
	}

	v.btnConnect.Disable()
	v.btnDisconnect.Enable()

	// send device to main loop:
	snesC <- snes.DriverDevicePair{Driver: v.Driver, Device: v.devices[i-1]}
}

func (v *SNESDriverView) doDisconnect() {
	v.btnDisconnect.Disable()
	v.btnConnect.Enable()
	//snesC <- nil
}

func (v *SNESDriverView) doDetect() {
	var err error
	v.devices, err = v.Driver.Detect()
	if err != nil {
		log.Println(err)
		return
	}

	options := make([]string, 0, len(v.devices)+1)
	i := 0
	if len(v.devices) == 0 {
		options = append(options, "(No Devices Detected)")
	} else {
		options = append(options, "(Select Device)")
		i = 1
	}
	for _, dev := range v.devices {
		options = append(options, dev.DisplayName())
	}
	v.ddlDevice.Options = options

	v.ddlDevice.SetSelectedIndex(i)
}

func (s *SNESScreen) View(w fyne.Window) fyne.CanvasObject {
	// reset map:
	s.drivers = make(map[string]*SNESDriverView)

	vb := container.NewVBox()
	for _, name := range snes.Drivers() {
		d, ok := snes.DriverByName(name)
		if !ok {
			continue
		}

		v := &SNESDriverView{
			DriverName: name,
			Driver:     d,
		}
		s.drivers[name] = v
		v.devices = make([]snes.DeviceDescriptor, 0, 4)
		v.ddlDevice = widget.NewSelect([]string{}, v.onDeviceChanged)
		v.ddlDevice.PlaceHolder = "(No Devices Detected)"
		v.btnConnect = widget.NewButtonWithIcon("Connect", theme.ConfirmIcon(), v.doConnect)
		v.btnDisconnect = widget.NewButtonWithIcon("Disconnect", theme.CancelIcon(), v.doDisconnect)
		v.btnConnect.Disable()
		v.btnDisconnect.Disable()

		var title, subtitle string
		if dd, ok := d.(snes.DriverDescriptor); ok {
			title = dd.DisplayName()
			subtitle = dd.DisplayDescription()
		} else {
			title = name
			subtitle = fmt.Sprintf("Driver for %s", name)
		}

		form := fyne.NewContainerWithLayout(layout.NewFormLayout())
		card := widget.NewCard(title, subtitle, form)
		vb.Add(card)

		v.doDetect()

		form.Objects = []fyne.CanvasObject{
			widget.NewLabel("Device:"),
			container.NewBorder(nil, nil, nil, widget.NewButtonWithIcon("Detect", theme.ViewRefreshIcon(), v.doDetect), v.ddlDevice),
			widget.NewLabel(""),
			container.NewHBox(
				v.btnConnect,
				v.btnDisconnect),
		}
		form.Refresh()
	}

	return vb
}
