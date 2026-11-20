package main

import (
	"os"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func press(a *App, k tcell.Key, r rune) { a.key(tcell.NewEventKey(k, r, tcell.ModNone)) }

// TestFluxo: menu → dirige → chega → oficina → compra → balsa → pane seca → guincho.
func TestFluxo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	routes, err := LoadRoutes(os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	s := tcell.NewSimulationScreen("UTF-8")
	s.Init()
	s.SetSize(140, 40)
	a := NewApp(routes, SavePath())
	frame := func() { a.tick(1.0 / 60); a.draw(s); s.Show() }

	frame()
	press(a, tcell.KeyEnter, 0)
	if a.scr != scDrive || a.game.Route != routes[0] {
		t.Fatal("Enter no menu deveria começar o trecho 1")
	}
	press(a, tcell.KeyUp, 0)
	for range 120 {
		frame()
	}
	if a.game.Speed <= 0 {
		t.Fatal("segurar ↑ deveria andar")
	}

	// força a chegada
	a.game.Pos = a.game.EndZ
	frame()
	if a.game.State != Arrived {
		t.Fatal("deveria ter chegado")
	}
	money := a.game.Money
	press(a, tcell.KeyEnter, 0)
	if a.scr != scShop || a.save.Trecho != 1 || a.save.Dinheiro != money {
		t.Fatalf("esperava oficina com progresso salvo: scr=%d trecho=%d", a.scr, a.save.Trecho)
	}
	if got := LoadSave(SavePath()); got.Trecho != 1 {
		t.Fatalf("save em disco não atualizou: %+v", got)
	}

	// compra farol de milha (item 5)
	for range 4 {
		press(a, tcell.KeyDown, 0)
	}
	press(a, tcell.KeyEnter, 0)
	if !a.save.Melhorias["farol"] || a.save.Dinheiro != money-250 {
		t.Fatalf("compra falhou: %v R$%.0f (%s)", a.save.Melhorias, a.save.Dinheiro, a.note)
	}
	frame()

	// seguir viagem → balsa
	press(a, tcell.KeyUp, 0) // volta pro topo… e vai até "Seguir viagem"
	a.sel = len(Upgrades) + 1
	press(a, tcell.KeyEnter, 0)
	if a.scr != scDrive || !a.game.Route.Boat() {
		t.Fatal("trecho 2 deveria ser a balsa")
	}
	if a.game.Money != a.save.Dinheiro-routes[1].Passagem {
		t.Fatal("passagem da balsa não cobrada")
	}
	for range 60 {
		frame()
	}

	// pane seca + guincho no trecho 3
	a.save.Trecho = 2
	a.startTrecho()
	a.game.Fuel = 0
	for range 30 {
		frame()
	}
	if a.game.State != OutOfFuel {
		t.Fatalf("sem gasolina e parado = pane seca (state=%d)", a.game.State)
	}
	before := a.game.Money
	press(a, tcell.KeyRune, 'g')
	if a.game.State != Playing || a.game.Fuel != GuinchoL || a.game.Money != before-GuinchoPreco {
		t.Fatal("guincho deveria cobrar e pôr gasolina")
	}
	frame()
}

// TestParada: passar por cidade salva; p pausa; q sai salvando; Enter continua do mesmo km.
func TestParada(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	routes, err := LoadRoutes(os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp(routes, SavePath())
	press(a, tcell.KeyEnter, 0)
	g := a.game
	g.Pos = 219.8*g.Upk - PlayerZ // logo antes de Calçoene (km 220)
	g.Speed = MaxSpeed * 0.5
	for range 120 {
		a.tick(1.0 / 60)
	}
	cp := LoadSave(SavePath()).Parada
	if cp == nil || cp.Km < 220 || cp.Km > 222 {
		t.Fatalf("passar por Calçoene deveria salvar parada: %+v", cp)
	}

	press(a, tcell.KeyRune, 'p')
	km := g.Km()
	for range 60 {
		a.tick(1.0 / 60)
	}
	if !g.Paused || g.Km() != km {
		t.Fatal("pausado não deveria andar")
	}
	g.Money = 777
	press(a, tcell.KeyRune, 'q')
	if a.scr != scTitle {
		t.Fatal("q deveria voltar ao menu")
	}

	b := NewApp(routes, SavePath()) // "reabre o jogo"
	press(b, tcell.KeyEnter, 0)
	if d := b.game.Km() - km; d < -0.01 || d > 0.01 || b.game.Money != 777 {
		t.Fatalf("deveria continuar do km %.2f com R$ 777: km %.2f R$ %.0f", km, b.game.Km(), b.game.Money)
	}
	if b.game.NextCity != 1 {
		t.Fatalf("Calçoene já passou, próxima cidade deveria ser a 2ª (nextCity=%d)", b.game.NextCity)
	}
}
