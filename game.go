package main

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/gdamore/tcell/v2"
)

const (
	maxSpeed     = segLen * 60 // 1 segmento por frame a 60 FPS (= velocidade base máx)
	accel        = maxSpeed / 6
	braking      = -maxSpeed
	decel        = -maxSpeed / 5
	offRoadDecel = -maxSpeed / 2
	offRoadLimit = maxSpeed / 4
	centrifugal  = 0.3
	gravity      = 0.3 // quanto a inclinação freia/empurra
	carW         = 0.45
	tankL        = 38.0
	nTraffic     = 20
	guinchoPreco = 300.0
	guinchoL     = 8.0
	radarLimite  = 80.0
)

var playerZ = camHeight * camDepth

type action int

const (
	actUp action = iota
	actDown
	actLeft
	actRight
	nActions
)

// Terminal não manda "soltou tecla": consideramos segurada enquanto chegam
// repetições. A 1ª repetição demora (delay do SO), por isso a janela maior.
type keyState struct {
	last    time.Time
	repeats int
}

func (k keyState) held(now time.Time) bool {
	win := 120 * time.Millisecond
	if k.repeats == 0 {
		win = 600 * time.Millisecond
	}
	return !k.last.IsZero() && now.Sub(k.last) < win
}

type state int

const (
	playing state = iota
	arrived
	outOfFuel
)

// vehicle: o 147 (com melhorias da oficina) ou a balsa.
type vehicle struct {
	top, accel, heat, tank, width float64
	grip                          float64 // multiplica o escorregão extra (chuva/terra)
	headlight                     float64 // alcance do farol em segmentos
	spr                           *Sprite
}

func makeVehicle(rt *Route, up map[string]bool) vehicle {
	if rt.boat() {
		return vehicle{top: boatTop, accel: maxSpeed / 10, width: 0.9, grip: 1, headlight: 60, spr: sprBalsa}
	}
	v := vehicle{top: carTop, accel: accel, heat: 1, tank: tankL, width: carW, grip: 1, headlight: 40, spr: spr147}
	if up["motor"] {
		v.top, v.accel, v.heat = 155, v.accel*1.25, v.heat*1.1
	}
	if up["radiador"] {
		v.heat *= 0.8
	}
	if up["tanque"] {
		v.tank += 12
	}
	if up["pneus"] {
		v.grip = 0.5
	}
	if up["farol"] {
		v.headlight = 90
	}
	return v
}

type Game struct {
	route *Route
	road  *Road
	veh   vehicle

	upk, vmax, endZ float64

	pos, playerX, speed      float64
	fuel, temp               float64
	tod, elapsed             float64
	skyOff, bmp              float64
	fogD, rainD, hillsK      float64
	money, paid              float64
	fines                    int
	braking, throttle        bool
	overheat, beep, gotPrize bool
	paused, cityPassed       bool
	cruise, steerKeep        bool      // piloto de velocidade; heurística de curva
	kb                       *Keyboard // estado real das setas (protocolo kitty)
	state                    state

	msg                         string
	msgT                        float64
	nextCity, terrain, nextRadr int
	warnedRadr                  int

	cars  []Car
	keys  [nActions]keyState
	frame *Frame
	proj  [drawDist]proj
	rng   *rand.Rand
}

