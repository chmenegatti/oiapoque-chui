package main

import "testing"

func trafficGame(t *testing.T) *Game {
	routes, err := loadRoutes()
	if err != nil {
		t.Fatal(err)
	}
	g := NewGame(routes[0], 1000, 30, nil) // BR-156: pista simples
	if !g.route.twoWay() {
		t.Fatal("BR-156 deveria ser mão dupla")
	}
	g.pos = 20 * g.upk // km 20, longe de radar/posto
	g.cars = g.cars[:1]
	g.cars[0] = Car{z: g.pos + playerZ + 40*segLen, x: -0.5, dir: -1, kind: kindTruck,
		speed: maxSpeed * 0.3, cap: maxSpeed * 0.3}
	g.speed = maxSpeed * 0.5
	g.cruise = true
	return g
}

func TestBatidaDeFrente(t *testing.T) {
	g := trafficGame(t)
	g.playerX = -0.5 // na contramão
	for range 120 {
		g.playerX = -0.5
		g.Update(1.0 / 60)
	}
	if g.money != 1000-funilaria {
		t.Fatalf("esperava batida de frente (R$ %.0f), dinheiro = %.0f", funilaria, g.money)
	}
}

func TestMaoCerta(t *testing.T) {
	g := trafficGame(t)
	g.playerX = 0.5 // na própria mão
	for range 60 {
		g.playerX = 0.5
		g.Update(1.0 / 60)
	}
	if g.money != 1000 {
		t.Fatalf("na própria mão não deveria bater, dinheiro = %.0f", g.money)
	}
	if !(g.cars[0].z < g.pos+playerZ) {
		t.Fatal("o caminhão deveria ter cruzado pelo jogador")
	}
}

func TestFilaAtrasDoCaminhao(t *testing.T) {
	g := trafficGame(t)
	g.playerX = 2.5 // fora do caminho
	z := g.pos + playerZ + 100*segLen
	g.cars = []Car{
		{z: z, x: 0.5, dir: 1, kind: kindTruck, speed: maxSpeed * 0.2, cap: maxSpeed * 0.2},
		{z: z - 3*segLen, x: 0.5, dir: 1, kind: kindCar, speed: maxSpeed * 0.6, cap: maxSpeed * 0.6},
	}
	for range 120 {
		g.traffic(1.0/60, g.pos+playerZ, g.pos+playerZ)
	}
	if g.cars[1].speed > g.cars[0].speed+1 || g.cars[1].z >= g.cars[0].z {
		t.Fatalf("carro deveria ficar na fila: carro %.0f, caminhão %.0f", g.cars[1].speed, g.cars[0].speed)
	}
}
