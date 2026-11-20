package gpu

import (
	"math"

	"github.com/chmenegatti/oiapoque-chui/game"
)

// Escala do 3D: 1 unidade 3D = 100 unidades do jogo ≈ 18 cm.
// Meia-pista (roadWidth) vira 20 unidades ≈ 3,5 m.
const (
	ws = 0.01
	hw = game.RoadWidth * ws // meia-pista em unidades 3D
)

// track transforma os segmentos do jogo numa curva 3D. A "curva" de cada
// segmento, que no pseudo-3D desloca a estrada na tela, vira mudança de
// direção: curva/segLen radianos por segmento. Assim a física do jogo
// (distância percorrida + deslocamento lateral) continua valendo igual.
type track struct {
	pos  []vec3
	head []float64
}

func buildTrack(r *game.Road) *track {
	n := len(r.Segs)
	t := &track{pos: make([]vec3, n+1), head: make([]float64, n+1)}
	var x, z, th float64
	for i, s := range r.Segs {
		t.pos[i] = vec3{x, s.Y1 * ws, z}
		t.head[i] = th
		th -= s.Curve / game.SegLen // curva positiva dobra à direita
		x += math.Sin(th) * game.SegLen * ws
		z += math.Cos(th) * game.SegLen * ws
	}
	t.pos[n] = vec3{x, r.Segs[n-1].Y2 * ws, z}
	t.head[n] = th
	return t
}

func dirs(th float64) (fwd, right vec3) {
	fwd = vec3{math.Sin(th), 0, math.Cos(th)}
	return fwd, vec3{-fwd.z, 0, fwd.x}
}

// frame: centro da pista, frente e direita na distância z (unidades do jogo).
func (t *track) frame(z float64) (c, fwd, right vec3) {
	f := z / game.SegLen
	i := max(0, min(int(f), len(t.pos)-2))
	k := game.Clamp(f-float64(i), 0, 1)
	c = lerp3(t.pos[i], t.pos[i+1], k)
	fwd, right = dirs(game.Lerp(t.head[i], t.head[i+1], k))
	return
}

// ---------- malha ----------

// mesh acumula triângulos com normal e cor (9 floats por vértice), já
// relativos à câmera (origin). Sem índice: cada face tem normal própria
// (sombreamento chapado, combina com o low-poly).
type mesh struct {
	f      []float32
	n      uint32
	origin vec3
}

type color3 [3]float32

func col(c game.RGB) color3 {
	return color3{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255}
}

// glow marca a cor como emissiva (lanterna, farol): o shader soma 2 aqui e
// desconta lá, e não aplica iluminação.
func glow(c game.RGB) color3 {
	k := col(c)
	return color3{k[0] + 2, k[1] + 2, k[2] + 2}
}

func (c color3) mul(k float32) color3 { return color3{c[0] * k, c[1] * k, c[2] * k} }

func (m *mesh) reset(origin vec3) { m.f, m.n, m.origin = m.f[:0], 0, origin }

func (m *mesh) tri(a, b, c vec3, k color3) {
	a, b, c = a.sub(m.origin), b.sub(m.origin), c.sub(m.origin)
	n := b.sub(a).cross(c.sub(a)).norm()
	if n.dot(a.add(b).add(c)) > 0 { // iluminação dos dois lados: normal virada pra câmera
		n = n.mul(-1)
	}
	for _, p := range [3]vec3{a, b, c} {
		m.f = append(m.f, float32(p.x), float32(p.y), float32(p.z),
			float32(n.x), float32(n.y), float32(n.z), k[0], k[1], k[2])
	}
	m.n += 3
}

func (m *mesh) quad(a, b, c, d vec3, k color3) {
	m.tri(a, b, c, k)
	m.tri(a, c, d, k)
}

