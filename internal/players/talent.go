package players

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/core/random"
)

// Talent and learning: the rules of docs/squad-development-design.md,
// sections 1–4. A player's hidden talent curve speeds up, slows or reverses
// his development by age; what he learns easily is read every year from his
// own attributes against his position's norms. Nothing here is stored yet:
// with neutral talent and no norms DevelopWith is exactly Develop.

// NumKnots counts a talent curve's knots.
const NumKnots = 6

// KnotAges are the ages of a talent curve's knots: youth, breakthrough,
// establishment, peak, ageing and late career.
var KnotAges = [NumKnots]int{17, 20, 23, 27, 31, 34}

// MaxKnot bounds a knot: -MaxKnot..MaxKnot tenths of a point a year.
const MaxKnot = 40

// Archetype is the timing shape a talent curve is drawn from. Values are
// durable; never reorder. The zero value is no archetype: neutral talent.
type Archetype uint8

const (
	Steady      Archetype = 1
	Prodigy     Archetype = 2
	EarlyPeaker Archetype = 3
	LateBloomer Archetype = 4
	Limited     Archetype = 5
	Grafter     Archetype = 6
	Evergreen   Archetype = 7
	EarlyFader  Archetype = 8
	Ageless     Archetype = 9
)

type archetypeDef struct {
	name   string
	weight int // permille of players
	knots  [NumKnots]int
}

// archetypes is indexed by Archetype. The knot values are normalized: each
// knot's weighted mean over the drawable archetypes rounds to zero, so the
// average player follows Growth (TestTalentIsNormalized).
var archetypes = [...]archetypeDef{
	Steady:      {"steady", 320, [NumKnots]int{0, 1, 2, 0, -1, 0}},
	Prodigy:     {"prodigy", 40, [NumKnots]int{25, 21, 12, 0, -1, 0}},
	EarlyPeaker: {"early peaker", 110, [NumKnots]int{15, 6, -18, -10, -1, 0}},
	LateBloomer: {"late bloomer", 100, [NumKnots]int{-10, -9, 17, 15, 4, 0}},
	Limited:     {"limited", 130, [NumKnots]int{-10, -14, -13, -5, -1, 0}},
	Grafter:     {"grafter", 80, [NumKnots]int{0, 11, 12, 5, -1, 0}},
	Evergreen:   {"evergreen", 100, [NumKnots]int{0, 1, 2, 5, 14, 10}},
	EarlyFader:  {"early fader", 100, [NumKnots]int{0, 1, -3, -10, -16, -15}},
	Ageless:     {"ageless", 20, [NumKnots]int{0, 1, 2, 10, 24, 30}},
}

// Archetypes lists every archetype in value order.
func Archetypes() []Archetype {
	out := make([]Archetype, 0, len(archetypes)-1)
	for a := Steady; int(a) < len(archetypes); a++ {
		out = append(out, a)
	}
	return out
}

func (a Archetype) Valid() bool { return a >= Steady && int(a) < len(archetypes) }

func (a Archetype) String() string {
	if a.Valid() {
		return archetypes[a].name
	}
	return fmt.Sprintf("Archetype(%d)", uint8(a))
}

// Weight is the archetype's share of drawn players in permille.
func (a Archetype) Weight() int {
	if !a.Valid() {
		return 0
	}
	return archetypes[a].weight
}

// Knots returns the archetype's mean curve, before a player's variation.
func (a Archetype) Knots() [NumKnots]int {
	if !a.Valid() {
		return [NumKnots]int{}
	}
	return archetypes[a].knots
}

// Talent is a player's hidden timing: the archetype it was drawn from and
// its six knots, in tenths of a point a year at KnotAges. The zero value is
// neutral talent, which develops exactly like Develop.
type Talent struct {
	Archetype Archetype
	Knots     [NumKnots]int
}

// KnotVariation bounds each half of a knot's variation within its
// archetype: the sum of two -KnotVariation..KnotVariation draws.
const KnotVariation = 4