func NewGame(rt *Route, money, fuel float64, up map[string]bool) *Game {
	g := &Game{
		route:  rt,
		road:   buildRoad(rt),
		veh:    makeVehicle(rt, up),
		upk:    rt.unitsPerKm(),
		money:  money,
		temp:   70,
		tod:    rt.Saida / 24,
		fogD:   2,
		hillsK: 1,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	g.vmax = maxSpeed * g.veh.top / rt.baseTop()
	g.fuel = fuel
	if !rt.boat() {
		g.fuel = math.Min(fuel, g.veh.tank)
	}
	g.endZ = rt.KmTotal*g.upk - playerZ
	g.cars = make([]Car, nTraffic)
	for i := range g.cars {
		g.spawn(&g.cars[i], playerZ+segLen*float64(10+g.rng.Intn(2*drawDist)))
	}
	intro := rt.Origem + " → " + rt.Destino + " · " + rt.Nome
	if rt.boat() {
		g.money -= rt.Passagem
		intro += fmt.Sprintf(" · passagem R$ %.0f · desvie dos troncos!", rt.Passagem)
	}
	g.say(intro+"   segure ↑ acelera · ↓ freia · ←→ dirige · c piloto de velocidade · p pausa", 8)
	return g
}

func (g *Game) say(s string, secs float64) { g.msg, g.msgT = s, secs }

func (g *Game) km() float64  { return (g.pos + playerZ) / g.upk }
func (g *Game) kmh() float64 { return g.speed / maxSpeed * g.route.baseTop() }

// tow: guincho leva até o próximo posto... na prática, põe uns litros.
func (g *Game) tow() bool {
	if g.state != outOfFuel || g.money < guinchoPreco {
		return false
	}
	g.money -= guinchoPreco
	g.fuel = guinchoL
	g.state = playing
	g.say(fmt.Sprintf("Guincho: R$ %.0f e %.0f L no tanque. Vai com calma até o posto!", guinchoPreco, guinchoL), 5)
	return true
}

func (g *Game) press(ev *tcell.EventKey) {
	a := action(-1)
	switch ev.Key() {
	case tcell.KeyUp:
		a = actUp
	case tcell.KeyDown:
		a = actDown
	case tcell.KeyLeft:
		a = actLeft
	case tcell.KeyRight:
		a = actRight
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'w':
			a = actUp
		case 's':
			a = actDown
		case 'a':
			a = actLeft
		case 'd':
			a = actRight
		}
	}
	if a < 0 {
		return
	}
	now := time.Now()
	k := &g.keys[a]
	if k.held(now) {
		k.repeats++
	} else {
		k.repeats = 0
	}
	k.last = now
	// direções opostas se cancelam
	g.keys[a^1] = keyState{}
	switch a {
	case actDown:
		g.cruise = false
	case actLeft, actRight:
		// sem aviso de "soltou", o terminal para de repetir ↑ quando a seta
		// de direção é apertada: assume ↑ ainda apertado enquanto vira
		if g.keys[actUp].held(now) || g.steerKeep {
			g.steerKeep = true
		}
	}
}

// toggleCruise liga/desliga o piloto de velocidade (acelerador travado).
func (g *Game) toggleCruise() {
	g.cruise = !g.cruise
	if g.cruise {
		g.say("Piloto de velocidade ligado · ↓ ou c desliga", 2)
	} else {
		g.say("Piloto de velocidade desligado", 1.5)
	}
}

// held: tecla apertada? Com o protocolo kitty, estado real das setas
// (w/a/s/d seguem pela repetição); senão, pela repetição do terminal.
func (g *Game) held(a action, now time.Time) bool {
	if g.kb.Active() && g.kb.Down(a) {
		return true
	}
	return g.keys[a].held(now)
}

