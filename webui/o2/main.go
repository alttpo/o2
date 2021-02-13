package main

import (
	"encoding/hex"
	"fmt"
	"github.com/skratchdot/open-golang/open"
	"io/ioutil"
	"log"
	"net"
	"o2/engine"
	"o2/snes"
	"o2/snes/fxpakpro"
	_ "o2/snes/mock"
	"os"
	"strconv"
	"sync"
)

var (
	listenHost  string // hostname/ip to listen on for webserver
	listenPort  int    // port number to listen on for webserver
	browserHost string // hostname to send as part of URL to browser to connect to webserver
	browserUrl  string // full URL that is sent to browser (composed of browserHost:listenPort)
)

func orElse(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.LUTC)

	// Parse env vars:
	listenHost = os.Getenv("O2_WEB_LISTEN_HOST")
	if listenHost == "" {
		listenHost = "0.0.0.0"
	}

	var err error
	listenPort, err = strconv.Atoi(orElse(os.Getenv("O2_WEB_LISTEN_PORT"), "27637"))
	if err != nil {
		listenPort = 27637
	}
	if listenPort <= 0 {
		listenPort = 27637
	}
	listenAddr := net.JoinHostPort(listenHost, strconv.Itoa(listenPort))

	browserHost = orElse(os.Getenv("O2_WEB_BROWSER_HOST"), "127.0.0.1")
	browserUrl = fmt.Sprintf("http://%s:%d/", browserHost, listenPort)

	// construct our controller and web server:
	controller := engine.NewController()
	webServer := NewWebServer(listenAddr)

	// inform controller of web server and vice versa:
	controller.ProvideViewNotifier(webServer)
	webServer.ProvideViewCommandHandler(controller)

	// start the web server:
	go func() {
		log.Fatal(webServer.Serve())
	}()

	// initialize controller now that all dependencies are set up:
	controller.Init()

	// start up a systray app (or just open web UI):
	createSystray()
}

func openWebUI() {
	err := open.Start(browserUrl)
	if err != nil {
		log.Println(err)
	}
}

func test() {
	conn, err := snes.Open("fxpakpro", fxpakpro.DeviceDescriptor{})
	if err != nil {
		log.Printf("%v\n", err)
		quitSystray()
		return
	}

	if rc, ok := conn.(snes.ROMControl); ok {
		rom, err := ioutil.ReadFile("lttpj.smc")
		if err != nil {
			log.Printf("%v\n", err)
			quitSystray()
			return
		}
		path, seq := rc.MakeUploadROMCommands("lttp.smc", rom)
		conn.EnqueueMulti(seq)
		seq = rc.MakeBootROMCommands(path)
		conn.EnqueueMulti(seq)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	conn.EnqueueMulti(conn.MakeReadCommands([]snes.ReadRequest{
		{
			Address: 0x007FC0,
			Size:    0x40,
			Completed: func(b snes.ReadOrWriteResponse) {
				fmt.Printf("read  %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	}))
	wg.Wait()

	wg = sync.WaitGroup{}
	wg.Add(1)
	conn.EnqueueMulti(conn.MakeWriteCommands([]snes.WriteRequest{
		{
			Address: 0x007FEA, // NMI vector in bank 00
			Size:    2,
			Data:    []byte{0xC9, 0x80},
			Completed: func(b snes.ReadOrWriteResponse) {
				fmt.Printf("write %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	}))
	wg.Wait()

	wg = sync.WaitGroup{}
	wg.Add(1)
	conn.EnqueueMulti(conn.MakeReadCommands([]snes.ReadRequest{
		{
			Address: 0x007FC0,
			Size:    0x40,
			Completed: func(b snes.ReadOrWriteResponse) {
				fmt.Printf("read  %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	}))
	wg.Wait()

	conn.Enqueue(&snes.CloseCommand{})
}
