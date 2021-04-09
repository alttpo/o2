package alttp

type SRAMSync struct {
	Mutate func(values []uint16) uint16
	Name   func(value uint16) string

	SyncEnabled *bool

	Offset uint16
	Size   int
}

func (g *Game) syncSRAM(offset uint16, pickValue func(values []uint16) uint16, name func(value uint16) string, enabled *bool) {
	g.sramItem[offset].Mutate = pickValue
	g.sramItem[offset].Name = name
	g.sramItem[offset].SyncEnabled = enabled
}

func (g *Game) initSync() {
	g.syncSRAM(0x340, Max, nil, &g.SyncItems)
	g.syncSRAM(0x341, Max, nil, &g.SyncItems)
	g.syncSRAM(0x342, Max, nil, &g.SyncItems)

}

func Max(values []uint16) uint16 {
	maxV := uint16(0)
	for _, v := range values {
		if v > maxV {
			maxV = v
		}
	}
	return maxV
}
