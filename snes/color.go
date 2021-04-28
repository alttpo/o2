package snes

func FromBGR16(c uint16) (r, g, b uint8) {
	b = uint8((c & 0x7E00) >> 10)
	g = uint8((c & 0x03E0) >> 5)
	r = uint8(c & 0x001F)
	return
}

func ToBGR16(r, g, b uint8) (c uint16) {
	c = (uint16(b&31) << 10) | (uint16(g&31) << 5) | uint16(r&31)
	return
}

// Luminosity applies 0.299*R + 0.587*G + 0.114*B and returns the value
// components scaled from 1.000 to 1024 to use an easier divide by 1024 aka SHR 10:
//   0.299*R  ->  306.176*R
//   0.587*G  ->  601.088*G
//   0.114*B  ->  116.736*B  ; truncate to 116 to get a max value of 1023
func Luminosity(r, g, b uint8) (l uint8) {
	sum := uint32(r)*306 + uint32(g)*601 + uint32(b)*116
	l = uint8(sum >> 10)
	return
}

func MulDiv(r, g, b uint8, multiplicand, divisor uint8) (mr, mg, mb uint8) {
	mr = uint8((uint16(r) * uint16(multiplicand)) / uint16(divisor))
	mg = uint8((uint16(g) * uint16(multiplicand)) / uint16(divisor))
	mb = uint8((uint16(b) * uint16(multiplicand)) / uint16(divisor))
	return
}
