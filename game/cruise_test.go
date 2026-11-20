package game

import "testing"

func driving(t *testing.T) *Game {
	g := trafficGame(t)
	g.Cars = nil
	g.Cruise = false
	g.Speed = MaxSpeed * 0.5
	return g
}

func TestPilotoDeVelocidade(t *testing.T) {
	g := driving(t)
	g.ToggleCruise()
	g.Update(1.0/60, Controls{})
	if !g.Throttle {
		t.Fatal("piloto ligado acelera sem tecla")
	}
	g.Update(1.0/60, Controls{Down: true})
	if g.Cruise {
		t.Fatal("frear desliga o piloto")
	}
}
