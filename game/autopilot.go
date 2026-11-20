package game

import "math"

// autopilot: acelera fundo, desvia de obstáculos, para no posto quando o
// tanque baixa e alivia quando o motor ferve.
func Autopilot(g *Game, stopped map[float64]bool, dt float64) {
	g.Cruise = !g.Overheat
	km := g.Km()

	// desvio: escolhe a faixa mais livre nos próximos 25 segmentos
	best, bestD := g.PlayerX, -1.0
	for _, x := range []float64{-0.8, -0.4, 0, 0.4, 0.8} {
		d := 10.0
		for i := 1; i < 25; i++ {
			s := g.Road.Find(g.Pos + PlayerZ + float64(i)*SegLen)
			for _, p := range s.Props {
				if p.hit > 0 && math.Abs(p.X) < 1.2 {
					d = math.Min(d, math.Abs(p.X-x))
				}
			}
		}
		if d > bestD+0.01 {
			best, bestD = x, d
		}
	}
	g.PlayerX += Clamp(best-g.PlayerX, -dt*2, dt*2)

	for _, p := range g.Route.Postos {
		if km > p-1.5 && km < p+postoZona && g.Fuel < g.Veh.Tank*0.6 && !stopped[p] {
			g.Cruise = g.Kmh() < 12 // vem devagarinho até a bomba
			if g.Kmh() > 15 {
				g.Speed += braking * dt
			}
			if g.Fuel > g.Veh.Tank*0.95 {
				stopped[p] = true
			}
		}
	}
}
