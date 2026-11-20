package main

import "math"

// RGB é uma cor truecolor.
type RGB struct{ R, G, B uint8 }

func Clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func Lerp(a, b, t float64) float64 { return a + (b-a)*t }

func Mix(a, b RGB, t float64) RGB {
	t = Clamp(t, 0, 1)
	return RGB{
		uint8(Lerp(float64(a.R), float64(b.R), t)),
		uint8(Lerp(float64(a.G), float64(b.G), t)),
		uint8(Lerp(float64(a.B), float64(b.B), t)),
	}
}

func Scale(c RGB, k float64) RGB {
	return RGB{
		uint8(Clamp(float64(c.R)*k, 0, 255)),
		uint8(Clamp(float64(c.G)*k, 0, 255)),
		uint8(Clamp(float64(c.B)*k, 0, 255)),
	}
}
