package asm

import (
	"fmt"
	"io"
	"strings"
)

// Emitter implements Assembler and bytes.Buffer; a 65816 immediate assembler that emits to the buffer
type Emitter struct {
	flagsTracker

	generateText bool

	code []byte
	n    int

	lines []asmLine

	base    uint32
	baseSet bool
	address uint32

	// label name to address map:
	labels map[string]uint32
	// dangling references to labels; stored as absolute addresses:
	danglingS8 map[string][]uint32
}

type asmLineType int

const (
	lineIns1 asmLineType = iota
	lineIns2
	lineIns3
	lineIns4
	lineBase
	lineDB
	lineComment
	lineLabel
	lineIns2Label
)

type asmLine struct {
	asmLineType

	// instruction and parameters:
	address    uint32
	ins        string
	argsFormat string
}

func NewEmitter(target []byte, generateText bool) *Emitter {
	a := &Emitter{
		flagsTracker: 0,
		generateText: generateText,
		code:         target,
		n:            0,
		lines:        make([]asmLine, 0, 0x100),
		base:         0,
		baseSet:      false,
		address:      0,
		labels:       make(map[string]uint32),
		danglingS8:   make(map[string][]uint32),
	}
	return a
}

func (a *Emitter) Clone() *Emitter {
	return &Emitter{
		flagsTracker: a.flagsTracker,
		generateText: a.generateText,
		code:         make([]byte, len(a.code)),
		n:            0,
		lines:        make([]asmLine, 0, 0x100),
		address:      a.address,
		base:         a.base,
		baseSet:      a.baseSet,
		// TODO: copy labels
		labels:     make(map[string]uint32),
		danglingS8: make(map[string][]uint32),
	}
}

func (a *Emitter) WriteTextTo(w io.Writer) (err error) {
	if len(a.danglingS8) > 0 {
		panic("dangling label references remain")
	}

	for _, line := range a.lines {
		offs := line.address - a.base
		switch line.asmLineType {
		case lineBase:
			_, err = fmt.Fprintf(w, "base $%06x\n", line.address)
		case lineComment:
			_, err = fmt.Fprintf(w, "    ; %s\n", line.ins)
		case lineLabel:
			_, err = fmt.Fprintf(w, "%s:\n", line.ins)
		case lineDB:
			_, err = fmt.Fprintf(w, "    ; $%06x\n    %s\n", line.address, line.ins)
		case lineIns1:
			d := a.code[offs : offs+1]
			_, err = fmt.Fprintf(w, "    %-5s %-8s ; $%06x  %02x\n", line.ins, "", line.address, d[0])
		case lineIns2:
			d := a.code[offs : offs+2]
			args := fmt.Sprintf(line.argsFormat, d[1])
			_, err = fmt.Fprintf(w, "    %-5s %-8s ; $%06x  %02x %02x\n", line.ins, args, line.address, d[0], d[1])
		case lineIns2Label:
			d := a.code[offs : offs+2]
			args := line.argsFormat
			_, err = fmt.Fprintf(w, "    %-5s %-8s ; $%06x  %02x %02x\n", line.ins, args, line.address, d[0], d[1])
		case lineIns3:
			d := a.code[offs : offs+3]
			args := fmt.Sprintf(line.argsFormat, d[1], d[2])
			_, err = fmt.Fprintf(w, "    %-5s %-8s ; $%06x  %02x %02x %02x\n", line.ins, args, line.address, d[0], d[1], d[2])
		case lineIns4:
			d := a.code[offs : offs+4]
			args := fmt.Sprintf(line.argsFormat, d[1], d[2], d[3])
			_, err = fmt.Fprintf(w, "    %-5s %-8s ; $%06x  %02x %02x %02x %02x\n", line.ins, args, line.address, d[0], d[1], d[2], d[3])
		}

		if err != nil {
			return
		}
	}
	return
}