// box: caixa com base centrada em base, eixos r (largura), u (altura) e f
// (comprimento); hx/hz são meias-medidas, h a altura. Sem a face de baixo.
func (m *mesh) box(base, r, u, f vec3, hx, h, hz float64, k color3) {
	p := func(sx, sy, sz float64) vec3 {
		return base.add(r.mul(sx * hx)).add(u.mul(sy * h)).add(f.mul(sz * hz))
	}
	a, b, c, d := p(-1, 0, -1), p(1, 0, -1), p(1, 0, 1), p(-1, 0, 1)
	e, g, i, j := p(-1, 1, -1), p(1, 1, -1), p(1, 1, 1), p(-1, 1, 1)
	m.quad(e, g, i, j, k)           // topo
	m.quad(a, b, g, e, k.mul(0.95)) // trás
	m.quad(d, c, i, j, k.mul(0.95)) // frente
	m.quad(a, d, j, e, k.mul(0.9))  // esquerda
	m.quad(b, c, i, g, k.mul(0.9))  // direita
}

// ---------- cena ----------

const (
	ahead3D  = 420 // segmentos desenhados à frente
	behind3D = 25
)

var (
	paint3D  = col(game.RGB{235, 235, 235})
	yellow3D = col(game.RGB{235, 190, 40})
)

// relevo fora da pista: sobe com a distância, mais alto na serra.
func rise(ter *game.TerrainCfg, x float64, i, side int) float64 {
	bank := ter.Bank
	if bank == 0 {
		bank = 1.2
	}
	ax := math.Abs(x)
	if ax <= bank+0.2 {
		return 0
	}
	t := (ax - bank) / (40 - bank)
	h := ter.Hills * 110 * (0.55 + 0.45*math.Sin(float64(i)*0.021+float64(side)*2.1))
	return h * math.Pow(t, 1.4)
}

func (sc *scene3d) segment(i int, g *game.Game) {
	r, t, m := g.Road, sc.trk, &sc.m
	seg := &r.Segs[i]
	ter := seg.Ter
	alt := (i / game.RumbleLen) % 2
	bank := ter.Bank
	if bank == 0 {
		bank = 1.2
	}
	xs := [...]float64{-160, -40, -9, -bank, -1, 1, bank, 9, 40, 160}
	far := col(game.Mix(ter.Grass[alt], game.RGB{90, 110, 120}, 0.35))
	cols := [...]color3{far.mul(0.85), far, col(ter.Grass[alt]), col(ter.Shoulder[alt]), col(ter.Road[alt]),
		col(ter.Shoulder[alt]), col(ter.Grass[alt]), far, far.mul(0.85)}
	_, r1 := dirs(t.head[i])
	_, r2 := dirs(t.head[i+1])
	pt := func(n int, rt vec3, x float64) vec3 {
		side := 0
		if x < 0 {
			side = 1
		}
		return t.pos[n].add(rt.mul(x * hw)).add(up3.mul(rise(ter, x, n, side)))
	}
	for k := range cols {
		m.quad(pt(i, r1, xs[k]), pt(i, r1, xs[k+1]), pt(i+1, r2, xs[k+1]), pt(i+1, r2, xs[k]), cols[k])
	}
	if !ter.Paint {
		return
	}
	line := func(x0, x1 float64, k color3) {
		lift := up3.mul(0.06)
		m.quad(t.pos[i].add(r1.mul(x0*hw)).add(lift), t.pos[i].add(r1.mul(x1*hw)).add(lift),
			t.pos[i+1].add(r2.mul(x1*hw)).add(lift), t.pos[i+1].add(r2.mul(x0*hw)).add(lift), k)
	}
	line(-1, -0.965, paint3D)
	line(0.965, 1, paint3D)
	switch {
	case g.Route.TwoWay():
		line(-0.05, -0.02, yellow3D)
		line(0.02, 0.05, yellow3D)
	case alt == 0:
		line(-0.017, 0.017, paint3D)
	}
}

func shade(i int, x float64) float32 { // variação de cor por objeto
	return 0.85 + float32(hash3D(i, int(x*97))%30)/100
}

