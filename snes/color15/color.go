package color15

type Color uint16

func (c Color) ToRGB() (r, g, b uint8) {
	b = uint8((c & 0x7E00) >> 10)
	g = uint8((c & 0x03E0) >> 5)
	r = uint8(c & 0x001F)
	return
}

func ToColor15(r, g, b uint8) (c Color) {
	c = Color((uint16(b&31) << 10) | (uint16(g&31) << 5) | uint16(r&31))
	return
}

func (c Color) Luminosity() (l uint8) {
	r, g, b := c.ToRGB()
	sum := (r + g + b) / 3
	l = sum
	return
}

func (c Color) MulDiv(multiplicand, divisor uint8) (m Color) {
	r, g, b := c.ToRGB()
	mr := uint8((uint16(r) * uint16(multiplicand)) / uint16(divisor))
	mg := uint8((uint16(g) * uint16(multiplicand)) / uint16(divisor))
	mb := uint8((uint16(b) * uint16(multiplicand)) / uint16(divisor))
	if mr > 31 {
		mr = 31
	}
	if mg > 31 {
		mg = 31
	}
	if mb > 31 {
		mb = 31
	}
	return ToColor15(mr, mg, mb)
}
