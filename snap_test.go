package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

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

// TestSnap gera imagens de cenas do jogo. SNAP=<pasta> (ou SNAP=1 → snap/).
//
//	SNAP=docs/img go test -run TestSnap .
func TestSnap(t *testing.T) {
	dir := os.Getenv("SNAP")
	if dir == "" {
		t.Skip("defina SNAP=<pasta> para gerar imagens")
	}
	if dir == "1" {
		dir = "snap"
	}
	os.MkdirAll(dir, 0o755)
	routes, err := loadRoutes()
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
		g := NewGame(routes[c.trecho], 500, 30, nil)
		g.pos = c.km*g.upk - playerZ
		g.tod = c.hora / 24
		g.speed = maxSpeed * 0.6
		seg := g.road.find(g.pos + playerZ)
		g.fogD, g.rainD, g.hillsK = 2+12*seg.fog, seg.rain, seg.ter.hills
		if g.route.twoWay() {
			g.playerX = 0.5
		}
		g.cars = g.cars[:2]
		g.spawn(&g.cars[0], g.pos+playerZ+5000)
		g.spawn(&g.cars[1], g.pos+playerZ+11000)
		if c.oncoming {
			g.cars[1] = Car{z: g.pos + playerZ + 7000, x: -0.5, dir: -1, kind: kindTruck,
				speed: maxSpeed * 0.3, cap: maxSpeed * 0.3, color: kindTruck.colors[2]}
		}
		if g.route.twoWay() {
			g.cars[0].x, g.cars[0].dir = 0.5, 1
		}
		g.Draw(s)
		if err := savePNG(g.frame, filepath.Join(dir, c.name+".png"), 3); err != nil {
			t.Fatal(err)
		}
	}
}