// prop: modelos low-poly do cenário, montados com caixas.
func (sc *scene3d) prop(i int, p game.Prop) {
	m, t := &sc.m, sc.trk
	fwd, r := dirs(t.head[i])
	at := t.pos[i].add(r.mul(p.X * hw))
	w := p.W * hw
	v := shade(i, p.X)
	trunk := col(game.RGB{95, 65, 38})
	switch p.Kind {
	case game.PropTree:
		m.box(at, r, up3, fwd, w*0.06, w*0.7, w*0.06, trunk)
		m.box(at.add(up3.mul(w*0.5)), r, up3, fwd, w*0.5, w*0.75, w*0.5, col(game.RGB{40, 112, 46}).mul(v))
		m.box(at.add(up3.mul(w*1.15)), r, up3, fwd, w*0.32, w*0.35, w*0.32, col(game.RGB{52, 128, 56}).mul(v))
	case game.PropAcai:
		m.box(at, r, up3, fwd, w*0.04, w*2.3, w*0.04, trunk)
		m.box(at.add(up3.mul(w*2.2)), r, up3, fwd, w*0.55, w*0.18, w*0.55, col(game.RGB{95, 165, 55}).mul(v))
	case game.PropBanana:
		m.box(at, r, up3, fwd, w*0.07, w*0.8, w*0.07, col(game.RGB{110, 130, 60}))
		m.box(at.add(up3.mul(w*0.7)), r, up3, fwd, w*0.6, w*0.4, w*0.6, col(game.RGB{110, 170, 50}).mul(v))
	case game.PropPequi:
		m.box(at, r, up3, fwd, w*0.05, w*0.55, w*0.05, trunk)
		m.box(at.add(up3.mul(w*0.5)), r, up3, fwd, w*0.55, w*0.3, w*0.55, col(game.RGB{120, 135, 55}).mul(v))
	case game.PropCow:
		m.box(at, r, up3, fwd, w*0.06, w*0.45, w*0.06, col(game.RGB{40, 40, 40}))
		m.box(at.add(up3.mul(w*0.4)), r, up3, fwd, w*0.7, w*0.45, w*0.28, col(game.RGB{235, 235, 230}))
		m.box(at.add(up3.mul(w*0.6)).add(r.mul(w*0.8)), r, up3, fwd, w*0.18, w*0.3, w*0.15, col(game.RGB{40, 40, 40}))
	case game.PropCitySign:
		for _, s := range []float64{-0.35, 0.35} {
			m.box(at.add(r.mul(w*s)), r, up3, fwd, w*0.03, w*0.9, w*0.03, col(game.RGB{140, 140, 140}))
		}
		m.box(at.add(up3.mul(w*0.55)), r, up3, fwd, w*0.5, w*0.4, w*0.03, col(game.RGB{20, 110, 60}))
	case game.PropGasStation:
		m.box(at.add(up3.mul(w*0.55)), r, up3, fwd, w*0.5, w*0.08, w*0.4, col(game.RGB{200, 30, 30}))
		for _, s := range []float64{-0.4, 0, 0.4} {
			m.box(at.add(r.mul(w*s)), r, up3, fwd, w*0.03, w*0.55, w*0.03, col(game.RGB{220, 220, 220}))
		}
		for _, s := range []float64{-0.2, 0.2} {
			m.box(at.add(r.mul(w*s)), r, up3, fwd, w*0.05, w*0.18, w*0.04, col(game.RGB{240, 180, 30}))
		}
	case game.PropKm:
		m.box(at, r, up3, fwd, 0.4, 2.4, 0.15, col(game.RGB{240, 240, 240}))
	case game.PropRadar:
		m.box(at, r, up3, fwd, 0.25, 9, 0.25, col(game.RGB{140, 140, 140}))
		m.box(at.add(up3.mul(9)), r, up3, fwd, 1.2, 1.6, 1.2, col(game.RGB{230, 230, 230}))
	case game.PropSpeedSign:
		m.box(at, r, up3, fwd, 0.2, 8, 0.2, col(game.RGB{140, 140, 140}))
		m.box(at.add(up3.mul(6.5)), r, up3, fwd, 1.6, 3.2, 0.1, col(game.RGB{240, 240, 240}))
		m.box(at.add(up3.mul(6.5)).add(fwd.mul(-0.12)), r, up3, fwd, 1.8, 3.4, 0.05, col(game.RGB{200, 30, 30}))
	case game.PropLog:
		m.box(at.add(up3.mul(-0.6)), r, up3, fwd, w*0.5, 1.2, 0.9, col(game.RGB{100, 70, 40}).mul(v))
	}
}

