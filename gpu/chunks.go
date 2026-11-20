package gpu

import (
	"math"
	"unsafe"

	"github.com/chmenegatti/oiapoque-chui/game"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// chunkSegs: segmentos por bloco. 64 segmentos ≈ 128 unidades (≈ 22 m): pequeno
// o bastante para o descarte por frustum valer, grande o bastante para não
// virar centenas de desenhos.
const chunkSegs = 64

// propInst: um objeto da estrada, relativo à origem do bloco.
type propInst struct {
	kind  game.PropKind
	at    vec3
	right vec3
	fwd   vec3
	scale vec3
	tint  float32
}

// chunk: estrada e relevo de chunkSegs segmentos num vertex/index buffer
// próprio, com coordenadas relativas a origin (float32 preciso mesmo a
// milhares de km do começo). Os objetos ficam numa lista de instâncias.
type chunk struct {
	idx        int
	origin     vec3
	lo, hi     vec3 // caixa envolvente, relativa a origin
	vbuf, ibuf *wgpu.Buffer
	vcap, icap uint64
	nidx       uint32
	props      []propInst
}

// chunkPool cria blocos sob demanda e reaproveita os buffers dos que saíram
// da distância de visão.
type chunkPool struct {
	dev    *wgpu.Device
	trk    *track
	road   *game.Road
	twoWay bool
	live   map[int]*chunk
	free   []*chunk
	m      mesh // rascunho reaproveitado na montagem
}

func newChunkPool(dev *wgpu.Device, trk *track, road *game.Road, twoWay bool) *chunkPool {
	return &chunkPool{dev: dev, trk: trk, road: road, twoWay: twoWay, live: map[int]*chunk{}}
}

// update garante os blocos que cobrem os segmentos [from, to) e devolve
// quantos foram montados neste quadro.
func (p *chunkPool) update(from, to int) (built int, err error) {
	first, last := from/chunkSegs, (to-1)/chunkSegs
	for i, c := range p.live {
		if i < first || i > last {
			delete(p.live, i)
			p.free = append(p.free, c)
		}
	}
	for i := first; i <= last; i++ {
		if p.live[i] != nil {
			continue
		}
		var c *chunk
		if n := len(p.free); n > 0 {
			c, p.free = p.free[n-1], p.free[:n-1]
		} else {
			c = &chunk{}
		}
		if err = p.build(c, i); err != nil {
			return built, err
		}
		p.live[i] = c
		built++
	}
	return built, nil
}

func (p *chunkPool) build(c *chunk, idx int) error {
	t := p.trk
	s0 := idx * chunkSegs
	s1 := min(s0+chunkSegs, len(p.road.Segs))
	c.idx, c.origin, c.props = idx, t.pos[s0], c.props[:0]
	m := &p.m
	m.reset(c.origin)
	for i := s0; i < s1; i++ {
		t.segment(m, p.road, i, p.twoWay)
		fwd, r := dirs(t.head[i])
		for _, pr := range p.road.Segs[i].Props {
			c.props = append(c.props, propInst{
				kind: pr.Kind, at: t.pos[i].add(r.mul(pr.X * hw)).sub(c.origin),
				right: r, fwd: fwd, scale: propScale(pr), tint: shade(i, pr.X),
			})
		}
	}
	// caixa envolvente: vértices da malha e a altura dos objetos
	c.lo = vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	c.hi = c.lo.mul(-1)
	grow := func(v vec3) {
		c.lo = vec3{min(c.lo.x, v.x), min(c.lo.y, v.y), min(c.lo.z, v.z)}
		c.hi = vec3{max(c.hi.x, v.x), max(c.hi.y, v.y), max(c.hi.z, v.z)}
	}
	for i := 0; i < len(m.f); i += 9 {
		grow(vec3{float64(m.f[i]), float64(m.f[i+1]), float64(m.f[i+2])})
	}
	for _, pi := range c.props {
		h := max(pi.scale.x, pi.scale.y) * 2.5 // nenhum modelo passa de 2,5×w de altura nem de largura
		grow(pi.at.add(vec3{h, max(h, 12), h}))
		grow(pi.at.sub(vec3{h, 1, h}))
	}

	vb, ib := uint64(len(m.f)*4), uint64(len(m.idx)*4)
	var err error
	if c.vbuf, c.vcap, err = ensureBuffer(p.dev, c.vbuf, c.vcap, vb, wgpu.BufferUsageVertex, "bloco:vértices"); err != nil {
		return err
	}
	if c.ibuf, c.icap, err = ensureBuffer(p.dev, c.ibuf, c.icap, ib, wgpu.BufferUsageIndex, "bloco:índices"); err != nil {
		return err
	}
	q := p.dev.Queue()
	q.WriteBuffer(c.vbuf, 0, floatBytes(m.f))
	q.WriteBuffer(c.ibuf, 0, uintBytes(m.idx))
	c.nidx = uint32(len(m.idx))
	return nil
}

func (p *chunkPool) release() {
	for _, c := range p.live {
		p.free = append(p.free, c)
	}
	for _, c := range p.free {
		c.vbuf.Release()
		c.ibuf.Release()
	}
	p.live, p.free = map[int]*chunk{}, nil
}

// ensureBuffer devolve buf se couber size; senão cria outro (com folga).
func ensureBuffer(dev *wgpu.Device, buf *wgpu.Buffer, capa, size uint64, usage gputypes.BufferUsage, label string) (*wgpu.Buffer, uint64, error) {
	size = max((size+3)&^3, 256)
	if buf != nil && size <= capa {
		return buf, capa, nil
	}
	if buf != nil {
		buf.Release()
	}
	capa = size + size/2
	capa = (capa + 3) &^ 3
	b, err := dev.CreateBuffer(&wgpu.BufferDescriptor{Label: label, Size: capa, Usage: usage | wgpu.BufferUsageCopyDst})
	return b, capa, err
}

func floatBytes(f []float32) []byte {
	if len(f) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&f[0])), len(f)*4)
}

func uintBytes(u []uint32) []byte {
	if len(u) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), len(u)*4)
}

// ---------- frustum ----------

// frustum: planos (a, b, c, d) com a·x+b·y+c·z+d ≥ 0 dentro, extraídos da
// matriz de projeção×visão (Gribb-Hartmann). Com profundidade invertida e
// infinita não há plano de fundo: a distância é cortada por bloco.
type frustum [5][4]float64

func makeFrustum(vp mat4) frustum {
	row := func(r int) [4]float64 {
		return [4]float64{float64(vp[r]), float64(vp[4+r]), float64(vp[8+r]), float64(vp[12+r])}
	}
	r0, r1, r2, r3 := row(0), row(1), row(2), row(3)
	var f frustum
	for k := range 4 {
		f[0][k] = r3[k] + r0[k] // esquerda
		f[1][k] = r3[k] - r0[k] // direita
		f[2][k] = r3[k] + r1[k] // baixo
		f[3][k] = r3[k] - r1[k] // cima
		f[4][k] = r3[k] - r2[k] // perto (profundidade ≤ 1)
	}
	return f
}

// visible: a caixa [lo, hi] (relativa à câmera) toca o frustum?
func (f *frustum) visible(lo, hi vec3) bool {
	for _, p := range f {
		x, y, z := lo.x, lo.y, lo.z // vértice mais "para dentro" do plano
		if p[0] > 0 {
			x = hi.x
		}
		if p[1] > 0 {
			y = hi.y
		}
		if p[2] > 0 {
			z = hi.z
		}
		if p[0]*x+p[1]*y+p[2]*z+p[3] < 0 {
			return false
		}
	}
	return true
}
