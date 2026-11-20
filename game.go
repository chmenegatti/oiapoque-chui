package main

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

const (
	MaxSpeed     = SegLen * 60 // 1 segmento por frame a 60 FPS (= velocidade base máx)
	accel        = MaxSpeed / 6
	braking      = -MaxSpeed
	decel        = -MaxSpeed / 5
	offRoadDecel = -MaxSpeed / 2
	offRoadLimit = MaxSpeed / 4
	centrifugal  = 0.3
	gravity      = 0.3 // quanto a inclinação freia/empurra
	carW         = 0.45
	tankL        = 38.0
	nTraffic     = 20
	GuinchoPreco = 300.0
	GuinchoL     = 8.0
	RadarLimite  = 80.0
)

var PlayerZ = CamHeight * CamDepth

// Controls é o estado dos comandos de direção num quadro. Cada frontend
// (terminal, janela 3D) traduz o seu teclado/controle para isto.
type Controls struct {
	Up, Down, Left, Right bool
}

type state int

const (
	Playing state = iota
	Arrived
	OutOfFuel
)

// vehicle: o 147 (com melhorias da oficina) ou a balsa.
type Vehicle struct {
	top, accel, heat, Tank, Width float64
	grip                          float64 // multiplica o escorregão extra (chuva/terra)
	Headlight                     float64 // alcance do farol em segmentos
	Boat                          bool
}

func MakeVehicle(rt *Route, up map[string]bool) Vehicle {
	if rt.Boat() {
		return Vehicle{top: boatTop, accel: MaxSpeed / 10, Width: 0.9, grip: 1, Headlight: 60, Boat: true}
	}
	v := Vehicle{top: carTop, accel: accel, heat: 1, Tank: tankL, Width: carW, grip: 1, Headlight: 40}
	if up["motor"] {
		v.top, v.accel, v.heat = 155, v.accel*1.25, v.heat*1.1
	}
	if up["radiador"] {
		v.heat *= 0.8
	}
	if up["tanque"] {
		v.Tank += 12
	}
	if up["pneus"] {
		v.grip = 0.5
	}
	if up["farol"] {
		v.Headlight = 90
	}
	return v
}

type Game struct {
	Route *Route
	Road  *Road
	Veh   Vehicle

	Upk, Vmax, EndZ float64

	Pos, PlayerX, Speed      float64
	Fuel, Temp               float64
	Tod, Elapsed             float64
	SkyOff, Bmp              float64
	FogD, RainD, HillsK      float64
	Money, paid              float64
	Fines                    int
	Braking, Throttle        bool
	Overheat, Beep, gotPrize bool
	Paused, CityPassed       bool
	Cruise                   bool // piloto de velocidade (acelerador travado)
	State                    state

	Msg                         string
	MsgT                        float64
	NextCity, terrain, nextRadr int
	warnedRadr                  int

	Cars []Car
	Rng  *rand.Rand
}