// DrawTalent draws a talent curve from rng: an archetype by weight, then
// each knot's centred variation. The caller keys rng by player.
func DrawTalent(rng *random.Stream) Talent {
	pick := rng.IntN(1000)
	t := Talent{Archetype: Steady}
	for _, a := range Archetypes() {
		if w := a.Weight(); pick < w {
			t.Archetype = a
			break
		} else {
			pick -= w
		}
	}
	t.Knots = t.Archetype.Knots()
	for k := range t.Knots {
		v := t.Knots[k] + rng.IntRange(-KnotVariation, KnotVariation) + rng.IntRange(-KnotVariation, KnotVariation)
		t.Knots[k] = min(max(v, -MaxKnot), MaxKnot)
	}
	return t
}

// Validate reports whether t is neutral or a known archetype with every
// knot within -MaxKnot..MaxKnot.
func (t Talent) Validate() error {
	if t.Archetype != 0 && !t.Archetype.Valid() {
		return fmt.Errorf("players: unknown talent archetype %d", t.Archetype)
	}
	if t.Archetype == 0 && t.Knots != ([NumKnots]int{}) {
		return fmt.Errorf("players: neutral talent with knots %v", t.Knots)
	}
	for k, v := range t.Knots {
		if v < -MaxKnot || v > MaxKnot {
			return fmt.Errorf("players: talent knot %d at age %d is %d, outside ±%d", k, KnotAges[k], v, MaxKnot)
		}
	}
	return nil
}

// At returns the talent curve at age, in tenths of a point a year: K1
// before 17, K6 after 34, and between knots linear interpolation truncated
// toward zero.
func (t Talent) At(age int) int {
	if age <= KnotAges[0] {
		return t.Knots[0]
	}
	for k := 1; k < NumKnots; k++ {
		if age <= KnotAges[k] {
			lo, hi := KnotAges[k-1], KnotAges[k]
			return t.Knots[k-1] + (t.Knots[k]-t.Knots[k-1])*(age-lo)/(hi-lo)
		}
	}
	return t.Knots[NumKnots-1]
}

// Group is a family of attributes that takes the talent curve through the
// same window and shares one learning factor.
type Group uint8

const (
	Technical Group = 0
	Physical  Group = 1
	Reading   Group = 2
	NumGroups       = 3
)

func (g Group) String() string {
	switch g {
	case Technical:
		return "technical"
	case Physical:
		return "physical"
	case Reading:
		return "reading"
	}
	return fmt.Sprintf("Group(%d)", uint8(g))
}

// GroupOf returns an attribute's group.
func GroupOf(a Attribute) Group {
	switch a {
	case Pace, Acceleration, Stamina, Strength:
		return Physical
	case Defending, Positioning:
		return Reading
	}
	return Technical
}

// Window returns the share of the talent curve, in permille, that a group
// takes at age. Technique is learned young, physique into the mid-twenties
// and reading the game at every age; from 29 every group takes the curve in
// full, which is then about ageing rather than learning.
func Window(g Group, age int) int {
	if age >= 29 {
		return 1000
	}
	switch g {
	case Technical:
		switch {
		case age <= 21:
			return 1000
		case age == 22:
			return 800
		case age == 23:
			return 600
		case age == 24:
			return 500
		}
		return 400
	case Physical:
		if age <= 25 {
			return 1000
		}
		return 600
	}
	return 1000
}

// Learning bounds and slope: a group's factor moves LearningSlope permille
// for each point its surplus stands above or below the player's own mean
// surplus, within MinLearning..MaxLearning.
const (
	LearningSlope = 25
	MinLearning   = 700
	MaxLearning   = 1300
)

// Norm is one position's reference profile for learning: the middle of
// each attribute's generated range, and the floors the position never uses
// (goalkeeping for outfield players; finishing and dribbling for
// goalkeepers), which do not count.
type Norm struct {
	Position Position
	Mid      Attributes
	Floor    [NumAttributes]bool
}

// Norms holds a Norm per position. The zero value has none, and then every
// learning factor is 1000.
type Norms struct {
	byPosition [Forward + 1]Norm
	set        [Forward + 1]bool
}

