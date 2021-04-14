package qusb2snes

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"log"
	"net"
)

type WebSocketClient struct {
	ws      net.Conn
	r       *wsutil.Reader
	w       *wsutil.Writer
	encoder *json.Encoder
	decoder *json.Decoder
}

func NewWebSocketClient(w *WebSocketClient, urlstr string, name string) (err error) {
	w.ws, _, _, err = ws.Dial(context.Background(), urlstr)
	if err != nil {
		return
	}

	w.r = wsutil.NewClientSideReader(w.ws)
	w.w = wsutil.NewWriter(w.ws, ws.StateClientSide, ws.OpText)
	w.encoder = json.NewEncoder(w.w)
	w.decoder = json.NewDecoder(w.r)

	err = w.SendCommand("Name", &map[string]interface{}{
		"Opcode":   "Name",
		"Space":    "SNES",
		"Operands": []string{name},
	})

	return
}

func (w *WebSocketClient) Close() error {
	return w.ws.Close()
}

func (w *WebSocketClient) SendCommand(name string, cmd interface{}) (err error) {
	//log.Printf("qusb2snes: Encode(%s)\n", name)
	err = w.encoder.Encode(cmd)
	if err != nil {
		err = fmt.Errorf("qusb2snes: %s command encode: %w", name, err)
		return
	}

	//log.Println("qusb2snes: Flush()")
	err = w.w.Flush()
	if err != nil {
		err = fmt.Errorf("qusb2snes: %s command flush: %w", name, err)
		return
	}
	return
}

func (w *WebSocketClient) ReadCommandResponse(name string, rsp interface{}) (err error) {
	//log.Println("qusb2snes: NextFrame()")
	hdr, err := w.r.NextFrame()
	if err != nil {
		err = fmt.Errorf("qusb2snes: %s command response: error reading next websocket frame: %w", name, err)
		return
	}
	if hdr.OpCode == ws.OpClose {
		err = fmt.Errorf("qusb2snes: %s command response: websocket closed: %w", name, err)
		return
	}

	//log.Println("qusb2snes: Decode()")
	err = w.decoder.Decode(rsp)
	if err != nil {
		err = fmt.Errorf("qusb2snes: %s command response: decode response: %w", name, err)
		return
	}

	log.Println("qusb2snes: response received")
	return
}
