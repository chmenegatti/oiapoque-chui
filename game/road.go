package game

import "math"

const (
	SegLen    = 200.0  // comprimento de um segmento (unidades de mundo)
	RumbleLen = 3      // segmentos por faixa de cor alternada
	RoadWidth = 2000.0 // meia-largura da pista
	CamHeight = 1000.0
	DrawDist  = 300 // segmentos desenhados
	fovDeg    = 100.0
)

var CamDepth = 1 / math.Tan(fovDeg/2*math.Pi/180)

const (
	curveEasy   = 2.0
	curveMedium = 4.0
	curveHard   = 6.0
	hillLow     = 20.0
	hillMedium  = 40.0
	hillHigh    = 60.0
)

type Segment struct {
	Index  int
	Z1, Z2 float64
	Y1, Y2 float64
	Curve  float64
	Fog    float64     // densidade extra de neblina (0..1)
	Rain   float64     // intensidade de chuva (0..1)
	Ter    *TerrainCfg // terreno (cores, aderência, vegetação)
	Props  []Prop      // cenário na beira da estrada
}

// PropKind diz o que é um objeto de cenário; cada frontend decide como desenhá-lo.
type PropKind uint8

const (
	PropTree PropKind = iota
	PropAcai
	PropBanana
	PropPequi
	PropCow
	PropLog
	PropKm
	PropCitySign
	PropSpeedSign
	PropRadar
	PropGasStation
)

// Prop é um objeto de cenário: x em meias-pistas (|x|>1 = fora do asfalto),
// w = largura no mundo em meias-pistas, hit = largura de colisão (0 = atravessável).
type Prop struct {
	Kind PropKind
	X, W float64
	hit  float64
	veg  bool // vegetação (pode ser removida perto de posto)
}

type Road struct {
	Name   string
	Segs   []Segment
	length float64
}

func (r *Road) lastY() float64 {
	if len(r.Segs) == 0 {
		return 0
	}
	return r.Segs[len(r.Segs)-1].Y2
}

func (r *Road) addSegment(curve, y float64) {
	n := len(r.Segs)
	r.Segs = append(r.Segs, Segment{
		Index: n,
		Z1:    float64(n) * SegLen, Z2: float64(n+1) * SegLen,
		Y1: r.lastY(), Y2: y,
		Curve: curve,
	})
	r.length = float64(len(r.Segs)) * SegLen
}

// addSection: entra na curva, mantém, sai. hill = variação de altura em segmentos.
func (r *Road) addSection(enter, hold, leave int, curve, hill float64) {
	startY := r.lastY()
	endY := startY + hill*SegLen
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

func (r *Road) Find(z float64) *Segment {
	i := int(math.Floor(z / SegLen))
	return &r.Segs[max(0, min(i, len(r.Segs)-1))]
}

func easeIn(a, b, p float64) float64    { return a + (b-a)*p*p }
func easeInOut(a, b, p float64) float64 { return a + (b-a)*(-math.Cos(p*math.Pi)/2+0.5) }

func Wrap(v, m float64) float64 {
	v = math.Mod(v, m)
	if v < 0 {
		v += m
	}
	return v
}
