// Package qr implements a minimal QR Code encoder (byte mode, ECC level
// L, versions 1-5) plus an ANSI terminal renderer — enough to turn short
// share links into scannable QR codes with zero external dependencies.
package qr

import (
	"fmt"
	"strings"
)

// Matrix is a QR code bitmap indexed [y][x]; true = dark module.
type Matrix [][]bool

// Size returns the width/height of the matrix in modules.
func (m Matrix) Size() int { return len(m) }

// Version tables (ECC level L, single RS block).
var (
	sizes       = [5]int{21, 25, 29, 33, 37}
	dataCW      = [5]int{19, 34, 55, 80, 108}
	eccCW       = [5]int{7, 10, 15, 20, 26}
	alignCenter = [5]int{0, 18, 22, 26, 30} // version 1 has no alignment pattern
)

// Encode returns the QR code for text using the smallest suitable
// version (byte mode, ECC L). Maximum length: 106 bytes.
func Encode(text string) (Matrix, error) {
	ver := -1
	for i := 0; i < len(sizes); i++ {
		capacity := (dataCW[i]*8 - 12) / 8
		if len(text) <= capacity {
			ver = i
			break
		}
	}
	if ver < 0 {
		return nil, fmt.Errorf("qr: text too long (%d bytes, max 106)", len(text))
	}

	// Bit stream: mode (byte), 8-bit length, data, terminator.
	bits := make([]bool, 0, dataCW[ver]*8)
	addBits := func(v uint, count int) {
		for i := count - 1; i >= 0; i-- {
			bits = append(bits, (v>>uint(i))&1 == 1)
		}
	}
	addBits(0b0100, 4) // byte mode
	addBits(uint(len(text)), 8)
	for i := 0; i < len(text); i++ {
		addBits(uint(text[i]), 8)
	}
	for i := 0; i < 4 && len(bits) < dataCW[ver]*8; i++ {
		bits = append(bits, false)
	}
	for len(bits)%8 != 0 {
		bits = append(bits, false)
	}

	data := make([]byte, 0, dataCW[ver])
	for i := 0; i+8 <= len(bits); i += 8 {
		var b byte
		for j := 0; j < 8; j++ {
			if bits[i+j] {
				b |= 1 << (7 - j)
			}
		}
		data = append(data, b)
	}
	for pad := 0; len(data) < dataCW[ver]; pad++ {
		if pad%2 == 0 {
			data = append(data, 0xEC)
		} else {
			data = append(data, 0x11)
		}
	}

	// Reed-Solomon error correction.
	gen := generatorPoly(eccCW[ver])
	rem := rsRemainder(data, gen)
	ecc := make([]byte, len(rem))
	for i := range rem {
		ecc[i] = rem[len(rem)-1-i] // highest-degree coefficient first
	}
	cw := make([]byte, 0, len(data)+len(ecc))
	cw = append(cw, data...)
	cw = append(cw, ecc...)

	bestScore := 1 << 30
	var best Matrix
	for mask := 0; mask < 8; mask++ {
		m, fn := newMatrix(ver)
		drawFunctionPatterns(m, fn, ver)
		placeCodewords(m, fn, cw)
		applyMask(m, fn, mask)
		drawFormatBits(m, fn, ver, mask)
		if s := penalty(m); s < bestScore {
			bestScore = s
			best = m
		}
	}
	return best, nil
}

// RenderANSI renders the matrix as truecolor half-block characters with
// a quiet zone, ready to print in a terminal.
func RenderANSI(m Matrix) string {
	const quiet = 4
	size := m.Size()
	module := func(x, y int) bool {
		if y < 0 || y >= size || x < 0 || x >= size {
			return false
		}
		return m[y][x]
	}
	var b strings.Builder
	for y := -quiet; y < size+quiet; y += 2 {
		for x := -quiet; x < size+quiet; x++ {
			tr, tg, tb := 255, 255, 255
			if module(x, y) {
				tr, tg, tb = 0, 0, 0
			}
			br, bg, bb := 255, 255, 255
			if module(x, y+1) {
				br, bg, bb = 0, 0, 0
			}
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", tr, tg, tb, br, bg, bb)
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String()
}

// ───────────────────────── GF(256) arithmetic ─────────────────────────

var gfExp, gfLog [256]byte

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[byte(x)] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11d
		}
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[(int(gfLog[a])+int(gfLog[b]))%255]
}