type dims3D struct{ len, h float64 }

var vehDims = map[*game.VehKind]dims3D{
	game.KindCar: {20, 6}, game.KindTruck: {46, 16}, game.KindBus: {52, 15}, game.KindBoat: {26, 5},
}

// vehicle: carro do tráfego. dir=-1 vem de frente (faróis pra câmera).
func (sc *scene3d) vehicle(c game.Car, night bool) {
	m := &sc.m
	center, fwd, r := sc.trk.frame(c.Z)
	pos := center.add(r.mul(c.X * hw))
	d := vehDims[c.Kind]
	hx, hz := c.Kind.W*hw/2, d.len/2
	body := col(c.Color)
	lift := 1.3
	if c.Kind == game.KindBoat {
		lift = -1
	}
	m.box(pos.add(up3.mul(lift)), r, up3, fwd, hx, d.h, hz, body)
	switch c.Kind {
	case game.KindCar:
		m.box(pos.add(up3.mul(lift+d.h)).add(fwd.mul(-1)), r, up3, fwd, hx*0.85, d.h*0.6, hz*0.5, col(game.RGB{70, 95, 115}))
	case game.KindTruck:
		m.box(pos.add(up3.mul(lift)).add(fwd.mul(c.Dir*(hz+5))), r, up3, fwd, hx*0.95, d.h*0.6, 5, body.mul(0.7))
	case game.KindBoat:
		m.box(pos.add(up3.mul(lift+d.h)), r, up3, fwd, hx*0.6, d.h, hz*0.4, col(game.RGB{235, 235, 235}))
	}
	if c.Kind != game.KindBoat {
		for _, sx := range []float64{-1, 1} {
			for _, sz := range []float64{-0.75, 0.75} {
				m.box(pos.add(r.mul(sx*hx*0.9)).add(fwd.mul(sz*hz)), r, up3, fwd, 1, 2.6, 1.6, col(game.RGB{20, 20, 20}))
			}
		}
	}
	// lanterna atrás, farol na frente (frente = sentido de deslocamento)
	tail, head := col(game.RGB{150, 20, 20}), col(game.RGB{200, 200, 180})
	if night {
		tail, head = glow(game.RGB{255, 50, 30}), glow(game.RGB{255, 250, 220})
	}
	for _, sx := range []float64{-0.7, 0.7} {
		m.box(pos.add(r.mul(sx*hx)).add(fwd.mul(-c.Dir*(hz+0.05))).add(up3.mul(lift+2)), r, up3, fwd, 1.1, 1, 0.1, tail)
		m.box(pos.add(r.mul(sx*hx)).add(fwd.mul(c.Dir*(hz+0.05))).add(up3.mul(lift+2)), r, up3, fwd, 1.1, 1, 0.1, head)
	}
}