func (g *Game) Update(dt float64) {
	if g.paused {
		return
	}
	g.msgT = math.Max(0, g.msgT-dt)
	if g.state != playing {
		g.speed = math.Max(0, g.speed+braking*0.3*dt)
		g.pos = math.Min(g.pos+g.speed*dt, g.road.length-drawDist*segLen)
		return
	}
	g.elapsed += dt
	now := time.Now()
	held := func(a action) bool { return g.held(a, now) }
	steering := held(actLeft) || held(actRight)
	up := held(actUp)
	if !g.kb.Active() {
		if up || !steering {
			g.steerKeep = up && steering
		}
		up = up || (g.steerKeep && steering)
	}
	g.throttle = up || g.cruise
	rt := g.route
	boat := rt.boat()
	seg := g.road.find(g.pos + playerZ)
	ter := seg.ter
	slope := (seg.y2 - seg.y1) / segLen
	pct := g.speed / maxSpeed
	dx := dt * 2 * math.Min(pct, 1)

	// direção: terra e chuva fazem escorregar mais na curva
	if held(actLeft) {
		g.playerX -= dx
	} else if held(actRight) {
		g.playerX += dx
	}
	slip := (ter.grip - 1) + 0.6*g.rainD
	g.playerX -= dx * pct * seg.curve * centrifugal * (1 + slip*g.veh.grip)

	// motor
	power := g.veh.accel
	if g.overheat {
		power *= 0.5
	}
	if !boat && g.fuel <= 0 {
		power = 0
	}
	g.braking = held(actDown)
	if g.braking {
		g.cruise = false
	}
	switch {
	case g.braking:
		g.speed += braking * dt
	case g.throttle && power > 0:
		g.speed += power * dt
	default:
		g.speed += decel * dt
	}
	g.speed -= slope * maxSpeed * gravity * dt
	if lim := g.vmax * ter.maxPct; g.speed > lim { // terra não deixa correr
		g.speed = math.Max(lim, g.speed+decel*2*dt)
	}
	if math.Abs(g.playerX) > 1 && g.speed > offRoadLimit {
		g.speed += offRoadDecel * dt
	}
	if boat {
		g.playerX = clamp(g.playerX, -1.15, 1.15) // margens
	} else {
		g.playerX = clamp(g.playerX, -3, 3)
	}
	g.speed = clamp(g.speed, 0, g.vmax)

	// colisão com cenário
	for _, p := range seg.props {
		if p.hit > 0 && math.Abs(p.x-g.playerX) < (p.hit+g.veh.width)/2 && g.speed > maxSpeed*0.05 {
			g.speed = maxSpeed * 0.05
			g.bmp = 0.5
			g.beep = true
			if boat {
				g.say("TRONCO! A balsa estremece…", 2.5)
			} else {
				g.say("PANCADA! O 147 é guerreiro, mas não abusa.", 2.5)
			}
		}
	}

	pz0 := g.pos + playerZ
	g.pos = math.Min(g.pos+g.speed*dt, g.endZ)
	g.traffic(dt, pz0, g.pos+playerZ)

	km := g.km()
	if !boat {
		g.engine(dt, pct, slope)
		g.gasStation(km, dt)
		g.radar(km)
	}

	// cidades e terrenos
	for g.nextCity < len(rt.Cidades) && km >= rt.Cidades[g.nextCity].Km {
		g.say("Bem-vindo a "+rt.Cidades[g.nextCity].Nome+" · progresso salvo", 3)
		g.nextCity++
		g.cityPassed = true
	}
	for g.terrain < len(rt.Terreno)-1 && km > rt.Terreno[g.terrain].AteKm {
		g.terrain++
		if t := rt.Terreno[g.terrain]; t.Nome != "" {
			g.say(t.Nome, 4)
		}
	}

	switch {
	case g.pos >= g.endZ:
		g.state = arrived
		g.money += rt.Premio
	case !boat && g.fuel <= 0 && g.speed < 1:
		g.state = outOfFuel
		g.beep = true
	}

	k := dt * 0.3
	g.fogD += (2 + 12*seg.fog - g.fogD) * k
	g.rainD += (seg.rain - g.rainD) * k
	g.hillsK += (ter.hills - g.hillsK) * k
	g.tod = wrap(g.tod+dt*rt.Escala/86400, 1)
	g.skyOff += seg.curve * pct * dt * 40
	g.bmp = math.Max(0, g.bmp-dt)
}

// engine: combustível e temperatura.
func (g *Game) engine(dt, pct, slope float64) {
	// acelerando ~10 km/l, embalado ~18 km/l
	dkm := g.kmh() * dt / 3600 * g.route.Escala
	rate := 18.0
	if g.throttle {
		rate = 10
	}
	g.fuel = math.Max(0, g.fuel-dkm/rate)

	// temperatura: sobe com pé embaixo, velocidade e subida; esfria à noite
	target := 78.0
	if g.throttle && g.fuel > 0 {
		target += (8 + 25*pct + 70*math.Max(slope, 0)) * g.veh.heat
	}
	if daylight(g.tod) < 0.5 {
		target -= 5
	}
	g.temp += (target - g.temp) * dt * 0.12
	if g.temp >= 118 && !g.overheat {
		g.overheat = true
		g.beep = true
		g.say("MOTOR FERVENDO! Alivia o pé até esfriar.", 4)
	}
	if g.overheat && g.temp < 95 {
		g.overheat = false
		g.say("Motor esfriou. Pode seguir.", 2.5)
	}
}

const (
	postoZona  = 0.5 // km antes/depois da bomba
	postoVel   = 25  // km/h máx para abastecer
	postoAviso = 2.0 // km de antecedência do aviso
)