func (a *Emitter) Finalize() {
	// resolves all dangling label references in prior code
	for label, refs := range a.danglingS8 {
		addr, ok := a.labels[label]
		if !ok {
			panic(fmt.Errorf("could not resolve label '%s'", label))
		}

		// fill in signed 8-bit (-128..+127) references to this label:
		for _, s8addr := range refs {
			// adding 1 here to accommodate the size of the S8 instruction parameter
			diff := int(addr) - int(s8addr+1)
			if diff > 127 || diff < -128 {
				panic(fmt.Errorf("branch from %#06x to %#06x too far for signed 8-bit; diff=%d", s8addr+1, addr, diff))
			}
			a.code[s8addr-a.base] = uint8(int8(diff))
		}

		delete(a.danglingS8, label)
	}
}

func (a *Emitter) Label(name string) uint32 {
	if oldAddr, ok := a.labels[name]; ok {
		panic(fmt.Errorf("label '%s' already defined at %#06x", name, oldAddr))
	}

	// define new label:
	a.labels[name] = a.address

	if a.generateText {
		a.lines = append(a.lines, asmLine{
			asmLineType: lineLabel,
			address:     a.address,
			ins:         name,
			argsFormat:  "",
		})
		//_, _ = fmt.Fprintf(a.Text, "%s:\n", name)
	}

	return a.address
}

func (a *Emitter) Len() int {
	//return a.code.Len()
	//return int(a.address) - int(a.base)
	return a.n
}

func (a *Emitter) Bytes() []byte {
	return a.code[0:a.Len()]
}

func (a *Emitter) PC() uint32 {
	return a.address
}

func (a *Emitter) Append(e *Emitter) {
	if a.n+e.n > len(a.code) {
		panic(fmt.Errorf("not enough space"))
	}

	a.address = e.address
	a.baseSet = e.baseSet
	a.flagsTracker = e.flagsTracker

	a.n += copy(a.code[a.n:], e.code[0:e.n])
	a.lines = append(a.lines, e.lines...)
}

func (a *Emitter) write(d []byte) (int, error) {
	if a.code == nil {
		return 0, nil
	}

	if a.n+len(d) > len(a.code) {
		panic(fmt.Errorf("not enough space"))
	}

	n := copy(a.code[a.n:], d)
	a.n += n
	return n, nil
}

func (a *Emitter) SetBase(addr uint32) {
	a.base = addr
	a.address = addr
	a.baseSet = true
}

func (a *Emitter) GetBase() uint32 {
	return a.address
}

func (a *Emitter) emitBase() {
	if !a.generateText {
		return
	}
	if !a.baseSet {
		return
	}

	a.lines = append(a.lines, asmLine{
		asmLineType: lineBase,
		address:     a.address,
		ins:         "",
		argsFormat:  "",
	})
	//_, _ = a.Text.WriteString(fmt.Sprintf("base $%06x\n", a.address))
	a.baseSet = false
}

func (a *Emitter) emit1(ins string, d [1]byte) {
	_, _ = a.write(d[:])
	if a.generateText {
		a.emitBase()
		a.lines = append(a.lines, asmLine{
			asmLineType: lineIns1,
			address:     a.address,
			ins:         ins,
			argsFormat:  "",
		})

		//_, _ = a.Text.WriteString(fmt.Sprintf("    %-5s %-8s ; $%06x  %02x\n", ins, "", a.address, d[0]))
	}
	a.address += 1
}

func (a *Emitter) emit2(ins, argsFormat string, d [2]byte) {
	_, _ = a.write(d[:])
	if a.generateText {
		a.emitBase()
		a.lines = append(a.lines, asmLine{
			asmLineType: lineIns2,
			address:     a.address,
			ins:         ins,
			argsFormat:  argsFormat,
		})

		//args := fmt.Sprintf(argsFormat, d[1])
		//_, _ = a.Text.WriteString(fmt.Sprintf("    %-5s %-8s ; $%06x  %02x %02x\n", ins, args, a.address, d[0], d[1]))
	}
	a.address += 2
}

func (a *Emitter) emit3(ins, argsFormat string, d [3]byte) {
	_, _ = a.write(d[:])
	if a.generateText {
		a.emitBase()
		a.lines = append(a.lines, asmLine{
			asmLineType: lineIns3,
			address:     a.address,
			ins:         ins,
			argsFormat:  argsFormat,
		})

		//args := fmt.Sprintf(argsFormat, d[1], d[2])
		//_, _ = a.Text.WriteString(fmt.Sprintf("    %-5s %-8s ; $%06x  %02x %02x %02x\n", ins, args, a.address, d[0], d[1], d[2]))
	}
	a.address += 3
}

