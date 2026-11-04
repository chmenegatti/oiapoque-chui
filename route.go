package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"math/rand"
	"slices"
)

//go:embed rotas/*.json
var rotasFS embed.FS

const (
	carTop  = 140.0 // Fiat 147 1050 de fábrica
	boatTop = 30.0
)

type Terrain struct {
	AteKm float64 `json:"ate_km"`
	Tipo  string  `json:"tipo"`
	Nevoa float64 `json:"nevoa"`
	Chuva float64 `json:"chuva"`
	Nome  string  `json:"nome"`
}

type City struct {
	Nome string  `json:"nome"`
	Km   float64 `json:"km"`
}

// Route é um trecho da viagem, lido de rotas/NN-*.json.
type Route struct {
	Nome     string    `json:"nome"`
	Origem   string    `json:"origem"`
	Destino  string    `json:"destino"`
	Modo     string    `json:"modo"`  // "" (estrada) ou "balsa"
	Pista    string    `json:"pista"` // "simples" (mão dupla) ou "dupla"
	KmTotal  float64   `json:"km_total"`
	Seed     int64     `json:"seed"`
	Escala   float64   `json:"escala"`   // hodômetro anda Escala× mais rápido que o real
	Saida    float64   `json:"saida"`    // hora de partida
	Gasolina float64   `json:"gasolina"` // R$/L
	Premio   float64   `json:"premio"`   // R$ ao chegar
	Passagem float64   `json:"passagem"` // R$ (balsa)
	Terreno  []Terrain `json:"terreno"`
	Cidades  []City    `json:"cidades"`
	Postos   []float64 `json:"postos"` // vazio = gera automático

	radares []float64
}

func (rt *Route) boat() bool   { return rt.Modo == "balsa" }
func (rt *Route) twoWay() bool { return rt.Pista == "simples" && !rt.boat() }

func (rt *Route) baseTop() float64 {
	if rt.boat() {
		return boatTop
	}
	return carTop
}

func (rt *Route) unitsPerKm() float64 { return maxSpeed / (rt.baseTop() * rt.Escala / 3600) }
func (rt *Route) segsPerKm() float64  { return rt.unitsPerKm() / segLen }

func loadRoutes() ([]*Route, error) {
	names, err := fs.Glob(rotasFS, "rotas/*.json")
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	var out []*Route
	for _, n := range names {
		b, err := rotasFS.ReadFile(n)
		if err != nil {
			return nil, err
		}
		rt := &Route{}
		if err := json.Unmarshal(b, rt); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		rt.prepare()
		out = append(out, rt)
	}
	return out, nil
}

func (rt *Route) prepare() {
	if rt.Escala == 0 {
		rt.Escala = 20
	}
	if rt.Saida == 0 {
		rt.Saida = 7
	}
	if rt.Gasolina == 0 {
		rt.Gasolina = 6.5
	}
	if rt.Premio == 0 {
		rt.Premio = math.Round((300+rt.KmTotal*0.8)/10) * 10
	}
	if rt.boat() {
		rt.Postos = nil
		return
	}
	// postos: logo depois de cada cidade, e nunca mais de 80 km sem posto
	if len(rt.Postos) == 0 {
		prev := 0.0
		for _, c := range rt.Cidades {
			if c.Km >= rt.KmTotal-3 {
				break
			}
			for c.Km-prev > 80 {
				prev += 70
				rt.Postos = append(rt.Postos, prev)
			}
			if c.Km > 5 {
				prev = c.Km + 0.8
				rt.Postos = append(rt.Postos, prev)
			}
		}
		for rt.KmTotal-prev > 80 {
			prev += 70
			rt.Postos = append(rt.Postos, prev)
		}
	}
	// radar 2 km antes de cada cidade
	for _, c := range rt.Cidades {
		if c.Km > 10 {
			rt.radares = append(rt.radares, c.Km-2)
		}
	}
}

// ---------- terrenos ----------

type flora struct {
	spr         *Sprite
	prob, w, wv float64 // chance por segmento, largura, variação
	hit         float64
	water       bool    // no meio do rio (obstáculo) em vez da margem
	xMin        float64 // distância mínima do centro (0 = padrão 1.6)
}

type terrainCfg struct {
	minLen, maxLen        int       // segmentos por seção
	curves                []float64 // sorteio de intensidade de curva
	amp, slope            float64   // altura máx (segLen) e inclinação máx
	flora                 []flora
	grass, road, shoulder [2]RGB
	paint                 bool    // faixas pintadas
	grip                  float64 // >1 = escorrega mais (terra, rio)
	maxPct                float64 // fração da velocidade máx (terra segura)
	hills                 float64 // altura da serra no horizonte
	bank                  float64 // largura do acostamento/margem (× meia-pista)
}

