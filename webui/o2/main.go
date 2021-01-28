package main

import (
	"encoding/hex"
	"fmt"
	"github.com/getlantern/systray"
	"github.com/skratchdot/open-golang/open"
	"io/ioutil"
	"log"
	"net"
	"o2/snes"
	"o2/snes/fxpakpro"
	_ "o2/snes/mock"
	"o2/webui"
	"os"
	"strconv"
	"sync"
)

var (
	listenHost string
	listenPort int
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

	// Start a web server:
	go webui.StartWebServer(listenAddr, "webui/static")

	// Start up a systray:
	systray.Run(trayStart, trayExit)
}

func trayExit() {
	fmt.Println("Finished quitting")
}

func trayStart() {
	// Set up the systray:
	//systray.SetTemplateIcon(icon.Data, icon.Data)
	systray.SetTitle("O2")
	systray.SetTooltip("O2 - SNES Online 2.0")
	mOpenWeb := systray.AddMenuItem("Web UI", "Opens the web UI in the default browser")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit")

	// Menu item click handler:
	go func() {
		for {
			select {
			case <-mOpenWeb.ClickedCh:
				err := open.Start(fmt.Sprintf("http://127.0.0.1:%d/", listenPort))
				if err != nil {
					log.Println(err)
				}
				break
			case <-mQuit.ClickedCh:
				fmt.Println("Requesting quit")
				systray.Quit()
				break
			}
		}
	}()
}

func test() {
	conn, err := snes.Open("fxpakpro", fxpakpro.DeviceDescriptor{})
	if err != nil {
		log.Printf("%v\n", err)
		systray.Quit()
		return
	}

	if rc, ok := conn.(snes.ROMControl); ok {
		rom, err := ioutil.ReadFile("lttpj.smc")
		if err != nil {
			log.Printf("%v\n", err)
			systray.Quit()
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