// generatorPoly returns the Reed-Solomon generator polynomial for n
// error-correction codewords, coefficients in ascending order (leading
// coefficient excluded, monic).
func generatorPoly(n int) []byte {
	gen := make([]byte, 1)
	gen[0] = 1
	for i := 0; i < n; i++ {
		a := gfExp[i]
		ng := make([]byte, i+2)
		for k := 0; k <= i+1; k++ {
			var v byte
			if k <= i {
				v ^= gfMul(gen[k], a)
			}
			if k >= 1 {
				v ^= gen[k-1]
			}
			ng[k] = v
		}
		gen = ng
	}
	return gen
}

// rsRemainder computes the remainder of data·x^n divided by gen, using the
// standard shift-register encoder. r[k] is the coefficient of x^k.
func rsRemainder(data, gen []byte) []byte {
	n := len(gen) - 1
	r := make([]byte, n)
	for _, b := range data {
		fb := b ^ r[n-1]
		for k := n - 1; k > 0; k-- {
			r[k] = r[k-1] ^ gfMul(fb, gen[k])
		}
		r[0] = gfMul(fb, gen[0])
	}
	return r
}

// ───────────────────────── matrix drawing ─────────────────────────

func newMatrix(ver int) (Matrix, [][]bool) {
	size := sizes[ver]
	m := make(Matrix, size)
	fn := make([][]bool, size)
	for i := range m {
		m[i] = make([]bool, size)
		fn[i] = make([]bool, size)
	}
	return m, fn
}

func set(m Matrix, fn [][]bool, x, y int, dark bool) {
	m[y][x] = dark
	fn[y][x] = true
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func drawFunctionPatterns(m Matrix, fn [][]bool, ver int) {
	size := sizes[ver]
	drawFinder(m, fn, 3, 3)
	drawFinder(m, fn, size-4, 3)
	drawFinder(m, fn, 3, size-4)
	for i := 8; i < size-8; i++ {
		set(m, fn, i, 6, i%2 == 0)
		set(m, fn, 6, i, i%2 == 0)
	}
	if c := alignCenter[ver]; c != 0 {
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				dist := absInt(dx)
				if absInt(dy) > dist {
					dist = absInt(dy)
				}
				set(m, fn, c+dx, c+dy, dist != 1)
			}
		}
	}
	set(m, fn, 8, size-8, true)      // dark module
	drawFormatBits(m, fn, ver, 0)    // reserve format areas as function modules
}

func drawFinder(m Matrix, fn [][]bool, cx, cy int) {
	size := len(m)
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			dist := absInt(dx)
			if absInt(dy) > dist {
				dist = absInt(dy)
			}
			x, y := cx+dx, cy+dy
			if x < 0 || y < 0 || x >= size || y >= size {
				continue
			}
			set(m, fn, x, y, dist != 2 && dist != 4)
		}
	}
}

// formatBits builds the 15-bit format information for ECC level L and the
// given mask (BCH code, XOR-masked with 0x5412).
func formatBits(mask int) uint {
	data := uint(1)<<3 | uint(mask) // ECC level L = 01
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return (data<<10 | rem) ^ 0x5412
}