func (a *Emitter) emit4(ins, argsFormat string, d [4]byte) {
	_, _ = a.write(d[:])
	if a.generateText {
		a.emitBase()
		a.lines = append(a.lines, asmLine{
			asmLineType: lineIns4,
			address:     a.address,
			ins:         ins,
			argsFormat:  argsFormat,
		})

		//args := fmt.Sprintf(argsFormat, d[1], d[2], d[3])
		//_, _ = a.Text.WriteString(fmt.Sprintf("    %-5s %-8s ; $%06x  %02x %02x %02x %02x\n", ins, args, a.address, d[0], d[1], d[2], d[3]))
	}
	a.address += 4
}

func imm24(v uint32) (byte, byte, byte) {
	return byte(v), byte(v >> 8), byte(v >> 16)
}

func imm16(v uint16) (byte, byte) {
	return byte(v), byte(v >> 8)
}

func (a *Emitter) Comment(s string) {
	if a.generateText {
		a.emitBase()
		a.lines = append(a.lines, asmLine{
			asmLineType: lineComment,
			address:     a.address,
			ins:         s,
			argsFormat:  "",
		})

		//_, _ = a.Text.WriteString(fmt.Sprintf("    ; %s\n", s))
	}
}

const hextable = "0123456789abcdef"

func (a *Emitter) EmitBytes(b []byte) {
	if a.generateText {
		a.emitBase()
		s := strings.Builder{}
		blen := len(b)
		for i, v := range b {
			s.Write([]byte{'$', hextable[(v>>4)&0xF], hextable[v&0xF]})
			if i < blen-1 {
				s.Write([]byte{',', ' '})
			}
		}
		a.lines = append(a.lines, asmLine{
			asmLineType: lineDB,
			address:     a.address,
			ins:         s.String(),
			argsFormat:  "",
		})
		//_, _ = a.Text.WriteString(fmt.Sprintf("    %-5s %s\n", "db", s.String()))
	}
	_, _ = a.write(b)
	a.address += uint32(len(b))
}

func (a *Emitter) REP(c Flags) {
	a.AssumeREP(c)
	a.emit2("rep", "#$%02x", [2]byte{0xC2, byte(c)})
}

func (a *Emitter) SEP(c Flags) {
	a.AssumeSEP(c)
	a.emit2("sep", "#$%02x", [2]byte{0xE2, byte(c)})
}

func (a *Emitter) NOP() {
	a.emit1("nop", [1]byte{0xEA})
}

func (a *Emitter) JSR_abs(addr uint16) {
	var d [3]byte
	d[0] = 0x20
	d[1], d[2] = imm16(addr)
	a.emit3("jsr", "$%02[2]x%02[1]x", d)
}

func (a *Emitter) JSL(addr uint32) {
	var d [4]byte
	d[0] = 0x22
	d[1], d[2], d[3] = imm24(addr)
	a.emit4("jsl", "$%02[3]x%02[2]x%02[1]x", d)
}

func (a *Emitter) JSL_lhb(lo, hi, bank uint8) {
	var d [4]byte
	d[0] = 0x22
	d[1], d[2], d[3] = lo, hi, bank
	a.emit4("jsl", "$%02[3]x%02[2]x%02[1]x", d)
}

func (a *Emitter) JML(addr uint32) {
	var d [4]byte
	d[0] = 0x5C
	d[1], d[2], d[3] = imm24(addr)
	a.emit4("jml", "$%02[3]x%02[2]x%02[1]x", d)
}

func (a *Emitter) RTS() {
	a.emit1("rts", [1]byte{0x60})
}

func (a *Emitter) RTL() {
	a.emit1("rtl", [1]byte{0x6B})
}

func (a *Emitter) LDA_imm8_b(m uint8) {
	if a.IsM16bit() {
		panic(fmt.Errorf("asm: LDA_imm8_b called but 'm' flag is 16-bit; call SEP(0x20) or AssumeSEP(0x20) first"))
	}
	var d [2]byte
	d[0] = 0xA9
	d[1] = m
	a.emit2("lda.b", "#$%02x", d)
}

