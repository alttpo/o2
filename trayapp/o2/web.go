package main

import (
	"encoding/json"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"log"
	"net"
	"net/http"
	"os"
)

// starts a web server with websockets support to enable bidirectional communication with the UI
func startWebServer() {
	listenAddr := os.Getenv("O2_WEB_LISTENADDR")
	if listenAddr == "" {
		listenAddr = ":27637"
	}

	mux := http.NewServeMux()

	// handle websockets:
	mux.Handle("/ws/", http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(req, rw)
		if err != nil {
			log.Println(err)
			rw.WriteHeader(400)
			return
		}

		go handleWebsocket(conn, req)
	}))

	// serve static content on /:
	mux.Handle("/", http.FileServer(http.Dir("static")))

	// start server:
	log.Fatal(http.ListenAndServe(listenAddr, mux))
}

type CommandRequest struct {
	Command string      `json:"c"`
	Data    interface{} `json:"d"`
}

func handleWebsocket(conn net.Conn, req *http.Request) {
	defer conn.Close()

	var (
		r       = wsutil.NewReader(conn, ws.StateServerSide)
		w       = wsutil.NewWriter(conn, ws.StateServerSide, ws.OpText)
		decoder = json.NewDecoder(r)
		encoder = json.NewEncoder(w)
	)

	for {
		hdr, err := r.NextFrame()
		if err != nil {
			log.Println(err)
			break
		}
		if hdr.OpCode == ws.OpClose {
			break
		}

		// read a command request:
		var creq CommandRequest
		var crsp struct {
			Command  string      `json:"c"`
			Response interface{} `json:"r"`
		}
		if err := decoder.Decode(&creq); err != nil {
			log.Println(err)
			continue
		}

		// command handler:
		crsp.Command = creq.Command
		crsp.Response, err = handleCommand(&creq)
		if err != nil {
			log.Println(err)
			continue
		}

		if err = encoder.Encode(&crsp); err != nil {
			log.Println(err)
			continue
		}
		if err = w.Flush(); err != nil {
			log.Println(err)
			continue
		}
	}
}

func handleCommand(creq *CommandRequest) (rsp interface{}, err error) {
	switch creq.Command {
	case "r": // request update
		rsp =  struct {
		}{
		}
		return
	case "devices": // request device list
		rsp = struct {
		}{
		}
		return
	default:
		return
	}
}
