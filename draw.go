package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// view guarda o estado de desenho do terminal para um jogo.
type view struct {
	frame *Frame
	proj  [DrawDist]proj
}

// proj guarda a projeção de cada segmento visível no frame atual.
type proj struct {
	x1, y1, w1, s1 float64
	x2, y2, w2, s2 float64
	clip           int
}

var (
	paint  = RGB{235, 235, 235}
	yellow = RGB{235, 190, 40} // faixa central de mão dupla
)

func project(camX, camY, camZ, xs float64, W, H int) (x, y, w, s float64) {
	s = CamDepth / camZ
	x = float64(W)/2 + s*camX*xs
	y = float64(H)/2 - s*camY*float64(H)/2
	w = s * RoadWidth * xs
	return
}

func drawRow(f *Frame, y int, cx, hw float64, seg *Segment, twoWay bool, shade func(RGB) RGB) {
	t := seg.Ter
	alt := (seg.Index / RumbleLen) % 2
	f.HLine(0, f.W-1, y, shade(t.Grass[alt]))
	bk := t.Bank
	if bk == 0 {
		bk = 1.2
	}
	f.Span(cx-hw*bk, cx+hw*bk, y, shade(t.Shoulder[alt]))
	f.Span(cx-hw, cx+hw, y, shade(t.Road[alt]))
	if !t.Paint || hw < 6 {
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

func mountains(g *Game, f *Frame, horizon, light float64, hor RGB) {
	layers := []struct {
		par, amp, haze float64
		col            RGB
	}{
		{0.3, 1.0, 0.55, RGB{70, 100, 90}},
		{0.7, 0.6, 0.30, RGB{35, 80, 45}},
	}
	unit := float64(f.H) / 50 * g.HillsK
	for li, L := range layers {
		c := Mix(Scale(L.col, light), hor, L.haze)
		for x := 0; x < f.W; x++ {
			xx := float64(x) + g.SkyOff*L.par
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

func (v *view) Draw(s tcell.Screen, g *Game) {
	cols, rows := s.Size()
	if cols < 60 || rows < 14 {
		s.Clear()
		drawText(s, 0, 0, "terminal pequeno demais (mín 60x14)", tcell.StyleDefault)
		return
	}
	if g.Beep {
		s.Beep()
		g.Beep = false
	}
	W, H := cols, (rows-1)*2
	if v.frame == nil || v.frame.W != W || v.frame.H != H {
		v.frame = NewFrame(W, H)
	}
	f := v.frame
	xs := float64(H) * 0.66 // escala horizontal (~4:3)
	light := Daylight(g.Tod) * (1 - 0.35*g.RainD)
	night := light < 0.6
	zen, hor := SkyColors(g.Tod)
	gray := RGB{110, 115, 125}
	zen = Mix(zen, Scale(gray, light), g.RainD*0.7)
	hor = Mix(hor, Scale(gray, light+0.2), g.RainD*0.7)
	horizon := float64(H) / 2

	// céu + estrelas + serra ao fundo
	for y := range H {
		t := Clamp(float64(y)/horizon, 0, 1)
		f.HLine(0, W-1, y, Mix(zen, hor, t*t))
	}
	if light < 0.45 && g.RainD < 0.3 {
		k := (0.45 - light) / 0.45
		for y := 0; y < int(horizon); y++ {
			for x := range W {
				if hash(x+int(g.SkyOff*0.2), y)%400 == 0 {
					f.Set(x, y, Mix(f.Get(x, y), RGB{255, 255, 255}, k))
				}
			}
		}
	}
	mountains(g, f, horizon, light, hor)

	// estrada: do mais perto pro mais longe, recortando por maxy
	r := g.Road
	base := r.Find(g.Pos)
	basePct := Wrap(g.Pos, SegLen) / SegLen
	pseg := r.Find(g.Pos + PlayerZ)
	playerY := Lerp(pseg.Y1, pseg.Y2, Wrap(g.Pos+PlayerZ, SegLen)/SegLen)
	camY := playerY + CamHeight
	camX := g.PlayerX * RoadWidth

	shadeAt := func(i int) func(RGB) RGB {
		d := float64(i) / DrawDist
		fog := 1 - math.Exp(-d*d*g.FogD)
		k := light + (1-light)*Clamp(1-float64(i)/g.Veh.Headlight, 0, 1)*0.8 // farol
		return func(c RGB) RGB { return Mix(Scale(c, k), hor, fog) }
	}

	twoWay := g.Route.TwoWay()
	maxy := H
	x, dx := 0.0, -(base.Curve * basePct)
	visible := min(DrawDist, len(r.Segs)-base.Index)
	for i := range visible {
		seg := &r.Segs[base.Index+i]
		p := &v.proj[i]
		p.clip = maxy
		z1 := math.Max(seg.Z1-g.Pos, CamDepth) // corta no plano próximo
		p.x1, p.y1, p.w1, p.s1 = project(x-camX, seg.Y1-camY, z1, xs, W, H)
		p.x2, p.y2, p.w2, p.s2 = project(x+dx-camX, seg.Y2-camY, seg.Z2-g.Pos, xs, W, H)
		x += dx
		dx += seg.Curve
		y0, y1 := int(math.Ceil(p.y2)), min(int(math.Ceil(p.y1)), maxy)
		if p.y2 >= p.y1 || y0 >= maxy {
			continue
		}
		shade := shadeAt(i)
		for y := max(y0, 0); y < y1; y++ {
			t := (float64(y) - p.y2) / (p.y1 - p.y2)
			drawRow(f, y, Lerp(p.x2, p.x1, t), Lerp(p.w2, p.w1, t), seg, twoWay, shade)
		}
		maxy = y0
	}

	// cenário + tráfego: do mais longe pro mais perto
	buckets := map[int][]int{}
	for ci, c := range g.Cars {
		if i := int(c.Z/SegLen) - base.Index; i > 0 && i < visible {
			buckets[i] = append(buckets[i], ci)
		}
	}
	for i := visible - 1; i > 0; i-- {
		p := v.proj[i]
		sh := shadeAt(i)
		shade := func(_ byte, c RGB) RGB { return sh(c) }
		for _, pr := range r.Segs[base.Index+i].Props {
			f.Sprite(propSprites[pr.Kind], p.x1+p.s1*pr.X*RoadWidth*xs, p.y1, p.s1*pr.W*RoadWidth*xs, float64(p.clip), nil, shade)
		}
		for _, ci := range buckets[i] {
			c := g.Cars[ci]
			t := Wrap(c.Z, SegLen) / SegLen
			sc := Lerp(p.s1, p.s2, t)
			cx := Lerp(p.x1, p.x2, t) + sc*c.X*RoadWidth*xs
			spr := vehSprites[c.Kind][0]
			if c.Dir < 0 {
				spr = vehSprites[c.Kind][1]
			}
			f.Sprite(spr, cx, Lerp(p.y1, p.y2, t), sc*c.Kind.W*RoadWidth*xs, float64(p.clip), &c.Color,
				func(ch byte, col RGB) RGB {
					switch {
					case ch == 'R' && night:
						return sh(RGB{255, 70, 50}) // lanterna acesa: não escurece
					case ch == 'H' && night:
						return Mix(RGB{255, 250, 215}, hor, 0.15) // farol: atravessa a neblina
					}
					return sh(col)
				})
		}
	}

	// jogador
	bounce := 0.0
	if (math.Abs(g.PlayerX) > 1 && g.Speed > 0) || g.Bmp > 0 {
		bounce = float64(g.Rng.Intn(2))
	}
	if g.Route.Boat() { // balsa balança
		bounce += math.Round(math.Sin(g.Elapsed*2) * 0.8)
	}
	pk := math.Max(light, 0.5)
	pw := g.Veh.Width * RoadWidth / CamHeight * xs
	f.Sprite(playerSprite(g.Veh), float64(W)/2, float64(H)-bounce, pw, float64(H)+1, nil,
		func(ch byte, col RGB) RGB {
			switch {
			case ch == 'R' && g.Braking:
				return RGB{255, 60, 40}
			case ch == 'R' && night:
				return RGB{200, 40, 30}
			}
			return Scale(col, pk)
		})
	if g.Overheat { // vapor saindo do capô
		for range 12 {
			px := W/2 + g.Rng.Intn(W/8) - W/16
			py := H - int(pw*0.55) - g.Rng.Intn(H/8)
			f.Set(px, py, RGB{220, 220, 220})
		}
	}

	// chuva
	if g.RainD > 0.05 {
		c := RGB{160, 175, 200}
		for range int(g.RainD * float64(W*H) / 120) {
			x, y := g.Rng.Intn(W), g.Rng.Intn(H-3)
			for j := range 3 { // risco inclinado e translúcido
				px, py := x-j/2, y+j
				if px >= 0 {
					f.Set(px, py, Mix(f.Get(px, py), c, 0.55))
				}
			}
		}
	}

	f.Flush(s)
	drawHUD(s, g, cols, rows)
}

type hudPart struct {
	txt string
	st  tcell.Style
}

func bar(v, maxV float64, n int) string {
	k := int(Clamp(v/maxV, 0, 1)*float64(n) + 0.5)
	return strings.Repeat("█", k) + strings.Repeat("░", n-k)
}

var (
	hudBg   = tcell.NewRGBColor(20, 20, 20)
	stHUD   = tcell.StyleDefault.Background(hudBg).Foreground(tcell.NewRGBColor(240, 200, 60))
	stWarn  = stHUD.Foreground(tcell.NewRGBColor(255, 80, 60)).Bold(true)
	stMsg   = tcell.StyleDefault.Background(tcell.NewRGBColor(0, 0, 0)).Foreground(tcell.NewRGBColor(255, 255, 255)).Bold(true)
	stTitle = stMsg.Foreground(tcell.NewRGBColor(240, 200, 60))
)

func drawHUD(s tcell.Screen, g *Game, cols, rows int) {
	pick := func(bad bool) tcell.Style {
		if bad {
			return stWarn
		}
		return stHUD
	}
	rt, km := g.Route, g.Km()

	pedal := " "
	switch {
	case g.Cruise:
		pedal = "C"
	case g.Throttle:
		pedal = "▲"
	}
	next := "—"
	if g.NextCity < len(rt.Cidades) {
		c := rt.Cidades[g.NextCity]
		next = fmt.Sprintf("%s %.0f km", c.Nome, c.Km-km)
	}
	h := int(g.Tod * 24)
	m := int(g.Tod*24*60) % 60

	name := " 147"
	if rt.Boat() {
		name = " BALSA"
	}
	parts := []hudPart{
		{fmt.Sprintf("%s %s %3.0f km/h ", name, pedal, g.Kmh()), pick(!rt.Boat() && g.Kmh() > RadarLimite*1.1)},
		{fmt.Sprintf("│ km %3.0f/%.0f ", km, rt.KmTotal), stHUD},
	}
	if !rt.Boat() {
		posto := "—"
		if p, ok := g.NextPosto(km); ok {
			posto = fmt.Sprintf("%.1f km", math.Max(p-km, 0))
		}
		parts = append(parts,
			hudPart{fmt.Sprintf("│ COMB %s %2.0fL ", bar(g.Fuel, g.Veh.Tank, 6), g.Fuel), pick(g.Fuel < 6)},
			hudPart{fmt.Sprintf("│ TEMP %3.0f° ", g.Temp), pick(g.Temp > 105)},
			hudPart{"│ posto " + posto + " ", stHUD},
		)
	}
	parts = append(parts,
		hudPart{fmt.Sprintf("│ R$ %.0f ", g.Money), pick(g.Money < 0)},
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

	if g.MsgT > 0 {
		center(s, cols, 1, g.Msg, stMsg)
	}
	if g.Paused {
		center(s, cols, rows/2-1, "PAUSADO", stTitle)
		center(s, cols, rows/2, "p continua · q salva e volta ao menu", stMsg)
	}
	switch g.State {
	case Arrived:
		t := int(g.Elapsed)
		center(s, cols, rows/2-2, fmt.Sprintf("CHEGOU EM %s!", strings.ToUpper(rt.Destino)), stTitle)
		center(s, cols, rows/2-1, fmt.Sprintf("tempo %d:%02d · prêmio R$ %.0f · multas %d", t/60, t%60, rt.Premio, g.Fines), stMsg)
		center(s, cols, rows/2+1, "Enter continua", stMsg)
	case OutOfFuel:
		center(s, cols, rows/2-2, fmt.Sprintf("PANE SECA no km %.0f", km), stWarn)
		if g.Money >= GuinchoPreco {
			center(s, cols, rows/2-1, fmt.Sprintf("g chama o guincho (R$ %.0f) · r recomeça o trecho do início", GuinchoPreco), stMsg)
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
