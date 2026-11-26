package underworldmap

import (
	"bytes"
	"hash/crc32"
	"image"
	"image/png"
	"log"
	"sync"
)

// OverworldVersion is part of an overworld stream's ID, as Version is of the underworld's.
const OverworldVersion = 1

// StreamArea is an area of an OverworldStream.
type StreamArea struct {
	*Area
	// PNG is the area's image at full size:
	PNG []byte
}

// OverworldStream is the overworld being drawn, area by area, as Stream is the underworld.
type OverworldStream struct {
	ID uint32

	lock  sync.Mutex
	areas [AreaCount]*StreamArea
	order []uint8
	done  bool
	err   error
	// closed and replaced whenever an area is added or the stream ends:
	changed chan struct{}
}

var (
	sharedOWLock   sync.Mutex
	sharedOWStream *OverworldStream
	sharedOWROM    []byte
)

// SharedOverworld returns the overworld stream of rom, starting it if the last one asked for was another ROM's.
func SharedOverworld(rom []byte) *OverworldStream {
	sharedOWLock.Lock()
	defer sharedOWLock.Unlock()
	if sharedOWStream != nil && bytes.Equal(sharedOWROM, rom) {
		return sharedOWStream
	}
	sharedOWROM = append([]byte(nil), rom...)
	s := &OverworldStream{
		ID:      crc32.Update(crc32.ChecksumIEEE(sharedOWROM), crc32.IEEETable, []byte{'o', OverworldVersion}),
		changed: make(chan struct{}),
	}
	sharedOWStream = s
	go s.generate(sharedOWROM)
	return s
}

func (s *OverworldStream) generate(rom []byte) {
	_, err := GenerateOverworld(rom, OverworldOptions{
		OnArea: func(a *Area, img *image.NRGBA) {
			var b bytes.Buffer
			enc := &png.Encoder{CompressionLevel: png.BestSpeed}
			if err := enc.Encode(&b, img); err != nil {
				log.Printf("underworldmap: area $%02x: %v\n", a.Area, err)
				return
			}
			s.lock.Lock()
			defer s.lock.Unlock()
			s.areas[a.Area] = &StreamArea{Area: a, PNG: b.Bytes()}
			s.order = append(s.order, a.Area)
			close(s.changed)
			s.changed = make(chan struct{})
		},
	})

	s.lock.Lock()
	defer s.lock.Unlock()
	if err != nil {
		log.Printf("underworldmap: overworld: %v\n", err)
		s.err = err
	}
	s.done = true
	close(s.changed)
	s.changed = make(chan struct{})
}

// Area returns the area, a parent area, or nil if it is not drawn yet.
func (s *OverworldStream) Area(area uint8) *StreamArea {
	if area >= AreaCount {
		return nil
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.areas[area]
}

// Progress returns the areas drawn after the first `from` ones, whether the overworld is done (and its error), and a
// channel closed when there is more.
func (s *OverworldStream) Progress(from int) (areas []uint8, done bool, err error, changed <-chan struct{}) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if from < len(s.order) {
		areas = append(areas, s.order[from:]...)
	}
	return areas, s.done, s.err, s.changed
}
