package main

import (
	"fmt"
	"math"
)

// vehKind: tipo de veículo do tráfego.
type vehKind struct {
	name        string
	rear, front *Sprite
	w           float64 // largura em meias-pistas
	minS, maxS  float64 // velocidade desejada (fração de maxSpeed)
	colors      []RGB
}

var (
	kindCar = &vehKind{"carro", sprCar, sprCarFront, carW, 0.30, 0.62,
		[]RGB{{200, 30, 30}, {30, 60, 160}, {230, 230, 230}, {40, 40, 40}, {20, 120, 60}, {180, 180, 190}, {150, 90, 40}}}
	kindTruck = &vehKind{"caminhão", sprTruck, sprTruckFront, 0.62, 0.18, 0.34,
		[]RGB{{235, 235, 235}, {190, 190, 195}, {40, 80, 160}, {170, 40, 30}, {230, 180, 40}}}
	kindBus = &vehKind{"ônibus", sprBus, sprBusFront, 0.62, 0.32, 0.45,
		[]RGB{{235, 235, 235}, {30, 90, 170}, {200, 60, 30}}}
	kindBoat = &vehKind{"barco", sprBarco, sprBarco, carW, 0.25, 0.6,
		[]RGB{{40, 90, 170}, {200, 60, 30}, {30, 130, 90}}}
)

// Car é um veículo do tráfego. dir = +1 (mesmo sentido) ou -1 (contramão).
type Car struct {
	z, x       float64
	speed, cap float64 // velocidade atual e desejada
	dir        float64
	kind       *vehKind
	color      RGB
	honked     bool
}

const (
	funilaria = 250.0 // R$ por batida de frente
	oncomingP = 0.4   // fração do tráfego na contramão (pista simples)
)

func (g *Game) spawn(c *Car, z float64) {
	rt := g.route
	*c = Car{z: z, dir: 1}
	switch r := g.rng.Float64(); {
	case rt.boat():
		c.kind = kindBoat
	case r < 0.22:
		c.kind = kindTruck
	case r < 0.30:
		c.kind = kindBus
	default:
		c.kind = kindCar
	}
	k := c.kind
	c.cap = maxSpeed * (k.minS + g.rng.Float64()*(k.maxS-k.minS))
	c.speed = c.cap
	c.color = k.colors[g.rng.Intn(len(k.colors))]
	switch {
	case rt.twoWay() && g.rng.Float64() < oncomingP:
		c.dir, c.x = -1, -0.5 // contramão vem pela faixa esquerda
	case rt.twoWay():
		c.x = 0.5 // mão: faixa direita
	default:
		c.x = []float64{-0.5, 0.5}[g.rng.Intn(2)]
	}
	c.x += (g.rng.Float64() - 0.5) * 0.08
}

// respawn recoloca um veículo fora da vista: à frente (o jogador é mais rápido
// que quase todos) ou bem à frente para quem vem na contramão.
func (g *Game) respawn(c *Car) {
	pz := g.pos + playerZ
	g.spawn(c, pz+segLen*float64(drawDist+g.rng.Intn(drawDist)))
}

// traffic move os veículos, forma fila atrás dos lentos e trata colisões.
// pz0/pz1: posição do jogador antes/depois deste frame (colisão "varrida",
// senão um carro na contramão atravessa o jogador entre dois frames).
func (g *Game) traffic(dt, pz0, pz1 float64) {
	cars := g.cars
	for i := range cars {
		c := &cars[i]
		// fila: mesmo sentido, mesma faixa, alguém mais lento à frente.
		// Velocidade limitada pela distância (folga de 2 segmentos).
		want := c.cap
		lead, gap := -1, math.Inf(1)
		for j := range cars {
			o := &cars[j]
			if j == i || o.dir != c.dir || math.Abs(o.x-c.x) > 0.3 {
				continue
			}
			if d := (o.z - c.z) * c.dir; d > 0 && d < 15*segLen && d < gap {
				lead, gap = j, d
			}
		}
		if lead >= 0 {
			want = math.Min(want, cars[lead].speed+math.Max(0, gap-2*segLen)*1.5)
		}
		c.speed += clamp(want-c.speed, -2*maxSpeed*dt, maxSpeed*0.2*dt)
		prev := c.z - pz0
		c.z += c.dir * c.speed * dt
		if lead >= 0 { // nunca atravessa o da frente
			o := &cars[lead]
			if (o.z-c.z)*c.dir < segLen {
				c.z = o.z - c.dir*segLen
				c.speed = math.Min(c.speed, o.speed)
			}
		}
		now := c.z - pz1
		overlap := math.Abs(c.x-g.playerX) < (g.veh.width+c.kind.w)/2*0.9

		if c.dir < 0 {
			// buzina quando vem de frente na mesma faixa
			if !c.honked && overlap && now > 0 && now < 25*segLen {
				c.honked = true
				g.beep = true
				g.say(fmt.Sprintf("BUZINA! %s vindo na contramão!", c.kind.name), 1.5)
			}
			if prev > 0 && now < segLen*0.5 && overlap {
				g.speed = 0
				g.bmp = 1
				g.beep = true
				g.money -= funilaria
				g.say(fmt.Sprintf("BATIDA DE FRENTE com %s! Funilaria R$ %.0f", c.kind.name, funilaria), 4)
				g.respawn(c)
				continue
			}
		} else if prev > 0 && now < segLen && g.speed > c.speed && overlap {
			g.speed = c.speed * 0.5
			g.pos = c.z - playerZ - segLen
			g.bmp = 0.3
		}
		if now < -30*segLen || now > 3*drawDist*segLen {
			g.respawn(c)
		}
	}
}
