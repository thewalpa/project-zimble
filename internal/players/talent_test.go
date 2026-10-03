package players

import (
	"fmt"
	"hash/fnv"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
)

// developmentPin hashes Develop over 300 players and ages 14..40. The value
// was recorded from DevelopmentVersion 1 before the talent rules existed:
// neutral talent with no norms must reproduce it exactly.
func developmentPin(develop func(Profile, int, int) Attributes) string {
	h := fnv.New64a()
	for id := 1; id <= 300; id++ {
		var a Attributes
		for i := range a {
			a[i] = Rating(1 + (id*7+i*13)%100)
		}
		p := Profile{Player: ids.PlayerID(id), Position: Position(1 + id%4), Attributes: a}
		for age := 14; age <= 40; age++ {
			for _, r := range develop(p, age, 2025+id%9) {
				h.Write([]byte{byte(r)})
			}
		}
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

func TestNeutralTalentIsVersionOne(t *testing.T) {
	const want = "befafb6af7d7e9bf"
	if got := developmentPin(func(p Profile, age, year int) Attributes { return Develop(42, p, age, year) }); got != want {
		t.Errorf("Develop pin %s, want %s", got, want)
	}
	got := developmentPin(func(p Profile, age, year int) Attributes {
		out, err := DevelopWith(42, p, Talent{}, Norms{}, age, year)
		if err != nil {
			t.Fatal(err)
		}
		return out
	})
	if got != want {
		t.Errorf("neutral DevelopWith pin %s, want %s", got, want)
	}
}

// The archetype table is normalized: weights cover every player, and each
// knot's weighted mean rounds to zero tenths. Drawn curves keep that mean.
func TestTalentIsNormalized(t *testing.T) {
	total := 0
	var weighted [NumKnots]int
	for _, a := range Archetypes() {
		total += a.Weight()
		for k, v := range a.Knots() {
			weighted[k] += a.Weight() * v
		}
	}
	if total != 1000 {
		t.Fatalf("archetype weights sum to %d permille", total)
	}
	for k, w := range weighted {
		if 2*w >= total || 2*w <= -total {
			t.Errorf("knot %d (age %d): weighted mean %d/1000 tenths does not round to 0", k, KnotAges[k], w)
		}
	}
	const n = 40000
	var drawn [NumKnots]int
	for id := 1; id <= n; id++ {
		tl := DrawTalent(random.Derive(5, "test/talent", uint64(id)))
		for k, v := range tl.Knots {
			drawn[k] += v
		}
	}
	for k, sum := range drawn {
		if mean := float64(sum) / n; mean < -0.6 || mean > 0.6 {
			t.Errorf("knot %d: drawn mean %.2f tenths, want about 0", k, mean)
		}
	}
}

func TestDrawTalent(t *testing.T) {
	counts := map[Archetype]int{}
	const n = 50000
	for id := 1; id <= n; id++ {
		a := DrawTalent(random.Derive(9, "test/talent", uint64(id)))
		if b := DrawTalent(random.Derive(9, "test/talent", uint64(id))); a != b {
			t.Fatalf("player %d: draws differ: %+v %+v", id, a, b)
		}
		if err := a.Validate(); err != nil || a.Archetype == 0 {
			t.Fatalf("player %d: %+v: %v", id, a, err)
		}
		for k, v := range a.Knots {
			if mean := a.Archetype.Knots()[k]; v < mean-2*KnotVariation || v > mean+2*KnotVariation {
				t.Fatalf("player %d: knot %d is %d, archetype mean %d", id, k, v, mean)
			}
		}
		counts[a.Archetype]++
	}
	for _, a := range Archetypes() {
		want := a.Weight() * n / 1000
		if got := counts[a]; got < want-want/8-30 || got > want+want/8+30 {
			t.Errorf("%s: drawn %d of %d, want about %d", a, got, n, want)
		}
	}
}

func TestTalentCurve(t *testing.T) {
	tl := Talent{Archetype: LateBloomer, Knots: [NumKnots]int{-10, -9, 17, 15, 4, 0}}
	for k, age := range KnotAges {
		if got := tl.At(age); got != tl.Knots[k] {
			t.Errorf("age %d: %d, want knot %d", age, got, tl.Knots[k])
		}
	}
	if tl.At(14) != -10 || tl.At(40) != 0 {
		t.Errorf("outside the knots: %d at 14, %d at 40", tl.At(14), tl.At(40))
	}
	// Between knots it moves monotonically from one to the next.
	for k := 1; k < NumKnots; k++ {
		lo, hi := tl.Knots[k-1], tl.Knots[k]
		for age := KnotAges[k-1]; age < KnotAges[k]; age++ {
			a, b := tl.At(age), tl.At(age+1)
			if (hi >= lo && b < a) || (hi < lo && b > a) {
				t.Errorf("ages %d-%d: %d then %d between knots %d and %d", age, age+1, a, b, lo, hi)
			}
		}
	}
}

func TestTalentValidate(t *testing.T) {
	for name, tl := range map[string]Talent{
		"unknown archetype":  {Archetype: Ageless + 1},
		"knot too high":      {Archetype: Steady, Knots: [NumKnots]int{0, 0, 0, MaxKnot + 1, 0, 0}},
		"knot too low":       {Archetype: Steady, Knots: [NumKnots]int{-MaxKnot - 1, 0, 0, 0, 0, 0}},
		"neutral with knots": {Knots: [NumKnots]int{1, 0, 0, 0, 0, 0}},
	} {
		if tl.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := DevelopWith(1, validProfile(1), tl, Norms{}, 20, 2030); err == nil {
			t.Errorf("%s: DevelopWith accepted", name)
		}
	}
	if err := (Talent{Archetype: Ageless, Knots: [NumKnots]int{MaxKnot, -MaxKnot}}).Validate(); err != nil {
		t.Error(err)
	}
}

func TestWindows(t *testing.T) {
	for _, c := range []struct {
		g    Group
		age  int
		want int
	}{
		{Technical, 21, 1000}, {Technical, 22, 800}, {Technical, 23, 600}, {Technical, 24, 500},
		{Technical, 25, 400}, {Technical, 28, 400}, {Technical, 29, 1000},
		{Physical, 25, 1000}, {Physical, 26, 600}, {Physical, 28, 600}, {Physical, 29, 1000},
		{Reading, 16, 1000}, {Reading, 27, 1000}, {Reading, 36, 1000},
	} {
		if got := Window(c.g, c.age); got != c.want {
			t.Errorf("%s at %d: %d, want %d", c.g, c.age, got, c.want)
		}
	}
	for a := range Attribute(NumAttributes) {
		want := Technical
		switch a {
		case Pace, Acceleration, Stamina, Strength:
			want = Physical
		case Defending, Positioning:
			want = Reading
		}
		if GroupOf(a) != want {
			t.Errorf("%s in %s, want %s", a, GroupOf(a), want)
		}
	}
}

// testNorms: every attribute's middle at 50, goalkeeping a 15 floor for
// outfield players and finishing and dribbling 15 floors for goalkeepers.
func testNorms(t *testing.T) Norms {
	t.Helper()
	var list []Norm
	for _, p := range Positions() {
		nm := Norm{Position: p}
		for a := range nm.Mid {
			nm.Mid[a] = 50
		}
		floors := []Attribute{Goalkeeping}
		if p == Goalkeeper {
			floors = []Attribute{Finishing, Dribbling}
			nm.Mid[Goalkeeping] = 60
		}
		for _, a := range floors {
			nm.Mid[a], nm.Floor[a] = 15, true
		}
		list = append(list, nm)
	}
	n, err := NewNorms(list)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNewNormsRejects(t *testing.T) {
	good := Norm{Position: Midfielder}
	for a := range good.Mid {
		good.Mid[a] = 50
	}
	var all []Norm
	for _, p := range Positions() {
		nm := good
		nm.Position = p
		all = append(all, nm)
	}
	if _, err := NewNorms(all); err != nil {
		t.Fatal(err)
	}
	zeroMid := good
	zeroMid.Mid[Pace] = 0
	noReading := good
	noReading.Floor[Defending], noReading.Floor[Positioning] = true, true
	for name, list := range map[string][]Norm{
		"missing position": all[:3],
		"repeated":         append(append([]Norm{}, all...), good),
		"invalid position": append(append([]Norm{}, all[:3]...), Norm{Position: 9, Mid: good.Mid}),
		"zero middle":      append(append([]Norm{}, all[:2]...), zeroMid, all[3]),
		"no reading":       append(append([]Norm{}, all[:2]...), noReading, all[3]),
	} {
		if _, err := NewNorms(list); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Learning follows the shape of the profile, not its level, and is clamped.
func TestLearning(t *testing.T) {
	norms := testNorms(t)
	at := func(tech, phys, read int) Profile {
		p := Profile{Player: 1, Position: Midfielder}
		for a := range p.Attributes {
			v := 50
			switch GroupOf(Attribute(a)) {
			case Technical:
				v += tech
			case Physical:
				v += phys
			case Reading:
				v += read
			}
			p.Attributes[a] = Rating(v)
		}
		p.Attributes[Goalkeeping] = 3 // a floor: never counted
		return p
	}
	if got := norms.Learning(at(0, 0, 0)); got != [NumGroups]int{1000, 1000, 1000} {
		t.Errorf("at the norm: %v", got)
	}
	// The design's athlete: technique 5 below, physique 7 above, reading at
	// the norm: about 870 technical and 1170 physical.
	athlete := norms.Learning(at(-5, 7, 0))
	if athlete[Technical] < 850 || athlete[Technical] > 890 || athlete[Physical] < 1150 || athlete[Physical] > 1190 {
		t.Errorf("athlete: %v, want about 870 technical and 1170 physical", athlete)
	}
	if level := norms.Learning(at(5, 17, 10)); level != athlete {
		t.Errorf("the same shape 10 points higher learns %v, not %v", level, athlete)
	}
	if got := norms.Learning(at(-40, 40, 0)); got[Technical] != MinLearning || got[Physical] != MaxLearning {
		t.Errorf("extreme profile: %v, want the clamps", got)
	}
	if got := (Norms{}).Learning(at(-5, 7, 0)); got != [NumGroups]int{1000, 1000, 1000} {
		t.Errorf("without norms: %v", got)
	}
	// It applies to gains only: a 33-year-old declines the same either way.
	old := at(-5, 7, 0)
	a, _ := DevelopWith(3, old, Talent{}, norms, 33, 2040)
	b, _ := DevelopWith(3, old, Talent{}, Norms{}, 33, 2040)
	if a != b {
		t.Errorf("learning changed a decline: %v against %v", a, b)
	}
	// And it widens a young athlete's profile.
	young := at(-5, 7, 0)
	var tech, phys int
	for id := range 500 {
		young.Player = ids.PlayerID(id + 1)
		out, _ := DevelopWith(3, young, Talent{}, norms, 17, 2040)
		tech += int(out[Passing]) - int(young.Attributes[Passing])
		phys += int(out[Pace]) - int(young.Attributes[Pace])
	}
	if phys <= tech {
		t.Errorf("an athlete at 17 gained %d pace against %d passing over 500 players", phys, tech)
	}
}

// Development stays on the scale for a century of extreme curves and
// profiles, and is a pure function of its inputs.
func TestDevelopWithIsBoundedAndDeterministic(t *testing.T) {
	norms := testNorms(t)
	high := Talent{Archetype: Ageless, Knots: [NumKnots]int{MaxKnot, MaxKnot, MaxKnot, MaxKnot, MaxKnot, MaxKnot}}
	low := Talent{Archetype: Limited, Knots: [NumKnots]int{-MaxKnot, -MaxKnot, -MaxKnot, -MaxKnot, -MaxKnot, -MaxKnot}}
	for _, tl := range []Talent{high, low, {}} {
		for _, start := range []Rating{MinRating, 50, MaxRating} {
			p := Profile{Player: 11, Position: Forward}
			for a := range p.Attributes {
				p.Attributes[a] = start
			}
			for year := 0; year < 100; year++ {
				age := 14 + year%30
				out, err := DevelopWith(77, p, tl, norms, age, 2025+year)
				if err != nil {
					t.Fatal(err)
				}
				again, _ := DevelopWith(77, p, tl, norms, age, 2025+year)
				if out != again {
					t.Fatalf("not repeatable at year %d", year)
				}
				for a, r := range out {
					if !r.Valid() {
						t.Fatalf("%s %s at %d: %d", tl.Archetype, Attribute(a), age, r)
					}
				}
				p.Attributes = out
			}
		}
	}
}

// meanChange averages the change per attribute group over ages from..to
// for a cohort with one archetype's mean curve, starting from 50.
func meanChange(arch Archetype, from, to int) [NumGroups]float64 {
	tl := Talent{Archetype: arch, Knots: arch.Knots()}
	const n = 1500
	var sum [NumGroups]int
	var count [NumGroups]int
	for id := 1; id <= n; id++ {
		p := Profile{Player: ids.PlayerID(id), Position: Midfielder}
		for a := range p.Attributes {
			p.Attributes[a] = 50
		}
		start := p.Attributes
		for age := from; age <= to; age++ {
			p.Attributes, _ = DevelopWith(21, p, tl, Norms{}, age, 2000+age)
		}
		for a := range p.Attributes {
			g := GroupOf(Attribute(a))
			sum[g] += int(p.Attributes[a]) - int(start[a])
			count[g]++
		}
	}
	var out [NumGroups]float64
	for g := range out {
		out[g] = float64(sum[g]) / float64(count[g])
	}
	return out
}

func overallChange(c [NumGroups]float64) float64 {
	return (5*c[Technical] + 4*c[Physical] + 2*c[Reading]) / 11
}

func TestArchetypeShapes(t *testing.T) {
	neutral := func(from, to int) float64 { return overallChange(meanChange(0, from, to)) }
	general1721, general2327 := neutral(17, 21), neutral(23, 27)

	late := meanChange(LateBloomer, 17, 21)
	late2 := meanChange(LateBloomer, 23, 27)
	if overallChange(late) >= general1721 || overallChange(late2) <= general2327 {
		t.Errorf("late bloomer: %.1f at 17-21 (general %.1f), %.1f at 23-27 (general %.1f); want behind early, ahead late",
			overallChange(late), general1721, overallChange(late2), general2327)
	}
	if early := meanChange(EarlyPeaker, 23, 27); overallChange(early) >= 0 || early[Physical] >= 0 || early[Reading] >= 0 {
		t.Errorf("early peaker at 23-27: %v; want a loss", early)
	}
	if early := meanChange(EarlyPeaker, 17, 20); overallChange(early) <= neutral(17, 20) {
		t.Errorf("early peaker at 17-20 gains %.1f, general %.1f; want ahead", overallChange(early), neutral(17, 20))
	}
	if prodigy := meanChange(Prodigy, 17, 22); overallChange(prodigy) < neutral(17, 22)+4 {
		t.Errorf("prodigy at 17-22 gains %.1f, general %.1f; want well ahead", overallChange(prodigy), neutral(17, 22))
	}
	ageless := meanChange(Ageless, 34, 38)
	if ageless[Technical] < -1.5 || ageless[Reading] < -1.5 || ageless[Physical] > -2 {
		t.Errorf("ageless at 34-38: %v; want technique and reading held, physique still declining", ageless)
	}
	if fader := meanChange(EarlyFader, 27, 33); overallChange(fader) >= neutral(27, 33)-3 {
		t.Errorf("early fader at 27-33: %.1f, general %.1f; want a faster decline", overallChange(fader), neutral(27, 33))
	}
}

// A drawn population with learning keeps the general curve on average: the
// mean change at every age within half a point of Growth's, and each
// position's group means within a point of the same cohort developed by
// Growth alone.
func TestPopulationFollowsGrowth(t *testing.T) {
	norms := testNorms(t)
	const n = 2000
	type pair struct{ talent, neutral Profile }
	cohort := make([]pair, n)
	talents := make([]Talent, n)
	for i := range cohort {
		id := ids.PlayerID(i + 1)
		rng := random.Derive(13, "test/cohort", uint64(id))
		pos := Positions()[i%4]
		p := Profile{Player: id, Position: pos}
		nm := norms.byPosition[pos]
		for a := range p.Attributes {
			p.Attributes[a] = Rating(max(int(nm.Mid[a])-13+rng.IntRange(-8, 8), 1))
		}
		cohort[i] = pair{p, p}
		talents[i] = DrawTalent(rng)
	}
	for age := 16; age <= 36; age++ {
		var change, growth, counted float64
		for i := range cohort {
			c := &cohort[i]
			next, err := DevelopWith(31, c.talent, talents[i], norms, age, 2000+age)
			if err != nil {
				t.Fatal(err)
			}
			for a := range next {
				if norms.byPosition[c.talent.Position].Floor[a] {
					continue // near 1, where the scale's clamp biases the mean
				}
				change += float64(int(next[a]) - int(c.talent.Attributes[a]))
				growth += float64(Growth(age, Attribute(a)))
				counted++
			}
			c.talent.Attributes = next
			c.neutral.Attributes = Develop(31, c.neutral, age, 2000+age)
		}
		change /= counted
		growth /= counted
		if change < growth-0.5 || change > growth+0.5 {
			t.Errorf("age %d: mean change %.2f, Growth %.2f", age, change, growth)
		}
		for _, pos := range Positions() {
			var withTalent, plain [NumGroups]float64
			var count [NumGroups]float64
			for _, c := range cohort {
				if c.talent.Position != pos {
					continue
				}
				for a := range c.talent.Attributes {
					g := GroupOf(Attribute(a))
					withTalent[g] += float64(c.talent.Attributes[a])
					plain[g] += float64(c.neutral.Attributes[a])
					count[g]++
				}
			}
			for g := range withTalent {
				if d := (withTalent[g] - plain[g]) / count[g]; d < -1 || d > 1 {
					t.Errorf("age %d %s %s: mean %.2f from the general curve's", age, pos, Group(g), d)
				}
			}
		}
	}
}