var (
	gGreen   = [2]RGB{{40, 120, 45}, {34, 105, 38}}
	gForest  = [2]RGB{{25, 90, 35}, {22, 80, 30}}
	gDry     = [2]RGB{{150, 140, 70}, {138, 128, 62}}
	gPampa   = [2]RGB{{95, 150, 60}, {86, 140, 55}}
	rAsphalt = [2]RGB{{92, 92, 98}, {86, 86, 92}}
	rDirt    = [2]RGB{{160, 100, 55}, {148, 92, 50}}
	rWater   = [2]RGB{{45, 95, 120}, {40, 88, 112}}
	sGravel  = [2]RGB{{125, 115, 100}, {112, 103, 90}}
	sMud     = [2]RGB{{110, 85, 55}, {100, 78, 50}}
)

func tree(p float64) flora   { return flora{spr: sprArvore, prob: p, w: 0.9, wv: 0.5, hit: 0.2} }
func acai(p float64) flora   { return flora{spr: sprAcai, prob: p, w: 0.6, wv: 0.3, hit: 0.1} }
func banana(p float64) flora { return flora{spr: sprBananeira, prob: p, w: 0.7, wv: 0.2, hit: 0.15} }
func pequi(p float64) flora  { return flora{spr: sprPequi, prob: p, w: 0.8, wv: 0.4, hit: 0.15} }
func cow(p float64) flora    { return flora{spr: sprVaca, prob: p, w: 0.5, wv: 0.1} }
func trunk(p float64) flora {
	return flora{spr: sprTronco, prob: p, w: 0.5, wv: 0.3, hit: 0.5, water: true}
}
func easyCurves() []float64  { return []float64{0, curveEasy, curveMedium} }
func straightish() []float64 { return []float64{0, 0, curveEasy} }

var terrainCfgs = map[string]*terrainCfg{
	"urbano": {minLen: 60, maxLen: 150, curves: straightish(), amp: 8, slope: 0.08,
		flora: []flora{tree(0.04)}, grass: gGreen, road: rAsphalt, shoulder: sGravel, paint: true, grip: 1, maxPct: 1, hills: 0.8},
	"ondulado": {minLen: 50, maxLen: 120, curves: easyCurves(), amp: 40, slope: 0.18,
		flora: []flora{tree(0.15)}, grass: gGreen, road: rAsphalt, shoulder: sGravel, paint: true, grip: 1, maxPct: 1, hills: 1},
	"serra": {minLen: 30, maxLen: 80, curves: []float64{curveMedium, curveHard, curveHard}, amp: 90, slope: 0.22,
		flora: []flora{tree(0.45)}, grass: gGreen, road: rAsphalt, shoulder: sGravel, paint: true, grip: 1, maxPct: 1, hills: 1.5},
	"bananal": {minLen: 50, maxLen: 120, curves: easyCurves(), amp: 25, slope: 0.12,
		flora: []flora{tree(0.05), banana(0.35)}, grass: gGreen, road: rAsphalt, shoulder: sGravel, paint: true, grip: 1, maxPct: 1, hills: 1.1},
	"terra": {minLen: 40, maxLen: 100, curves: []float64{curveEasy, curveMedium, curveMedium}, amp: 20, slope: 0.12,
		flora: []flora{tree(0.3), acai(0.2)}, grass: gForest, road: rDirt, shoulder: sMud, grip: 1.4, maxPct: 0.7, hills: 0.6},
	"floresta": {minLen: 60, maxLen: 140, curves: easyCurves(), amp: 15, slope: 0.08,
		flora: []flora{tree(0.45), acai(0.25)}, grass: gForest, road: rAsphalt, shoulder: sGravel, paint: true, grip: 1, maxPct: 1, hills: 0.6},
	"cerrado": {minLen: 80, maxLen: 200, curves: straightish(), amp: 15, slope: 0.06,
		flora: []flora{pequi(0.12)}, grass: gDry, road: rAsphalt, shoulder: sGravel, paint: true, grip: 1, maxPct: 1, hills: 0.4},
	"pampa": {minLen: 80, maxLen: 200, curves: straightish(), amp: 6, slope: 0.04,
		flora: []flora{tree(0.02), cow(0.05)}, grass: gPampa, road: rAsphalt, shoulder: sGravel, paint: true, grip: 1, maxPct: 1, hills: 0.2},
	"litoral": {minLen: 50, maxLen: 120, curves: easyCurves(), amp: 25, slope: 0.12,
		flora: []flora{acai(0.12), tree(0.12)}, grass: gGreen, road: rAsphalt, shoulder: sGravel, paint: true, grip: 1, maxPct: 1, hills: 1},
	"rio": {minLen: 80, maxLen: 200, curves: []float64{0, curveEasy, curveEasy},
		flora: []flora{bank(tree(0.6)), bank(acai(0.4)), trunk(0.012)}, grass: gForest, road: rWater, shoulder: rWater, grip: 1.2, maxPct: 1, hills: 0.5, bank: 4},
}

