package gpu

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/chmenegatti/oiapoque-chui/game"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gogpu/input"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

const shader3D = `
struct U {
  viewProj: mat4x4<f32>,
  sunDir: vec4<f32>,   // xyz direção do sol, w luz ambiente
  sunCol: vec4<f32>,   // rgb cor do sol, w quanto é dia (0..1)
  fogCol: vec4<f32>,   // rgb cor da neblina, w densidade
  zen: vec4<f32>,
  hor: vec4<f32>,
  headPos: vec4<f32>,  // xyz farol, w intensidade
  headDir: vec4<f32>,  // xyz direção, w alcance
  camR: vec4<f32>,     // xyz direita da câmera, w tan(fov/2)*aspecto
  camU: vec4<f32>,     // xyz cima da câmera, w tan(fov/2)
  camF: vec4<f32>,     // xyz frente da câmera
};
@group(0) @binding(0) var<uniform> u: U;

struct VOut {
  @builtin(position) pos: vec4<f32>,
  @location(0) col: vec3<f32>,
  @location(1) n: vec3<f32>,
  @location(2) wp: vec3<f32>,
};

// Instância: deslocamento até a câmera, eixos direita/frente no plano
// (y é sempre "cima"), escala por eixo e variação de cor. Os blocos de
// estrada e a malha do tráfego são uma instância identidade.
struct Inst {
  @location(3) off: vec3<f32>,
  @location(4) rf: vec4<f32>,  // xy = direita.xz, zw = frente.xz
  @location(5) scale: vec3<f32>,
  @location(6) tint: f32,
};

@vertex
fn vs_world(@location(0) p: vec3<f32>, @location(1) n: vec3<f32>, @location(2) c: vec3<f32>, it: Inst) -> VOut {
  let r = vec3<f32>(it.rf.x, 0.0, it.rf.y);
  let f = vec3<f32>(it.rf.z, 0.0, it.rf.w);
  let lp = p * it.scale;
  let wp = it.off + r * lp.x + vec3<f32>(0.0, lp.y, 0.0) + f * lp.z;
  var o: VOut;
  o.pos = u.viewProj * vec4<f32>(wp, 1.0);
  o.col = select(c * it.tint, c, c.r > 1.5);
  o.n = r * n.x + vec3<f32>(0.0, n.y, 0.0) + f * n.z;
  o.wp = wp;
  return o;
}

fn fog(c: vec3<f32>, wp: vec3<f32>) -> vec3<f32> {
  let d = length(wp) * u.fogCol.w;
  return mix(c, u.fogCol.rgb, clamp(1.0 - exp(-d * d), 0.0, 1.0));
}

@fragment
fn fs_world(i: VOut) -> @location(0) vec4<f32> {
  if (i.col.r > 1.5) { // emissiva: lanterna, farol
    return vec4<f32>(fog(i.col - vec3<f32>(2.0), i.wp * 0.6), 1.0);
  }
  var n = normalize(i.n);
  if (dot(n, i.wp) > 0.0) { n = -n; } // iluminação dos dois lados: normal virada pra câmera
  var light = vec3<f32>(u.sunDir.w) + u.sunCol.rgb * max(dot(n, u.sunDir.xyz), 0.0);
  let toP = i.wp - u.headPos.xyz;
  let dist = length(toP);
  let cone = smoothstep(0.80, 0.96, dot(toP / max(dist, 0.001), u.headDir.xyz));
  let fall = clamp(1.0 - dist / u.headDir.w, 0.0, 1.0);
  light += vec3<f32>(1.0, 0.94, 0.8) * (u.headPos.w * cone * fall * fall);
  return vec4<f32>(fog(i.col * light, i.wp), 1.0);
}

struct SOut {
  @builtin(position) pos: vec4<f32>,
  @location(0) ndc: vec2<f32>,
};

@vertex
fn vs_sky(@builtin(vertex_index) vi: u32) -> SOut {
  let uv = vec2<f32>(f32((vi << 1u) & 2u), f32(vi & 2u));
  let ndc = uv * 2.0 - vec2<f32>(1.0, 1.0);
  var o: SOut;
  o.pos = vec4<f32>(ndc, 0.0, 1.0);
  o.ndc = ndc;
  return o;
}

fn hash3(p: vec3<f32>) -> f32 {
  return fract(sin(dot(p, vec3<f32>(12.9898, 78.233, 37.719))) * 43758.5453);
}

@fragment
fn fs_sky(i: SOut) -> @location(0) vec4<f32> {
  let dir = normalize(u.camF.xyz + i.ndc.x * u.camR.w * u.camR.xyz + i.ndc.y * u.camU.w * u.camU.xyz);
  let t = clamp(dir.y * 2.5, 0.0, 1.0);
  var c = mix(u.hor.rgb, u.zen.rgb, sqrt(t));
  let s = max(dot(dir, u.sunDir.xyz), 0.0);
  let day = u.sunCol.w;
  c += vec3<f32>(1.0, 0.85, 0.6) * (pow(s, 600.0) * 3.0 + pow(s, 12.0) * 0.25) * day;
  let star = step(0.9975, hash3(floor(dir * 300.0)));
  c += vec3<f32>(star * (1.0 - day) * t);
  return vec4<f32>(c, 1.0);
}
`

