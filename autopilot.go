package main

import "math"

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
