package game

import (
	"os"
	"testing"
)

func trafficGame(t *testing.T) *Game {
	routes, err := LoadRoutes(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	g := NewGame(routes[0], 1000, 30, nil) // BR-156: pista simples
	if !g.Route.TwoWay() {
		t.Fatal("BR-156 deveria ser mão dupla")
	}
	g.Pos = 20 * g.Upk // km 20, longe de radar/posto
	g.Cars = g.Cars[:1]
	g.Cars[0] = Car{Z: g.Pos + PlayerZ + 40*SegLen, X: -0.5, Dir: -1, Kind: KindTruck,
		Speed: MaxSpeed * 0.3, Cap: MaxSpeed * 0.3}
	g.Speed = MaxSpeed * 0.5
	g.Cruise = true
	return g
}

func TestBatidaDeFrente(t *testing.T) {
	g := trafficGame(t)
	g.PlayerX = -0.5 // na contramão
	for range 120 {
		g.PlayerX = -0.5
		g.Update(1.0/60, Controls{})
	}
	if g.Money != 1000-funilaria {
		t.Fatalf("esperava batida de frente (R$ %.0f), dinheiro = %.0f", funilaria, g.Money)
	}
}

func TestMaoCerta(t *testing.T) {
	g := trafficGame(t)
	g.PlayerX = 0.5 // na própria mão
	for range 60 {
		g.PlayerX = 0.5
		g.Update(1.0/60, Controls{})
	}
	if g.Money != 1000 {
		t.Fatalf("na própria mão não deveria bater, dinheiro = %.0f", g.Money)
	}
	if !(g.Cars[0].Z < g.Pos+PlayerZ) {
		t.Fatal("o caminhão deveria ter cruzado pelo jogador")
	}
}

func TestFilaAtrasDoCaminhao(t *testing.T) {
	g := trafficGame(t)
	g.PlayerX = 2.5 // fora do caminho
	z := g.Pos + PlayerZ + 100*SegLen
	g.Cars = []Car{
		{Z: z, X: 0.5, Dir: 1, Kind: KindTruck, Speed: MaxSpeed * 0.2, Cap: MaxSpeed * 0.2},
		{Z: z - 3*SegLen, X: 0.5, Dir: 1, Kind: KindCar, Speed: MaxSpeed * 0.6, Cap: MaxSpeed * 0.6},
	}
	for range 120 {
		g.traffic(1.0/60, g.Pos+PlayerZ, g.Pos+PlayerZ)
	}
	if g.Cars[1].Speed > g.Cars[0].Speed+1 || g.Cars[1].Z >= g.Cars[0].Z {
		t.Fatalf("carro deveria ficar na fila: carro %.0f, caminhão %.0f", g.Cars[1].Speed, g.Cars[0].Speed)
	}
}