const (
	vertexBytes  = 9 * 4
	instFloats   = 11
	instBytes    = instFloats * 4
	uniformBytes = 64 + 10*16
	// profundidade invertida (perto = 1, infinito = 0) em float: precisão
	// boa de 0,8 unidade até o horizonte, sem z-fighting na pintura.
	depthFormat = gputypes.TextureFormatDepth32Float
	fovY        = 58 * math.Pi / 180
)

type renderer3d struct {
	dev        *wgpu.Device
	world, sky *wgpu.RenderPipeline
	layout     *wgpu.PipelineLayout
	bgl        *wgpu.BindGroupLayout
	bg         *wgpu.BindGroup
	ubuf       *wgpu.Buffer
	depth      *wgpu.Texture
	depthView  *wgpu.TextureView
	dw, dh     int
	scene      scene3d
	ubytes     []byte
	format     gputypes.TextureFormat

	chunks *chunkPool

	// tráfego e 147, remontados a cada quadro
	dynV, dynI       *wgpu.Buffer
	dynVCap, dynICap uint64

	// modelos unitários dos objetos num buffer só
	propV, propI *wgpu.Buffer
	propRange    map[game.PropKind][2]uint32 // primeiro índice, quantidade

	inst    []float32 // instâncias do quadro
	instBuf *wgpu.Buffer
	instCap uint64
	byKind  map[game.PropKind][]float32

	vis    []visChunk
	kindAt []kindSpan

	stats frameStats
}

// kindSpan: instâncias de um tipo de objeto, contíguas no buffer.
type kindSpan struct {
	kind     game.PropKind
	first, n int
}

// frameStats: contadores do último quadro (para o título e o -bench).
type frameStats struct {
	draws, chunks, visible, built, props, tris int
}

