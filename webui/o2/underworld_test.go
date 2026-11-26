package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"o2/interfaces"
	"os"
	"strings"
	"testing"
)

type romCmd struct{ rom []byte }

func (c *romCmd) CreateArgs() interfaces.CommandArgs { return nil }
func (c *romCmd) Execute(args interfaces.CommandArgs) error {
	*args.(*[]byte) = append([]byte(nil), c.rom...)
	return nil
}

type romHandler struct{ cmd *romCmd }

func (h *romHandler) CommandFor(view, command string) (interfaces.Command, error) {
	if view == "rom" && command == "unpatched" {
		return h.cmd, nil
	}
	return nil, fmt.Errorf("no command")
}
func (h *romHandler) NotifyViewTo(interfaces.ViewNotifier) {}

// TestUnderworldStream streams the underworld map of the vanilla JP ROM and fetches a room's images and walk.
func TestUnderworldStream(t *testing.T) {
	rom, err := os.ReadFile("../../games/alttp/alttp-jp-vanilla.sfc")
	if err != nil {
		t.Skip(err)
	}
	s := NewWebServer("127.0.0.1:0")
	s.commandHandler = &romHandler{&romCmd{rom}}
	srv := httptest.NewServer(s.mux)
	defer srv.Close()

	rsp, err := http.Get(srv.URL + "/rom/underworld/events")
	if err != nil {
		t.Fatal(err)
	}
	defer rsp.Body.Close()
	if ct := rsp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type: %s", ct)
	}
	var id string
	rooms := 0
	event := ""
	sc := bufio.NewScanner(rsp.Body)
	done := false
	for !done && sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = line[7:]
		case strings.HasPrefix(line, "data: "):
			switch event {
			case "map":
				id = line[6:]
			case "room":
				rooms++
			case "done":
				done = true
			case "failed":
				t.Fatal(line)
			}
		}
	}
	if !done || id == "" || rooms < 250 {
		t.Fatalf("done %v, id %q, %d rooms", done, id, rooms)
	}

	for _, q := range []string{"room=97", "room=97&full=1"} {
		r, err := http.Get(srv.URL + "/rom/underworld/room.png?id=" + id + "&" + q)
		if err != nil || r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("%s: %v %v", q, err, r.Status)
		}
		r.Body.Close()
	}
	r, err := http.Get(srv.URL + "/rom/underworld/walk.json?id=" + id + "&room=97")
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("walk: %v %v", err, r.Status)
	}
	var w struct {
		Layers uint8  `json:"layers"`
		Cells  []byte `json:"cells"`
	}
	if err = json.NewDecoder(r.Body).Decode(&w); err != nil || w.Layers == 0 || len(w.Cells) != 2*4096 {
		t.Fatalf("walk: %v, %d layers, %d cells", err, w.Layers, len(w.Cells))
	}
	r.Body.Close()

	// another map's id is not served:
	if r, _ := http.Get(srv.URL + "/rom/underworld/room.png?id=1&room=97"); r.StatusCode != 404 {
		t.Errorf("expected 404 for another map; got %s", r.Status)
	}
}

// TestOverworldStream streams the overworld's areas of the vanilla JP ROM and fetches an area's image and walk.
func TestOverworldStream(t *testing.T) {
	rom, err := os.ReadFile("../../games/alttp/alttp-jp-vanilla.sfc")
	if err != nil {
		t.Skip(err)
	}
	s := NewWebServer("127.0.0.1:0")
	s.commandHandler = &romHandler{&romCmd{rom}}
	srv := httptest.NewServer(s.mux)
	defer srv.Close()

	rsp, err := http.Get(srv.URL + "/rom/overworld/events")
	if err != nil {
		t.Fatal(err)
	}
	defer rsp.Body.Close()
	var id string
	areas := 0
	event := ""
	sc := bufio.NewScanner(rsp.Body)
	done := false
	for !done && sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = line[7:]
		case strings.HasPrefix(line, "data: "):
			switch event {
			case "map":
				id = line[6:]
			case "area":
				areas++
			case "done":
				done = true
			case "failed":
				t.Fatal(line)
			}
		}
	}
	if !done || id == "" || areas != 80 {
		t.Fatalf("done %v, id %q, %d areas", done, id, areas)
	}

	// Hyrule Castle, a large area:
	r, err := http.Get(srv.URL + "/rom/overworld/area.png?id=" + id + "&area=27")
	if err != nil || r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("area.png: %v %v", err, r.Status)
	}
	r.Body.Close()
	r, err = http.Get(srv.URL + "/rom/overworld/walk.json?id=" + id + "&area=27")
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("walk: %v %v", err, r.Status)
	}
	var w struct {
		Size  int    `json:"size"`
		Cells []byte `json:"cells"`
	}
	if err = json.NewDecoder(r.Body).Decode(&w); err != nil || w.Size != 1024 || len(w.Cells) != 128*128 {
		t.Fatalf("walk: %v, size %d, %d cells", err, w.Size, len(w.Cells))
	}
	r.Body.Close()

	// a screen that isn't an area's parent has no image:
	if r, _ := http.Get(srv.URL + "/rom/overworld/area.png?id=" + id + "&area=28"); r.StatusCode != 404 {
		t.Errorf("expected 404 for a screen of a large area; got %s", r.Status)
	}
}