func (a *Emitter) LDA_imm16_w(m uint16) {
	if !a.IsM16bit() {
		panic(fmt.Errorf("asm: LDA_imm16_w called but 'm' flag is 8-bit; call REP(0x20) or AssumeREP(0x20) first"))
	}
	var d [3]byte
	d[0] = 0xA9
	d[1], d[2] = imm16(m)
	a.emit3("lda.w", "#$%02[2]x%02[1]x", d)
}

func (a *Emitter) LDA_imm16_lh(lo, hi uint8) {
	if !a.IsM16bit() {
		panic(fmt.Errorf("asm: LDA_imm16_lh called but 'm' flag is 8-bit; call REP(0x20) or AssumeREP(0x20) first"))
	}
	var d [3]byte
	d[0] = 0xA9
	d[1], d[2] = lo, hi
	a.emit3("lda.w", "#$%02[2]x%02[1]x", d)
}

func (a *Emitter) LDA_long(addr uint32) {
	var d [4]byte
	d[0] = 0xAF
	d[1], d[2], d[3] = imm24(addr)
	a.emit4("lda.l", "$%02[3]x%02[2]x%02[1]x", d)
}

func (a *Emitter) LDA_abs(addr uint16) {
	var d [3]byte
	d[0] = 0xAD
	d[1], d[2] = imm16(addr)
	a.emit3("lda.w", "$%02[2]x%02[1]x", d)
}

func (a *Emitter) LDA_abs_x(addr uint16) {
	var d [3]byte
	d[0] = 0xBD
	d[1], d[2] = imm16(addr)
	a.emit3("lda.w", "$%02[2]x%02[1]x,X", d)
}

func (a *Emitter) STA_long(addr uint32) {
	var d [4]byte
	d[0] = 0x8F
	d[1], d[2], d[3] = imm24(addr)
	a.emit4("sta.l", "$%02[3]x%02[2]x%02[1]x", d)
}

func (a *Emitter) STA_abs(addr uint16) {
	var d [3]byte
	d[0] = 0x8D
	d[1], d[2] = imm16(addr)
	a.emit3("sta.w", "$%02[2]x%02[1]x", d)
}

func (a *Emitter) STA_abs_x(addr uint16) {
	var d [3]byte
	d[0] = 0x9D
	d[1], d[2] = imm16(addr)
	a.emit3("sta.w", "$%02[2]x%02[1]x,X", d)
}

func (a *Emitter) STA_dp(addr uint8) {
	var d [2]byte
	d[0] = 0x85
	d[1] = addr
	a.emit2("sta.b", "$%02[1]x", d)
}

func (a *Emitter) ORA_long(addr uint32) {
	var d [4]byte
	d[0] = 0x0F
	d[1], d[2], d[3] = imm24(addr)
	a.emit4("ora.l", "$%02[3]x%02[2]x%02[1]x", d)
}

func (a *Emitter) ORA_imm8_b(m uint8) {
	if a.IsM16bit() {
		panic(fmt.Errorf("asm: ORA_imm8_b called but 'm' flag is 16-bit; call SEP(0x20) or AssumeSEP(0x20) first"))
	}
	var d [2]byte
	d[0] = 0x09
	d[1] = m
	a.emit2("ora.b", "#$%02x", d)
}

func (a *Emitter) CMP_imm8_b(m uint8) {
	if a.IsM16bit() {
		panic(fmt.Errorf("asm: CMP_imm8_b called but 'm' flag is 16-bit; call SEP(0x20) or AssumeSEP(0x20) first"))
	}
	var d [2]byte
	d[0] = 0xC9
	d[1] = m
	a.emit2("cmp.b", "#$%02x", d)
}

func (a *Emitter) BNE_imm8(m int8) {
	var d [2]byte
	d[0] = 0xD0
	d[1] = uint8(m)
	a.emit2("bne", "$%02x", d)
}

