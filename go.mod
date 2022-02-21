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
	github.com/stretchr/testify v1.7.0 // indirect
	go.bug.st/serial v1.3.3
	golang.org/x/sys v0.0.0-20210823070655-63515b42dcdf
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/protobuf v1.26.0
)

replace github.com/getlantern/systray => github.com/alttpo/systray v1.3.0

//replace github.com/alttpo/snes => ../snes
