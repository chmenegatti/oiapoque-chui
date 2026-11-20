package gpu

import (
	"math"
	"os"
	"testing"

	"github.com/chmenegatti/oiapoque-chui/game"
)

// project aplica vp num ponto e devolve a profundidade (z/w).
func depth(vp mat4, p vec3) float64 {
	x := [4]float64{p.x, p.y, p.z, 1}
	var z, w float64
	for k := range 4 {
		z += float64(vp[k*4+2]) * x[k]
		w += float64(vp[k*4+3]) * x[k]
	}
	return z / w
}

func TestProfundidadeInvertida(t *testing.T) {
	view, _, _, _ := lookDir(vec3{0, 0, 1})
	vp := perspectiveRevInf(fovY, 16.0/9, 0.8).mul(view)
	if d := depth(vp, vec3{0, 0, 0.8}); math.Abs(d-1) > 1e-5 {
		t.Errorf("no plano de perto a profundidade deveria ser 1, deu %v", d)
	}
	perto, longe := depth(vp, vec3{0, 0, 10}), depth(vp, vec3{0, 0, 8000})
	if !(perto > longe && longe > 0) {
		t.Errorf("profundidade deveria cair com a distância e ficar > 0: %v, %v", perto, longe)
	}
}

func TestFrustum(t *testing.T) {
	view, _, _, _ := lookDir(vec3{0, 0, 1}) // olhando para +z
	fr := makeFrustum(perspectiveRevInf(fovY, 16.0/9, 0.8).mul(view))
	caixa := func(c vec3) (vec3, vec3) { return c.sub(vec3{5, 5, 5}), c.add(vec3{5, 5, 5}) }
	casos := []struct {
		nome string
		c    vec3
		vê   bool
	}{
		{"à frente", vec3{0, 0, 100}, true},
		{"muito longe à frente", vec3{0, 0, 8000}, true},
		{"atrás", vec3{0, 0, -100}, false},
		{"bem à esquerda", vec3{-500, 0, 50}, false},
		{"bem acima", vec3{0, 500, 50}, false},
		{"encostada na câmera", vec3{0, 0, 0}, true},
	}
	for _, c := range casos {
		lo, hi := caixa(c.c)
		if got := fr.visible(lo, hi); got != c.vê {
			t.Errorf("%s: visível=%v, esperado %v", c.nome, got, c.vê)
		}
	}
}

func TestMalhaQuadIndexado(t *testing.T) {
	var m mesh
	m.reset(vec3{10, 0, 10})
	m.quad(vec3{10, 0, 10}, vec3{11, 0, 10}, vec3{11, 0, 11}, vec3{10, 0, 11}, color3{1, 1, 1})
	if m.nv() != 4 || len(m.idx) != 6 {
		t.Fatalf("quad deveria ter 4 vértices e 6 índices: %d, %d", m.nv(), len(m.idx))
	}
	if m.f[0] != 0 || m.f[2] != 0 {
		t.Errorf("vértices deveriam ser relativos à origem: %v", m.f[:3])
	}
	if ny := m.f[4]; math.Abs(float64(ny)) != 1 {
		t.Errorf("normal de um quad no chão deveria ser vertical: %v", m.f[3:6])
	}
}

// Em nenhum trecho o terreno de um lado pode avançar sobre outro trecho da
// estrada nem dobrar por dentro da curva: a borda do terreno deve estar mais
// perto do próprio segmento que de qualquer outro ponto da estrada.
func TestTerrenoNaoInvadeOutroTrecho(t *testing.T) {
	rs, err := game.LoadRoutes(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	presas, total := 0, 0
	for _, rt := range rs {
		g := game.NewGame(rt, 500, 30, nil)
		trk := buildTrack(g.Road)
		n := len(trk.pos) - 1
		for i := 0; i < n; i += 997 {
			l, r := trk.reach(i, 1.2)
			total += 2
			_, rv := dirs(trk.head[i])
			for _, s := range []struct {
				x    float64
				sign float64
			}{{l, -1}, {r, 1}} {
				if s.x <= 1.5+1e-9 { // preso no mínimo: curva fechada demais ou pista encostando noutro trecho (traçado, #14)
					presas++
					continue
				}
				edge := trk.pos[i].add(rv.mul(s.sign * s.x * hw))
				own := s.x * hw
				for j := 0; j < n; j += 3 {
					d := edge.sub(trk.pos[j])
					d.y = 0
					if d.length() < own*0.9 {
						t.Fatalf("%s seg %d: borda %s (%.0f un) está a %.0f un do seg %d",
							rt.Origem, i, map[float64]string{-1: "esquerda", 1: "direita"}[s.sign], own, d.length(), j)
					}
				}
			}
		}
	}
	t.Logf("bordas presas no acostamento (curva fechada ou pista encostada noutro trecho): %d de %d", presas, total)
}
