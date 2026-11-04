package main

import (
	"testing"
	"time"
)

func TestFiltroKitty(t *testing.T) {
	kb := &Keyboard{}
	feed := func(in string) string {
		out, rest := kb.filter([]byte(in))
		if len(rest) > 0 {
			t.Fatalf("sobrou %q", rest)
		}
		return string(out)
	}
	if out := feed("\x1b[?3u"); out != "" || !kb.Active() {
		t.Fatalf("resposta do protocolo deveria ativar e sumir: %q", out)
	}
	if out := feed("\x1b[A"); out != "\x1b[A" || !kb.Down(actUp) {
		t.Fatalf("apertou ↑: %q", out)
	}
	if out := feed("\x1b[1;1:2A"); out != "\x1b[A" || !kb.Down(actUp) {
		t.Fatalf("repetição de ↑ vira seta clássica: %q", out)
	}
	if out := feed("\x1b[1;1:3A"); out != "" || kb.Down(actUp) {
		t.Fatalf("soltou ↑ deveria sumir e marcar solto: %q", out)
	}
	if out := feed("\x1b[1;5:1D"); out != "\x1b[1;5D" || !kb.Down(actLeft) {
		t.Fatalf("ctrl+← apertado: %q", out)
	}
	if out := feed("\x1b[13;1:3u" + "q" + "\x1b[13u"); out != "q\x1b[13u" {
		t.Fatalf("soltar Enter some, resto passa: %q", out)
	}
	// sequência cortada entre duas leituras
	out, rest := kb.filter([]byte("w\x1b[1;1"))
	if string(out) != "w" || string(rest) != "\x1b[1;1" {
		t.Fatalf("corte: out=%q rest=%q", out, rest)
	}
	out, rest = kb.filter(append(rest, ":3D"...))
	if len(out) != 0 || len(rest) != 0 || kb.Down(actLeft) {
		t.Fatalf("soltou ← após corte: out=%q rest=%q", out, rest)
	}
}

func TestFiltroTerminalComum(t *testing.T) {
	kb := &Keyboard{}
	out, rest := kb.filter([]byte("\x1b[A\x1b"))
	if string(out) != "\x1b[A\x1b" || len(rest) != 0 || kb.Active() {
		t.Fatalf("sem protocolo: repassa tudo, inclusive ESC sozinho: %q %q", out, rest)
	}
}

func driving(t *testing.T) *Game {
	g := trafficGame(t)
	g.cars = nil
	g.cruise = false
	g.speed = maxSpeed * 0.5
	return g
}

func TestSoltouDesacelera(t *testing.T) {
	g := driving(t)
	g.kb = &Keyboard{active: true}
	g.kb.down[actUp] = true
	g.Update(1.0 / 60)
	if !g.throttle {
		t.Fatal("↑ apertado deveria acelerar")
	}
	g.kb.down[actLeft] = true // virando com ↑ apertado
	g.Update(1.0 / 60)
	if !g.throttle {
		t.Fatal("virando com ↑ apertado continua acelerando")
	}
	g.kb.down[actUp] = false // soltou ↑ no meio da curva
	v := g.speed
	g.Update(1.0 / 60)
	if g.throttle || g.speed >= v {
		t.Fatal("soltou ↑: deveria desacelerar mesmo virando")
	}
}

func TestHeuristicaCurva(t *testing.T) {
	g := driving(t)
	now := time.Now()
	g.keys[actUp] = keyState{last: now, repeats: 5}
	g.keys[actLeft] = keyState{last: now, repeats: 0}
	g.Update(1.0 / 60)
	if !g.throttle {
		t.Fatal("↑ + ← deveria acelerar")
	}
	g.keys[actUp] = keyState{last: now.Add(-time.Second), repeats: 5} // terminal parou de repetir ↑
	g.keys[actLeft] = keyState{last: now, repeats: 3}
	g.Update(1.0 / 60)
	if !g.throttle {
		t.Fatal("virando depois de ↑: assume ↑ ainda apertado")
	}
	g.keys[actLeft] = keyState{last: now.Add(-time.Second), repeats: 3} // soltou tudo
	g.Update(1.0 / 60)
	if g.throttle {
		t.Fatal("soltou tudo: deveria desacelerar")
	}
	g.keys[actLeft] = keyState{last: now, repeats: 0} // virar sem ↑ não acelera
	g.Update(1.0 / 60)
	if g.throttle {
		t.Fatal("virar sem ter acelerado não deveria acelerar")
	}
}

func TestPilotoDeVelocidade(t *testing.T) {
	g := driving(t)
	g.toggleCruise()
	g.Update(1.0 / 60)
	if !g.throttle {
		t.Fatal("piloto ligado acelera sem tecla")
	}
	g.keys[actDown] = keyState{last: time.Now()}
	g.Update(1.0 / 60)
	if g.cruise {
		t.Fatal("frear desliga o piloto")
	}
}
