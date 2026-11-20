package term

import (
	"image"
	"image/color"
	"image/gif"
	"os"
	"slices"
	"testing"

	"github.com/chmenegatti/oiapoque-chui/game"
	"github.com/gdamore/tcell/v2"
)

// TestDemoGIF grava docs/demo.gif com o piloto automático em três cenas.
// Roda só com DEMO=1 go test -run TestDemoGIF ./term
func TestDemoGIF(t *testing.T) {
	if os.Getenv("DEMO") == "" {
		t.Skip("defina DEMO=1 para gerar docs/demo.gif")
	}
	routes, err := game.LoadRoutes(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	s := tcell.NewSimulationScreen("UTF-8")
	s.Init()
	s.SetSize(100, 30)

	scenes := []struct {
		trecho   int
		km, hora float64
		seconds  float64
		lane     float64
	}{
		{3, 150, 16.8, 5, 0.5}, // cerrado, mão dupla, caminhões
		{7, 104, 19.6, 4, 0.5}, // Serra do Cafezal à noite, neblina
		{1, 60, 17.4, 3.5, 0},  // balsa no Amazonas
	}
	const scale, every = 4, 6 // 60 FPS / 6 = 10 FPS
	var frames [][]game.RGB
	var W, H int
	for _, sc := range scenes {
		g := game.NewGame(routes[sc.trecho], 1e6, 30, nil)
		g.Pos = sc.km*g.Upk - game.PlayerZ
		g.Tod = sc.hora / 24
		g.Speed = g.Vmax * 0.7
		g.Cruise = true
		seg := g.Road.Find(g.Pos + game.PlayerZ)
		g.FogD, g.RainD, g.HillsK = 2+12*seg.Fog, seg.Rain, seg.Ter.Hills
		g.MsgT = 0
		for i := range g.Cars { // tráfego já na vista
			g.Spawn(&g.Cars[i], g.Pos+game.PlayerZ+game.SegLen*float64(15+g.Rng.Intn(game.DrawDist)))
		}
		stopped := map[float64]bool{}
		var v view
		for f := 0; f < int(sc.seconds*60); f++ {
			if g.Route.Boat() {
				game.Autopilot(g, stopped, 1.0/60) // desvia dos troncos
			} else {
				g.Cruise = true
				g.PlayerX += game.Clamp(sc.lane-g.PlayerX, -0.03, 0.03) // mantém a mão
			}
			g.Update(1.0/60, game.Controls{})
			if f%every == 0 {
				v.Draw(s, g)
				frames = append(frames, slices.Clone(v.frame.Pix))
				W, H = v.frame.W, v.frame.H
			}
		}
	}

	pal, index := quantize(frames)
	anim := &gif.GIF{}
	for _, px := range frames {
		img := image.NewPaletted(image.Rect(0, 0, W*scale, H*scale), pal)
		for y := 0; y < H*scale; y++ {
			for x := 0; x < W*scale; x++ {
				img.Pix[y*img.Stride+x] = index(px[(y/scale)*W+x/scale])
			}
		}
		anim.Image = append(anim.Image, img)
		anim.Delay = append(anim.Delay, 100/(60/every))
	}
	os.MkdirAll("../docs", 0o755)
	out, err := os.Create("../docs/demo.gif")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := gif.EncodeAll(out, anim); err != nil {
		t.Fatal(err)
	}
	st, _ := out.Stat()
	t.Logf("%d frames, %dx%d, %.1f MB", len(frames), W*scale, H*scale, float64(st.Size())/1e6)
}

// quantize: paleta com as 256 cores (5 bits/canal) mais frequentes; mapeia
// cada cor para a mais próxima, com cache.
func quantize(frames [][]game.RGB) (color.Palette, func(game.RGB) uint8) {
	key := func(c game.RGB) uint16 { return uint16(c.R>>3)<<10 | uint16(c.G>>3)<<5 | uint16(c.B>>3) }
	counts := map[uint16]int{}
	for _, f := range frames {
		for _, c := range f {
			counts[key(c)]++
		}
	}
	keys := make([]uint16, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b uint16) int { return counts[b] - counts[a] })
	keys = keys[:min(256, len(keys))]
	pal := make(color.Palette, len(keys))
	for i, k := range keys {
		pal[i] = color.RGBA{uint8(k>>10&31)<<3 | 4, uint8(k>>5&31)<<3 | 4, uint8(k&31)<<3 | 4, 255}
	}
	cache := map[game.RGB]uint8{}
	return pal, func(c game.RGB) uint8 {
		if i, ok := cache[c]; ok {
			return i
		}
		i := uint8(pal.Index(color.RGBA{c.R, c.G, c.B, 255}))
		cache[c] = i
		return i
	}
}
