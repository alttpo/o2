package main

import (
	"bytes"
	"fmt"
	"github.com/aybabtme/uniplot/histogram"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
	"os"
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

	OpMGET

	OpSRAM_ENABLE
	OpSRAM_WRITE

	OpNMI_WAIT
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

func nmiWait(f serial.Port) (err error) {
	sb := [512]byte{}
	sb[0] = byte('U')
	sb[1] = byte('S')
	sb[2] = byte('B')
	sb[3] = byte('A')
	sb[4] = byte(OpNMI_WAIT)
	sb[5] = byte(SpaceSNES)
	sb[6] = byte(0)

	// send the command:
	err = sendSerial(f, sb[:])
	if err != nil {
		return
	}

	err = recvSerial(f, sb[:], 512)
	if err != nil {
		return
	}

	if sb[0] != 'U' || sb[1] != 'S' || sb[2] != 'B' || sb[3] != 'A' {
		return fmt.Errorf("nmiWait: bad response")
	}

	ec := sb[5]
	if ec != 0 {
		return fmt.Errorf("nmiWait: error %d", ec)
	}

	return
}

func sramEnable(f serial.Port, enabled bool) (err error) {
	sb := [512]byte{}
	sb[0] = byte('U')
	sb[1] = byte('S')
	sb[2] = byte('B')
	sb[3] = byte('A')
	sb[4] = byte(OpSRAM_ENABLE)
	sb[5] = byte(SpaceSNES)
	sb[6] = byte(0)

	if enabled {
		sb[7] = byte(1)
	} else {
		sb[7] = byte(0)
	}

	// send the command:
	err = sendSerial(f, sb[:])
	if err != nil {
		return
	}

	err = recvSerial(f, sb[:], 512)
	if err != nil {
		return
	}
	if sb[0] != 'U' || sb[1] != 'S' || sb[2] != 'B' || sb[3] != 'A' {
		return fmt.Errorf("sramEnable: bad response")
	}

	ec := sb[5]
	if ec != 0 {
		return fmt.Errorf("sramEnable: error %d", ec)
	}

	return
}

type vgetRead struct {
	Address  uint32
	Size     uint8
	Response []byte
}

func vget(f serial.Port, reqs [8]vgetRead, rsp []byte) (err error) {
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

type mgetReadGroup struct {
	// input:
	Bank  uint8 // shared bank byte for all addresses in Reads
	Reads []mgetRead
}

type mgetRead struct {
	// input:
	Offset uint16 // 16-bit offset within Bank (from Group)
	Size   uint16 // 1 .. 256 only

	// output:
	Response []byte
}

func mget(f serial.Port, grps []mgetReadGroup, rsp []byte) (err error) {
	sb := [64]byte{}
	sb[0] = byte('U')
	sb[1] = byte('S')
	sb[2] = byte('B')
	sb[3] = byte('A')
	sb[4] = byte(OpMGET)
	sb[5] = byte(SpaceSNES)
	sb[6] = byte(FlagDATA64B | FlagNORESP)

	total := 0
	j := 7
	for i := range grps {
		if j >= 64 {
			panic(fmt.Errorf("group %d would exceed 64-byte command", i))
		}
		// number of reads:
		nReads := len(grps[i].Reads)
		sb[j] = byte(nReads)
		j++

		if expected := j + 1 + (nReads * 3); expected >= 64 {
			panic(fmt.Errorf("too many reads in group %d to fit in remainder of 64-byte command; %d > 64", i, expected))
		}

		// bank byte for all addresses in the group:
		sb[j] = grps[i].Bank
		j++
		for k := range grps[i].Reads {
			// low byte:
			sb[j] = byte(grps[i].Reads[k].Offset & 0xFF)
			j++
			// high byte:
			sb[j] = byte(grps[i].Reads[k].Offset >> 8 & 0xFF)
			j++
			// size:
			z := grps[i].Reads[k].Size
			if z < 1 || z > 256 {
				panic("mget read size out of range")
			}
			total += int(z)
			// 256 -> 0
			sb[j] = byte(z & 0xFF)
			j++
		}
	}
	for ; j < 64; j++ {
		sb[j] = 0
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

	// send the MGET request:
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
	for i := 0; i < len(grps); i++ {
		for k := range grps[i].Reads {
			size := int(grps[i].Reads[k].Size)
			grps[i].Reads[k].Response = rsp[o : o+size]
			o += size
		}
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

	// disable periodic SRAM writes to SD card:
	err = sramEnable(f, false)
	if err != nil {
		panic(err)
	}

	buf := [2040]byte{}

	vgetReads := [8]vgetRead{
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
	mgetReadGroups := []mgetReadGroup{
		{
			Bank: 0xF5,
			Reads: []mgetRead{
				// main chunk of SPR[0-F] properties:
				{Offset: 0x0D00, Size: 0x100},
				{Offset: 0x0E00, Size: 0x100},
				{Offset: 0x0F00, Size: 0x0A5},
				// 0FA2..0FA4 = free memory!
				{Offset: 0x0BC0, Size: 0x010}, // slot
				// o2 memory fetches:
				{Offset: 0x0100, Size: 0x036},
				{Offset: 0x02E0, Size: 0x008},
				{Offset: 0x0400, Size: 0x020},
				{Offset: 0x1980, Size: 0x06A},
				{Offset: 0xF340, Size: 0x100},
				// Link's palette:
				//{Offset: 0xC6E0, Size: 0x20},
			},
		},
	}

	timesArr := [32768]float64{}
	times := timesArr[:0]
	t := 0

	sb := bytes.Buffer{}
	offs := [...]uint16{
		0x0BC0,
		0x0D00,
		0x0D10,
		0x0D20,
		0x0D30,
		0x0D40,
		0x0D50,
		0x0D60,
		0x0D70,
		0x0D80,
		0x0D90,
		0x0DA0,
		0x0DB0,
		0x0DC0,
		0x0DD0,
		0x0DE0,
		0x0DF0,
		0x0E00,
		0x0E10,
		0x0E20,
		0x0E30,
		0x0E40,
		0x0E50,
		0x0E60,
		0x0E70,
		0x0E80,
		0x0E90,
		0x0EA0,
		0x0EB0,
		0x0EC0,
		0x0ED0,
		0x0EE0,
		0x0EF0,
		0x0F00,
		0x0F10,
		0x0F20,
		0x0F30,
		0x0F40,
		0x0F50,
		0x0F60,
		0x0F70,
		0x0F80,
		0x0F90,
	}

	wram := [0x10000]byte{}

	fmt.Printf("\u001B[2J")
	for {
		tStart := time.Now()
		// wait for NMI:
		err = nmiWait(f)
		if err != nil {
			panic(err)
		}

		if true {
			_ = mgetReadGroups
			err = mget(f, mgetReadGroups, buf[:])
		} else {
			_ = vgetReads
			err = vget(f, vgetReads, buf[:])
		}
		tEnd := time.Now()
		if err != nil {
			panic(err)
		}

		// copy data from buf to wram
		for i := range mgetReadGroups[0].Reads {
			read := &mgetReadGroups[0].Reads[i]
			copy(
				wram[read.Offset:read.Offset+read.Size],
				read.Response,
			)
		}

		delta := tEnd.Sub(tStart).Nanoseconds()
		if len(times) < 32768 {
			times = append(times, float64(delta))
		} else {
			times[t] = float64(delta)
			t = (t + 1) & 32767
		}

		fmt.Fprint(&sb, "\u001B[2J\u001B[?25l\033[39m\033[1;95H####: -----------------------------------------------\n")
		line := [16 * (3 + 5 + 5)]byte{}
		for n := 0; n < len(offs); n++ {
			j := 0
			a := offs[n]

			changed := false
			dimmed := false
			for i := uint16(0); i < 16; i++ {
				const hextable = "0123456789abcdef"
				b := wram[a+i]
				if b == 0 {
					line[j+0] = ' '
					line[j+1] = ' '
					line[j+2] = ' '
					j += 3
					continue
				}

				line[j+0] = ' '
				j++

				if wram[0x0DD0+i] == 0 {
					// disabled sprite:
					if !dimmed || !changed {
						line[j+0] = 033
						line[j+1] = '['
						line[j+2] = '3'
						line[j+3] = '1'
						line[j+4] = 'm'
						j += 5
						dimmed = true
						changed = true
					}
				} else {
					// enabled sprite:
					if dimmed || !changed {
						line[j+0] = 033
						line[j+1] = '['
						line[j+2] = '3'
						line[j+3] = '4'
						line[j+4] = 'm'
						j += 5
						dimmed = false
						changed = true
					}
				}
				line[j+0] = hextable[b>>4]
				line[j+1] = hextable[b&15]
				j += 2
			}

			fmt.Fprintf(
				&sb,
				"\033[%d;95H\033[39m%04x:%s",
				n+2,
				offs[n],
				line[:j],
			)
		}

		fmt.Fprint(&sb, "\033[H\033[39m")
		h := histogram.PowerHist(1.125, times)
		histogram.Fprintf(&sb, h, histogram.Linear(40), func(v float64) string {
			return fmt.Sprintf("% 11dns", time.Duration(v).Nanoseconds())
		})

		sb.WriteTo(os.Stdout)
		sb.Truncate(0)
	}
}
