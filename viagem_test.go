package main

import (
	"math"
	"testing"
)

// autopilot: acelera fundo, desvia de obstáculos, para no posto quando o
// tanque baixa e alivia quando o motor ferve.
func autopilot(g *Game, stopped map[float64]bool, dt float64) {
	g.cruise = !g.overheat
	km := g.km()

	// desvio: escolhe a faixa mais livre nos próximos 25 segmentos
	best, bestD := g.playerX, -1.0
	for _, x := range []float64{-0.8, -0.4, 0, 0.4, 0.8} {
		d := 10.0
		for i := 1; i < 25; i++ {
			s := g.road.find(g.pos + playerZ + float64(i)*segLen)
			for _, p := range s.props {
				if p.hit > 0 && math.Abs(p.x) < 1.2 {
					d = math.Min(d, math.Abs(p.x-x))
				}
			}
		}
		if d > bestD+0.01 {
			best, bestD = x, d
		}
	}
	g.playerX += clamp(best-g.playerX, -dt*2, dt*2)

	for _, p := range g.route.Postos {
		if km > p-1.5 && km < p+postoZona && g.fuel < g.veh.tank*0.6 && !stopped[p] {
			g.cruise = g.kmh() < 12 // vem devagarinho até a bomba
			if g.kmh() > 15 {
				g.speed += braking * dt
			}
			if g.fuel > g.veh.tank*0.95 {
				stopped[p] = true
			}
		}
	}
}

// TestCampanha: o piloto automático faz as 10 etapas. Garante que toda
// rota é vencível e que o balanço (tempo, gasolina, temperatura) faz sentido.
func TestCampanha(t *testing.T) {
	routes, err := loadRoutes()
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
			g.Update(dt)
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
