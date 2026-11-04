package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// proj guarda a projeção de cada segmento visível no frame atual.
type proj struct {
	x1, y1, w1, s1 float64
	x2, y2, w2, s2 float64
	clip           int
}

type skyKey struct {
	t        float64
	zen, hor RGB
}

var skyKeys = []skyKey{
	{0.00, RGB{4, 6, 20}, RGB{18, 22, 48}},
	{0.22, RGB{10, 14, 40}, RGB{40, 40, 80}},
	{0.27, RGB{70, 90, 160}, RGB{250, 160, 90}},
	{0.35, RGB{70, 130, 220}, RGB{180, 215, 250}},
	{0.70, RGB{70, 130, 220}, RGB{180, 215, 250}},
	{0.76, RGB{60, 50, 120}, RGB{255, 120, 50}},
	{0.81, RGB{10, 14, 40}, RGB{40, 40, 80}},
	{1.00, RGB{4, 6, 20}, RGB{18, 22, 48}},
}

func skyColors(t float64) (RGB, RGB) {
	for i := 0; i < len(skyKeys)-1; i++ {
		a, b := skyKeys[i], skyKeys[i+1]
		if t >= a.t && t <= b.t {
			k := (t - a.t) / (b.t - a.t)
			return mix(a.zen, b.zen, k), mix(a.hor, b.hor, k)
		}
	}
	return skyKeys[0].zen, skyKeys[0].hor
}

// daylight: 0.15 (noite) .. 1 (dia), com amanhecer ~6h e anoitecer ~19h.
func daylight(t float64) float64 {
	h := t * 24
	switch {
	case h < 5.5 || h > 19.5:
		return 0.15
	case h < 7:
		return 0.15 + 0.85*(h-5.5)/1.5
	case h > 18:
		return 0.15 + 0.85*(19.5-h)/1.5
	}
	return 1
}

var (
	paint  = RGB{235, 235, 235}
	yellow = RGB{235, 190, 40} // faixa central de mão dupla
)

func project(camX, camY, camZ, xs float64, W, H int) (x, y, w, s float64) {
	s = camDepth / camZ
	x = float64(W)/2 + s*camX*xs
	y = float64(H)/2 - s*camY*float64(H)/2
	w = s * roadWidth * xs
	return
}

func drawRow(f *Frame, y int, cx, hw float64, seg *Segment, twoWay bool, shade func(RGB) RGB) {
	t := seg.ter
	alt := (seg.index / rumbleLen) % 2
	f.HLine(0, f.W-1, y, shade(t.grass[alt]))
	bk := t.bank
	if bk == 0 {
		bk = 1.2
	}
	f.Span(cx-hw*bk, cx+hw*bk, y, shade(t.shoulder[alt]))
	f.Span(cx-hw, cx+hw, y, shade(t.road[alt]))
	if !t.paint || hw < 6 {
		return
	}
	l := math.Max(hw/30, 1)
	f.Span(cx-hw, cx-hw+l, y, shade(paint))
	f.Span(cx+hw-l, cx+hw, y, shade(paint))
	switch {
	case twoWay: // amarela contínua dupla: proibido ultrapassar… mas é jogo
		f.Span(cx-l*1.5, cx-l*0.5, y, shade(yellow))
		f.Span(cx+l*0.5, cx+l*1.5, y, shade(yellow))
	case alt == 0:
		f.Span(cx-l/2, cx+l/2, y, shade(paint))
	}
}

func (g *Game) mountains(f *Frame, horizon, light float64, hor RGB) {
	layers := []struct {
		par, amp, haze float64
		col            RGB
	}{
		{0.3, 1.0, 0.55, RGB{70, 100, 90}},
		{0.7, 0.6, 0.30, RGB{35, 80, 45}},
	}
	unit := float64(f.H) / 50 * g.hillsK
	for li, L := range layers {
		c := mix(scale(L.col, light), hor, L.haze)
		for x := 0; x < f.W; x++ {
			xx := float64(x) + g.skyOff*L.par
			h := (6 + 3*math.Sin(xx*0.045+float64(li)) + 2.5*math.Sin(xx*0.11+2) + 1.5*math.Sin(xx*0.017)) * unit * L.amp
			for y := int(horizon - h); y < int(horizon); y++ {
				f.Set(x, y, c)
			}
		}
	}
}

