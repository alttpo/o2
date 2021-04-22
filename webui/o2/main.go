package main

import (
	"fmt"
	"github.com/skratchdot/open-golang/open"
	"io"
	"log"
	"net"
	"o2/engine"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// include these SNES drivers:
import (
	_ "o2/snes/fxpakpro"
	_ "o2/snes/mock"
	_ "o2/snes/qusb2snes"
	_ "o2/snes/retroarch"
)

// include these game providers:
import (
	_ "o2/games"
	_ "o2/games/alttp"
)

var (
	listenHost  string // hostname/ip to listen on for webserver
	listenPort  int    // port number to listen on for webserver
	browserHost string // hostname to send as part of URL to browser to connect to webserver
	browserUrl  string // full URL that is sent to browser (composed of browserHost:listenPort)
	logPath     string
)

func orElse(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

// init is called first before all other package inits so it is best to set up log here:
func init() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.LUTC)

	ts := time.Now().Format("2006-01-02T15:04:05.000Z")
	ts = strings.ReplaceAll(ts, ":", "-")
	ts = strings.ReplaceAll(ts, ".", "-")
	logPath = filepath.Join(os.TempDir(), fmt.Sprintf("o2-%s.log", ts))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		log.Printf("logging to '%s'\n", logPath)
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	} else {
		log.Printf("could not open log file '%s' for writing\n", logPath)
	}
}

func main() {
	var err error

	// Parse env vars:
	listenHost = os.Getenv("O2_WEB_LISTEN_HOST")
	if listenHost == "" {
		listenHost = "0.0.0.0"
	}

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

	// construct our viewModel and web server:
	viewModel := engine.NewViewModel()
	webServer := NewWebServer(listenAddr)

	// inform viewModel of web server and vice versa:
	viewModel.ProvideViewNotifier(webServer)
	webServer.ProvideViewCommandHandler(viewModel)

	// start the web server:
	go func() {
		log.Fatal(webServer.Serve())
	}()

	// initialize viewModel now that all dependencies are set up:
	viewModel.Init()

	// start up a systray app (or just open web UI):
	createSystray()
}

func openWebUI() {
	err := open.Start(browserUrl)
	if err != nil {
		log.Println(err)
	}
}
