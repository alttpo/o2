package protocol02

type Kind byte

const (
	RequestIndex      = Kind(0x00)
	Broadcast         = Kind(0x01)
	BroadcastToSector = Kind(0x02)
)

func (k Kind) String() string {
	var rsp string = ""
	if k&0x80 != 0 {
		rsp = " response"
	}
	switch k & 0x7F {
	case RequestIndex:
		return "request_index" + rsp
	case Broadcast:
		return "broadcast" + rsp
	case BroadcastToSector:
		return "broadcast_to_sector" + rsp
	}
	return "unknown" + rsp
}