func newRenderer3D(dev *wgpu.Device, format gputypes.TextureFormat, trk *track, g *game.Game) (*renderer3d, error) {
	r := &renderer3d{dev: dev, format: format, scene: scene3d{trk: trk}, ubytes: make([]byte, uniformBytes),
		chunks: newChunkPool(dev, trk, g.Road, g.Route.TwoWay()), byKind: map[game.PropKind][]float32{}}
	var err error
	if r.ubuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Label: "uniforms", Size: 256,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst}); err != nil {
		return nil, err
	}
	if err = r.buildProps(); err != nil {
		return nil, err
	}
	mod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: shader3D})
	if err != nil {
		return nil, err
	}
	defer mod.Release()

	if r.bgl, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Entries: []gputypes.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
			Buffer:     &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, MinBindingSize: uniformBytes},
		}},
	}); err != nil {
		return nil, err
	}
	if r.layout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{r.bgl}}); err != nil {
		return nil, err
	}
	if r.bg, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: r.bgl,
		Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: r.ubuf, Size: uniformBytes}}}); err != nil {
		return nil, err
	}
	target := []gputypes.ColorTargetState{{Format: format, WriteMask: gputypes.ColorWriteMaskAll}}

	if r.world, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Layout: r.layout,
		Vertex: wgpu.VertexState{Module: mod, EntryPoint: "vs_world", Buffers: []gputypes.VertexBufferLayout{{
			ArrayStride: vertexBytes,
			StepMode:    gputypes.VertexStepModeVertex,
			Attributes: []gputypes.VertexAttribute{
				{Format: gputypes.VertexFormatFloat32x3, Offset: 0, ShaderLocation: 0},
				{Format: gputypes.VertexFormatFloat32x3, Offset: 12, ShaderLocation: 1},
				{Format: gputypes.VertexFormatFloat32x3, Offset: 24, ShaderLocation: 2},
			},
		}, {
			ArrayStride: instBytes,
			StepMode:    gputypes.VertexStepModeInstance,
			Attributes: []gputypes.VertexAttribute{
				{Format: gputypes.VertexFormatFloat32x3, Offset: 0, ShaderLocation: 3},
				{Format: gputypes.VertexFormatFloat32x4, Offset: 12, ShaderLocation: 4},
				{Format: gputypes.VertexFormatFloat32x3, Offset: 28, ShaderLocation: 5},
				{Format: gputypes.VertexFormatFloat32, Offset: 40, ShaderLocation: 6},
			},
		}}},
		Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		DepthStencil: &wgpu.DepthStencilState{Format: depthFormat, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionGreater},
		Fragment:     &wgpu.FragmentState{Module: mod, EntryPoint: "fs_world", Targets: target},
	}); err != nil {
		return nil, fmt.Errorf("pipeline do mundo: %w", err)
	}
	if r.sky, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Layout:       r.layout,
		Vertex:       wgpu.VertexState{Module: mod, EntryPoint: "vs_sky"},
		Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		DepthStencil: &wgpu.DepthStencilState{Format: depthFormat, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionAlways},
		Fragment:     &wgpu.FragmentState{Module: mod, EntryPoint: "fs_sky", Targets: target},
	}); err != nil {
		return nil, fmt.Errorf("pipeline do céu: %w", err)
	}
	return r, nil
}

// buildProps monta os modelos unitários de todos os tipos de objeto num
// vertex/index buffer só; cada tipo vira um intervalo de índices.
func (r *renderer3d) buildProps() error {
	var m mesh
	r.propRange = map[game.PropKind][2]uint32{}
	for _, k := range propKinds {
		first := uint32(len(m.idx))
		propMesh(&m, k)
		r.propRange[k] = [2]uint32{first, uint32(len(m.idx)) - first}
	}
	var err error
	if r.propV, _, err = ensureBuffer(r.dev, nil, 0, uint64(len(m.f)*4), wgpu.BufferUsageVertex, "objetos:vértices"); err != nil {
		return err
	}
	if r.propI, _, err = ensureBuffer(r.dev, nil, 0, uint64(len(m.idx)*4), wgpu.BufferUsageIndex, "objetos:índices"); err != nil {
		return err
	}
	q := r.dev.Queue()
	q.WriteBuffer(r.propV, 0, floatBytes(m.f))
	q.WriteBuffer(r.propI, 0, uintBytes(m.idx))
	return nil
}

// addInst acrescenta uma instância em dst.
func addInst(dst []float32, off, right, fwd, scale vec3, tint float32) []float32 {
	return append(dst, float32(off.x), float32(off.y), float32(off.z),
		float32(right.x), float32(right.z), float32(fwd.x), float32(fwd.z),
		float32(scale.x), float32(scale.y), float32(scale.z), tint)
}

var (
	idRight = vec3{1, 0, 0}
	idFwd   = vec3{0, 0, 1}
	idScale = vec3{1, 1, 1}
)

func (r *renderer3d) ensureDepth(w, h int) error {
	if r.depth != nil && w == r.dw && h == r.dh {
		return nil
	}
	if r.depthView != nil {
		r.depthView.Release()
		r.depth.Release()
	}
	var err error
	if r.depth, err = r.dev.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "depth",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        depthFormat,
		Usage:         gputypes.TextureUsageRenderAttachment,
	}); err != nil {
		return err
	}
	r.depthView, err = r.dev.CreateTextureView(r.depth, nil)
	r.dw, r.dh = w, h
	return err
}

