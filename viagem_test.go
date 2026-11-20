package main

import (
	"math"
	"os"
	"testing"
)

// TestCampanha: o piloto automático faz as 10 etapas. Garante que toda
// rota é vencível e que o balanço (tempo, gasolina, temperatura) faz sentido.
func TestCampanha(t *testing.T) {
	routes, err := loadRoutes(os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 10 {
		t.Fatalf("esperava 10 trechos, achei %d", len(routes))
	}
	fuel := 20.0
	total := 0.0
	for _, rt := range routes {
		g := NewGame(rt, 1e6, fuel, nil)
		g.cars = nil
		dt := 1.0 / 60
		stopped := map[float64]bool{}
		overheats, wasOH, minFuel := 0, false, fuel
		for step := 0; step < 60*60*30 && g.state == playing; step++ {
			autopilot(g, stopped, dt)
			g.Update(dt, Controls{})
			if g.overheat && !wasOH {
				overheats++
			}
			wasOH = g.overheat
			minFuel = math.Min(minFuel, g.fuel)
		}
		if g.state != arrived {
			t.Fatalf("%s → %s: não chegou (state=%d km=%.1f fuel=%.1f)", rt.Origem, rt.Destino, g.state, g.km(), g.fuel)
		}
		total += g.elapsed
		t.Logf("%-12s → %-13s %4.0f km  %5.1f min  min comb %4.1f L  ferveu %d  multas %d",
			rt.Origem, rt.Destino, rt.KmTotal, g.elapsed/60, minFuel, overheats, g.fines)
		fuel = g.fuel
	}
	t.Logf("total %.0f min", total/60)
}
