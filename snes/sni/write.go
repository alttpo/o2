package sni

import (
	"context"
	"fmt"
	"o2/snes"
)

type multiWriteCommand struct {
	reqs []snes.Write
}

func (m *multiWriteCommand) Execute(queue snes.Queue, keepAlive snes.KeepAlive) (err error) {
	q := queue.(*Queue)

	req := MultiWriteMemoryRequest{
		Uri:      q.uri,
		Requests: make([]*WriteMemoryRequest, len(m.reqs)),
	}
	for i := range m.reqs {
		wr := &m.reqs[i]
		req.Requests[i] = &WriteMemoryRequest{
			RequestAddress:      wr.Address,
			RequestAddressSpace: AddressSpace_FxPakPro,
			// TODO: undo this hard-coding of LoROM mapping but since we only have ALTTP game right now it's ok
			RequestMemoryMapping: MemoryMapping_LoROM,
			Data:                 wr.Data,
		}
	}

	// TODO: yuck
	keepAlive <- struct{}{}

	var rsp *MultiWriteMemoryResponse
	rsp, err = q.memoryClient.MultiWrite(context.TODO(), &req)
	if err != nil {
		return
	}
	if rsp == nil {
		err = fmt.Errorf("unexpected nil response")
		return
	}

	for i, sp := range rsp.Responses {
		// TODO: yuck
		keepAlive <- struct{}{}

		wr := m.reqs[i]
		if wr.Address != sp.RequestAddress {
			err = fmt.Errorf("mismatched address between request and response")
			return
		}
		if uint32(wr.Size) != sp.Size {
			err = fmt.Errorf("mismatched size between request and response")
			return
		}
		if wr.Completion == nil {
			continue
		}

		wr.Completion(snes.Response{
			IsWrite: true,
			Address: wr.Address,
			Size:    wr.Size,
			Data:    wr.Data,
			Extra:   wr.Extra,
		})
	}

	return
}