// buildRoad gera a estrada a partir da rota: o terreno define o "caráter"
// das curvas/morros/vegetação; seed fixo → mesma estrada toda vez.
func buildRoad(rt *Route) *Road {
	r := &Road{Name: rt.Nome}
	rng := rand.New(rand.NewSource(rt.Seed))
	spk := rt.segsPerKm()
	var cfg *terrainCfg
	for _, t := range rt.Terreno {
		if cfg = terrainCfgs[t.Tipo]; cfg == nil {
			panic("terreno desconhecido: " + t.Tipo)
		}
		target := int(t.AteKm * spk)
		for len(r.segs) < target {
			n := min(cfg.minLen+rng.Intn(cfg.maxLen-cfg.minLen+1), target-len(r.segs))
			n = max(n, 3)
			curve := cfg.curves[rng.Intn(len(cfg.curves))]
			if rng.Intn(2) == 0 {
				curve = -curve
			}
			ty := (rng.Float64()*2 - 1) * cfg.amp * segLen
			lim := cfg.slope * float64(n) * segLen
			dy := clamp(ty-r.lastY(), -lim, lim)
			start := len(r.segs)
			r.addSection(n/3, n-2*(n/3), n/3, curve, dy/segLen)
			for i := start; i < len(r.segs); i++ {
				s := &r.segs[i]
				s.ter, s.fog, s.rain = cfg, t.Nevoa, t.Chuva
				scatter(s, cfg, rng)
			}
		}
	}
	end := len(r.segs)
	r.addSection(drawDist, drawDist, drawDist, 0, -r.lastY()/segLen) // folga após o destino
	for i := end; i < len(r.segs); i++ {
		r.segs[i].ter = cfg
	}

	at := func(km float64) *Segment { return &r.segs[max(0, min(int(km*spk), end-1))] }
	add := func(km float64, p Prop) { s := at(km); s.props = append(s.props, p) }
	if !rt.boat() {
		for k := 1; k < int(rt.KmTotal); k++ {
			add(float64(k), Prop{spr: sprKm, x: 1.25, w: 0.12})
		}
	}
	for _, c := range rt.Cidades {
		add(math.Max(c.Km-0.5, 0), Prop{spr: sprPlaca, x: 1.5, w: 0.9, hit: 0.15})
	}
	for _, km := range rt.radares {
		add(km-1, Prop{spr: sprLimite, x: 1.35, w: 0.3, hit: 0.1})
		add(km, Prop{spr: sprRadar, x: 1.3, w: 0.25, hit: 0.1})
	}
	for _, p := range rt.Postos {
		s := at(p)
		for i := -40; i < 40; i++ { // limpa a vegetação em volta do posto
			if j := s.index + i; j >= 0 && j < end {
				r.segs[j].props = slices.DeleteFunc(r.segs[j].props, func(p Prop) bool { return p.veg })
			}
		}
		s.props = append(s.props, Prop{spr: sprPosto, x: 2.3, w: 2.2, hit: 1.6})
	}
	return r
}

func scatter(s *Segment, cfg *terrainCfg, rng *rand.Rand) {
	for _, f := range cfg.flora {
		if rng.Float64() >= f.prob {
			continue
		}
		var x float64
		if f.water {
			x = (rng.Float64()*2 - 1) * 0.85
		} else {
			xm := f.xMin
			if xm == 0 {
				xm = 1.6
			}
			x = xm + rng.Float64()*2.5
			if rng.Intn(2) == 0 {
				x = -x
			}
		}
		s.props = append(s.props, Prop{spr: f.spr, x: x, w: f.w + rng.Float64()*f.wv, hit: f.hit, veg: !f.water})
	}
}

// bank: vegetação só na margem distante do rio.
func bank(f flora) flora { f.xMin = 4.3; return f }