func hash(x, y int) uint32 {
	h := uint32(x)*73856093 ^ uint32(y)*19349663
	h ^= h >> 13
	return h * 0x5bd1e995
}

func (g *Game) Draw(s tcell.Screen) {
	cols, rows := s.Size()
	if cols < 60 || rows < 14 {
		s.Clear()
		drawText(s, 0, 0, "terminal pequeno demais (mín 60x14)", tcell.StyleDefault)
		return
	}
	if g.beep {
		s.Beep()
		g.beep = false
	}
	W, H := cols, (rows-1)*2
	if g.frame == nil || g.frame.W != W || g.frame.H != H {
		g.frame = NewFrame(W, H)
	}
	f := g.frame
	xs := float64(H) * 0.66 // escala horizontal (~4:3)
	light := daylight(g.tod) * (1 - 0.35*g.rainD)
	night := light < 0.6
	zen, hor := skyColors(g.tod)
	gray := RGB{110, 115, 125}
	zen = mix(zen, scale(gray, light), g.rainD*0.7)
	hor = mix(hor, scale(gray, light+0.2), g.rainD*0.7)
	horizon := float64(H) / 2

	// céu + estrelas + serra ao fundo
	for y := range H {
		t := clamp(float64(y)/horizon, 0, 1)
		f.HLine(0, W-1, y, mix(zen, hor, t*t))
	}
	if light < 0.45 && g.rainD < 0.3 {
		k := (0.45 - light) / 0.45
		for y := 0; y < int(horizon); y++ {
			for x := range W {
				if hash(x+int(g.skyOff*0.2), y)%400 == 0 {
					f.Set(x, y, mix(f.Get(x, y), RGB{255, 255, 255}, k))
				}
			}
		}
	}
	g.mountains(f, horizon, light, hor)

	// estrada: do mais perto pro mais longe, recortando por maxy
	r := g.road
	base := r.find(g.pos)
	basePct := wrap(g.pos, segLen) / segLen
	pseg := r.find(g.pos + playerZ)
	playerY := lerp(pseg.y1, pseg.y2, wrap(g.pos+playerZ, segLen)/segLen)
	camY := playerY + camHeight
	camX := g.playerX * roadWidth

	shadeAt := func(i int) func(RGB) RGB {
		d := float64(i) / drawDist
		fog := 1 - math.Exp(-d*d*g.fogD)
		k := light + (1-light)*clamp(1-float64(i)/g.veh.headlight, 0, 1)*0.8 // farol
		return func(c RGB) RGB { return mix(scale(c, k), hor, fog) }
	}

	twoWay := g.route.twoWay()
	maxy := H
	x, dx := 0.0, -(base.curve * basePct)
	visible := min(drawDist, len(r.segs)-base.index)
	for i := range visible {
		seg := &r.segs[base.index+i]
		p := &g.proj[i]
		p.clip = maxy
		z1 := math.Max(seg.z1-g.pos, camDepth) // corta no plano próximo
		p.x1, p.y1, p.w1, p.s1 = project(x-camX, seg.y1-camY, z1, xs, W, H)
		p.x2, p.y2, p.w2, p.s2 = project(x+dx-camX, seg.y2-camY, seg.z2-g.pos, xs, W, H)
		x += dx
		dx += seg.curve
		y0, y1 := int(math.Ceil(p.y2)), min(int(math.Ceil(p.y1)), maxy)
		if p.y2 >= p.y1 || y0 >= maxy {
			continue
		}
		shade := shadeAt(i)
		for y := max(y0, 0); y < y1; y++ {
			t := (float64(y) - p.y2) / (p.y1 - p.y2)
			drawRow(f, y, lerp(p.x2, p.x1, t), lerp(p.w2, p.w1, t), seg, twoWay, shade)
		}
		maxy = y0
	}

	// cenário + tráfego: do mais longe pro mais perto
	buckets := map[int][]int{}
	for ci, c := range g.cars {
		if i := int(c.z/segLen) - base.index; i > 0 && i < visible {
			buckets[i] = append(buckets[i], ci)
		}
	}
	for i := visible - 1; i > 0; i-- {
		p := g.proj[i]
		sh := shadeAt(i)
		shade := func(_ byte, c RGB) RGB { return sh(c) }
		for _, pr := range r.segs[base.index+i].props {
			f.Sprite(pr.spr, p.x1+p.s1*pr.x*roadWidth*xs, p.y1, p.s1*pr.w*roadWidth*xs, float64(p.clip), nil, shade)
		}
		for _, ci := range buckets[i] {
			c := g.cars[ci]
			t := wrap(c.z, segLen) / segLen
			sc := lerp(p.s1, p.s2, t)
			cx := lerp(p.x1, p.x2, t) + sc*c.x*roadWidth*xs
			spr := c.kind.rear
			if c.dir < 0 {
				spr = c.kind.front
			}
			f.Sprite(spr, cx, lerp(p.y1, p.y2, t), sc*c.kind.w*roadWidth*xs, float64(p.clip), &c.color,
				func(ch byte, col RGB) RGB {
					switch {
					case ch == 'R' && night:
						return sh(RGB{255, 70, 50}) // lanterna acesa: não escurece
					case ch == 'H' && night:
						return mix(RGB{255, 250, 215}, hor, 0.15) // farol: atravessa a neblina
					}
					return sh(col)
				})
		}
	}

	// jogador
	bounce := 0.0
	if (math.Abs(g.playerX) > 1 && g.speed > 0) || g.bmp > 0 {
		bounce = float64(g.rng.Intn(2))
	}
	if g.route.boat() { // balsa balança
		bounce += math.Round(math.Sin(g.elapsed*2) * 0.8)
	}
	pk := math.Max(light, 0.5)
	pw := g.veh.width * roadWidth / camHeight * xs
	f.Sprite(g.veh.spr, float64(W)/2, float64(H)-bounce, pw, float64(H)+1, nil,
		func(ch byte, col RGB) RGB {
			switch {
			case ch == 'R' && g.braking:
				return RGB{255, 60, 40}
			case ch == 'R' && night:
				return RGB{200, 40, 30}
			}
			return scale(col, pk)
		})
	if g.overheat { // vapor saindo do capô
		for range 12 {
			px := W/2 + g.rng.Intn(W/8) - W/16
			py := H - int(pw*0.55) - g.rng.Intn(H/8)
			f.Set(px, py, RGB{220, 220, 220})
		}
	}

	// chuva
	if g.rainD > 0.05 {
		c := RGB{160, 175, 200}
		for range int(g.rainD * float64(W*H) / 120) {
			x, y := g.rng.Intn(W), g.rng.Intn(H-3)
			for j := range 3 { // risco inclinado e translúcido
				px, py := x-j/2, y+j
				if px >= 0 {
					f.Set(px, py, mix(f.Get(px, py), c, 0.55))
				}
			}
		}
	}

	f.Flush(s)
	g.drawHUD(s, cols, rows)
}