func (a *Emitter) BNE(label string) {
	var d [2]byte
	d[0] = 0xD0
	d[1] = 0xFF // will be overwritten by Finalize()
	_, _ = a.write(d[:])
	if a.generateText {
		a.emitBase()
		a.lines = append(a.lines, asmLine{
			asmLineType: lineIns2Label,
			address:     a.address,
			ins:         "bne",
			argsFormat:  label,
		})

		//_, _ = a.Text.WriteString(fmt.Sprintf("    %-5s %-8s ; $%06x  %02x __\n", "bne", label, a.address, d[0]))
	}
	a.address += 2

	refs := a.danglingS8[label]
	refs = append(refs, a.address-1)
	a.danglingS8[label] = refs
}

func (a *Emitter) BEQ_imm8(m int8) {
	var d [2]byte
	d[0] = 0xF0
	d[1] = uint8(m)
	a.emit2("beq", "$%02x", d)
}

func (a *Emitter) BPL_imm8(m int8) {
	var d [2]byte
	d[0] = 0x10
	d[1] = uint8(m)
	a.emit2("bpl", "$%02x", d)
}

func (a *Emitter) BRA_imm8(m int8) {
	var d [2]byte
	d[0] = 0x80
	d[1] = uint8(m)
	a.emit2("bra", "$%02x", d)
}

func (a *Emitter) ADC_imm8_b(m uint8) {
	if a.IsM16bit() {
		panic(fmt.Errorf("asm: ADC_imm8_b called but 'm' flag is 16-bit; call SEP(0x20) or AssumeSEP(0x20) first"))
	}
	var d [2]byte
	d[0] = 0x69
	d[1] = m
	a.emit2("adc.b", "#$%02x", d)
}

func (a *Emitter) CPY_imm8_b(m uint8) {
	if a.IsX16bit() {
		panic(fmt.Errorf("asm: CPY_imm8_b called but 'x' flag is 16-bit; call SEP(0x10) or AssumeSEP(0x10) first"))
	}
	var d [2]byte
	d[0] = 0xC0
	d[1] = m
	a.emit2("cpy.b", "#$%02x", d)
}

func (a *Emitter) LDY_abs(offs uint16) {
	var d [3]byte
	d[0] = 0xAC
	d[1], d[2] = imm16(offs)
	a.emit3("ldy.w", "$%02[2]x%02[1]x", d)
}

func (a *Emitter) STZ_abs(offs uint16) {
	var d [3]byte
	d[0] = 0x9C
	d[1], d[2] = imm16(offs)
	a.emit3("stz.w", "$%02[2]x%02[1]x", d)
}

func (a *Emitter) STZ_abs_x(addr uint16) {
	var d [3]byte
	d[0] = 0x9E
	d[1], d[2] = imm16(addr)
	a.emit3("stz.w", "$%02[2]x%02[1]x,X", d)
}

func (a *Emitter) INC_dp(addr uint8) {
	var d [2]byte
	d[0] = 0xE6
	d[1] = addr
	a.emit2("inc.b", "$%02[1]x", d)
}

func (a *Emitter) LDA_dp(addr uint8) {
	var d [2]byte
	d[0] = 0xA5
	d[1] = addr
	a.emit2("lda.b", "$%02[1]x", d)
}

func (a *Emitter) LDX_imm8_b(m uint8) {
	if a.IsX16bit() {
		panic(fmt.Errorf("asm: LDX_imm8_b called but 'x' flag is 16-bit; call SEP(0x10) or AssumeSEP(0x10) first"))
	}
	var d [2]byte
	d[0] = 0xA2
	d[1] = m
	a.emit2("ldx.b", "#$%02x", d)
}

func (a *Emitter) DEX() {
	a.emit1("dex", [1]byte{0xCA})
}

func (a *Emitter) DEY() {
	a.emit1("dey", [1]byte{0x88})
}

func (a *Emitter) AND_imm8_b(m uint8) {
	if a.IsM16bit() {
		panic(fmt.Errorf("asm: AND_imm8_b called but 'm' flag is 16-bit; call SEP(0x20) or AssumeSEP(0x20) first"))
	}
	var d [2]byte
	d[0] = 0x29
	d[1] = m
	a.emit2("and.b", "#$%02x", d)
}