func putVec4(b []byte, off int, x, y, z, w float64) int {
	for i, v := range [4]float64{x, y, z, w} {
		binary.LittleEndian.PutUint32(b[off+i*4:], math.Float32bits(float32(v)))
	}
	return off + 16
}

func rgbf(c game.RGB) (float64, float64, float64) {
	return float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255
}

func (r *renderer3d) frame(sv *wgpu.TextureView, w, h int, g *game.Game) error {
	if w <= 0 || h <= 0 {
		return nil
	}
	if err := r.ensureDepth(w, h); err != nil {
		return err
	}
	sc := &r.scene
	cam, look, headPos, headDir := sc.camera(g)
	sc.dynamic(g, cam)

	// câmera
	aspect := float64(w) / float64(h)
	view, s, u, f := lookDir(look)
	vp := perspectiveRevInf(fovY, aspect, 0.8).mul(view)
	tanY := math.Tan(fovY / 2)

	// luz e céu
	day := game.Daylight(g.Tod) * (1 - 0.35*g.RainD)
	zen, hor := game.SkyColors(g.Tod)
	gray := game.RGB{110, 115, 125}
	zen = game.Mix(zen, game.Scale(gray, day), g.RainD*0.7)
	hor = game.Mix(hor, game.Scale(gray, day+0.2), g.RainD*0.7)
	a := (g.Tod - 0.25) * 2 * math.Pi
	sun := vec3{math.Cos(a) * 0.6, math.Sin(a), 0.45}.norm()
	sunC := game.Mix(game.RGB{255, 140, 60}, game.RGB{255, 245, 225}, game.Clamp(sun.y*4, 0, 1))
	sr, sg, sb := rgbf(sunC)
	k := day * game.Clamp(sun.y*5+0.3, 0, 1)
	hr, hg, hb := rgbf(hor)
	zr, zg, zb := rgbf(zen)
	headI := (1 - day) * 1.6

	b := r.ubytes
	for i, v := range vp {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	o := 64
	o = putVec4(b, o, sun.x, sun.y, sun.z, 0.16+0.3*day)
	o = putVec4(b, o, sr*k, sg*k, sb*k, day)
	o = putVec4(b, o, hr, hg, hb, math.Sqrt(g.FogD)/7000)
	o = putVec4(b, o, zr, zg, zb, 0)
	o = putVec4(b, o, hr, hg, hb, 0)
	o = putVec4(b, o, headPos.x, headPos.y, headPos.z, headI)
	o = putVec4(b, o, headDir.x, headDir.y, headDir.z, 260*g.Veh.Headlight/40)
	o = putVec4(b, o, s.x, s.y, s.z, tanY*aspect)
	o = putVec4(b, o, u.x, u.y, u.z, tanY)
	putVec4(b, o, f.x, f.y, f.z, 0)

	q := r.dev.Queue()
	q.WriteBuffer(r.ubuf, 0, b)
	if err := r.prepare(g, cam, makeFrustum(vp)); err != nil {
		return err
	}

	enc, err := r.dev.CreateCommandEncoder(nil)
	if err != nil {
		return err
	}
	rp, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View: sv, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore,
			ClearValue: gputypes.Color{R: hr, G: hg, B: hb, A: 1},
		}},
		DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
			View: r.depthView, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 0,
		},
	})
	if err != nil {
		return err
	}
	rp.SetBindGroup(0, r.bg, nil)
	rp.SetPipeline(r.sky)
	rp.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
	r.drawWorld(rp)
	rp.End()
	cmds, err := enc.Finish()
	if err != nil {
		return err
	}
	_, err = q.Submit(cmds)
	return err
}

// visChunk: bloco visível neste quadro e o índice da instância dele.
type visChunk struct {
	c    *chunk
	inst int
}

