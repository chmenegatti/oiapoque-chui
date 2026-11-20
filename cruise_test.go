package main

import "testing"

func driving(t *testing.T) *Game {
	g := trafficGame(t)
	g.cars = nil
	g.cruise = false
	g.speed = maxSpeed * 0.5
	return g
}

func TestPilotoDeVelocidade(t *testing.T) {
	g := driving(t)
	g.toggleCruise()
	g.Update(1.0/60, Controls{})
	if !g.throttle {
		t.Fatal("piloto ligado acelera sem tecla")
	}
	g.Update(1.0/60, Controls{Down: true})
	if g.cruise {
		t.Fatal("frear desliga o piloto")
	}
}
