package sni

import (
	"context"
	"fmt"
	"o2/snes"
)

type multiReadCommand struct {
	reqs []snes.Read
}

func (m *multiReadCommand) Execute(queue snes.Queue, keepAlive snes.KeepAlive) (err error) {
	q := queue.(*Queue)

	req := MultiReadMemoryRequest{
		Uri:      q.uri,
		Requests: make([]*ReadMemoryRequest, len(m.reqs)),
	}
	for i := range m.reqs {
		sr := &m.reqs[i]
		req.Requests[i] = &ReadMemoryRequest{
			RequestAddress:       sr.Address,
			RequestAddressSpace:  AddressSpace_FxPakPro,
			RequestMemoryMapping: MemoryMapping_Unknown,
			Size:                 uint32(sr.Size),
		}
	}

	// TODO: yuck
	keepAlive <- struct{}{}

	var rsp *MultiReadMemoryResponse
	rsp, err = q.memoryClient.MultiRead(context.TODO(), &req)
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

		sr := m.reqs[i]
		if sr.Address != sp.RequestAddress {
			err = fmt.Errorf("mismatched address between request and response")
			return
		}
		if sr.Completion == nil {
			continue
		}

		sr.Completion(snes.Response{
			IsWrite: false,
			Address: sr.Address,
			Size:    sr.Size,
			Data:    sp.Data,
			Extra:   sr.Extra,
		})
	}

	return
}
