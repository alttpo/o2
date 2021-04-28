package snes

import "testing"

func TestFromBGR16(t *testing.T) {
	type args struct {
		c uint16
	}
	tests := []struct {
		name  string
		args  args
		wantR uint8
		wantG uint8
		wantB uint8
	}{
		{
			name:  "3647",
			args:  args{c: 0x3647},
			wantR: 7,
			wantG: 18,
			wantB: 13,
		},
		{
			name:  "3b68",
			args:  args{c: 0x3b68},
			wantR: 8,
			wantG: 27,
			wantB: 14,
		},
		{
			name:  "0a4a",
			args:  args{c: 0x0a4a},
			wantR: 10,
			wantG: 18,
			wantB: 2,
		},
		{
			name:  "12ef",
			args:  args{c: 0x12ef},
			wantR: 15,
			wantG: 23,
			wantB: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotR, gotG, gotB := FromBGR16(tt.args.c)
			if gotR != tt.wantR {
				t.Errorf("FromBGR16() gotR = %v, want %v", gotR, tt.wantR)
			}
			if gotG != tt.wantG {
				t.Errorf("FromBGR16() gotG = %v, want %v", gotG, tt.wantG)
			}
			if gotB != tt.wantB {
				t.Errorf("FromBGR16() gotB = %v, want %v", gotB, tt.wantB)
			}
		})
	}
}

func TestToBGR16(t *testing.T) {
	type args struct {
		r uint8
		g uint8
		b uint8
	}
	tests := []struct {
		name  string
		args  args
		wantC uint16
	}{
		{
			name: "3647",
			args: args{
				r: 7,
				g: 18,
				b: 13,
			},
			wantC: 0x3647,
		},
		{
			name: "3b68",
			args: args{
				r: 8,
				g: 27,
				b: 14,
			},
			wantC: 0x3b68,
		},
		{
			name: "0a4a",
			args: args{
				r: 10,
				g: 18,
				b: 2,
			},
			wantC: 0x0a4a,
		},
		{
			name: "12ef",
			args: args{
				r: 15,
				g: 23,
				b: 4,
			},
			wantC: 0x12ef,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotC := ToBGR16(tt.args.r, tt.args.g, tt.args.b); gotC != tt.wantC {
				t.Errorf("ToBGR16() = %v, want %v", gotC, tt.wantC)
			}
		})
	}
}

func TestLuminosity(t *testing.T) {
	type args struct {
		r uint8
		g uint8
		b uint8
	}
	tests := []struct {
		name  string
		args  args
		wantL uint8
	}{
		{
			name: "0a4a",
			args: args{
				r: 10,
				g: 18,
				b: 2,
			},
			wantL: 13,
		},
		{
			name: "12ef",
			args: args{
				r: 15,
				g: 23,
				b: 4,
			},
			wantL: 18,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotL := Luminosity(tt.args.r, tt.args.g, tt.args.b); gotL != tt.wantL {
				t.Errorf("Luminosity() = %v, want %v", gotL, tt.wantL)
			}
		})
	}
}

func TestMulDiv(t *testing.T) {
	type args struct {
		r            uint8
		g            uint8
		b            uint8
		multiplicand uint8
		divisor      uint8
	}
	tests := []struct {
		name   string
		args   args
		wantMr uint8
		wantMg uint8
		wantMb uint8
	}{
		{
			name: "12ef",
			args: args{
				r:            15,
				g:            23,
				b:            4,
				multiplicand: 25,
				divisor:      31,
			},
			wantMr: 12,
			wantMg: 18,
			wantMb: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMr, gotMg, gotMb := MulDiv(tt.args.r, tt.args.g, tt.args.b, tt.args.multiplicand, tt.args.divisor)
			if gotMr != tt.wantMr {
				t.Errorf("MulDiv() gotMr = %v, want %v", gotMr, tt.wantMr)
			}
			if gotMg != tt.wantMg {
				t.Errorf("MulDiv() gotMg = %v, want %v", gotMg, tt.wantMg)
			}
			if gotMb != tt.wantMb {
				t.Errorf("MulDiv() gotMb = %v, want %v", gotMb, tt.wantMb)
			}
		})
	}
}
