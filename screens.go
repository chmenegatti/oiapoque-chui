package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// ---------- progresso salvo ----------

type Save struct {
	Trecho    int             `json:"trecho"`
	Dinheiro  float64         `json:"dinheiro"`
	Gasolina  float64         `json:"gasolina"`
	Melhorias map[string]bool `json:"melhorias"`
	Tempo     float64         `json:"tempo"`
	Multas    int             `json:"multas"`
	Parada    *Checkpoint     `json:"parada,omitempty"`
}

func newSave() Save {
	return Save{Dinheiro: 500, Gasolina: 20, Melhorias: map[string]bool{}}
}

func savePath() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "oiapoque-chui", "save.json")
}

func loadSave(path string) Save {
	s := newSave()
	if path == "" {
		return s
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Melhorias == nil {
		s.Melhorias = map[string]bool{}
	}
	return s
}

func (s Save) write(p string) error {
	if p == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

// ---------- oficina ----------

type upgrade struct {
	id, nome, desc string
	preco          float64
}

var upgrades = []upgrade{
	{"motor", "Motor 1.3 envenenado", "+15 km/h de final e arranque, esquenta mais", 900},
	{"radiador", "Radiador novo", "motor esquenta 20% menos", 400},
	{"tanque", "Tanque extra", "+12 L de autonomia", 350},
	{"pneus", "Pneus de chuva", "escorrega bem menos na chuva e na terra", 450},
	{"farol", "Farol de milha", "enxerga o dobro à noite", 250},
}

// ---------- app ----------

type screen int

const (
	scTitle screen = iota
	scDrive
	scShop
	scFinal
)

type App struct {
	path    string    // arquivo de save ("" = não salva)
	kb      *Keyboard // estado real do teclado (nil em testes)
	routes  []*Route
	save    Save
	scr     screen
	game    *Game
	sel     int
	confirm bool
	note    string
	bg      *Frame
}

func NewApp(routes []*Route, path string) *App {
	return &App{routes: routes, path: path, save: loadSave(path)}
}

func (a *App) persist() {
	if err := a.save.write(a.path); err != nil {
		a.note = "erro ao salvar: " + err.Error()
	}
}

func (a *App) done() bool { return a.save.Trecho >= len(a.routes) }

func (a *App) startTrecho() {
	rt := a.routes[a.save.Trecho]
	a.game = NewGame(rt, a.save.Dinheiro, a.save.Gasolina, a.save.Melhorias)
	a.game.kb = a.kb
	if cp := a.save.Parada; cp != nil && cp.Trecho == a.save.Trecho {
		a.game.restore(cp)
	}
	a.scr = scDrive
}

// saveStop grava a parada no meio do trecho (se estiver dirigindo).
func (a *App) saveStop() {
	if a.scr == scDrive && a.game.state == playing {
		a.save.Parada = a.game.checkpoint(a.save.Trecho)
		a.persist()
	}
}

// Close salva antes de encerrar (Ctrl-C, desconexão ssh).
func (a *App) Close() { a.saveStop() }

func (a *App) finishTrecho() {
	g := a.game
	a.save.Dinheiro = g.money
	a.save.Gasolina = g.fuel
	a.save.Tempo += g.elapsed
	a.save.Multas += g.fines
	a.save.Trecho++
	a.save.Parada = nil
	a.persist()
	a.sel, a.note = 0, ""
	if a.done() {
		a.scr = scFinal
	} else {
		a.scr = scShop
	}
}

// key devolve true para sair do programa.
func (a *App) key(ev *tcell.EventKey) bool {
	k, r := ev.Key(), ev.Rune()
	if k == tcell.KeyCtrlC {
		a.saveStop()
		return true
	}
	enter := k == tcell.KeyEnter || r == ' '
	switch a.scr {
	case scTitle:
		if a.confirm {
			if r == 's' {
				a.save = newSave()
				a.persist()
			}
			a.confirm = false
			return false
		}
		switch {
		case k == tcell.KeyEscape || r == 'q':
			return true
		case r == 'n' || (enter && a.done()):
			a.confirm = true
		case enter:
			a.startTrecho()
		}
	case scDrive:
		g := a.game
		switch {
		case k == tcell.KeyEscape || r == 'q':
			if g.state == arrived {
				a.finishTrecho()
			} else {
				a.saveStop()
			}
			a.scr = scTitle
		case r == 'p' && g.state == playing:
			g.paused = !g.paused
		case g.paused:
		case r == 'c' && g.state == playing:
			g.toggleCruise()
		case g.state == arrived && enter:
			a.finishTrecho()
		case g.state == outOfFuel && r == 'g':
			g.tow()
		case g.state == outOfFuel && r == 'r':
			a.save.Parada = nil
			a.persist()
			a.startTrecho()
		case g.state == playing:
			g.press(ev)
		}
	case scShop:
		n := len(upgrades) + 2 // + encher tanque + seguir
		switch {
		case k == tcell.KeyEscape || r == 'q':
			a.scr = scTitle
		case k == tcell.KeyUp || r == 'w':
			a.sel = (a.sel + n - 1) % n
		case k == tcell.KeyDown || r == 's':
			a.sel = (a.sel + 1) % n
		case enter:
			a.shopSelect()
		}
	case scFinal:
		if enter || k == tcell.KeyEscape || r == 'q' {
			a.scr = scTitle
		}
	}
	return false
}

func (a *App) tank() float64 { return makeVehicle(&Route{}, a.save.Melhorias).tank }

func (a *App) shopSelect() {
	sv := &a.save
	switch {
	case a.sel < len(upgrades):
		u := upgrades[a.sel]
		switch {
		case sv.Melhorias[u.id]:
			a.note = "Já tá instalado."
		case sv.Dinheiro < u.preco:
			a.note = "O dinheiro não dá, chefe."
		default:
			sv.Dinheiro -= u.preco
			sv.Melhorias[u.id] = true
			a.note = u.nome + " instalado!"
			a.persist()
		}
	case a.sel == len(upgrades):
		price := a.routes[sv.Trecho].Gasolina
		l := math.Min(a.tank()-sv.Gasolina, math.Max(sv.Dinheiro, 0)/price)
		if l <= 0.05 {
			a.note = "Tanque já tá cheio (ou o bolso vazio)."
			return
		}
		sv.Gasolina += l
		sv.Dinheiro -= l * price
		a.note = fmt.Sprintf("Colocou %.0f L por R$ %.0f.", l, l*price)
		a.persist()
	default:
		a.startTrecho()
	}
}

func (a *App) tick(dt float64) {
	if a.scr == scDrive {
		a.game.Update(dt)
		if a.game.cityPassed {
			a.game.cityPassed = false
			a.saveStop()
		}
	}
}

func (a *App) draw(s tcell.Screen) {
	if a.scr == scDrive {
		a.game.Draw(s)
		return
	}
	cols, rows := s.Size()
	a.backdrop(s, cols, rows)
	var lines []string
	foot := ""
	switch a.scr {
	case scTitle:
		lines, foot = a.titleLines()
	case scShop:
		lines, foot = a.shopLines()
	case scFinal:
		lines, foot = a.finalLines()
	}
	w := 0
	for _, l := range lines {
		w = max(w, runeLen(l))
	}
	x0 := max(0, (cols-w-4)/2)
	y0 := max(1, rows/2-len(lines)/2-4)
	for i, l := range lines {
		st := stMsg
		if i == 0 {
			st = stTitle
		}
		drawText(s, x0, y0+i, "  "+pad(l, w)+"  ", st)
	}
	if a.note != "" {
		center(s, cols, y0+len(lines)+1, a.note, stTitle)
	}
	center(s, cols, rows-2, foot, stMsg)
}

func (a *App) titleLines() ([]string, string) {
	sv := a.save
	lines := []string{
		"OIAPOQUE AO CHUÍ",
		"um Fiat 147 atravessando o Brasil",
		"",
	}
	for i, rt := range a.routes {
		mark := "  "
		switch {
		case i < sv.Trecho:
			mark = "✓ "
		case i == sv.Trecho:
			mark = "▶ "
		}
		km := fmt.Sprintf("%4.0f km", rt.KmTotal)
		if cp := sv.Parada; cp != nil && i == sv.Trecho && cp.Trecho == i {
			km = fmt.Sprintf("%4.0f km  (parado no km %.0f)", rt.KmTotal, cp.Km)
		}
		via := rt.Nome
		if rt.boat() {
			via = "balsa"
		}
		lines = append(lines, fmt.Sprintf("%s%02d %s %s %s", mark, i+1,
			pad(rt.Origem+" → "+rt.Destino, 30), pad(via, 26), km))
	}
	t := int(sv.Tempo)
	lines = append(lines, "", fmt.Sprintf("R$ %.0f · %.0f L no tanque · tempo %d:%02d:%02d · multas %d",
		sv.Dinheiro, sv.Gasolina, t/3600, t/60%60, t%60, sv.Multas))
	foot := fmt.Sprintf("Enter joga o trecho %02d · n nova viagem · q sai", sv.Trecho+1)
	if cp := sv.Parada; cp != nil && cp.Trecho == sv.Trecho {
		foot = fmt.Sprintf("Enter continua do km %.0f · n nova viagem · q sai", cp.Km)
	}
	if a.done() {
		foot = "Viagem completa! n começa outra · q sai"
	}
	if a.kb.Active() {
		lines = append(lines, "teclado: protocolo kitty ✓ · soltou ↑, desacelera")
	} else {
		lines = append(lines, "teclado: modo compatível · segure ↑ (virando, segue acelerando) · c = piloto de velocidade")
	}
	if a.confirm {
		foot = "Apagar o progresso e começar do Oiapoque? s confirma · outra tecla cancela"
	}
	return lines, foot
}

func (a *App) shopLines() ([]string, string) {
	sv := a.save
	next := a.routes[sv.Trecho]
	prev := a.routes[sv.Trecho-1]
	lines := []string{
		"OFICINA DO SEU ZÉ · " + prev.Destino,
		fmt.Sprintf("R$ %.0f no bolso · %.0f/%.0f L · próximo: %s → %s", sv.Dinheiro, sv.Gasolina, a.tank(), next.Origem, next.Destino),
		"",
	}
	cur := func(i int) string {
		if i == a.sel {
			return "▶ "
		}
		return "  "
	}
	for i, u := range upgrades {
		box := "[ ]"
		if sv.Melhorias[u.id] {
			box = "[x]"
		}
		lines = append(lines, fmt.Sprintf("%s%s %s R$ %4.0f  %s", cur(i), box, pad(u.nome, 22), u.preco, u.desc))
	}
	lines = append(lines, "",
		fmt.Sprintf("%sEncher o tanque (R$ %.2f/L)", cur(len(upgrades)), next.Gasolina),
		cur(len(upgrades)+1)+"Seguir viagem →")
	return lines, "↑↓ escolhe · Enter compra/segue · q menu"
}

func (a *App) finalLines() ([]string, string) {
	sv := a.save
	t := int(sv.Tempo)
	km := 0.0
	for _, rt := range a.routes {
		km += rt.KmTotal
	}
	return []string{
		"CHEGOU NO CHUÍ!",
		"",
		fmt.Sprintf("Do Oiapoque ao Chuí: %.0f km em %d trechos", km, len(a.routes)),
		fmt.Sprintf("tempo de jogo %d:%02d:%02d · multas %d · sobrou R$ %.0f", t/3600, t/60%60, t%60, sv.Multas, sv.Dinheiro),
		"",
		"O 147 merece uma lavagem.",
	}, "Enter volta ao menu"
}

// backdrop: pôr do sol, estrada e o 147 de costas.
func (a *App) backdrop(s tcell.Screen, cols, rows int) {
	W, H := cols, rows*2
	if a.bg == nil || a.bg.W != W || a.bg.H != H {
		a.bg = NewFrame(W, H)
	}
	f := a.bg
	zen, hor := skyColors(0.755)
	horizon := H * 3 / 5
	for y := range H {
		if y < horizon {
			t := float64(y) / float64(horizon)
			f.HLine(0, W-1, y, mix(zen, hor, t*t))
			continue
		}
		t := float64(y-horizon) / float64(H-horizon)
		f.HLine(0, W-1, y, scale(gGreen[0], 0.5))
		hw := lerp(1, float64(W)*0.45, t)
		f.Span(float64(W)/2-hw, float64(W)/2+hw, y, scale(rAsphalt[0], 0.6))
		if int(t*12)%2 == 0 {
			f.Span(float64(W)/2-math.Max(hw/40, 0.5), float64(W)/2+math.Max(hw/40, 0.5), y, scale(paint, 0.6))
		}
	}
	f.Sprite(spr147, float64(W)/2, float64(H), float64(W)/5, float64(H)+1, nil,
		func(ch byte, c RGB) RGB {
			if ch == 'R' {
				return RGB{220, 50, 40}
			}
			return scale(c, 0.6)
		})
	f.Flush(s)
}

func runeLen(s string) int { return len([]rune(s)) }

func pad(s string, n int) string { return s + strings.Repeat(" ", max(0, n-runeLen(s))) }
