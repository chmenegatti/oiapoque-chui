package main

import "math"

const (
	segLen    = 200.0  // comprimento de um segmento (unidades de mundo)
	rumbleLen = 3      // segmentos por faixa de cor alternada
	roadWidth = 2000.0 // meia-largura da pista
	camHeight = 1000.0
	drawDist  = 300 // segmentos desenhados
	fovDeg    = 100.0
)

var camDepth = 1 / math.Tan(fovDeg/2*math.Pi/180)

const (
	curveEasy   = 2.0
	curveMedium = 4.0
	curveHard   = 6.0
	hillLow     = 20.0
	hillMedium  = 40.0
	hillHigh    = 60.0
)

type Segment struct {
	index  int
	z1, z2 float64
	y1, y2 float64
	curve  float64
	fog    float64     // densidade extra de neblina (0..1)
	rain   float64     // intensidade de chuva (0..1)
	ter    *terrainCfg // terreno (cores, aderência, vegetação)
	props  []Prop      // cenário na beira da estrada
}

// Prop é um objeto de cenário: x em meias-pistas (|x|>1 = fora do asfalto),
// w = largura no mundo em meias-pistas, hit = largura de colisão (0 = atravessável).
type Prop struct {
	spr  *Sprite
	x, w float64
	hit  float64
	veg  bool // vegetação (pode ser removida perto de posto)
}

type Road struct {
	Name   string
	segs   []Segment
	length float64
}

func (r *Road) lastY() float64 {
	if len(r.segs) == 0 {
		return 0
	}
	return r.segs[len(r.segs)-1].y2
}

func (r *Road) addSegment(curve, y float64) {
	n := len(r.segs)
	r.segs = append(r.segs, Segment{
		index: n,
		z1:    float64(n) * segLen, z2: float64(n+1) * segLen,
		y1: r.lastY(), y2: y,
		curve: curve,
	})
	r.length = float64(len(r.segs)) * segLen
}

// addSection: entra na curva, mantém, sai. hill = variação de altura em segmentos.
func (r *Road) addSection(enter, hold, leave int, curve, hill float64) {
	startY := r.lastY()
	endY := startY + hill*segLen
	total := float64(enter + hold + leave)
	i := 0
	y := func() float64 { i++; return easeInOut(startY, endY, float64(i)/total) }
	for n := 0; n < enter; n++ {
		r.addSegment(easeIn(0, curve, float64(n)/float64(enter)), y())
	}
	for n := 0; n < hold; n++ {
		r.addSegment(curve, y())
	}
	for n := 0; n < leave; n++ {
		r.addSegment(easeInOut(curve, 0, float64(n)/float64(leave)), y())
	}
}

func (r *Road) addStraight(n int) { r.addSection(n/3, n/3, n/3, 0, 0) }

func (r *Road) find(z float64) *Segment {
	i := int(math.Floor(z / segLen))
	return &r.segs[max(0, min(i, len(r.segs)-1))]
}

func easeIn(a, b, p float64) float64    { return a + (b-a)*p*p }
func easeInOut(a, b, p float64) float64 { return a + (b-a)*(-math.Cos(p*math.Pi)/2+0.5) }

func wrap(v, m float64) float64 {
	v = math.Mod(v, m)
	if v < 0 {
		v += m
	}
	return v
}