func NewGame(rt *Route, money, fuel float64, up map[string]bool) *Game {
	g := &Game{
		Route:  rt,
		Road:   buildRoad(rt),
		Veh:    MakeVehicle(rt, up),
		Upk:    rt.unitsPerKm(),
		Money:  money,
		Temp:   70,
		Tod:    rt.Saida / 24,
		FogD:   2,
		HillsK: 1,
		Rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	g.Vmax = MaxSpeed * g.Veh.top / rt.baseTop()
	g.Fuel = fuel
	if !rt.Boat() {
		g.Fuel = math.Min(fuel, g.Veh.Tank)
	}
	g.EndZ = rt.KmTotal*g.Upk - PlayerZ
	g.Cars = make([]Car, nTraffic)
	for i := range g.Cars {
		g.Spawn(&g.Cars[i], PlayerZ+SegLen*float64(10+g.Rng.Intn(2*DrawDist)))
	}
	intro := rt.Origem + " → " + rt.Destino + " · " + rt.Nome
	if rt.Boat() {
		g.Money -= rt.Passagem
		intro += fmt.Sprintf(" · passagem R$ %.0f · desvie dos troncos!", rt.Passagem)
	}
	g.say(intro+"   segure ↑ acelera · ↓ freia · ←→ dirige · c piloto de velocidade · p pausa", 8)
	return g
}

func (g *Game) say(s string, secs float64) { g.Msg, g.MsgT = s, secs }

func (g *Game) Km() float64  { return (g.Pos + PlayerZ) / g.Upk }
func (g *Game) Kmh() float64 { return g.Speed / MaxSpeed * g.Route.baseTop() }

// tow: guincho leva até o próximo posto... na prática, põe uns litros.
func (g *Game) Tow() bool {
	if g.State != OutOfFuel || g.Money < GuinchoPreco {
		return false
	}
	g.Money -= GuinchoPreco
	g.Fuel = GuinchoL
	g.State = Playing
	g.say(fmt.Sprintf("Guincho: R$ %.0f e %.0f L no tanque. Vai com calma até o posto!", GuinchoPreco, GuinchoL), 5)
	return true
}

// toggleCruise liga/desliga o piloto de velocidade (acelerador travado).
func (g *Game) ToggleCruise() {
	g.Cruise = !g.Cruise
	if g.Cruise {
		g.say("Piloto de velocidade ligado · ↓ ou c desliga", 2)
	} else {
		g.say("Piloto de velocidade desligado", 1.5)
	}
}

func (g *Game) Update(dt float64, in Controls) {
	if g.Paused {
		return
	}
	g.MsgT = math.Max(0, g.MsgT-dt)
	if g.State != Playing {
		g.Speed = math.Max(0, g.Speed+braking*0.3*dt)
		g.Pos = math.Min(g.Pos+g.Speed*dt, g.Road.length-DrawDist*SegLen)
		return
	}
	g.Elapsed += dt
	g.Throttle = in.Up || g.Cruise
	rt := g.Route
	boat := rt.Boat()
	seg := g.Road.Find(g.Pos + PlayerZ)
	ter := seg.Ter
	slope := (seg.Y2 - seg.Y1) / SegLen
	pct := g.Speed / MaxSpeed
	dx := dt * 2 * math.Min(pct, 1)

	// direção: terra e chuva fazem escorregar mais na curva
	if in.Left {
		g.PlayerX -= dx
	} else if in.Right {
		g.PlayerX += dx
	}
	slip := (ter.grip - 1) + 0.6*g.RainD
	g.PlayerX -= dx * pct * seg.Curve * centrifugal * (1 + slip*g.Veh.grip)

	// motor
	power := g.Veh.accel
	if g.Overheat {
		power *= 0.5
	}
	if !boat && g.Fuel <= 0 {
		power = 0
	}
	g.Braking = in.Down
	if g.Braking {
		g.Cruise = false
	}
	switch {
	case g.Braking:
		g.Speed += braking * dt
	case g.Throttle && power > 0:
		g.Speed += power * dt
	default:
		g.Speed += decel * dt
	}
	g.Speed -= slope * MaxSpeed * gravity * dt
	if lim := g.Vmax * ter.maxPct; g.Speed > lim { // terra não deixa correr
		g.Speed = math.Max(lim, g.Speed+decel*2*dt)
	}
	if math.Abs(g.PlayerX) > 1 && g.Speed > offRoadLimit {
		g.Speed += offRoadDecel * dt
	}
	if boat {
		g.PlayerX = Clamp(g.PlayerX, -1.15, 1.15) // margens
	} else {
		g.PlayerX = Clamp(g.PlayerX, -3, 3)
	}
	g.Speed = Clamp(g.Speed, 0, g.Vmax)

	// colisão com cenário
	for _, p := range seg.Props {
		if p.hit > 0 && math.Abs(p.X-g.PlayerX) < (p.hit+g.Veh.Width)/2 && g.Speed > MaxSpeed*0.05 {
			g.Speed = MaxSpeed * 0.05
			g.Bmp = 0.5
			g.Beep = true
			if boat {
				g.say("TRONCO! A balsa estremece…", 2.5)
			} else {
				g.say("PANCADA! O 147 é guerreiro, mas não abusa.", 2.5)
			}
		}
	}

	pz0 := g.Pos + PlayerZ
	g.Pos = math.Min(g.Pos+g.Speed*dt, g.EndZ)
	g.traffic(dt, pz0, g.Pos+PlayerZ)

	km := g.Km()
	if !boat {
		g.engine(dt, pct, slope)
		g.gasStation(km, dt)
		g.radar(km)
	}

	// cidades e terrenos
	for g.NextCity < len(rt.Cidades) && km >= rt.Cidades[g.NextCity].Km {
		g.say("Bem-vindo a "+rt.Cidades[g.NextCity].Nome+" · progresso salvo", 3)
		g.NextCity++
		g.CityPassed = true
	}
	for g.terrain < len(rt.Terreno)-1 && km > rt.Terreno[g.terrain].AteKm {
		g.terrain++
		if t := rt.Terreno[g.terrain]; t.Nome != "" {
			g.say(t.Nome, 4)
		}
	}

	switch {
	case g.Pos >= g.EndZ:
		g.State = Arrived
		g.Money += rt.Premio
	case !boat && g.Fuel <= 0 && g.Speed < 1:
		g.State = OutOfFuel
		g.Beep = true
	}

	k := dt * 0.3
	g.FogD += (2 + 12*seg.Fog - g.FogD) * k
	g.RainD += (seg.Rain - g.RainD) * k
	g.HillsK += (ter.Hills - g.HillsK) * k
	g.Tod = Wrap(g.Tod+dt*rt.Escala/86400, 1)
	g.SkyOff += seg.Curve * pct * dt * 40
	g.Bmp = math.Max(0, g.Bmp-dt)
}

// engine: combustível e temperatura.
func (g *Game) engine(dt, pct, slope float64) {
	// acelerando ~10 km/l, embalado ~18 km/l
	dkm := g.Kmh() * dt / 3600 * g.Route.Escala
	rate := 18.0
	if g.Throttle {
		rate = 10
	}
	g.Fuel = math.Max(0, g.Fuel-dkm/rate)

	// temperatura: sobe com pé embaixo, velocidade e subida; esfria à noite
	target := 78.0
	if g.Throttle && g.Fuel > 0 {
		target += (8 + 25*pct + 70*math.Max(slope, 0)) * g.Veh.heat
	}
	if Daylight(g.Tod) < 0.5 {
		target -= 5
	}
	g.Temp += (target - g.Temp) * dt * 0.12
	if g.Temp >= 118 && !g.Overheat {
		g.Overheat = true
		g.Beep = true
		g.say("MOTOR FERVENDO! Alivia o pé até esfriar.", 4)
	}
	if g.Overheat && g.Temp < 95 {
		g.Overheat = false
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
	p, ok := g.NextPosto(km)
	if !ok {
		return
	}
	d := p - km
	switch {
	case d > postoZona && d < postoAviso && g.Fuel < g.Veh.Tank-2:
		g.say(fmt.Sprintf("POSTO em %.1f km · freie (↓) e pare na bomba pra abastecer", d), 0.2)
	case math.Abs(d) <= postoZona && g.Fuel < g.Veh.Tank-0.05:
		if g.Kmh() >= postoVel {
			g.say("Devagar! Abaixo de 25 km/h pra abastecer", 0.2)
			return
		}
		dl := math.Min(8*dt, g.Veh.Tank-g.Fuel)
		cost := dl * g.Route.Gasolina
		if g.Money < cost {
			g.say("Sem dinheiro pra gasolina!", 0.5)
			return
		}
		g.Fuel += dl
		g.Money -= cost
		g.paid += cost
		g.say(fmt.Sprintf("Abastecendo… %.0f/%.0f L · R$ %.0f  (↑ segue viagem)", g.Fuel, g.Veh.Tank, g.paid), 0.4)
	case math.Abs(d) <= postoZona && g.paid > 0:
		g.say(fmt.Sprintf("Tanque cheio! Deu R$ %.0f. ↑ segue viagem", g.paid), 0.4)
	default:
		g.paid = 0
	}
}

func (g *Game) NextPosto(km float64) (float64, bool) {
	for _, p := range g.Route.Postos {
		if p > km-postoZona {
			return p, true
		}
	}
	return 0, false
}

func (g *Game) radar(km float64) {
	rs := g.Route.radares
	for g.warnedRadr < len(rs) && km >= rs[g.warnedRadr]-1.2 {
		g.warnedRadr++
		g.say(fmt.Sprintf("Placa: %.0f km/h · radar fixo em 1 km", RadarLimite), 3)
	}
	for g.nextRadr < len(rs) && km >= rs[g.nextRadr] {
		g.nextRadr++
		if v := g.Kmh(); v > RadarLimite*1.1 {
			fine := 130.0
			if v > RadarLimite*1.5 {
				fine = 880
			} else if v > RadarLimite*1.2 {
				fine = 195
			}
			g.Money -= fine
			g.Fines++
			g.Beep = true
			g.say(fmt.Sprintf("FLASH! Radar pegou a %.0f km/h (limite %.0f). Multa R$ %.0f", v, RadarLimite, fine), 4)
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

func (g *Game) Checkpoint(trecho int) *Checkpoint {
	return &Checkpoint{Trecho: trecho, Km: g.Km(), Combustivel: g.Fuel, Dinheiro: g.Money,
		Hora: g.Tod, Tempo: g.Elapsed, Temp: g.Temp, Multas: g.Fines}
}

// restore continua do checkpoint, sem repetir avisos de cidade/radar já passados.
func (g *Game) Restore(cp *Checkpoint) {
	rt := g.Route
	g.Pos = math.Min(cp.Km*g.Upk-PlayerZ, g.EndZ)
	g.Fuel, g.Money, g.Tod = cp.Combustivel, cp.Dinheiro, cp.Hora
	g.Elapsed, g.Temp, g.Fines = cp.Tempo, cp.Temp, cp.Multas
	km := g.Km()
	for g.NextCity < len(rt.Cidades) && km >= rt.Cidades[g.NextCity].Km {
		g.NextCity++
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
	seg := g.Road.Find(g.Pos + PlayerZ)
	g.FogD, g.RainD, g.HillsK = 2+12*seg.Fog, seg.Rain, seg.Ter.Hills
	for i := range g.Cars {
		g.Spawn(&g.Cars[i], g.Pos+PlayerZ+SegLen*float64(10+g.Rng.Intn(2*DrawDist)))
	}
	g.say(fmt.Sprintf("Continuando do km %.0f · p pausa", km), 4)
}
