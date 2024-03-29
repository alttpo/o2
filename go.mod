module o2

go 1.21

require (
	fyne.io/systray v1.10.0
	github.com/alttpo/snes v0.0.0-20240207011716-ced93427843c
	github.com/gobwas/ws v1.3.2
	github.com/skratchdot/open-golang v0.0.0-20200116055534-eef842397966
	go.bug.st/serial v1.6.1
	golang.org/x/sys v0.16.0
	google.golang.org/grpc v1.61.0
	google.golang.org/protobuf v1.31.0
)

require (
	github.com/creack/goselect v0.1.2 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/stretchr/testify v1.8.4 // indirect
	github.com/tevino/abool v1.2.0 // indirect
	golang.org/x/net v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20231106174013-bbf56f31fb17 // indirect
)

//replace github.com/getlantern/systray => github.com/alttpo/systray v1.3.5

//replace github.com/alttpo/snes => ../snes