type hudPart struct {
	txt string
	st  tcell.Style
}

func bar(v, maxV float64, n int) string {
	k := int(clamp(v/maxV, 0, 1)*float64(n) + 0.5)
	return strings.Repeat("█", k) + strings.Repeat("░", n-k)
}

var (
	hudBg   = tcell.NewRGBColor(20, 20, 20)
	stHUD   = tcell.StyleDefault.Background(hudBg).Foreground(tcell.NewRGBColor(240, 200, 60))
	stWarn  = stHUD.Foreground(tcell.NewRGBColor(255, 80, 60)).Bold(true)
	stMsg   = tcell.StyleDefault.Background(tcell.NewRGBColor(0, 0, 0)).Foreground(tcell.NewRGBColor(255, 255, 255)).Bold(true)
	stTitle = stMsg.Foreground(tcell.NewRGBColor(240, 200, 60))
)

func (g *Game) drawHUD(s tcell.Screen, cols, rows int) {
	pick := func(bad bool) tcell.Style {
		if bad {
			return stWarn
		}
		return stHUD
	}
	rt, km := g.route, g.km()

	pedal := " "
	switch {
	case g.cruise:
		pedal = "C"
	case g.throttle:
		pedal = "▲"
	}
	next := "—"
	if g.nextCity < len(rt.Cidades) {
		c := rt.Cidades[g.nextCity]
		next = fmt.Sprintf("%s %.0f km", c.Nome, c.Km-km)
	}
	h := int(g.tod * 24)
	m := int(g.tod*24*60) % 60

	name := " 147"
	if rt.boat() {
		name = " BALSA"
	}
	parts := []hudPart{
		{fmt.Sprintf("%s %s %3.0f km/h ", name, pedal, g.kmh()), pick(!rt.boat() && g.kmh() > radarLimite*1.1)},
		{fmt.Sprintf("│ km %3.0f/%.0f ", km, rt.KmTotal), stHUD},
	}
	if !rt.boat() {
		posto := "—"
		if p, ok := g.nextPosto(km); ok {
			posto = fmt.Sprintf("%.1f km", math.Max(p-km, 0))
		}
		parts = append(parts,
			hudPart{fmt.Sprintf("│ COMB %s %2.0fL ", bar(g.fuel, g.veh.tank, 6), g.fuel), pick(g.fuel < 6)},
			hudPart{fmt.Sprintf("│ TEMP %3.0f° ", g.temp), pick(g.temp > 105)},
			hudPart{"│ posto " + posto + " ", stHUD},
		)
	}
	parts = append(parts,
		hudPart{fmt.Sprintf("│ R$ %.0f ", g.money), pick(g.money < 0)},
		hudPart{"│ " + next + " ", stHUD},
		hudPart{fmt.Sprintf("│ %02d:%02d ", h, m), stHUD},
	)
	for x := range cols {
		s.SetContent(x, rows-1, ' ', nil, stHUD)
	}
	x := 0
	for _, p := range parts {
		x = drawText(s, x, rows-1, p.txt, p.st)
	}

	if g.msgT > 0 {
		center(s, cols, 1, g.msg, stMsg)
	}
	if g.paused {
		center(s, cols, rows/2-1, "PAUSADO", stTitle)
		center(s, cols, rows/2, "p continua · q salva e volta ao menu", stMsg)
	}
	switch g.state {
	case arrived:
		t := int(g.elapsed)
		center(s, cols, rows/2-2, fmt.Sprintf("CHEGOU EM %s!", strings.ToUpper(rt.Destino)), stTitle)
		center(s, cols, rows/2-1, fmt.Sprintf("tempo %d:%02d · prêmio R$ %.0f · multas %d", t/60, t%60, rt.Premio, g.fines), stMsg)
		center(s, cols, rows/2+1, "Enter continua", stMsg)
	case outOfFuel:
		center(s, cols, rows/2-2, fmt.Sprintf("PANE SECA no km %.0f", km), stWarn)
		if g.money >= guinchoPreco {
			center(s, cols, rows/2-1, fmt.Sprintf("g chama o guincho (R$ %.0f) · r recomeça o trecho do início", guinchoPreco), stMsg)
		} else {
			center(s, cols, rows/2-1, "Sem dinheiro pro guincho… r recomeça o trecho do início", stMsg)
		}
	}
}

func center(s tcell.Screen, cols, y int, txt string, st tcell.Style) {
	txt = " " + txt + " "
	drawText(s, max(0, (cols-len([]rune(txt)))/2), y, txt, st)
}

func drawText(s tcell.Screen, x, y int, txt string, st tcell.Style) int {
	for _, ch := range txt {
		s.SetContent(x, y, ch, nil, st)
		x++
	}
	return x
}