// NewNorms validates and copies one norm per position: every position
// exactly once, each with a valid Mid and at least one counted attribute in
// every group. An empty list gives the zero Norms.
func NewNorms(norms []Norm) (Norms, error) {
	var n Norms
	if len(norms) == 0 {
		return n, nil
	}
	for _, nm := range norms {
		if !nm.Position.Valid() || n.set[nm.Position] {
			return Norms{}, fmt.Errorf("players: norm for position %s is invalid or repeated", nm.Position)
		}
		var counted [NumGroups]int
		for a, r := range nm.Mid {
			if !r.Valid() {
				return Norms{}, fmt.Errorf("players: %s norm %s is %d", nm.Position, Attribute(a), r)
			}
			if !nm.Floor[a] {
				counted[GroupOf(Attribute(a))]++
			}
		}
		for g, c := range counted {
			if c == 0 {
				return Norms{}, fmt.Errorf("players: %s norm counts no %s attribute", nm.Position, Group(g))
			}
		}
		n.byPosition[nm.Position], n.set[nm.Position] = nm, true
	}
	for _, p := range Positions() {
		if !n.set[p] {
			return Norms{}, fmt.Errorf("players: no norm for position %s", p)
		}
	}
	return n, nil
}

// Learning returns a player's learning factor per group, in permille, from
// his current attributes: how far each group's mean surplus over his
// position's norm stands from his mean surplus over every counted
// attribute. It reflects the shape of his profile, not its level. With the
// zero Norms every factor is 1000.
func (n Norms) Learning(p Profile) [NumGroups]int {
	out := [NumGroups]int{1000, 1000, 1000}
	if !p.Position.Valid() || !n.set[p.Position] {
		return out
	}
	nm := n.byPosition[p.Position]
	var sum, count [NumGroups]int
	all, allCount := 0, 0
	for a, r := range p.Attributes {
		if nm.Floor[a] {
			continue
		}
		d := int(r) - int(nm.Mid[a])
		g := GroupOf(Attribute(a))
		sum[g] += d
		count[g]++
		all += d
		allCount++
	}
	mean := 1000 * all / allCount // permille of a point
	for g := range out {
		relative := 1000*sum[g]/count[g] - mean
		out[g] = min(max(1000+LearningSlope*relative/1000, MinLearning), MaxLearning)
	}
	return out
}

// DevelopWith returns a player's attributes after a year at age, with his
// talent curve and learning from his profile. For each attribute, in
// tenths of a point:
//
//	gain = 10×Growth(age, a) + talent.At(age)×Window(group, age)/1000
//	if gain > 0: gain = gain×Learning(group)/1000
//
// The gain is rounded to whole points, up with a probability of its tenths,
// and the player's form and the attribute's variation are added as in
// Develop, within MinRating..MaxRating. Form and variation come from
// Develop's stream and the rounding from its own, both keyed by the player
// and the year, so neutral talent with no norms reproduces Develop exactly
// and the result does not depend on order.
func DevelopWith(seed random.Seed, p Profile, talent Talent, norms Norms, age, year int) (Attributes, error) {
	if err := talent.Validate(); err != nil {
		return Attributes{}, err
	}
	learning := norms.Learning(p)
	curve := talent.At(age)
	rng := random.Derive(seed, "players/development", DevelopmentVersion, uint64(p.Player), uint64(year))
	var rounding *random.Stream
	form := rng.IntRange(-FormRange, FormRange)
	var out Attributes
	for a, r := range p.Attributes {
		g := GroupOf(Attribute(a))
		gain := 10*Growth(age, Attribute(a)) + curve*Window(g, age)/1000
		if gain > 0 {
			gain = gain * learning[g] / 1000
		}
		whole, tenths := gain/10, gain%10
		if tenths < 0 {
			whole, tenths = whole-1, tenths+10
		}
		if tenths > 0 {
			if rounding == nil {
				rounding = random.Derive(seed, "players/development-rounding", DevelopmentVersion, uint64(p.Player), uint64(year))
			}
			if rounding.IntN(10) < tenths {
				whole++
			}
		}
		v := int(r) + whole + form + rng.IntRange(-1, 1)
		out[a] = Rating(min(max(v, int(MinRating)), int(MaxRating)))
	}
	return out, nil
}