func drawFormatBits(m Matrix, fn [][]bool, ver, mask int) {
	size := sizes[ver]
	bits := formatBits(mask)
	bit := func(i int) bool {
		return (bits>>uint(i))&1 == 1
	}
	// First copy (around the top-left finder).
	for i := 0; i <= 5; i++ {
		set(m, fn, 8, i, bit(i))
	}
	set(m, fn, 8, 7, bit(6))
	set(m, fn, 8, 8, bit(7))
	set(m, fn, 7, 8, bit(8))
	for i := 9; i < 15; i++ {
		set(m, fn, 14-i, 8, bit(i))
	}
	// Second copy (split around the other finders).
	for i := 0; i < 8; i++ {
		set(m, fn, size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		set(m, fn, 8, size-15+i, bit(i))
	}
}

// placeCodewords writes the codeword bits into the matrix in the
// standard zigzag order (two-module columns, right to left, skipping
// the timing column), skipping function modules.
func placeCodewords(m Matrix, fn [][]bool, cw []byte) {
	size := len(m)
	i := 0
	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				upward := (right+1)&2 == 0
				y := vert
				if upward {
					y = size - 1 - vert
				}
				if !fn[y][x] && i < len(cw)*8 {
					m[y][x] = (cw[i>>3]>>(7-(i%8)))&1 == 1
					i++
				}
			}
		}
	}
}

func maskBit(mask, x, y int) bool {
	switch mask {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return (x*y)%2+(x*y)%3 == 0
	case 6:
		return ((x*y)%2+(x*y)%3)%2 == 0
	default:
		return ((x+y)%2+(x*y)%3)%2 == 0
	}
}

// applyMask XORs the mask pattern into all non-function modules.
func applyMask(m Matrix, fn [][]bool, mask int) {
	size := len(m)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !fn[y][x] && maskBit(mask, x, y) {
				m[y][x] = !m[y][x]
			}
		}
	}
}

// penalty scores a masked matrix with the standard QR penalty rules so
// the best mask can be chosen.
func penalty(m Matrix) int {
	size := m.Size()
	result := 0

	// Rule 1: runs of five or more same-colored modules.
	for y := 0; y < size; y++ {
		runColor, runLen := false, 0
		for x := 0; x < size; x++ {
			if m[y][x] == runColor {
				runLen++
				if runLen == 5 {
					result += 3
				} else if runLen > 5 {
					result++
				}
			} else {
				runColor, runLen = m[y][x], 1
			}
		}
	}
	for x := 0; x < size; x++ {
		runColor, runLen := false, 0
		for y := 0; y < size; y++ {
			if m[y][x] == runColor {
				runLen++
				if runLen == 5 {
					result += 3
				} else if runLen > 5 {
					result++
				}
			} else {
				runColor, runLen = m[y][x], 1
			}
		}
	}

	// Rule 2: 2x2 blocks of the same color.
	for y := 0; y < size-1; y++ {
		for x := 0; x < size-1; x++ {
			c := m[y][x]
			if c == m[y][x+1] && c == m[y+1][x] && c == m[y+1][x+1] {
				result += 3
			}
		}
	}

	// Rule 3: finder-like 1:1:3:1:1 patterns.
	pat1 := "10111010000"
	pat2 := "00001011101"
	for y := 0; y < size; y++ {
		var sb strings.Builder
		for x := 0; x < size; x++ {
			if m[y][x] {
				sb.WriteByte('1')
			} else {
				sb.WriteByte('0')
			}
		}
		s := sb.String()
		result += 40 * (strings.Count(s, pat1) + strings.Count(s, pat2))
	}
	for x := 0; x < size; x++ {
		var sb strings.Builder
		for y := 0; y < size; y++ {
			if m[y][x] {
				sb.WriteByte('1')
			} else {
				sb.WriteByte('0')
			}
		}
		s := sb.String()
		result += 40 * (strings.Count(s, pat1) + strings.Count(s, pat2))
	}

	// Rule 4: balance of dark and light modules.
	dark, total := 0, size*size
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if m[y][x] {
				dark++
			}
		}
	}
	k := (absInt(dark*20-total*10) + total - 1) / total - 1
	if k > 0 {
		result += k * 10
	}
	return result
}
