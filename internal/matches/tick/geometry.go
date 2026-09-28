package tick

import (
	"math/bits"

	"github.com/thewalpa/project-zimble/internal/matches"
)

const (
	pitchL = matches.PitchLength
	pitchW = matches.PitchWidth
	// Penalty area: 16.5 m deep, 40.32 m wide.
	boxDepth    = 1650
	boxHalfWide = 2016
	postHalf    = matches.GoalWidth / 2
)

// vec is a point or a displacement in centimetres (or cm per tick).
type vec struct{ x, y int64 }

func (a vec) add(b vec) vec        { return vec{a.x + b.x, a.y + b.y} }
func (a vec) sub(b vec) vec        { return vec{a.x - b.x, a.y - b.y} }
func (a vec) dot(b vec) int64      { return a.x*b.x + a.y*b.y }
func (a vec) scale(n, d int64) vec { return vec{a.x * n / d, a.y * n / d} }
func (a vec) norm() int64          { return isqrt(a.dot(a)) }

func dist(a, b vec) int64 { return b.sub(a).norm() }

// dist2 is the squared distance, for comparisons without a square root.
func dist2(a, b vec) int64 { d := b.sub(a); return d.dot(d) }

// within reports whether a and b are at most r apart.
func within(a, b vec, r int64) bool { return dist2(a, b) <= r*r }

// withLength returns a in the same direction with length n (zero stays
// zero).
func (a vec) withLength(n int64) vec {
	l := a.norm()
	if l == 0 {
		return vec{}
	}
	return a.scale(n, l)
}

func (a vec) point() matches.PitchPoint { return matches.PitchPoint{X: int32(a.x), Y: int32(a.y)} }

func clampPitch(a vec) vec {
	return vec{min(max(a.x, 0), pitchL), min(max(a.y, 0), pitchW)}
}

// moveToward returns pos moved towards target by at most step.
func moveToward(pos, target vec, step int64) vec {
	if within(pos, target, step) {
		return target
	}
	d := target.sub(pos)
	return pos.add(d.scale(step, d.norm()))
}

// closest returns the parameter (permille of the segment from a to b) of
// the point on the segment closest to p, and that point's squared distance
// to p.
func closest(a, b, p vec) (t, d2 int64) {
	ab := b.sub(a)
	den := ab.dot(ab)
	if den > 0 {
		t = min(max(p.sub(a).dot(ab)*permille/den, 0), permille)
	}
	return t, dist2(a.add(ab.scale(t, permille)), p)
}

// isqrt returns floor(sqrt(n)) for n >= 0.
func isqrt(n int64) int64 {
	if n <= 0 {
		return 0
	}
	x := int64(1) << ((bits.Len64(uint64(n)) + 1) / 2) // >= sqrt(n)
	for {
		y := (x + n/x) / 2
		if y >= x {
			return x
		}
		x = y
	}
}
