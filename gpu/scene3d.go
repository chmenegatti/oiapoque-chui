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
	raw  [][2]float32 // reachRaw memorizado (0 = ainda não calculado)
	grid map[[2]int32][]int32
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
	t.near(vec3{}, 0, func(int) {}) // monta a grade agora, não no primeiro quadro
	return t
}

// Limite lateral do terreno. Na serra a estrada dá voltas de 180° e a faixa
// de relevo de um trecho (até 160 meias-pistas para cada lado) passaria por
// cima de outro trecho, ou dobraria sobre si mesma por dentro da curva. Cada
// ponto do terreno fica com o trecho de estrada mais próximo: o lado vai até
// a fronteira com o outro trecho (diagrama de Voronoi). Para pontos da mesma
// curva a fronteira dá o raio dela, que é onde o lado de dentro dobraria.
const (
	reachMax  = 160.0 // meias-pistas
	reachStep = 6     // amostragem da estrada na grade
	reachCell = 512.0 // lado da célula da grade, em unidades 3D
	reachKeep = 6     // janela do mínimo, para o limite não serrilhar
)

func cellOf(p vec3) [2]int32 {
	return [2]int32{int32(math.Floor(p.x / reachCell)), int32(math.Floor(p.z / reachCell))}
}

// near chama fn para cada amostra da estrada a até rad unidades de p.
func (t *track) near(p vec3, rad float64, fn func(j int)) {
	if t.grid == nil {
		t.grid = map[[2]int32][]int32{}
		for j := 0; j < len(t.pos); j += reachStep {
			c := cellOf(t.pos[j])
			t.grid[c] = append(t.grid[c], int32(j))
		}
	}
	lo, hi := cellOf(p.sub(vec3{rad, 0, rad})), cellOf(p.add(vec3{rad, 0, rad}))
	for cx := lo[0]; cx <= hi[0]; cx++ {
		for cz := lo[1]; cz <= hi[1]; cz++ {
			for _, j := range t.grid[[2]int32{cx, cz}] {
				fn(int(j))
			}
		}
	}
}

// reachRaw: até onde (em meias-pistas) vão o lado esquerdo e o direito do
// segmento n antes de encostar no terreno de outro trecho.
func (t *track) reachRaw(n int) [2]float64 {
	if t.raw == nil {
		t.raw = make([][2]float32, len(t.pos))
	}
	if v := t.raw[n]; v[0] != 0 {
		return [2]float64{float64(v[0]), float64(v[1])}
	}
	lim := [2]float64{reachMax * hw, reachMax * hw}
	_, r := dirs(t.head[n])
	// um trecho a mais de 2×alcance não limita nada
	t.near(t.pos[n], 2*reachMax*hw, func(j int) {
		p := t.pos[j].sub(t.pos[n])
		p.y = 0
		d2 := p.dot(p)
		if d2 < 1 {
			return
		}
		a := p.dot(r) // componente lateral; > 0 à direita
		side := 1
		if a < 0 {
			side, a = 0, -a
		}
		if a < 1e-6 {
			return
		}
		// o ponto a x da pista fica mais perto de j quando x > |p|²/(2a)
		lim[side] = min(lim[side], d2/(2*a))
	})
	out := [2]float64{lim[0] / hw, lim[1] / hw}
	t.raw[n] = [2]float32{float32(out[0]), float32(out[1])}
	return out
}