// player: o 147 (ou a balsa com o 147 em cima).
func (sc *scene3d) player(g *game.Game, night bool) {
	m := &sc.m
	center, fwd, r := sc.trk.frame(g.Pos + game.PlayerZ)
	pos := center.add(r.mul(g.PlayerX * hw))
	if g.Bmp > 0 || (math.Abs(g.PlayerX) > 1 && g.Speed > 0) {
		pos = pos.add(up3.mul(float64(g.Rng.Intn(3)) * 0.15))
	}
	yellow := col(game.RGB{230, 180, 40})
	lift := 1.2
	if g.Route.Boat() {
		m.box(pos.add(up3.mul(-1.5)), r, up3, fwd, 10, 4, 24, col(game.RGB{40, 40, 45}))
		m.box(pos.add(up3.mul(2.5)), r, up3, fwd, 10.2, 0.3, 24.2, col(game.RGB{150, 150, 150}))
		lift = 4
	}
	m.box(pos.add(up3.mul(lift)), r, up3, fwd, 4.5, 3.4, 9, yellow)                                    // carroceria
	m.box(pos.add(up3.mul(lift+3.4)).add(fwd.mul(-1.5)), r, up3, fwd, 4.1, 2.9, 5.5, yellow.mul(0.97)) // cabine
	m.box(pos.add(up3.mul(lift+3.6)).add(fwd.mul(-7.05)), r, up3, fwd, 3.6, 2.3, 0.05, col(game.RGB{70, 95, 115}))
	m.box(pos.add(up3.mul(lift+0.4)).add(fwd.mul(-9.1)), r, up3, fwd, 4.6, 0.7, 0.2, col(game.RGB{170, 170, 170}))
	for _, sx := range []float64{-1, 1} {
		for _, sz := range []float64{-6, 6} {
			m.box(pos.add(r.mul(sx*4)).add(fwd.mul(sz)).add(up3.mul(lift-1.2)), r, up3, fwd, 0.9, 2.4, 1.5, col(game.RGB{20, 20, 20}))
		}
	}
	tail := col(game.RGB{170, 25, 25})
	switch {
	case g.Braking:
		tail = glow(game.RGB{255, 60, 40})
	case night:
		tail = glow(game.RGB{200, 40, 30})
	}
	for _, sx := range []float64{-3.3, 3.3} {
		m.box(pos.add(r.mul(sx)).add(fwd.mul(-9.06)).add(up3.mul(lift+1.6)), r, up3, fwd, 1, 1.1, 0.06, tail)
	}
}

type scene3d struct {
	trk *track
	m   mesh
}

// build monta a cena do quadro. Devolve câmera, direção e posição/direção do farol.
func (sc *scene3d) build(g *game.Game) (cam, look, headPos, headDir vec3) {
	camZ := g.Pos + game.PlayerZ - 3600
	cc, _, cr := sc.trk.frame(camZ)
	cam = cc.add(cr.mul(g.PlayerX * hw * 0.9)).add(up3.mul(13))
	if g.Bmp > 0 {
		cam = cam.add(vec3{g.Rng.Float64() - 0.5, g.Rng.Float64() - 0.5, 0}.mul(0.6))
	}
	tc, _, tr := sc.trk.frame(g.Pos + game.PlayerZ + 2500)
	look = tc.add(tr.mul(g.PlayerX * hw * 0.6)).add(up3.mul(4)).sub(cam)

	pc, pf, pr := sc.trk.frame(g.Pos + game.PlayerZ + 900)
	headPos = pc.add(pr.mul(g.PlayerX * hw)).add(up3.mul(3)).sub(cam)
	headDir = pf.add(up3.mul(-0.07)).norm()

	sc.m.reset(cam)
	r := g.Road
	base := int(g.Pos / game.SegLen)
	from, to := max(base-behind3D, 0), min(base+ahead3D, len(r.Segs)-1)
	for i := from; i < to; i++ {
		sc.segment(i, g)
	}
	for i := from; i < to; i++ {
		for _, p := range r.Segs[i].Props {
			sc.prop(i, p)
		}
	}
	night := game.Daylight(g.Tod) < 0.6
	for _, c := range g.Cars {
		if s := int(c.Z / game.SegLen); s >= from && s < to {
			sc.vehicle(c, night)
		}
	}
	sc.player(g, night)
	return
}

func hash3D(x, y int) uint32 {
	h := uint32(x)*73856093 ^ uint32(y)*19349663
	h ^= h >> 13
	return h * 0x5bd1e995
}
