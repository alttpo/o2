package main

import (
	"fmt"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
	"time"
)

type opcode uint8

const (
	OpGET opcode = iota
	OpPUT
	OpVGET
	OpVPUT

	OpLS
	OpMKDIR
	OpRM
	OpMV

	OpRESET
	OpBOOT
	OpPOWER_CYCLE
	OpINFO
	OpMENU_RESET
	OpSTREAM
	OpTIME
	OpRESPONSE
)

type space uint8

const (
	SpaceFILE space = iota
	SpaceSNES
	SpaceMSU
	SpaceCMD
	SpaceCONFIG
)

type server_flags uint8

const FlagNONE server_flags = 0
const (
	FlagSKIPRESET server_flags = 1 << iota
	FlagONLYRESET
	FlagCLRX
	FlagSETX
	FlagSTREAM_BURST
	_
	FlagNORESP
	FlagDATA64B
)

type info_flags uint8

const (
	FeatDSPX info_flags = 1 << iota
	FeatST0010
	FeatSRTC
	FeatMSU1
	Feat213F
	FeatCMD_UNLOCK
	FeatUSB1
	FeatDMA1
)

type file_type uint8

const (
	FtDIRECTORY file_type = 0
	FtFILE      file_type = 1
)

func sendSerial(f serial.Port, buf []byte) error {
	sent := 0
	for sent < len(buf) {
		n, e := f.Write(buf[sent:])
		if e != nil {
			return e
		}
		sent += n
	}
	return nil
}

func recvSerial(f serial.Port, rsp []byte, expected int) error {
	o := 0
	for o < expected {
		n, err := f.Read(rsp[o:expected])
		if err != nil {
			return err
		}
		if n <= 0 {
			return fmt.Errorf("recvSerial: Read returned %d", n)
		}
		o += n
	}
	return nil
}

type req struct {
	Address  uint32
	Size     uint8
	Response []byte
}

func vget(f serial.Port, reqs [8]req, rsp []byte) (err error) {
	sb := [64]byte{}
	sb[0] = byte('U')
	sb[1] = byte('S')
	sb[2] = byte('B')
	sb[3] = byte('A')
	sb[4] = byte(OpVGET)
	sb[5] = byte(SpaceSNES)
	sb[6] = byte(FlagDATA64B | FlagNORESP)

	total := 0
	for i := 0; i < len(reqs); i++ {
		// 4-byte struct: 1 byte size, 3 byte address
		sb[32+(i*4)] = reqs[i].Size
		sb[33+(i*4)] = byte((reqs[i].Address >> 16) & 0xFF)
		sb[34+(i*4)] = byte((reqs[i].Address >> 8) & 0xFF)
		sb[35+(i*4)] = byte((reqs[i].Address >> 0) & 0xFF)
		total += int(reqs[i].Size)
	}

	// calculate expected number of 64-byte packets:
	packets := total / 64
	remainder := total & 63
	if remainder > 0 {
		packets++
	}

	expected := packets * 64
	// we must be able to read full 64-byte packets, so the slice must be a capacity multiple of 64:
	if cap(rsp) < expected {
		return fmt.Errorf("not enough capacity in rsp slice; %d < %d", cap(rsp), expected)
	}

	rsp = rsp[0:expected]

	// send the VGET request:
	err = sendSerial(f, sb[:])
	if err != nil {
		return err
	}

	// read all 64-byte packets we expect:
	err = recvSerial(f, rsp, expected)
	if err != nil {
		return err
	}

	// shrink down to exact size:
	//rsp = rsp[0:total]

	// fill in response data:
	o := 0
	for i := 0; i < len(reqs); i++ {
		size := int(reqs[i].Size)

		reqs[i].Response = rsp[o : o+size]

		o += size
	}

	return nil
}

func main() {
	var err error

	var ports []*enumerator.PortDetails

	ports, err = enumerator.GetDetailedPortsList()
	if err != nil {
		panic(err)
	}

	var selected *enumerator.PortDetails = nil
	for _, port := range ports {
		if !port.IsUSB {
			continue
		}

		//log.Printf("   USB ID     %s:%s\n", port.VID, port.PID)
		//log.Printf("   USB serial %s\n", port.SerialNumber)

		if (port.SerialNumber == "DEMO00000000") || (port.VID == "1209" && port.PID == "5A22") {
			selected = port
			break
		}
	}

	var f serial.Port
	f, err = serial.Open(selected.Name, &serial.Mode{
		BaudRate: 9600, // doesn't affect USB speed at all
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		panic(err)
	}
	defer f.Close()

	err = f.SetDTR(true)
	if err != nil {
		panic(err)
	}

	buf := [2040]byte{}

	reqs := [8]req{
		// sprite props before $7E0D00:
		// SPR0STUN        = $7E0B58
		// SPR0TILEDIE     = $7E0B6B
		// SPR0PRIO        = $7E0B89
		// SPR0BPF         = $7E0BA0
		// SPR0ANCID       = $7E0BB0
		// SPR0SLOT        = $7E0BC0
		// SPR0PRIZE       = $7E0BE0
		// SPR0SCR         = $7E0C9A
		// SPR0DEFL        = $7E0CAA
		// SPR0DROP        = $7E0CBA
		// SPR0BUMP        = $7E0CD2
		// SPR0DMG         = $7E0CE2
		// end             = $7E0CF2
		// main chunk of SPR[0-F] properties:
		{Address: 0xF50D00, Size: 0xFF},
		{Address: 0xF50DFF, Size: 0xFF},
		{Address: 0xF50EFE, Size: 0xFA5 - 0xEFE},
		// 0FA2..0FA4 = free memory!
		{Address: 0xF50100, Size: 0x36},
		{Address: 0xF502E0, Size: 0x08},
		{Address: 0xF50400, Size: 0x20},
		{Address: 0xF51980, Size: 0x6A},
		{Address: 0xF5F340, Size: 0xFF},
		// Link's palette:
		//{Address: 0xF5C6E0, Size: 0x20},
	}

	fmt.Printf("\u001B[2J")
	for {
		tStart := time.Now()
		err = vget(f, reqs, buf[:])
		tEnd := time.Now()
		if err != nil {
			panic(err)
		}

		delta := tEnd.Sub(tStart).Nanoseconds()
		fmt.Printf("\033[H\033[0m\033[2K%10d | ####: -----------------------------------------------\n", delta)
		line := [16 * (3 + 4 + 5)]byte{}
		for n := 0; n < 0x2A; n++ {
			j := 0
			a := n << 4

			dimmed := false
			for i := 0; i < 16; i++ {
				const hextable = "0123456789abcdef"
				b := buf[a]
				a++
				if b == 0 {
					line[j+0] = ' '
					line[j+1] = ' '
					line[j+2] = ' '
					j += 3
					continue
				}

				line[j+0] = ' '
				j++

				if buf[0xD0+i] == 0 {
					// disabled sprite:
					if !dimmed {
						line[j+0] = 033
						line[j+1] = '['
						line[j+2] = '2'
						line[j+3] = 'm'
						j += 4
						dimmed = true
					}
				} else {
					// enabled sprite:
					if dimmed {
						line[j+0] = 033
						line[j+1] = '['
						line[j+2] = '2'
						line[j+3] = '2'
						line[j+4] = 'm'
						j += 5
						dimmed = false
					}
				}
				line[j+0] = hextable[b>>4]
				line[j+1] = hextable[b&15]
				j += 2
			}

			fmt.Printf(
				"\033[0m%10d | %04x:%s\n",
				delta,
				0xD00+n<<4,
				line[:j],
			)
		}
	}
}