// prepare atualiza os blocos, descarta os fora do frustum, junta as
// instâncias (tráfego, blocos, objetos por tipo) e envia tudo à GPU.
func (r *renderer3d) prepare(g *game.Game, cam vec3, fr frustum) error {
	st := &r.stats
	*st = frameStats{}
	base := int(g.Pos / game.SegLen)
	from, to := max(base-behind3D, 0), min(base+ahead3D, len(g.Road.Segs)-1)
	built, err := r.chunks.update(from, to)
	if err != nil {
		return err
	}
	st.built, st.chunks = built, len(r.chunks.live)

	// instância 0: tráfego e 147 (já relativos à câmera)
	r.inst = addInst(r.inst[:0], vec3{}, idRight, idFwd, idScale, 1)
	r.vis = r.vis[:0]
	for k := range r.byKind {
		r.byKind[k] = r.byKind[k][:0]
	}
	for _, c := range r.chunks.live {
		off := c.origin.sub(cam)
		if !fr.visible(c.lo.add(off), c.hi.add(off)) {
			continue
		}
		r.vis = append(r.vis, visChunk{c, len(r.inst) / instFloats})
		r.inst = addInst(r.inst, off, idRight, idFwd, idScale, 1)
		st.tris += int(c.nidx / 3)
		for _, p := range c.props {
			r.byKind[p.kind] = addInst(r.byKind[p.kind], p.at.add(off), p.right, p.fwd, p.scale, p.tint)
		}
	}
	st.visible = len(r.vis)
	r.kindAt = r.kindAt[:0]
	for _, k := range propKinds {
		if l := r.byKind[k]; len(l) > 0 {
			r.kindAt = append(r.kindAt, kindSpan{k, len(r.inst) / instFloats, len(l) / instFloats})
			r.inst = append(r.inst, l...)
			n := len(l) / instFloats
			st.props += n
			st.tris += n * int(r.propRange[k][1]/3)
		}
	}

	if r.instBuf, r.instCap, err = ensureBuffer(r.dev, r.instBuf, r.instCap, uint64(len(r.inst)*4), wgpu.BufferUsageVertex, "instâncias"); err != nil {
		return err
	}
	d := &r.scene.dyn
	if r.dynV, r.dynVCap, err = ensureBuffer(r.dev, r.dynV, r.dynVCap, uint64(len(d.f)*4), wgpu.BufferUsageVertex, "tráfego:vértices"); err != nil {
		return err
	}
	if r.dynI, r.dynICap, err = ensureBuffer(r.dev, r.dynI, r.dynICap, uint64(len(d.idx)*4), wgpu.BufferUsageIndex, "tráfego:índices"); err != nil {
		return err
	}
	q := r.dev.Queue()
	q.WriteBuffer(r.instBuf, 0, floatBytes(r.inst))
	q.WriteBuffer(r.dynV, 0, floatBytes(d.f))
	q.WriteBuffer(r.dynI, 0, uintBytes(d.idx))
	st.tris += len(d.idx) / 3
	return nil
}

func (r *renderer3d) drawWorld(rp *wgpu.RenderPassEncoder) {
	st := &r.stats
	rp.SetPipeline(r.world)
	inst := func(i int) { rp.SetVertexBuffer(1, r.instBuf, uint64(i*instBytes)) }
	for _, v := range r.vis {
		rp.SetVertexBuffer(0, v.c.vbuf, 0)
		rp.SetIndexBuffer(v.c.ibuf, gputypes.IndexFormatUint32, 0)
		inst(v.inst)
		rp.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: v.c.nidx, InstanceCount: 1})
		st.draws++
	}
	if len(r.kindAt) > 0 {
		rp.SetVertexBuffer(0, r.propV, 0)
		rp.SetIndexBuffer(r.propI, gputypes.IndexFormatUint32, 0)
		for _, k := range r.kindAt {
			rg := r.propRange[k.kind]
			inst(k.first)
			rp.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: rg[1], FirstIndex: rg[0], InstanceCount: uint32(k.n)})
			st.draws++
		}
	}
	if n := len(r.scene.dyn.idx); n > 0 {
		rp.SetVertexBuffer(0, r.dynV, 0)
		rp.SetIndexBuffer(r.dynI, gputypes.IndexFormatUint32, 0)
		inst(0)
		rp.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: uint32(n), InstanceCount: 1})
		st.draws++
	}
}