// gasStation: chegar perto avisa; parar (< 25 km/h) na bomba abastece e cobra.
func (g *Game) gasStation(km, dt float64) {
	p, ok := g.nextPosto(km)
	if !ok {
		return
	}
	d := p - km
	switch {
	case d > postoZona && d < postoAviso && g.fuel < g.veh.tank-2:
		g.say(fmt.Sprintf("POSTO em %.1f km · freie (↓) e pare na bomba pra abastecer", d), 0.2)
	case math.Abs(d) <= postoZona && g.fuel < g.veh.tank-0.05:
		if g.kmh() >= postoVel {
			g.say("Devagar! Abaixo de 25 km/h pra abastecer", 0.2)
			return
		}
		dl := math.Min(8*dt, g.veh.tank-g.fuel)
		cost := dl * g.route.Gasolina
		if g.money < cost {
			g.say("Sem dinheiro pra gasolina!", 0.5)
			return
		}
		g.fuel += dl
		g.money -= cost
		g.paid += cost
		g.say(fmt.Sprintf("Abastecendo… %.0f/%.0f L · R$ %.0f  (↑ segue viagem)", g.fuel, g.veh.tank, g.paid), 0.4)
	case math.Abs(d) <= postoZona && g.paid > 0:
		g.say(fmt.Sprintf("Tanque cheio! Deu R$ %.0f. ↑ segue viagem", g.paid), 0.4)
	default:
		g.paid = 0
	}
}

func (g *Game) nextPosto(km float64) (float64, bool) {
	for _, p := range g.route.Postos {
		if p > km-postoZona {
			return p, true
		}
	}
	return 0, false
}

func (g *Game) radar(km float64) {
	rs := g.route.radares
	for g.warnedRadr < len(rs) && km >= rs[g.warnedRadr]-1.2 {
		g.warnedRadr++
		g.say(fmt.Sprintf("Placa: %.0f km/h · radar fixo em 1 km", radarLimite), 3)
	}
	for g.nextRadr < len(rs) && km >= rs[g.nextRadr] {
		g.nextRadr++
		if v := g.kmh(); v > radarLimite*1.1 {
			fine := 130.0
			if v > radarLimite*1.5 {
				fine = 880
			} else if v > radarLimite*1.2 {
				fine = 195
			}
			g.money -= fine
			g.fines++
			g.beep = true
			g.say(fmt.Sprintf("FLASH! Radar pegou a %.0f km/h (limite %.0f). Multa R$ %.0f", v, radarLimite, fine), 4)
		}
	}
}

// Checkpoint: parada no meio de um trecho (salva a cada cidade e ao sair).
type Checkpoint struct {
	Trecho      int     `json:"trecho"`
	Km          float64 `json:"km"`
	Combustivel float64 `json:"combustivel"`
	Dinheiro    float64 `json:"dinheiro"`
	Hora        float64 `json:"hora"`
	Tempo       float64 `json:"tempo"`
	Temp        float64 `json:"temp"`
	Multas      int     `json:"multas"`
}

func (g *Game) checkpoint(trecho int) *Checkpoint {
	return &Checkpoint{Trecho: trecho, Km: g.km(), Combustivel: g.fuel, Dinheiro: g.money,
		Hora: g.tod, Tempo: g.elapsed, Temp: g.temp, Multas: g.fines}
}

// restore continua do checkpoint, sem repetir avisos de cidade/radar já passados.
func (g *Game) restore(cp *Checkpoint) {
	rt := g.route
	g.pos = math.Min(cp.Km*g.upk-playerZ, g.endZ)
	g.fuel, g.money, g.tod = cp.Combustivel, cp.Dinheiro, cp.Hora
	g.elapsed, g.temp, g.fines = cp.Tempo, cp.Temp, cp.Multas
	km := g.km()
	for g.nextCity < len(rt.Cidades) && km >= rt.Cidades[g.nextCity].Km {
		g.nextCity++
	}
	for g.terrain < len(rt.Terreno)-1 && km > rt.Terreno[g.terrain].AteKm {
		g.terrain++
	}
	for g.nextRadr < len(rt.radares) && km >= rt.radares[g.nextRadr] {
		g.nextRadr++
	}
	for g.warnedRadr < len(rt.radares) && km >= rt.radares[g.warnedRadr]-1.2 {
		g.warnedRadr++
	}
	seg := g.road.find(g.pos + playerZ)
	g.fogD, g.rainD, g.hillsK = 2+12*seg.fog, seg.rain, seg.ter.hills
	for i := range g.cars {
		g.spawn(&g.cars[i], g.pos+playerZ+segLen*float64(10+g.rng.Intn(2*drawDist)))
	}
	g.say(fmt.Sprintf("Continuando do km %.0f · p pausa", km), 4)
}
