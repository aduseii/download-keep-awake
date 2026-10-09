//go:build windows

package main

import "unsafe"

type rgb struct{ R, G, B uint8 }

var (
	colorActive = rgb{0x22, 0xA4, 0x5D} // green  - keeping PC awake for a download
	colorIdle   = rgb{0x8A, 0x8F, 0x98} // grey   - normal sleep allowed
	colorPaused = rgb{0xE8, 0x9B, 0x17} // amber  - paused by the user
	colorManual = rgb{0x2F, 0x80, 0xED} // blue   - "keep awake now" turned on
)

// The icon is a filled circle with a white "download" arrow over a tray line.
// Coordinates are normalised to 0..1 so the icon renders sharply at any size.
func inCircle(x, y float64) bool {
	dx, dy := x-0.5, y-0.5
	return dx*dx+dy*dy <= 0.47*0.47
}

func sign(px, py, ax, ay, bx, by float64) float64 {
	return (px-bx)*(ay-by) - (ax-bx)*(py-by)
}

func inTriangle(px, py, ax, ay, bx, by, cx, cy float64) bool {
	d1 := sign(px, py, ax, ay, bx, by)
	d2 := sign(px, py, bx, by, cx, cy)
	d3 := sign(px, py, cx, cy, ax, ay)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

func inGlyph(x, y float64) bool {
	if x >= 0.41 && x <= 0.59 && y >= 0.17 && y <= 0.50 { // arrow shaft
		return true
	}
	if inTriangle(x, y, 0.21, 0.43, 0.79, 0.43, 0.50, 0.74) { // arrow head
		return true
	}
	if x >= 0.27 && x <= 0.73 && y >= 0.79 && y <= 0.87 { // tray line
		return true
	}
	return false
}

// renderIcon draws the icon into top-down BGRA pixels (straight alpha).
func renderIcon(size int, c rgb) []byte {
	const ss = 4 // 4x4 supersampling for smooth edges
	px := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			circle, glyph := 0, 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					fx := (float64(x) + (float64(sx)+0.5)/ss) / float64(size)
					fy := (float64(y) + (float64(sy)+0.5)/ss) / float64(size)
					if inCircle(fx, fy) {
						circle++
						if inGlyph(fx, fy) {
							glyph++
						}
					}
				}
			}
			if circle == 0 {
				continue
			}
			t := float64(glyph) / float64(circle) // share of white within the covered area
			mix := func(v uint8) byte { return byte(float64(v)*(1-t) + 255*t + 0.5) }
			i := (y*size + x) * 4
			px[i+0] = mix(c.B)
			px[i+1] = mix(c.G)
			px[i+2] = mix(c.R)
			px[i+3] = byte(255*circle/(ss*ss) + 0)
		}
	}
	return px
}

// makeIcon builds an HICON of the given size and colour.
func makeIcon(size int, c rgb) uintptr {
	pixels := renderIcon(size, c)

	bi := BITMAPINFOHEADER{
		BiSize:     uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
		BiWidth:    int32(size),
		BiHeight:   -int32(size), // top-down
		BiPlanes:   1,
		BiBitCount: 32,
	}
	var bits unsafe.Pointer
	hColor, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hColor == 0 || bits == nil {
		return 0
	}
	copy(unsafe.Slice((*byte)(bits), len(pixels)), pixels)

	stride := ((size + 15) / 16) * 2  // monochrome rows are WORD-aligned
	mask := make([]byte, stride*size) // all zero: alpha channel decides transparency
	hMask, _, _ := procCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, uintptr(unsafe.Pointer(&mask[0])))

	ii := ICONINFO{FIcon: 1, HbmMask: hMask, HbmColor: hColor}
	hIcon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))

	procDeleteObject.Call(hColor)
	procDeleteObject.Call(hMask)
	return hIcon
}

func trayIconSize() int {
	n, _, _ := procGetSystemMetrics.Call(SM_CXSMICON)
	if n < 16 {
		return 16
	}
	return int(n)
}
