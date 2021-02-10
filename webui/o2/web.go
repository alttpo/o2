package main

import (
	"encoding/json"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"log"
	"net"
	"net/http"
	"sync"
)

type WebServer struct {
	listenAddr string

	commandHandler ViewCommandHandler

	mux *http.ServeMux

	socketsRw sync.RWMutex
	sockets   []*Socket

	// broadcast channel to all sockets:
	q chan ViewModelUpdate
}

type Socket struct {
	ws   *WebServer
	req  *http.Request
	conn net.Conn

	// write channel:
	q chan ViewModelUpdate
}

type ViewModelUpdate struct {
	View      string      `json:"v"`
	ViewModel interface{} `json:"m"`
}

// starts a web server with websockets support to enable bidirectional communication with the UI
func NewWebServer(listenAddr string) *WebServer {
	s := &WebServer{
		listenAddr: listenAddr,
		mux:        http.NewServeMux(),
		socketsRw:  sync.RWMutex{},
		sockets:    make([]*Socket, 0, 2),
		q:          make(chan ViewModelUpdate, 10),
	}

	// handle websockets:
	s.mux.Handle("/ws/", http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(req, rw)
		if err != nil {
			log.Println(err)
			rw.WriteHeader(400)
			return
		}

		// create the Socket to handle bidirectional communication:
		socket := NewSocket(s, req, conn)
		s.appendSocket(socket)

		// start by sending all view models to this new socket:
		s.commandHandler.NotifyViewTo(socket)
	}))

	// serve static content from go-bindata:
	s.mux.Handle("/", http.FileServer(AssetFile()))

	// handle the broadcast channel:
	go s.handleBroadcast()

	return s
}

func (s *WebServer) appendSocket(socket *Socket) {
	s.socketsRw.Lock()
	defer s.socketsRw.Unlock()
	s.sockets = append(s.sockets, socket)
}

func (s *WebServer) removeSocket(k *Socket) {
	s.socketsRw.Lock()
	defer s.socketsRw.Unlock()

	for i, sk := range s.sockets {
		if sk == k {
			s.sockets = append(s.sockets[:i], s.sockets[i+1:]...)
			break
		}
	}
}

func (s *WebServer) Serve() error {
	// start server:
	return http.ListenAndServe(s.listenAddr, s.mux)
}

func (s *WebServer) NotifyView(view string, viewModel interface{}) {
	// send to the broadcast channel so that all connected websockets get the update:
	s.q <- ViewModelUpdate{
		View:      view,
		ViewModel: viewModel,
	}
}

func (s *WebServer) ProvideViewCommandHandler(commandHandler ViewCommandHandler) {
	s.commandHandler = commandHandler
}

func (s *WebServer) handleBroadcast() {
	// read updates from the broadcast channel:
	for u := range s.q {
		s.socketsRw.RLock()
		sockets := s.sockets
		s.socketsRw.RUnlock()

		// broadcast to all connected sockets:
		for _, k := range sockets {
			k.q <- u
		}
	}
}

type CommandRequest struct {
	View    string          `json:"v"`
	Command string          `json:"c"`
	Args    json.RawMessage `json:"a"`
}

func NewSocket(s *WebServer, req *http.Request, conn net.Conn) *Socket {
	k := &Socket{
		ws:   s,
		req:  req,
		conn: conn,
		q:    make(chan ViewModelUpdate, 10),
	}

	go k.readHandler()
	go k.writeHandler()

	return k
}

func (k *Socket) NotifyView(view string, viewModel interface{}) {
	k.q <- ViewModelUpdate{
		View:      view,
		ViewModel: viewModel,
	}
}

func (k *Socket) readHandler() {
	// the reader is in control of the lifetime of the socket:
	defer func() {
		_ = k.conn.Close()

		// remove self from sockets array:
		k.ws.removeSocket(k)
	}()

	var (
		r       = wsutil.NewReader(k.conn, ws.StateServerSide)
		decoder = json.NewDecoder(r)
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
		if err := decoder.Decode(&creq); err != nil {
			log.Println(err)
			continue
		}

		// command handler:
		if k.ws.commandHandler == nil {
			log.Println("no view command handler provided!")
			continue
		}

		ce, err := k.ws.commandHandler.CommandExecutor(creq.View, creq.Command)
		if err != nil {
			log.Println(err)
			continue
		}

		// instantiate a specific args type for the command:
		args := ce.CreateArgs()

		// deserialize json:
		err = json.Unmarshal(creq.Args, args)
		if err != nil {
			log.Println(err)
			continue
		}

		// execute the command:
		err = ce.Execute(args)
		if err != nil {
			log.Println(err)
			continue
		}
	}
}

func (k *Socket) writeHandler() {
	var (
		w       = wsutil.NewWriter(k.conn, ws.StateServerSide, ws.OpText)
		encoder = json.NewEncoder(w)
	)

	// wait for ViewModelUpdates on the channel:
	for u := range k.q {
		var err error
		if err = encoder.Encode(&u); err != nil {
			log.Println(err)
			continue
		}
		if err = w.Flush(); err != nil {
			log.Println(err)
			continue
		}
	}
}
