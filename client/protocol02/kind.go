package protocol02

type Kind byte

const (
	RequestIndex      = Kind(0x00)
	Broadcast         = Kind(0x01)
	BroadcastToSector = Kind(0x02)
)

func (k Kind) String() string {
	switch k {
	case RequestIndex:
		return "request_index"
	case Broadcast:
		return "broadcast"
	case BroadcastToSector:
		return "broadcast_to_sector"
	}
	return "unknown"
}
