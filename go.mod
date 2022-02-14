module o2

go 1.16

require (
	github.com/alttpo/snes v0.0.0-20220221221359-6e235411a74d
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/getlantern/systray v1.3.0
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.0.4
	github.com/skratchdot/open-golang v0.0.0-20200116055534-eef842397966
	go.bug.st/serial v1.3.3
	golang.org/x/sys v0.0.0-20210823070655-63515b42dcdf
	google.golang.org/grpc v1.44.0
	google.golang.org/protobuf v1.26.0
)

replace github.com/getlantern/systray => github.com/alttpo/systray v1.3.0

//replace github.com/alttpo/snes => ../snes