func (r *renderer3d) release() {
	r.chunks.release()
	for _, x := range []interface{ Release() }{r.world, r.sky, r.layout, r.bg, r.bgl, r.ubuf,
		r.dynV, r.dynI, r.propV, r.propI, r.instBuf, r.depthView, r.depth} {
		if x != nil {
			x.Release()
		}
	}
}

// Options3D: modos de medição da janela 3D.
type Options3D struct {
	// Bench > 0: o piloto automático dirige por esse tempo e, ao fechar,
	// imprime FPS e tempos de quadro.
	Bench time.Duration
	// Shot: grava o último quadro neste PNG e fecha (no fim do Bench, ou
	// depois de 5 s).
	Shot string
}

// Run3D abre a janela 3D num trecho (1..10). Mesma física, rotas e regras
// do jogo de terminal; só o desenho muda.
func Run3D(routes []*game.Route, trecho int, opt Options3D) error {
	bench := opt.Bench
	if opt.Shot != "" && bench == 0 {
		bench = 5 * time.Second
	}
	if trecho < 1 || trecho > len(routes) {
		return fmt.Errorf("trecho %d não existe (1..%d)", trecho, len(routes))
	}
	rt := routes[trecho-1]
	g := game.NewGame(rt, 500, 30, nil)
	trk := buildTrack(g.Road)

	app := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle("Oiapoque ao Chuí 3D").
		WithSize(1280, 720).
		WithContinuousRender(true))

	var r *renderer3d
	last := time.Now()
	var titleT float64
	debug := bench > 0
	stopped := map[float64]bool{}
	var bm benchStats
	start := time.Now()
	app.OnDraw(func(dc *gogpu.Context) {
		p := app.GPUContextProvider()
		sv := dc.SurfaceView()
		if p == nil || sv == nil {
			return
		}
		if r == nil {
			var err error
			if r, err = newRenderer3D(wgpu.DeviceFromHandle(p.Device()), p.SurfaceFormat(), trk, g); err != nil {
				fmt.Println("erro no renderer 3D:", err)
				app.Quit()
				return
			}
			start = time.Now()
		}
		now := time.Now()
		frameDt := now.Sub(last)
		dt := min(frameDt.Seconds(), 0.1)
		last = now

		k := app.Input().Keyboard()
		in := game.Controls{
			Up:    k.Pressed(input.KeyUp) || k.Pressed(input.KeyW),
			Down:  k.Pressed(input.KeyDown) || k.Pressed(input.KeyS),
			Left:  k.Pressed(input.KeyLeft) || k.Pressed(input.KeyA),
			Right: k.Pressed(input.KeyRight) || k.Pressed(input.KeyD),
		}
		switch {
		case k.JustPressed(input.KeyEscape):
			app.Quit()
		case k.JustPressed(input.KeyC):
			g.ToggleCruise()
		case k.JustPressed(input.KeyP):
			g.Paused = !g.Paused
		case k.JustPressed(input.KeyG):
			g.Tow()
		case k.JustPressed(input.KeyF3):
			debug = !debug
		}
		if bench > 0 {
			in = game.Controls{}
			game.Autopilot(g, stopped, dt)
			if g.State == game.OutOfFuel {
				g.Tow()
			}
		}
		tu := time.Now()
		g.Update(dt, in)
		bm.upd = max(bm.upd, time.Since(tu))

		w, h := app.PhysicalSize()
		t0 := time.Now()
		if err := r.frame(sv, w, h, g); err != nil {
			fmt.Println("erro no quadro:", err)
		}
		cpu := time.Since(t0)
		bm.add(frameDt, cpu, r.stats)

		if titleT += dt; titleT > 0.25 {
			titleT = 0
			t := title3D(g)
			if debug {
				t = bm.window() + " · " + t
			}
			app.SetTitle(t)
		}
		if bench > 0 && time.Since(start) > bench {
			if opt.Bench > 0 {
				fmt.Println(bm.report(time.Since(start), g))
			}
			if opt.Shot != "" {
				if err := r.screenshot(opt.Shot, w, h, g); err != nil {
					fmt.Println("erro na captura:", err)
				}
			}
			app.Quit()
			bench = 0
		}
	})
	app.OnClose(func() {
		if r != nil {
			r.release()
		}
	})
	return app.Run()
}

