package main

import (
	"strconv"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/gdamore/tcell/v2/terminfo"
)

// Terminais comuns não avisam quando uma tecla é solta. Terminais com o
// protocolo de teclado do kitty (kitty, WezTerm, Ghostty, foot, Alacritty…)
// avisam, se pedirmos a flag "report event types". O tcell liga só a flag
// básica e ignora soltar, então interceptamos a entrada antes dele:
// registramos apertou/soltou das setas e repassamos a ele só o que entende.

const (
	kittyPush  = "\x1b[>3u\x1b[?u" // flags: desambiguar + tipos de evento; pergunta se suporta
	kittyPop   = "\x1b[<u"
	evPress    = 1
	evRepeat   = 2
	evRelease  = 3
	arrowFinal = "ABCD"
)

var arrowAction = map[byte]action{'A': actUp, 'B': actDown, 'C': actRight, 'D': actLeft}

// Keyboard guarda o estado real das setas (quando o terminal informa).
type Keyboard struct {
	mu     sync.Mutex
	down   [nActions]bool
	active bool // terminal confirmou o protocolo
}

func (kb *Keyboard) Active() bool {
	if kb == nil {
		return false
	}
	kb.mu.Lock()
	defer kb.mu.Unlock()
	return kb.active
}

func (kb *Keyboard) Down(a action) bool {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	return kb.down[a]
}

// filter processa bytes do terminal: devolve o que vai pro tcell e o resto
// incompleto (sequência cortada entre duas leituras).
func (kb *Keyboard) filter(in []byte) (out, rest []byte) {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	for i := 0; i < len(in); {
		if in[i] != 0x1b {
			out = append(out, in[i])
			i++
			continue
		}
		if i+1 >= len(in) { // ESC no fim: pode ser início de sequência
			if kb.active {
				return out, in[i:]
			}
			return append(out, in[i]), nil
		}
		if in[i+1] != '[' {
			out = append(out, in[i])
			i++
			continue
		}
		j := i + 2
		for j < len(in) && in[j] >= 0x20 && in[j] <= 0x3f { // parâmetros e intermediários
			j++
		}
		if j >= len(in) {
			return out, in[i:]
		}
		seq := in[i : j+1]
		out = append(out, kb.csi(string(in[i+2:j]), in[j], seq)...)
		i = j + 1
	}
	return out, nil
}

// csi trata uma sequência CSI; devolve o que repassar ao tcell.
func (kb *Keyboard) csi(params string, final byte, seq []byte) []byte {
	if final == 'u' && strings.HasPrefix(params, "?") { // resposta a "\x1b[?u"
		kb.active = true
		return nil
	}
	event, mod := evPress, ""
	parts := strings.Split(params, ";")
	if len(parts) >= 2 {
		mod = parts[1]
		if m, ev, ok := strings.Cut(mod, ":"); ok {
			kb.active = true
			mod = m
			if n, err := strconv.Atoi(ev); err == nil {
				event = n
			}
			parts[1] = m
		}
	}
	if strings.IndexByte(arrowFinal, final) >= 0 && (params == "" || parts[0] == "1") {
		kb.down[arrowAction[final]] = event != evRelease
	}
	if event == evRelease {
		return nil // tcell não entende "soltou"
	}
	if !strings.Contains(params, ":") {
		return seq
	}
	if strings.IndexByte(arrowFinal, final) >= 0 && (mod == "" || mod == "1") {
		return []byte{0x1b, '[', final} // seta sem modificador: forma clássica
	}
	return []byte("\x1b[" + strings.Join(parts, ";") + string(final))
}

// kittyTty envolve um tcell.Tty filtrando a entrada pelo Keyboard.
type kittyTty struct {
	tcell.Tty
	kb        *Keyboard
	pend, out []byte
}

func (t *kittyTty) Read(p []byte) (int, error) {
	for len(t.out) == 0 {
		buf := make([]byte, 512)
		n, err := t.Tty.Read(buf)
		if n > 0 {
			t.out, t.pend = t.kb.filter(append(t.pend, buf[:n]...))
		}
		if err != nil || n == 0 {
			if len(t.out) == 0 {
				return 0, err
			}
			break
		}
	}
	n := copy(p, t.out)
	t.out = t.out[n:]
	return n, nil
}

// openScreen cria a tela sobre tty com o filtro de teclado e liga o protocolo.
func openScreen(tty tcell.Tty, ti *terminfo.Terminfo) (tcell.Screen, *Keyboard, error) {
	kb := &Keyboard{}
	kt := &kittyTty{Tty: tty, kb: kb}
	var s tcell.Screen
	var err error
	if ti != nil {
		s, err = tcell.NewTerminfoScreenFromTtyTerminfo(kt, ti)
	} else {
		s, err = tcell.NewTerminfoScreenFromTty(kt)
	}
	if err == nil {
		err = s.Init()
	}
	if err != nil {
		return nil, nil, err
	}
	_, _ = tty.Write([]byte(kittyPush))
	return s, kb, nil
}

// closeScreen desliga o protocolo (tira nossas flags da pilha) e fecha a tela.
func closeScreen(s tcell.Screen, tty tcell.Tty) {
	_, _ = tty.Write([]byte(kittyPop))
	s.Fini()
}
