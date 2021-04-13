package interfaces

import (
	"encoding/hex"
	"encoding/json"
)

type HexBytes []byte

func (b *HexBytes) UnmarshalJSON(j []byte) (err error) {
	var s string
	err = json.Unmarshal(j, &s)
	if err != nil {
		return
	}
	*b, err = hex.DecodeString(s)
	return
}
