package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
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

	OpSRAM_ENABLE
	OpSRAM_WRITE

	OpIOVM_UPLOAD
	OpIOVM_EXEC

	OpMGET
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
	FlagWAIT_FOR_NMI
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

func mget(f serial.Port, grps []mgetReadGroup, waitForNMI bool, rsp []byte) (err error) {
	sb := [64]byte{}
	sb[0] = byte('U')
	sb[1] = byte('S')
	sb[2] = byte('B')
	sb[3] = byte('A')
	sb[4] = byte(OpMGET)
	sb[5] = byte(SpaceSNES)
	sb[6] = byte(FlagDATA64B | FlagNORESP)
	if waitForNMI {
		sb[6] |= byte(FlagWAIT_FOR_NMI)
	}

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

func iovmUpload(f serial.Port, proc []byte) (size uint32, err error) {
	sb := [512]byte{}
	sb[0] = byte('U')
	sb[1] = byte('S')
	sb[2] = byte('B')
	sb[3] = byte('A')
	sb[4] = byte(OpIOVM_UPLOAD)
	sb[5] = byte(SpaceSNES)
	sb[6] = byte(0)

	if copy(sb[7:], proc) < len(proc) {
		err = fmt.Errorf("procedure too big to fit in USBA command")
		return
	}

	// send the IOVM_UPLOAD request:
	err = sendSerial(f, sb[:])
	if err != nil {
		return
	}

	// get the response:
	err = recvSerial(f, sb[:], 512)
	if err != nil {
		return
	}
	if sb[0] != 'U' || sb[1] != 'S' || sb[2] != 'B' || sb[3] != 'A' {
		err = fmt.Errorf("iovmUpload: bad response")
		return
	}

	ec := sb[5]
	if ec != 0 {
		err = fmt.Errorf("iovmUpload: error %d", ec)
		return
	}

	// IOVM_EXEC must now always return this many bytes of data:
	size = binary.BigEndian.Uint32(sb[252:256])

	return
}

func iovmExecute(f serial.Port, waitForNMI bool, rsp []byte) (size uint32, err error) {
	sb := [512]byte{}
	sb[0] = byte('U')
	sb[1] = byte('S')
	sb[2] = byte('B')
	sb[3] = byte('A')
	sb[4] = byte(OpIOVM_EXEC)
	sb[5] = byte(SpaceSNES)
	sb[6] = byte(0)
	if waitForNMI {
		sb[6] |= byte(FlagWAIT_FOR_NMI)
	}

	// send the IOVM_EXEC request:
	err = sendSerial(f, sb[:])
	if err != nil {
		return
	}

	// get the response:
	err = recvSerial(f, sb[:], 512)
	if err != nil {
		return
	}
	if sb[0] != 'U' || sb[1] != 'S' || sb[2] != 'B' || sb[3] != 'A' {
		err = fmt.Errorf("iovmExecute: bad response")
		return
	}

	ec := sb[5]
	if ec != 0 {
		err = fmt.Errorf("iovmExecute: error %d", ec)
		return
	}

	// IOVM_EXEC must now always return this many bytes of data:
	size = binary.BigEndian.Uint32(sb[252:256])
	if size > uint32(len(rsp)) {
		err = fmt.Errorf("iovmExecute: rsp buffer too small %d to fit whole response size of %d", len(rsp), size)
		return
	}

	// read full 512-byte packets and copy into rsp:
	r := rsp
	packets := size / 512
	for i := uint32(0); i < packets; i++ {
		err = recvSerial(f, sb[:], 512)
		if err != nil {
			return
		}
		copy(r, sb[:])
		r = r[512:]
	}

	// read any remainder (padded to 512 bytes):
	remainder := size & 511
	if remainder > 0 {
		err = recvSerial(f, sb[:], 512)
		if err != nil {
			return
		}
		copy(r[:remainder], sb[:remainder])
	}

	return
}

const hextable = "0123456789abcdef"

func main() {
	var err error

	initConsole()

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

	if selected == nil {
		panic(fmt.Errorf("no fx pak pro found"))
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

	f.SetReadTimeout(time.Second)

	err = f.SetDTR(true)
	if err != nil {
		panic(err)
	}

	// disable periodic SRAM writes to SD card:
	err = sramEnable(f, false)
	if err != nil {
		panic(err)
	}

	buf := [8192]byte{}

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
				//{Offset: 0x0C9A, Size: 0xCF2 - 0xC9A},
				//{Offset: 0x0B58, Size: 0xBF0 - 0xB58},
				{Offset: 0x0BC0, Size: 0x010},
				{Offset: 0x0CAA, Size: 0x010},
				// main chunk of SPR[0-F] properties:
				{Offset: 0x0D00, Size: 0x100},
				{Offset: 0x0E00, Size: 0x100},
				{Offset: 0x0F00, Size: 0x0A2},
				// 0FA2..0FA4 = free memory!
				// top bit of CPU stack:
				//{Offset: 0x01D0, Size: 0x100 - 0x037},
				// o2 memory fetches:
				{Offset: 0x0100, Size: 0x036},
				{Offset: 0x02E0, Size: 0x008},
				{Offset: 0x0400, Size: 0x020},
				{Offset: 0x1980, Size: 0x06A},
				{Offset: 0xF340, Size: 0x100},
				// Link's palette:
				{Offset: 0xC6E0, Size: 0x20},
			},
		},
	}

	// IOVM instruction byte format:
	//
	//    76 54 3210
	//   [-- tt oooo]
	//
	//     o = opcode
	//     t = target
	//     - = reserved for future extension
	//
	//
	// 0=END
	// 1=SETOFFS
	// 2=SETBANK
	// 3=READ
	// 4=WRITE
	// 5=WHILE_NEQ
	// 6=WHILE_EQ

	proc := make([]byte, 0, 512-7)

	// TODO: add implicit WHILE_NEQ for WAIT_FOR_NMI
	proc = append(
		proc,
		// SETBANK SRAM
		0b0000_0010,
		0xF5,
	)
	for _, g := range mgetReadGroups[0].Reads {
		proc = append(
			proc,
			// SETOFFS SRAM
			0b0000_0001,
			byte(g.Offset&0xFF),
			byte(g.Offset>>8),
			// READ SRAM
			0b0000_0011,
			byte(g.Size&0xFF),
		)
	}
	// END
	proc = append(proc, 0)

	hex.Dumper(os.Stdout).Write(proc)

	// upload the IOVM procedure:
	var expectedSize uint32
	expectedSize, err = iovmUpload(f, proc)
	if err != nil {
		panic(err)
	}

	timesArr := [32768]float64{}
	times := timesArr[:0]
	t := 0

	sb := bytes.Buffer{}
	offs := [...]uint16{
		//0x0B58,
		//0x0B6B,
		//0x0B89,
		//0x0BA0,
		//0x0BB0,
		0x0BC0, // enemy slot in underworld
		//0x0BE0,
		//0x0C9A,
		0x0CAA, // flags
		//0x0CBA,
		//0x0CD2,
		//0x0CE2,
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
		0x0F00, // = #$01 if disabled off screen
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
	line := [4096]byte{}

	for {
		var readSize uint32
		tStart := time.Now()
		if true {
			readSize, err = iovmExecute(f, true, buf[:])
		} else {
			if true {
				_ = mgetReadGroups
				err = mget(f, mgetReadGroups, true, buf[:])
			} else {
				_ = vgetReads
				err = vget(f, vgetReads, buf[:])
			}
		}
		tEnd := time.Now()
		if err != nil {
			panic(err)
		}

		sb.Truncate(0)
		sb.WriteString("\033[3J")
		if true {
			fmt.Fprint(&sb, "\033[?25l\033[39m\033[1;1H")
			fmt.Fprintf(&sb, "iovm_upload expected emit_size=%d, actual emit_size=%d\n", expectedSize, readSize)
			hex.Dumper(&sb).Write(buf[0:readSize])
			os.Stdout.WriteString("\n")
		}

		if true {
			// copy data from buf to wram
			o := uint32(0)
			for i := range mgetReadGroups[0].Reads {
				read := &mgetReadGroups[0].Reads[i]
				copy(
					wram[read.Offset:read.Offset+read.Size],
					buf[o:o+uint32(read.Size)],
				)
				o += uint32(read.Size)
			}
		} else {
			// copy data from buf to wram
			for i := range mgetReadGroups[0].Reads {
				read := &mgetReadGroups[0].Reads[i]
				copy(
					wram[read.Offset:read.Offset+read.Size],
					read.Response,
				)
			}
		}

		const timingHist = false

		if timingHist {
			delta := tEnd.Sub(tStart).Nanoseconds()
			if len(times) < 32768 {
				times = append(times, float64(delta))
			} else {
				times[t] = float64(delta)
				t = (t + 1) & 32767
			}
		}

		if false {
			{
				j := 0
				fmt.Fprint(&sb, "\033[?25l\033[39m\033[1;1H  ")
				for n := 0; n < len(offs); n++ {
					a := offs[n]
					line[j+0] = ' '
					line[j+1] = hextable[(a>>8)&0xF]
					line[j+2] = hextable[(a>>4)&0xF]
					j += 3
				}
				line[j] = '\n'
				j++
				sb.Write(line[:j])
			}
			for i := uint16(0); i < 16; i++ {
				j := 0

				if wram[0x0DD0+i] == 0 {
					// disabled sprite:
					line[j+0] = 033
					line[j+1] = '['
					line[j+2] = '3'
					line[j+3] = '1'
					line[j+4] = 'm'
					j += 5
				} else {
					// enabled sprite:
					line[j+0] = 033
					line[j+1] = '['
					line[j+2] = '3'
					line[j+3] = '4'
					line[j+4] = 'm'
					j += 5
				}

				for n := 0; n < len(offs); n++ {
					a := offs[n]
					b := wram[a+i]

					line[j+0] = ' '
					j++

					if b == 0 {
						line[j+0] = ' '
						line[j+1] = ' '
					} else {
						line[j+0] = hextable[b>>4]
						line[j+1] = hextable[b&15]
					}
					j += 2
				}

				fmt.Fprintf(
					&sb,
					"\033[39m%01x:%s\n",
					i,
					line[:j],
				)
			}
		}

		if timingHist {
			fmt.Fprint(&sb, "\033[H\033[39m")
			h := histogram.PowerHist(1.0625, times)
			histogram.Fprintf(&sb, h, histogram.Linear(40), func(v float64) string {
				return fmt.Sprintf("% 11dns", time.Duration(v).Nanoseconds())
			})
		}

		// dump a bit of CPU stack:
		if false {
			j := 0
			for n := 0x1FF; n >= 0x1D0; n-- {
				b := wram[n]
				line[j+0] = hextable[b>>4]
				line[j+1] = hextable[b&15]
				line[j+2] = ' '
				j += 3
			}
			fmt.Fprint(&sb, "\033[55H")
			sb.Write(line[:j])
		}

		sb.WriteTo(os.Stdout)
	}
}
