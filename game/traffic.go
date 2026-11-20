package game

import (
	"fmt"
	"math"
)

// vehKind: tipo de veículo do tráfego.
type VehKind struct {
	name       string
	W          float64 // largura em meias-pistas
	minS, maxS float64 // velocidade desejada (fração de maxSpeed)
	Colors     []RGB
}

var (
	KindCar = &VehKind{"carro", carW, 0.30, 0.62,
		[]RGB{{200, 30, 30}, {30, 60, 160}, {230, 230, 230}, {40, 40, 40}, {20, 120, 60}, {180, 180, 190}, {150, 90, 40}}}
	KindTruck = &VehKind{"caminhão", 0.62, 0.18, 0.34,
		[]RGB{{235, 235, 235}, {190, 190, 195}, {40, 80, 160}, {170, 40, 30}, {230, 180, 40}}}
	KindBus = &VehKind{"ônibus", 0.62, 0.32, 0.45,
		[]RGB{{235, 235, 235}, {30, 90, 170}, {200, 60, 30}}}
	KindBoat = &VehKind{"barco", carW, 0.25, 0.6,
		[]RGB{{40, 90, 170}, {200, 60, 30}, {30, 130, 90}}}
)

// Car é um veículo do tráfego. dir = +1 (mesmo sentido) ou -1 (contramão).
type Car struct {
	Z, X       float64
	Speed, Cap float64 // velocidade atual e desejada
	Dir        float64
	Kind       *VehKind
	Color      RGB
	honked     bool
}

const (
	funilaria = 250.0 // R$ por batida de frente
	oncomingP = 0.4   // fração do tráfego na contramão (pista simples)
)

func (g *Game) Spawn(c *Car, z float64) {
	rt := g.Route
	*c = Car{Z: z, Dir: 1}
	switch r := g.Rng.Float64(); {
	case rt.Boat():
		c.Kind = KindBoat
	case r < 0.22:
		c.Kind = KindTruck
	case r < 0.30:
		c.Kind = KindBus
	default:
		c.Kind = KindCar
	}
	k := c.Kind
	c.Cap = MaxSpeed * (k.minS + g.Rng.Float64()*(k.maxS-k.minS))
	c.Speed = c.Cap
	c.Color = k.Colors[g.Rng.Intn(len(k.Colors))]
	switch {
	case rt.TwoWay() && g.Rng.Float64() < oncomingP:
		c.Dir, c.X = -1, -0.5 // contramão vem pela faixa esquerda
	case rt.TwoWay():
		c.X = 0.5 // mão: faixa direita
	default:
		c.X = []float64{-0.5, 0.5}[g.Rng.Intn(2)]
	}
	c.X += (g.Rng.Float64() - 0.5) * 0.08
}

// respawn recoloca um veículo fora da vista: à frente (o jogador é mais rápido
// que quase todos) ou bem à frente para quem vem na contramão.
func (g *Game) respawn(c *Car) {
	pz := g.Pos + PlayerZ
	g.Spawn(c, pz+SegLen*float64(DrawDist+g.Rng.Intn(DrawDist)))
}

// traffic move os veículos, forma fila atrás dos lentos e trata colisões.
// pz0/pz1: posição do jogador antes/depois deste frame (colisão "varrida",
// senão um carro na contramão atravessa o jogador entre dois frames).
func (g *Game) traffic(dt, pz0, pz1 float64) {
	cars := g.Cars
	for i := range cars {
		c := &cars[i]
		// fila: mesmo sentido, mesma faixa, alguém mais lento à frente.
		// Velocidade limitada pela distância (folga de 2 segmentos).
		want := c.Cap
		lead, gap := -1, math.Inf(1)
		for j := range cars {
			o := &cars[j]
			if j == i || o.Dir != c.Dir || math.Abs(o.X-c.X) > 0.3 {
				continue
			}
			if d := (o.Z - c.Z) * c.Dir; d > 0 && d < 15*SegLen && d < gap {
				lead, gap = j, d
			}
		}
		if lead >= 0 {
			want = math.Min(want, cars[lead].Speed+math.Max(0, gap-2*SegLen)*1.5)
		}
		c.Speed += Clamp(want-c.Speed, -2*MaxSpeed*dt, MaxSpeed*0.2*dt)
		prev := c.Z - pz0
		c.Z += c.Dir * c.Speed * dt
		if lead >= 0 { // nunca atravessa o da frente
			o := &cars[lead]
			if (o.Z-c.Z)*c.Dir < SegLen {
				c.Z = o.Z - c.Dir*SegLen
				c.Speed = math.Min(c.Speed, o.Speed)
			}
		}
		now := c.Z - pz1
		overlap := math.Abs(c.X-g.PlayerX) < (g.Veh.Width+c.Kind.W)/2*0.9

		if c.Dir < 0 {
			// buzina quando vem de frente na mesma faixa
			if !c.honked && overlap && now > 0 && now < 25*SegLen {
				c.honked = true
				g.Beep = true
				g.say(fmt.Sprintf("BUZINA! %s vindo na contramão!", c.Kind.name), 1.5)
			}
			if prev > 0 && now < SegLen*0.5 && overlap {
				g.Speed = 0
				g.Bmp = 1
				g.Beep = true
				g.Money -= funilaria
				g.say(fmt.Sprintf("BATIDA DE FRENTE com %s! Funilaria R$ %.0f", c.Kind.name, funilaria), 4)
				g.respawn(c)
				continue
			}
		} else if prev > 0 && now < SegLen && g.Speed > c.Speed && overlap {
			g.Speed = c.Speed * 0.5
			g.Pos = c.Z - PlayerZ - SegLen
			g.Bmp = 0.3
		}
		if now < -30*SegLen || now > 3*DrawDist*SegLen {
			g.respawn(c)
		}
	}
}
