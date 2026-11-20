package gpu

import "math"

// vec3 em float64: as contas de posição são feitas em precisão dupla e só
// viram float32 já relativas à câmera (evita tremedeira a milhares de km).
type vec3 struct{ x, y, z float64 }

func (a vec3) add(b vec3) vec3    { return vec3{a.x + b.x, a.y + b.y, a.z + b.z} }
func (a vec3) sub(b vec3) vec3    { return vec3{a.x - b.x, a.y - b.y, a.z - b.z} }
func (a vec3) mul(k float64) vec3 { return vec3{a.x * k, a.y * k, a.z * k} }
func (a vec3) dot(b vec3) float64 { return a.x*b.x + a.y*b.y + a.z*b.z }
func (a vec3) cross(b vec3) vec3 {
	return vec3{a.y*b.z - a.z*b.y, a.z*b.x - a.x*b.z, a.x*b.y - a.y*b.x}
}
func (a vec3) length() float64        { return math.Sqrt(a.dot(a)) }
func lerp3(a, b vec3, t float64) vec3 { return a.add(b.sub(a).mul(t)) }

func (a vec3) norm() vec3 {
	if l := a.length(); l > 0 {
		return a.mul(1 / l)
	}
	return a
}

var up3 = vec3{0, 1, 0}

// mat4 em ordem de coluna (como o WGSL espera): elemento (linha r, coluna c) em m[c*4+r].
type mat4 [16]float32

func (a mat4) mul(b mat4) mat4 {
	var m mat4
	for c := range 4 {
		for r := range 4 {
			var s float32
			for k := range 4 {
				s += a[k*4+r] * b[c*4+k]
			}
			m[c*4+r] = s
		}
	}
	return m
}

// perspective: projeção com profundidade 0..1 (convenção WebGPU), mão direita.
func perspective(fovY, aspect, near, far float64) mat4 {
	f := 1 / math.Tan(fovY/2)
	var m mat4
	m[0] = float32(f / aspect)
	m[5] = float32(f)
	m[10] = float32(far / (near - far))
	m[11] = -1
	m[14] = float32(near * far / (near - far))
	return m
}

// lookDir: rotação da câmera parada na origem, olhando para fwd.
func lookDir(fwd vec3) (m mat4, s, u, f vec3) {
	f = fwd.norm()
	s = f.cross(up3).norm()
	u = s.cross(f)
	m[0], m[4], m[8] = float32(s.x), float32(s.y), float32(s.z)
	m[1], m[5], m[9] = float32(u.x), float32(u.y), float32(u.z)
	m[2], m[6], m[10] = float32(-f.x), float32(-f.y), float32(-f.z)
	m[15] = 1
	return
}
