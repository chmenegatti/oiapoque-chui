package term

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/chmenegatti/oiapoque-chui/game"
	"github.com/gdamore/tcell/v2"
)

func savePNG(f *Frame, path string, scale int) error {
	img := image.NewRGBA(image.Rect(0, 0, f.W*scale, f.H*scale))
	for y := 0; y < f.H*scale; y++ {
		for x := 0; x < f.W*scale; x++ {
			p := f.Pix[(y/scale)*f.W+x/scale]
			img.Set(x, y, color.RGBA{p.R, p.G, p.B, 255})
		}
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	return png.Encode(out, img)
}

// TestSnap gera imagens de cenas do jogo. SNAP=<pasta, relativa a term/> (ou SNAP=1 → ../snap/).
//
//	SNAP=../docs/img go test -run TestSnap ./term
func TestSnap(t *testing.T) {
	dir := os.Getenv("SNAP")
	if dir == "" {
		t.Skip("defina SNAP=<pasta> para gerar imagens")
	}
	if dir == "1" {
		dir = "../snap"
	}
	os.MkdirAll(dir, 0o755)
	routes, err := game.LoadRoutes(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	s := tcell.NewSimulationScreen("UTF-8")
	s.Init()
	s.SetSize(160, 46)
	scenes := []struct {
		name     string
		trecho   int
		km, hora float64
		oncoming bool
	}{
		{"br156-terra-chuva", 0, 50, 10, true},
		{"balsa-amazonas", 1, 100, 17, false},
		{"cerrado-mao-dupla", 4, 300, 15.5, true},
		{"serra-cafezal-noite", 7, 108, 21, false},
		{"litoral-catarinense", 8, 250, 18.4, false},
		{"pampa-taim", 9, 150, 18.6, true},
	}
	for _, c := range scenes {
		g := game.NewGame(routes[c.trecho], 500, 30, nil)
		g.Pos = c.km*g.Upk - game.PlayerZ
		g.Tod = c.hora / 24
		g.Speed = game.MaxSpeed * 0.6
		seg := g.Road.Find(g.Pos + game.PlayerZ)
		g.FogD, g.RainD, g.HillsK = 2+12*seg.Fog, seg.Rain, seg.Ter.Hills
		if g.Route.TwoWay() {
			g.PlayerX = 0.5
		}
		g.Cars = g.Cars[:2]
		g.Spawn(&g.Cars[0], g.Pos+game.PlayerZ+5000)
		g.Spawn(&g.Cars[1], g.Pos+game.PlayerZ+11000)
		if c.oncoming {
			g.Cars[1] = game.Car{Z: g.Pos + game.PlayerZ + 7000, X: -0.5, Dir: -1, Kind: game.KindTruck,
				Speed: game.MaxSpeed * 0.3, Cap: game.MaxSpeed * 0.3, Color: game.KindTruck.Colors[2]}
		}
		if g.Route.TwoWay() {
			g.Cars[0].X, g.Cars[0].Dir = 0.5, 1
		}
		var v view
		v.Draw(s, g)
		if err := savePNG(v.frame, filepath.Join(dir, c.name+".png"), 3); err != nil {
			t.Fatal(err)
		}
	}
}
