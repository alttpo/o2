package retroarch

import (
	"bytes"
	"fmt"
	"net"
	"o2/snes"
	"o2/snes/lorom"
	"o2/udpclient"
	"strings"
	"time"
)

type RAClient struct {
	udpclient.UDPClient

	addr *net.UDPAddr

	version string
	useRCR  bool
}

func (c *RAClient) GetId() string {
	return c.addr.String()
}

func (c *RAClient) Version() (err error) {
	var rsp []byte
	rsp, err = c.WriteThenReadTimeout([]byte("VERSION\n"), time.Second*5)
	if err != nil {
		return
	}

	if rsp == nil {
		return
	}

	c.version = string(rsp)

	// parse the version string:
	var n int
	var major, minor, patch int
	n, err = fmt.Sscanf(c.version, "%d.%d.%d", &major, &minor, &patch)
	if err != nil || n != 3 {
		return
	}
	err = nil

	// use READ_CORE_RAM for <= 1.9.0, use READ_CORE_MEMORY otherwise:
	c.useRCR = false
	if major < 1 {
		// 0.x.x
		c.useRCR = true
		return
	} else if major > 1 {
		// 2+.x.x
		c.useRCR = false
		return
	}
	if minor < 9 {
		// 1.0-8.x
		c.useRCR = true
		return
	} else if minor > 9 {
		// 1.10+.x
		c.useRCR = false
		return
	}
	if patch < 1 {
		// 1.9.0
		c.useRCR = true
		return
	}

	// 1.9.1+
	return
}

func (c *RAClient) ReadMemory(busAddr uint32, size uint8) (data []byte, err error) {
	var sb strings.Builder
	if c.useRCR {
		sb.WriteString("READ_CORE_RAM ")
	} else {
		sb.WriteString("READ_CORE_MEMORY ")
	}
	expectedAddr := busAddr
	sb.WriteString(fmt.Sprintf("%06x %d\n", expectedAddr, size))

	reqStr := sb.String()
	var rsp []byte

	defer func() {
		c.Unlock()
	}()
	c.Lock()

	err = c.WriteTimeout([]byte(reqStr), time.Second*5)
	if err != nil {
		return
	}

	rsp, err = c.ReadTimeout(time.Second * 5)
	if err != nil {
		return
	}

	r := bytes.NewReader(rsp)
	data, err = c.parseReadMemoryResponse(r, expectedAddr, size)
	if err != nil {
		return
	}

	return
}

func (c *RAClient) ReadMemoryBatch(batch []snes.Read, keepAlive snes.KeepAlive) (err error) {
	// build multiple requests:
	var sb strings.Builder
	for _, req := range batch {
		// nowhere to put the response?
		completed := req.Completion
		if completed == nil {
			continue
		}

		if c.useRCR {
			sb.WriteString("READ_CORE_RAM ")
		} else {
			sb.WriteString("READ_CORE_MEMORY ")
		}
		expectedAddr := lorom.PakAddressToBus(req.Address)
		sb.WriteString(fmt.Sprintf("%06x %d\n", expectedAddr, req.Size))
	}

	reqStr := sb.String()
	var rsp []byte

	defer func() {
		c.Unlock()
	}()
	c.Lock()

	// send all commands up front in one packet:
	err = c.WriteTimeout([]byte(reqStr), time.Second*5)
	if err != nil {
		return
	}
	if keepAlive != nil {
		keepAlive <- struct{}{}
	}

	// responses come in multiple packets:
	for _, req := range batch {
		// nowhere to put the response?
		completed := req.Completion
		if completed == nil {
			continue
		}

		rsp, err = c.ReadTimeout(time.Second * 5)
		if err != nil {
			return
		}
		if keepAlive != nil {
			keepAlive <- struct{}{}
		}

		expectedAddr := lorom.PakAddressToBus(req.Address)

		// parse ASCII response:
		r := bytes.NewReader(rsp)
		var data []byte
		data, err = c.parseReadMemoryResponse(r, expectedAddr, req.Size)
		if err != nil {
			return
		}

		completed(snes.Response{
			IsWrite: false,
			Address: req.Address,
			Size:    req.Size,
			Extra:   req.Extra,
			Data:    data,
		})
	}

	err = nil
	return
}

func (c *RAClient) parseReadMemoryResponse(r *bytes.Reader, expectedAddr uint32, size uint8) (data []byte, err error) {
	var n int
	var addr uint32
	if c.useRCR {
		n, err = fmt.Fscanf(r, "READ_CORE_RAM %x", &addr)
	} else {
		n, err = fmt.Fscanf(r, "READ_CORE_MEMORY %x", &addr)
	}
	if err != nil {
		return
	}
	if addr != expectedAddr {
		err = fmt.Errorf("retroarch: read response for wrong request %06x != %06x", addr, expectedAddr)
		return
	}

	data = make([]byte, 0, size)
	for {
		var v byte
		n, err = fmt.Fscanf(r, " %02x", &v)
		if err != nil || n == 0 {
			break
		}
		data = append(data, v)
	}

	err = nil
	return
}

func (c *RAClient) HasVersion() bool {
	return c.version != ""
}
