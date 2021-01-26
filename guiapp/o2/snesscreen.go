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
	view *fyne.Container

	views map[string]*SNESDriverView
}

type SNESDriverView struct {
	NamedDriver snes.NamedDriver

	isDisabled  bool
	isConnected bool

	devices       []snes.DeviceDescriptor
	ddlDevice     *widget.Select
	btnConnect    *widget.Button
	btnDisconnect *widget.Button
}

func (s *SNESScreen) Title() string { return "SNES" }

func (s *SNESScreen) Description() string { return "Connect to a SNES" }

func (s *SNESScreen) Refresh() {
	isConnected := controller.IsConnected()

	for _, view := range s.views {
		if isConnected {
			if controller.IsConnectedToDriver(view.NamedDriver) {
				view.setConnected(true)
				view.setDisabled(false)
			} else {
				// disable all other driver views when connected to a driver:
				view.setConnected(false)
				view.setDisabled(true)
			}
		} else {
			// set all driver views to a disconnected state:
			view.setConnected(false)
			view.setDisabled(false)
		}

		// update view:
		view.refresh()
	}
}

func (v *SNESDriverView) refresh() {
	if v.isDisabled {
		v.btnConnect.Disable()
		v.btnDisconnect.Disable()
		return
	}

	if v.isConnected {
		v.btnConnect.Disable()
		v.btnDisconnect.Enable()
		return
	}

	i := v.ddlDevice.SelectedIndex()
	if i == 0 {
		v.btnConnect.Disable()
	} else {
		v.btnConnect.Enable()
	}
	v.btnDisconnect.Disable()
}

func (v *SNESDriverView) setConnected(connected bool) {
	v.isConnected = connected
}

func (v *SNESDriverView) setDisabled(disabled bool) {
	v.isDisabled = disabled
}

func (v *SNESDriverView) onDeviceChanged(id string) {
	v.refresh()
}

func (v *SNESDriverView) doConnect() {
	i := v.ddlDevice.SelectedIndex()
	if i == 0 {
		return
	}

	// send device to main loop:
	controller.SNESConnected(snes.NamedDriverDevicePair{NamedDriver: v.NamedDriver, Device: v.devices[i-1]})
}

func (v *SNESDriverView) doDisconnect() {
	controller.SNESDisconnected()
}

func (v *SNESDriverView) doDetect() {
	var err error
	v.devices, err = v.NamedDriver.Driver.Detect()
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
	if s.view != nil {
		return s.view
	}

	s.view = container.NewVBox()

	// reset map:
	s.views = make(map[string]*SNESDriverView)
	for _, namedDriver := range snes.Drivers() {
		v := &SNESDriverView{
			NamedDriver: namedDriver,
		}
		s.views[namedDriver.Name] = v

		v.ddlDevice = widget.NewSelect([]string{}, v.onDeviceChanged)
		v.ddlDevice.PlaceHolder = "(No Devices Detected)"
		v.btnConnect = widget.NewButtonWithIcon("Connect", theme.ConfirmIcon(), v.doConnect)
		v.btnDisconnect = widget.NewButtonWithIcon("Disconnect", theme.CancelIcon(), v.doDisconnect)
		v.btnConnect.Disable()
		v.btnDisconnect.Disable()
		v.doDetect()

		var title, subtitle string
		if dd, ok := namedDriver.Driver.(snes.DriverDescriptor); ok {
			title = dd.DisplayName()
			subtitle = dd.DisplayDescription()
		} else {
			title = namedDriver.Name
			subtitle = fmt.Sprintf("NamedDriver for %s", title)
		}

		form := fyne.NewContainerWithLayout(layout.NewFormLayout())
		card := widget.NewCard(title, subtitle, form)
		s.view.Add(card)

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

	return s.view
}
