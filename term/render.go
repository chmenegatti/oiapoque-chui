package term

import (
	"fmt"
	"math"

	"github.com/chmenegatti/oiapoque-chui/game"
	"github.com/gdamore/tcell/v2"
)

func tc(c game.RGB) tcell.Color {
	return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
}

// Frame é um buffer de pixels. Cada célula do terminal mostra 2 pixels
// verticais usando '▀' (cor de frente = pixel de cima, fundo = pixel de baixo).
type Frame struct {
	W, H int
	Pix  []game.RGB
}

func NewFrame(w, h int) *Frame { return &Frame{W: w, H: h, Pix: make([]game.RGB, w*h)} }

func (f *Frame) Set(x, y int, c game.RGB) {
	if x < 0 || y < 0 || x >= f.W || y >= f.H {
		return
	}
	f.Pix[y*f.W+x] = c
}

func (f *Frame) Get(x, y int) game.RGB { return f.Pix[y*f.W+x] }

func (f *Frame) HLine(x1, x2, y int, c game.RGB) {
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
func (f *Frame) Span(a, b float64, y int, c game.RGB) {
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
			st := tcell.StyleDefault.Foreground(tc(top[x])).Background(tc(bot[x]))
			s.SetContent(x, row, '▀', nil, st)
		}
	}
}

// Sprite é pixel art em texto: cada caractere indexa a paleta, '.' é transparente.
type Sprite struct {
	rows []string
	w, h int
	pal  map[byte]game.RGB
}

func newSprite(pal map[byte]game.RGB, rows ...string) *Sprite {
	for i, r := range rows {
		if len(r) != len(rows[0]) {
			panic(fmt.Sprintf("sprite: linha %d com largura %d, esperado %d", i, len(r), len(rows[0])))
		}
	}
	return &Sprite{rows: rows, w: len(rows[0]), h: len(rows), pal: pal}
}

// Sprite desenha sp escalado para largura w, centrado em cx, com a base em bottom.
// Linhas >= clip ficam ocultas (atrás de morro). body substitui a cor 'b'.
func (f *Frame) Sprite(sp *Sprite, cx, bottom, w, clip float64, body *game.RGB, shade func(byte, game.RGB) game.RGB) {
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