// reach: reachRaw suavizado (mínimo numa janela), nunca cortando a pista e
// o acostamento.
func (t *track) reach(n int, bank float64) (left, right float64) {
	left, right = reachMax, reachMax
	for j := max(0, n-reachKeep); j <= min(len(t.pos)-1, n+reachKeep); j++ {
		v := t.reachRaw(j)
		left, right = min(left, v[0]), min(right, v[1])
	}
	floor := bank + 0.3
	return max(left*0.97, floor), max(right*0.97, floor)
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

// mesh acumula vértices (posição, normal, cor: 9 floats) e índices, já
// relativos a origin. Cada face tem vértices próprios com a normal dela
// (sombreamento chapado, combina com o low-poly); o quad reaproveita 2
// vértices via índice. A normal pode sair de qualquer lado: o shader a vira
// para a câmera (iluminação dos dois lados).
type mesh struct {
	f      []float32
	idx    []uint32
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

func (m *mesh) reset(origin vec3) { m.f, m.idx, m.origin = m.f[:0], m.idx[:0], origin }

func (m *mesh) nv() uint32 { return uint32(len(m.f) / 9) }

func (m *mesh) vert(p, n vec3, k color3) {
	p = p.sub(m.origin)
	m.f = append(m.f, float32(p.x), float32(p.y), float32(p.z),
		float32(n.x), float32(n.y), float32(n.z), k[0], k[1], k[2])
}

func (m *mesh) tri(a, b, c vec3, k color3) {
	n := b.sub(a).cross(c.sub(a)).norm()
	i := m.nv()
	m.vert(a, n, k)
	m.vert(b, n, k)
	m.vert(c, n, k)
	m.idx = append(m.idx, i, i+1, i+2)
}

func (m *mesh) quad(a, b, c, d vec3, k color3) {
	n := b.sub(a).cross(c.sub(a))
	if n.dot(n) < 1e-12 { // triângulo abc degenerado: usa acd
		n = c.sub(a).cross(d.sub(a))
	}
	n = n.norm()
	i := m.nv()
	m.vert(a, n, k)
	m.vert(b, n, k)
	m.vert(c, n, k)
	m.vert(d, n, k)
	m.idx = append(m.idx, i, i+1, i+2, i, i+2, i+3)
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

// Distância de visão: 1 unidade 3D ≈ 18 cm, segmento = 2 unidades ≈ 35 cm,
// então 1,5 km ≈ 4300 segmentos à frente.
const (
	ahead3D  = 4300
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

// segment: chão, acostamento, relevo e pintura do segmento i.
func (t *track) segment(m *mesh, r *game.Road, i int, twoWay bool) {
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
	clampX := func(n int) [len(xs)]float64 {
		l, rr := t.reach(n, bank)
		c := xs
		for k, x := range c {
			if x < 0 {
				c[k] = max(x, -l)
			} else {
				c[k] = min(x, rr)
			}
		}
		return c
	}
	x1, x2 := clampX(i), clampX(i+1)
	pt := func(n int, rt vec3, x float64) vec3 {
		side := 0
		if x < 0 {
			side = 1
		}
		return t.pos[n].add(rt.mul(x * hw)).add(up3.mul(rise(ter, x, n, side)))
	}
	for k := range cols {
		if x1[k] == x1[k+1] && x2[k] == x2[k+1] {
			continue // faixa cortada pelo limite
		}
		m.quad(pt(i, r1, x1[k]), pt(i, r1, x1[k+1]), pt(i+1, r2, x2[k+1]), pt(i+1, r2, x2[k]), cols[k])
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
	case twoWay:
		line(-0.05, -0.02, yellow3D)
		line(0.02, 0.05, yellow3D)
	case alt == 0:
		line(-0.017, 0.017, paint3D)
	}
}

func shade(i int, x float64) float32 { // variação de cor por objeto
	return 0.85 + float32(hash3D(i, int(x*97))%30)/100
}

// propMesh: modelo low-poly de um tipo de objeto, montado com caixas, na
// origem e em tamanho unitário (w=1). Cada objeto da estrada é uma
// instância dele: posição, giro, escala (propInst) e variação de cor.
func propMesh(m *mesh, kind game.PropKind) {
	at, r, fwd := vec3{}, vec3{1, 0, 0}, vec3{0, 0, 1}
	const w, v = 1.0, float32(1)
	trunk := col(game.RGB{95, 65, 38})
	switch kind {
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

// propScale: escala da instância. Marco, radar e placa têm medida fixa; o
// tronco caído só estica na largura; o resto cresce por inteiro.
func propScale(p game.Prop) vec3 {
	w := p.W * hw
	switch p.Kind {
	case game.PropKm, game.PropRadar, game.PropSpeedSign:
		return vec3{1, 1, 1}
	case game.PropLog:
		return vec3{w, 1, 1}
	}
	return vec3{w, w, w}
}

// propKinds: tipos com modelo 3D, na ordem dos desenhos.
var propKinds = []game.PropKind{game.PropTree, game.PropAcai, game.PropBanana, game.PropPequi,
	game.PropCow, game.PropCitySign, game.PropGasStation, game.PropKm, game.PropRadar,
	game.PropSpeedSign, game.PropLog}

type dims3D struct{ len, h float64 }

var vehDims = map[*game.VehKind]dims3D{
	game.KindCar: {20, 6}, game.KindTruck: {46, 16}, game.KindBus: {52, 15}, game.KindBoat: {26, 5},
}

// vehicle: carro do tráfego. dir=-1 vem de frente (faróis pra câmera).
func (sc *scene3d) vehicle(c game.Car, night bool) {
	m := &sc.dyn
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
	m := &sc.dyn
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

// scene3d: o que muda a cada quadro (câmera, tráfego e o 147). Estrada,
// relevo e cenário ficam nos blocos estáticos (chunks.go).
type scene3d struct {
	trk *track
	dyn mesh
}

// camera devolve posição da câmera, direção do olhar e posição/direção do
// farol (relativas à câmera).
func (sc *scene3d) camera(g *game.Game) (cam, look, headPos, headDir vec3) {
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
	return
}

// dynamic monta tráfego e jogador, relativos à câmera.
func (sc *scene3d) dynamic(g *game.Game, cam vec3) {
	sc.dyn.reset(cam)
	base := int(g.Pos / game.SegLen)
	from, to := max(base-behind3D, 0), min(base+ahead3D, len(g.Road.Segs)-1)
	night := game.Daylight(g.Tod) < 0.6
	for _, c := range g.Cars {
		if s := int(c.Z / game.SegLen); s >= from && s < to {
			sc.vehicle(c, night)
		}
	}
	sc.player(g, night)
}

func hash3D(x, y int) uint32 {
	h := uint32(x)*73856093 ^ uint32(y)*19349663
	h ^= h >> 13
	return h * 0x5bd1e995
}
