package qusb2snes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"log"
	"net"
	"syscall"
)

type WebSocketClient struct {
	urlstr  string
	appName string

	ws      net.Conn
	r       *wsutil.Reader
	w       *wsutil.Writer
	encoder *json.Encoder
	decoder *json.Decoder
}

type qusbCommand struct {
	Opcode   string   `json:"Opcode"`
	Space    string   `json:"Space"`
	Operands []string `json:"Operands"`
}

type qusbResult struct {
	Results []string `json:"Results"`
}

func NewWebSocketClient(w *WebSocketClient, urlstr string, name string) (err error) {
	w.urlstr = urlstr
	w.appName = name
	return w.Dial()
}

func (w *WebSocketClient) Dial() (err error) {
	log.Printf("qusb2snes: [%s] dial %s", w.appName, w.urlstr)
	w.ws, _, _, err = ws.Dial(context.Background(), w.urlstr)
	if err != nil {
		err = fmt.Errorf("qusb2snes: [%s] dial: %w", w.appName, err)
		return
	}

	w.r = wsutil.NewClientSideReader(w.ws)
	w.w = wsutil.NewWriter(w.ws, ws.StateClientSide, ws.OpText)
	w.encoder = json.NewEncoder(w.w)
	w.decoder = json.NewDecoder(w.r)

	err = w.SendCommand(qusbCommand{
		Opcode:   "Name",
		Space:    "SNES",
		Operands: []string{w.appName},
	})
	if err != nil {
		var serr syscall.Errno
		if errors.Is(err, &serr) {
			if !serr.Temporary() {
				w.Close()
			}
		}
	}

	return
}

func (w *WebSocketClient) tryOpen() (err error) {
	if w.ws == nil {
		err = w.Dial()
	}
	return
}

func (w *WebSocketClient) Close() (err error) {
	log.Printf("qusb2snes: [%s] close websocket\n", w.appName)
	if w.ws != nil {
		err = w.ws.Close()
	}

	w.ws = nil
	w.r = nil
	w.w = nil
	w.encoder = nil
	w.decoder = nil

	return
}

func (w *WebSocketClient) SendCommand(cmd qusbCommand) (err error) {
	err = w.tryOpen()
	if err != nil {
		return
	}

	//log.Printf("qusb2snes: Encode(%s)\n", deviceName)
	err = w.encoder.Encode(cmd)
	if err != nil {
		var serr syscall.Errno
		if errors.Is(err, &serr) {
			if !serr.Temporary() {
				w.Close()
			}
		}
		err = fmt.Errorf("qusb2snes: [%s] %s command encode: %w", w.appName, cmd.Opcode, err)
		return
	}

	//log.Println("qusb2snes: Flush()")
	err = w.w.Flush()
	if err != nil {
		var serr syscall.Errno
		if errors.Is(err, &serr) {
			if !serr.Temporary() {
				w.Close()
			}
		}
		err = fmt.Errorf("qusb2snes: [%s] %s command flush: %w", w.appName, cmd.Opcode, err)
		return
	}
	return
}

func (w *WebSocketClient) ReadCommandResponse(name string, rsp *qusbResult) (err error) {
	err = w.tryOpen()
	if err != nil {
		return
	}

	//log.Println("qusb2snes: NextFrame()")
	hdr, err := w.r.NextFrame()
	if err != nil {
		var serr syscall.Errno
		if errors.Is(err, &serr) {
			if !serr.Temporary() {
				w.Close()
			}
		}
		err = fmt.Errorf("qusb2snes: [%s] %s command response: error reading next websocket frame: %w", w.appName, name, err)
		return
	}
	if hdr.OpCode == ws.OpClose {
		w.Close()
		err = fmt.Errorf("qusb2snes: [%s] %s command response: websocket closed: %w", w.appName, name, err)
		return
	}

	//log.Println("qusb2snes: Decode()")
	err = w.decoder.Decode(rsp)
	if err != nil {
		var serr syscall.Errno
		if errors.Is(err, &serr) {
			if !serr.Temporary() {
				w.Close()
			}
		}
		err = fmt.Errorf("qusb2snes: [%s] %s command response: decode response: %w", w.appName, name, err)
		return
	}

	//log.Println("qusb2snes: response received")
	return
}
