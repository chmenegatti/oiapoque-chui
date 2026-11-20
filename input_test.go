package main

import (
	"os"
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

// jogoDeTeste: BR-156 no km 20, sem tráfego, a meia velocidade.
func jogoDeTeste(t *testing.T) *Game {
	routes, err := LoadRoutes(os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	g := NewGame(routes[0], 1000, 30, nil)
	g.Pos = 20 * g.Upk
	g.Cars = nil
	g.Speed = MaxSpeed * 0.5
	return g
}

func TestSoltouDesacelera(t *testing.T) {
	g := jogoDeTeste(t)
	in := &termInput{kb: &Keyboard{active: true}}
	step := func() { g.Update(1.0/60, in.controls(time.Now())) }
	in.kb.down[actUp] = true
	step()
	if !g.Throttle {
		t.Fatal("↑ apertado deveria acelerar")
	}
	in.kb.down[actLeft] = true // virando com ↑ apertado
	step()
	if !g.Throttle {
		t.Fatal("virando com ↑ apertado continua acelerando")
	}
	in.kb.down[actUp] = false // soltou ↑ no meio da curva
	v := g.Speed
	step()
	if g.Throttle || g.Speed >= v {
		t.Fatal("soltou ↑: deveria desacelerar mesmo virando")
	}
}

func TestHeuristicaCurva(t *testing.T) {
	now := time.Now()
	in := &termInput{}
	in.keys[actUp] = keyState{last: now, repeats: 5}
	in.keys[actLeft] = keyState{last: now, repeats: 0}
	if c := in.controls(now); !c.Up || !c.Left {
		t.Fatalf("↑ + ← deveria acelerar e virar: %+v", c)
	}
	in.keys[actUp] = keyState{last: now.Add(-time.Second), repeats: 5} // terminal parou de repetir ↑
	in.keys[actLeft] = keyState{last: now, repeats: 3}
	if c := in.controls(now); !c.Up {
		t.Fatal("virando depois de ↑: assume ↑ ainda apertado")
	}
	in.keys[actLeft] = keyState{last: now.Add(-time.Second), repeats: 3} // soltou tudo
	if c := in.controls(now); c.Up {
		t.Fatal("soltou tudo: deveria desacelerar")
	}
	in.keys[actLeft] = keyState{last: now, repeats: 0} // virar sem ↑ não acelera
	if c := in.controls(now); c.Up {
		t.Fatal("virar sem ter acelerado não deveria acelerar")
	}
}
