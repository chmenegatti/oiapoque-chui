package main

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

func SkyColors(t float64) (RGB, RGB) {
	for i := 0; i < len(skyKeys)-1; i++ {
		a, b := skyKeys[i], skyKeys[i+1]
		if t >= a.t && t <= b.t {
			k := (t - a.t) / (b.t - a.t)
			return Mix(a.zen, b.zen, k), Mix(a.hor, b.hor, k)
		}
	}
	return skyKeys[0].zen, skyKeys[0].hor
}

// daylight: 0.15 (noite) .. 1 (dia), com amanhecer ~6h e anoitecer ~19h.
func Daylight(t float64) float64 {
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
