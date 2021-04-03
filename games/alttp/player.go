package alttp

type Player struct {
	Index int
	TTL   uint8

	Team uint8
	Name string

	Frame uint8

	Module       uint8
	SubModule    uint8
	SubSubModule uint8
	Location     uint32

	X uint16
	Y uint16

	Dungeon         uint16
	DungeonEntrance uint16

	LastOverworldX uint16
	LastOverworldY uint16

	XOffs uint16
	YOffs uint16

	PlayerColor uint16

	SRAM [0x500]byte
}
