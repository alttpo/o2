package underworldmap

import (
	"bytes"
	"hash/crc32"
	"image"
	"image/png"
	"log"
	"sync"
)

// Streaming the map
//
// Generating every room takes a few seconds. A Stream generates them in the background and makes each room available
// as soon as it is drawn: its full-size image and a quarter-size one, both as PNG, and where Link can be in it. The
// web UI shows the rooms as they come, and the debug warp looks rooms up in it. Shared keeps the stream of the last
// ROM asked for, so that the web server and the game share one.

// ThumbScale is how many times smaller than full size StreamRoom.Thumb is.
const ThumbScale = 4

// Version is part of a stream's ID: increment it for any change to what a map has (the images, the rooms the flood
// fill reaches, the walk cells), so that the web UI doesn't keep what it cached of the maps made before.
const Version = 7

// StreamRoom is a room of a Stream.
type StreamRoom struct {
	*Room
	// PNG is the room's image at full size, Thumb ThumbScale times smaller:
	PNG   []byte
	Thumb []byte
}

// Stream is a map being generated.
type Stream struct {
	// ID identifies the ROM the map is made from and the Version it is made by, for the web UI to tell maps apart:
	ID uint32

	lock  sync.Mutex
	rooms [RoomCount]*StreamRoom
	// the rooms in the order they were drawn:
	order []uint16
	done  bool
	err   error
	// closed and replaced whenever a room is added or the stream ends:
	changed chan struct{}
}

var (
	sharedLock   sync.Mutex
	sharedStream *Stream
	sharedROM    []byte
)

// Shared returns the stream of the map of rom, starting it if the last one asked for was another ROM's.
func Shared(rom []byte) *Stream {
	sharedLock.Lock()
	defer sharedLock.Unlock()
	if sharedStream != nil && bytes.Equal(sharedROM, rom) {
		return sharedStream
	}
	sharedROM = append([]byte(nil), rom...)
	sharedStream = newStream(sharedROM)
	return sharedStream
}

func newStream(rom []byte) *Stream {
	s := &Stream{
		ID:      crc32.Update(crc32.ChecksumIEEE(rom), crc32.IEEETable, []byte{Version}),
		changed: make(chan struct{}),
	}
	go s.generate(rom)
	return s
}

func (s *Stream) generate(rom []byte) {
	_, err := Generate(rom, Options{
		NoImage: true,
		OnRoom: func(r *Room, img *image.NRGBA) {
			sr := &StreamRoom{Room: r}
			enc := &png.Encoder{CompressionLevel: png.BestSpeed}
			var b bytes.Buffer
			if err := enc.Encode(&b, img); err != nil {
				log.Printf("underworldmap: room $%03x: %v\n", r.Room, err)
				return
			}
			sr.PNG = b.Bytes()

			thumb := image.NewNRGBA(image.Rect(0, 0, RoomPixels/ThumbScale, RoomPixels/ThumbScale))
			downscale(thumb, 0, 0, img, ThumbScale)
			var t bytes.Buffer
			if err := enc.Encode(&t, thumb); err != nil {
				log.Printf("underworldmap: room $%03x: %v\n", r.Room, err)
				return
			}
			sr.Thumb = t.Bytes()
			s.addRoom(sr)
		},
	})

	s.lock.Lock()
	defer s.lock.Unlock()
	if err != nil {
		log.Printf("underworldmap: %v\n", err)
		s.err = err
	}
	s.done = true
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *Stream) addRoom(r *StreamRoom) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.rooms[r.Room.Room] = r
	s.order = append(s.order, r.Room.Room)
	close(s.changed)
	s.changed = make(chan struct{})
}

// Room returns room n, or nil if it is not drawn yet or never is.
func (s *Stream) Room(n uint16) *StreamRoom {
	if n >= RoomCount {
		return nil
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.rooms[n]
}

// Progress returns the rooms drawn after the first `from` ones, whether the map is done (and its error), and a
// channel closed when there is more.
func (s *Stream) Progress(from int) (rooms []uint16, done bool, err error, changed <-chan struct{}) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if from < len(s.order) {
		rooms = append(rooms, s.order[from:]...)
	}
	return rooms, s.done, s.err, s.changed
}
