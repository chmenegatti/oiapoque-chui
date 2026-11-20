package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
	"unsafe"

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

@vertex
fn vs_world(@location(0) p: vec3<f32>, @location(1) n: vec3<f32>, @location(2) c: vec3<f32>) -> VOut {
  var o: VOut;
  o.pos = u.viewProj * vec4<f32>(p, 1.0);
  o.col = c;
  o.n = n;
  o.wp = p;
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
  let n = normalize(i.n);
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
	uniformBytes = 64 + 10*16
	depthFormat  = gputypes.TextureFormatDepth24Plus
	fovY         = 58 * math.Pi / 180
)

type renderer3d struct {
	dev        *wgpu.Device
	world, sky *wgpu.RenderPipeline
	layout     *wgpu.PipelineLayout
	bgl        *wgpu.BindGroupLayout
	bg         *wgpu.BindGroup
	ubuf, vbuf *wgpu.Buffer
	vcap       uint64
	depth      *wgpu.Texture
	depthView  *wgpu.TextureView
	dw, dh     int
	scene      scene3d
	ubytes     []byte
}

func newRenderer3D(dev *wgpu.Device, format gputypes.TextureFormat, trk *track) (*renderer3d, error) {
	r := &renderer3d{dev: dev, scene: scene3d{trk: trk}, ubytes: make([]byte, uniformBytes)}
	var err error
	if r.ubuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Label: "uniforms", Size: 256,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst}); err != nil {
		return nil, err
	}
	if err = r.growVertices(8 << 20); err != nil {
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
		}}},
		Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		DepthStencil: &wgpu.DepthStencilState{Format: depthFormat, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLess},
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

func (r *renderer3d) growVertices(size uint64) error {
	if r.vbuf != nil {
		r.vbuf.Release()
	}
	var err error
	r.vbuf, err = r.dev.CreateBuffer(&wgpu.BufferDescriptor{Label: "vertices", Size: size,
		Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst})
	r.vcap = size
	return err
}

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

func rgbf(c RGB) (float64, float64, float64) {
	return float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255
}

func (r *renderer3d) frame(sv *wgpu.TextureView, w, h int, g *Game) error {
	if w <= 0 || h <= 0 {
		return nil
	}
	if err := r.ensureDepth(w, h); err != nil {
		return err
	}
	cam, look, headPos, headDir := r.scene.build(g)
	_ = cam

	// câmera
	aspect := float64(w) / float64(h)
	view, s, u, f := lookDir(look)
	vp := perspective(fovY, aspect, 0.8, 2600).mul(view)
	tanY := math.Tan(fovY / 2)

	// luz e céu
	day := Daylight(g.Tod) * (1 - 0.35*g.RainD)
	zen, hor := SkyColors(g.Tod)
	gray := RGB{110, 115, 125}
	zen = Mix(zen, Scale(gray, day), g.RainD*0.7)
	hor = Mix(hor, Scale(gray, day+0.2), g.RainD*0.7)
	a := (g.Tod - 0.25) * 2 * math.Pi
	sun := vec3{math.Cos(a) * 0.6, math.Sin(a), 0.45}.norm()
	sc := Mix(RGB{255, 140, 60}, RGB{255, 245, 225}, Clamp(sun.y*4, 0, 1))
	sr, sg, sb := rgbf(sc)
	k := day * Clamp(sun.y*5+0.3, 0, 1)
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
	o = putVec4(b, o, hr, hg, hb, math.Sqrt(g.FogD)/750)
	o = putVec4(b, o, zr, zg, zb, 0)
	o = putVec4(b, o, hr, hg, hb, 0)
	o = putVec4(b, o, headPos.x, headPos.y, headPos.z, headI)
	o = putVec4(b, o, headDir.x, headDir.y, headDir.z, 260*g.Veh.Headlight/40)
	o = putVec4(b, o, s.x, s.y, s.z, tanY*aspect)
	o = putVec4(b, o, u.x, u.y, u.z, tanY)
	putVec4(b, o, f.x, f.y, f.z, 0)

	q := r.dev.Queue()
	q.WriteBuffer(r.ubuf, 0, b)
	m := &r.scene.m
	if n := uint64(len(m.f) * 4); n > 0 {
		if n > r.vcap {
			if err := r.growVertices(n * 2); err != nil {
				return err
			}
		}
		q.WriteBuffer(r.vbuf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&m.f[0])), n))
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
			View: r.depthView, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1,
		},
	})
	if err != nil {
		return err
	}
	rp.SetBindGroup(0, r.bg, nil)
	rp.SetPipeline(r.sky)
	rp.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
	if m.n > 0 {
		rp.SetPipeline(r.world)
		rp.SetVertexBuffer(0, r.vbuf, 0)
		rp.Draw(gputypes.DrawArgs{VertexCount: m.n, InstanceCount: 1})
	}
	rp.End()
	cmds, err := enc.Finish()
	if err != nil {
		return err
	}
	_, err = q.Submit(cmds)
	return err
}

func (r *renderer3d) release() {
	for _, x := range []interface{ Release() }{r.world, r.sky, r.layout, r.bg, r.bgl, r.ubuf, r.vbuf, r.depthView, r.depth} {
		if x != nil {
			x.Release()
		}
	}
}

// run3D abre a janela 3D num trecho (1..10). Mesma física, rotas e regras
// do jogo de terminal; só o desenho muda.
func Run3D(routes []*Route, trecho int) error {
	if trecho < 1 || trecho > len(routes) {
		return fmt.Errorf("trecho %d não existe (1..%d)", trecho, len(routes))
	}
	rt := routes[trecho-1]
	g := NewGame(rt, 500, 30, nil)
	trk := buildTrack(g.Road)

	app := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle("Oiapoque ao Chuí 3D").
		WithSize(1280, 720).
		WithContinuousRender(true))

	var r *renderer3d
	last := time.Now()
	var titleT float64
	app.OnDraw(func(dc *gogpu.Context) {
		p := app.GPUContextProvider()
		sv := dc.SurfaceView()
		if p == nil || sv == nil {
			return
		}
		if r == nil {
			var err error
			if r, err = newRenderer3D(wgpu.DeviceFromHandle(p.Device()), p.SurfaceFormat(), trk); err != nil {
				fmt.Println("erro no renderer 3D:", err)
				app.Quit()
				return
			}
		}
		now := time.Now()
		dt := min(now.Sub(last).Seconds(), 0.1)
		last = now

		k := app.Input().Keyboard()
		in := Controls{
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
		}
		g.Update(dt, in)

		w, h := app.PhysicalSize()
		if err := r.frame(sv, w, h, g); err != nil {
			fmt.Println("erro no quadro:", err)
		}

		if titleT += dt; titleT > 0.25 {
			titleT = 0
			app.SetTitle(title3D(g))
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
func title3D(g *Game) string {
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
	case g.State == Arrived:
		s = "CHEGOU! · " + s
	case g.State == OutOfFuel:
		s = "PANE SECA (g = guincho) · " + s
	case g.MsgT > 0:
		s = g.Msg + " · " + s
	}
	return s
}
