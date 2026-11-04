package main

import (
	"fmt"
	"math"

	"github.com/gdamore/tcell/v2"
)

// RGB é uma cor truecolor.
type RGB struct{ R, G, B uint8 }

func (c RGB) tc() tcell.Color {
	return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func mix(a, b RGB, t float64) RGB {
	t = clamp(t, 0, 1)
	return RGB{
		uint8(lerp(float64(a.R), float64(b.R), t)),
		uint8(lerp(float64(a.G), float64(b.G), t)),
		uint8(lerp(float64(a.B), float64(b.B), t)),
	}
}

func scale(c RGB, k float64) RGB {
	return RGB{
		uint8(clamp(float64(c.R)*k, 0, 255)),
		uint8(clamp(float64(c.G)*k, 0, 255)),
		uint8(clamp(float64(c.B)*k, 0, 255)),
	}
}

// Frame é um buffer de pixels. Cada célula do terminal mostra 2 pixels
// verticais usando '▀' (cor de frente = pixel de cima, fundo = pixel de baixo).
type Frame struct {
	W, H int
	Pix  []RGB
}

func NewFrame(w, h int) *Frame { return &Frame{W: w, H: h, Pix: make([]RGB, w*h)} }

func (f *Frame) Set(x, y int, c RGB) {
	if x < 0 || y < 0 || x >= f.W || y >= f.H {
		return
	}
	f.Pix[y*f.W+x] = c
}

func (f *Frame) Get(x, y int) RGB { return f.Pix[y*f.W+x] }

func (f *Frame) HLine(x1, x2, y int, c RGB) {
	if y < 0 || y >= f.H {
		return
	}
	x1, x2 = max(x1, 0), min(x2, f.W-1)
	row := f.Pix[y*f.W:]
	for x := x1; x <= x2; x++ {
		row[x] = c
	}
}

// Span pinta [a,b) em coordenadas float, garantindo ao menos 1 pixel.
func (f *Frame) Span(a, b float64, y int, c RGB) {
	x1, x2 := int(math.Floor(a)), int(math.Ceil(b))-1
	if x2 < x1 {
		x2 = x1
	}
	f.HLine(x1, x2, y, c)
}

func (f *Frame) Flush(s tcell.Screen) {
	for row := 0; row < f.H/2; row++ {
		top, bot := f.Pix[2*row*f.W:], f.Pix[(2*row+1)*f.W:]
		for x := 0; x < f.W; x++ {
			st := tcell.StyleDefault.Foreground(top[x].tc()).Background(bot[x].tc())
			s.SetContent(x, row, '▀', nil, st)
		}
	}
}

// Sprite é pixel art em texto: cada caractere indexa a paleta, '.' é transparente.
type Sprite struct {
	rows []string
	w, h int
	pal  map[byte]RGB
}

func newSprite(pal map[byte]RGB, rows ...string) *Sprite {
	for i, r := range rows {
		if len(r) != len(rows[0]) {
			panic(fmt.Sprintf("sprite: linha %d com largura %d, esperado %d", i, len(r), len(rows[0])))
		}
	}
	return &Sprite{rows: rows, w: len(rows[0]), h: len(rows), pal: pal}
}

// Sprite desenha sp escalado para largura w, centrado em cx, com a base em bottom.
// Linhas >= clip ficam ocultas (atrás de morro). body substitui a cor 'b'.
func (f *Frame) Sprite(sp *Sprite, cx, bottom, w, clip float64, body *RGB, shade func(byte, RGB) RGB) {
	if w < 1.5 {
		return
	}
	h := w * float64(sp.h) / float64(sp.w)
	x0, y0 := cx-w/2, bottom-h
	for py := int(math.Floor(y0)); py < int(math.Ceil(bottom)); py++ {
		if py < 0 || py >= f.H || float64(py) >= clip {
			continue
		}
		sy := int((float64(py) + 0.5 - y0) / h * float64(sp.h))
		if sy < 0 || sy >= sp.h {
			continue
		}
		for px := int(math.Floor(x0)); px < int(math.Ceil(x0+w)); px++ {
			if px < 0 || px >= f.W {
				continue
			}
			sx := int((float64(px) + 0.5 - x0) / w * float64(sp.w))
			if sx < 0 || sx >= sp.w {
				continue
			}
			ch := sp.rows[sy][sx]
			if ch == '.' {
				continue
			}
			c := sp.pal[ch]
			if ch == 'b' && body != nil {
				c = *body
			}
			f.Pix[py*f.W+px] = shade(ch, c)
		}
	}
}