// title3D: enquanto não há HUD na janela, o painel vai no título.
func title3D(g *game.Game) string {
	rt := g.Route
	h, m := int(g.Tod*24), int(g.Tod*24*60)%60
	s := fmt.Sprintf("%s → %s · %3.0f km/h · km %.0f/%.0f · %02d:%02d · R$ %.0f",
		rt.Origem, rt.Destino, g.Kmh(), g.Km(), rt.KmTotal, h, m, g.Money)
	if !rt.Boat() {
		s += fmt.Sprintf(" · %.0f L · %.0f°", g.Fuel, g.Temp)
	}
	switch {
	case g.Paused:
		s = "PAUSADO · " + s
	case g.State == game.Arrived:
		s = "CHEGOU! · " + s
	case g.State == game.OutOfFuel:
		s = "PANE SECA (g = guincho) · " + s
	case g.MsgT > 0:
		s = g.Msg + " · " + s
	}
	return s
}

// benchStats: tempos de quadro para o título (janela de 0,25 s) e para o
// relatório do -bench (viagem inteira).
type benchStats struct {
	frames []time.Duration // intervalo entre quadros
	cpu    time.Duration   // soma do tempo de montagem+envio no quadro
	cpuMax time.Duration
	upd    time.Duration // pior game.Update
	last   frameStats
	built  int
	win    struct {
		n   int
		t   time.Duration
		cpu time.Duration
	}
}

func (b *benchStats) add(dt, cpu time.Duration, st frameStats) {
	b.frames = append(b.frames, dt)
	b.cpu += cpu
	b.cpuMax = max(b.cpuMax, cpu)
	b.last = st
	b.built += st.built
	b.win.n++
	b.win.t += dt
	b.win.cpu += cpu
}

// window: FPS e contadores desde a última chamada (para o título).
func (b *benchStats) window() string {
	w := &b.win
	if w.n == 0 || w.t == 0 {
		return ""
	}
	s := b.last
	out := fmt.Sprintf("%.0f fps · cpu %.1f ms · %d draws · blocos %d/%d · %d objetos · %dk tri",
		float64(w.n)/w.t.Seconds(), float64(w.cpu.Microseconds())/1000/float64(w.n),
		s.draws, s.visible, s.chunks, s.props, s.tris/1000)
	w.n, w.t, w.cpu = 0, 0, 0
	return out
}

func (b *benchStats) report(total time.Duration, g *game.Game) string {
	f := b.frames
	if len(f) > 30 {
		f = f[30:] // descarta o aquecimento (blocos iniciais, compilação)
	}
	if len(f) == 0 {
		return "bench: nenhum quadro"
	}
	s := slices.Clone(f)
	slices.Sort(s)
	var sum time.Duration
	for _, d := range s {
		sum += d
	}
	avg := sum / time.Duration(len(s))
	p := func(q float64) time.Duration { return s[min(len(s)-1, int(q*float64(len(s))))] }
	slow := 0
	for _, d := range s {
		if d > 20*time.Millisecond { // perdeu mais de um vsync a 60 Hz
			slow++
		}
	}
	ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	return fmt.Sprintf("bench %s · trecho %s → %s · %.0f km rodados\n"+
		"quadros %d · média %.1f fps · quadro médio %.2f ms · p50 %.2f · p99 %.2f · pior %.2f ms · >20 ms: %d (%.2f%%)\n"+
		"cpu por quadro (montagem+envio) %.2f ms, pior %.2f ms · pior game.Update %.2f ms · blocos montados %d · último quadro: %d draws, %d/%d blocos, %d objetos, %dk triângulos",
		total.Round(time.Second), g.Route.Origem, g.Route.Destino, g.Km(),
		len(s), 1/avg.Seconds(), ms(avg), ms(p(0.5)), ms(p(0.99)), ms(s[len(s)-1]), slow, 100*float64(slow)/float64(len(s)),
		ms(b.cpu)/float64(len(b.frames)), ms(b.cpuMax), ms(b.upd), b.built, b.last.draws, b.last.visible, b.last.chunks, b.last.props, b.last.tris/1000)
}
