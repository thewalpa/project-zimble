package content

import (
	"cmp"
	"fmt"
	"io"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/players"
)

// Set is everything a career is built from: the generation definitions and
// the competition definitions. It holds no runtime state.
type Set struct {
	Definitions Definitions
	Leagues     []League
	Promotions  []Promotion
	Cups        []Cup
}

// DefaultSet returns the built-in definitions together with the built-in
// leagues, promotion links and cups.
func DefaultSet() Set {
	return Set{Definitions: Default(), Leagues: DefaultLeagues(), Promotions: DefaultPromotions(), Cups: DefaultCups()}
}

// Report describes a set to help balance work: what it defines, the figures
// that follow from it (clubs, squads, expected ratings, wages) and every
// problem found. Problems include what each part's own Validate finds and
// whether the leagues line up with the clubs the nations generate. A report
// is derived on demand and never stored.
type Report struct {
	DefinitionsVersion int
	LeagueVersion      int

	Nations   []NationReport
	Clubs     int
	SquadSize int
	SquadMax  int
	Roster    []QuotaReport
	Leagues   []LeagueReport
	Cups      []CupReport
	Wages     []WageReport

	// Problems lists what is wrong, in a stable order; empty when the set is
	// consistent.
	Problems []string
}

// NationReport is one nation's clubs and name pools.
type NationReport struct {
	Name string
	// Divisions lists each division's clubs and the towns left unused.
	Divisions []DivisionReport
	// Names is the number of distinct first/last name combinations.
	FirstNames, LastNames, Names int
}

// DivisionReport is one division of a nation.
type DivisionReport struct {
	Clubs, SpareTowns int
}

// QuotaReport is a position's quota and the overall its generated players
// can have, from the lowest range to the highest, and for a youth intake.
type QuotaReport struct {
	Position   players.Position
	Count, Min int
	// Overall is the overall rating at the minimum, the middle and the
	// maximum of the position's ranges; Youth is the same for the youth
	// intake.
	Overall, Youth [3]int
}

// LeagueReport is a league and the block of generated clubs it takes.
type LeagueReport struct {
	ID       ids.CompetitionID
	Name     string
	Entrants int
	Rounds   int
	// Nation and Tier name the division whose clubs fill it ("" and 0 when
	// the league matches no division). Tier counts from 1, the top.
	Nation string
	Tier   int
	// Below is the ID of the league below that exchanges places with it,
	// zero if none; Places is how many.
	Below  ids.CompetitionID
	Places int
}

// CupReport is a cup and its bracket size.
type CupReport struct {
	ID         ids.CompetitionID
	Name       string
	Entrants   int
	Rounds     int
	Qualifiers []QualifierReport
}

// QualifierReport is one qualifying league's places.
type QualifierReport struct {
	League ids.CompetitionID
	Places int
}

// WageReport is the weekly demand and offer ceiling for an overall rating.
type WageReport struct {
	Overall int
	Demand  string
	Ceiling string
}

// wageOveralls are the ratings the report prices.
var wageOveralls = []int{30, 50, 70, 90}

// Describe builds the report of a set.
func Describe(s Set) Report {
	d := s.Definitions
	r := Report{
		DefinitionsVersion: d.Version,
		LeagueVersion:      LeagueVersion,
		Clubs:              d.ClubCount(),
		SquadSize:          d.SquadSize(),
		SquadMax:           d.SquadLimit,
	}
	if err := d.Validate(); err != nil {
		r.Problems = append(r.Problems, err.Error())
	}
	for _, na := range d.Nations {
		nr := NationReport{Name: na.Name, FirstNames: len(na.FirstNames), LastNames: len(na.LastNames), Names: len(na.FirstNames) * len(na.LastNames)}
		for _, dv := range na.Divisions {
			nr.Divisions = append(nr.Divisions, DivisionReport{Clubs: dv.Clubs, SpareTowns: len(dv.Towns) - dv.Clubs})
		}
		r.Nations = append(r.Nations, nr)
	}
	for _, q := range d.Roster {
		qr := QuotaReport{Position: q.Position, Count: q.Count, Min: q.Min}
		if pp, ok := d.Profile(q.Position); ok {
			qr.Overall = profileOveralls(pp, func(v Range) Range { return v })
			qr.Youth = profileOveralls(pp, d.Youth.Range)
		}
		r.Roster = append(r.Roster, qr)
	}
	for _, o := range wageOveralls {
		r.Wages = append(r.Wages, WageReport{Overall: o, Demand: d.Economy.Demand(o).String(), Ceiling: d.Economy.OfferCeiling(o).String()})
	}
	r.describeCompetitions(s)
	return r
}

// profileOveralls returns the overall rating a player has when every
// attribute sits at the minimum, the middle and the maximum of its range
// (after adjust).
func profileOveralls(pp PositionProfile, adjust func(Range) Range) [3]int {
	var out [3]int
	for i := range out {
		prof := players.Profile{Position: pp.Position}
		for a, rg := range pp.Ranges {
			rg = adjust(rg)
			v := [3]int{int(rg.Min), (int(rg.Min) + int(rg.Max)) / 2, int(rg.Max)}[i]
			prof.Attributes[a] = players.Rating(v)
		}
		out[i] = prof.Overall()
	}
	return out
}

// block is a division's clubs, in generation order.
type block struct {
	nation  string
	tier    int
	clubs   int
	startAt int
}

// blocks lists the divisions in generation order: every nation's top division
// first, then every nation's second division, and so on.
func (d Definitions) blocks() []block {
	var out []block
	start := 0
	for tier := 0; ; tier++ {
		more := false
		for _, na := range d.Nations {
			if tier < len(na.Divisions) {
				more = true
				out = append(out, block{nation: na.Name, tier: tier + 1, clubs: na.Divisions[tier].Clubs, startAt: start})
				start += na.Divisions[tier].Clubs
			}
		}
		if !more {
			return out
		}
	}
}

func (r *Report) describeCompetitions(s Set) {
	problem := func(format string, args ...any) { r.Problems = append(r.Problems, fmt.Sprintf(format, args...)) }
	leagues := slices.Clone(s.Leagues)
	slices.SortFunc(leagues, func(a, b League) int { return cmp.Compare(a.ID, b.ID) })
	blocks := s.Definitions.blocks()
	known := map[ids.CompetitionID]bool{}
	total, offset := 0, 0
	for i, l := range leagues {
		known[l.ID] = true
		if err := l.Validate(); err != nil {
			problem("%v", err)
		}
		if i > 0 && leagues[i-1].ID == l.ID {
			problem("content: duplicate league ID %d", l.ID)
		}
		lr := LeagueReport{ID: l.ID, Name: l.Name, Entrants: l.Entrants, Rounds: l.Rounds()}
		for _, b := range blocks {
			if b.startAt == offset && b.clubs == l.Entrants {
				lr.Nation, lr.Tier = b.nation, b.tier
				break
			}
		}
		if lr.Tier == 0 {
			problem("content: league %d (%d entrants, clubs %d..%d) matches no division of the nations", l.ID, l.Entrants, offset+1, offset+l.Entrants)
		}
		for _, p := range s.Promotions {
			if p.Upper == lr.ID {
				lr.Below, lr.Places = p.Lower, p.Places
			}
		}
		offset += l.Entrants
		total += l.Entrants
		r.Leagues = append(r.Leagues, lr)
	}
	if total != r.Clubs {
		problem("content: leagues take %d entrants but the nations generate %d clubs", total, r.Clubs)
	}
	if err := ValidatePromotions(s.Leagues, s.Promotions); err != nil {
		problem("%v", err)
	}
	cups := slices.Clone(s.Cups)
	slices.SortFunc(cups, func(a, b Cup) int { return cmp.Compare(a.ID, b.ID) })
	taken := map[ids.CompetitionID]bool{}
	for _, l := range leagues {
		taken[l.ID] = true
	}
	for _, c := range cups {
		if err := c.Validate(); err != nil {
			problem("%v", err)
		}
		if taken[c.ID] {
			problem("content: cup %d reuses a competition ID", c.ID)
		}
		taken[c.ID] = true
		cr := CupReport{ID: c.ID, Name: c.Name, Entrants: c.Entrants()}
		for n := c.Entrants(); n > 1; n /= 2 {
			cr.Rounds++
		}
		for _, q := range c.Qualifiers {
			cr.Qualifiers = append(cr.Qualifiers, QualifierReport{League: q.League, Places: q.Places})
			size := 0
			for _, l := range leagues {
				if l.ID == q.League {
					size = l.Entrants
				}
			}
			if !known[q.League] || q.Places > size {
				problem("content: cup %d takes %d places from league %d, which is missing or smaller", c.ID, q.Places, q.League)
			}
		}
		r.Cups = append(r.Cups, cr)
	}
}

// Write prints the report as plain text, one section per topic.
func (r Report) Write(w io.Writer) error {
	var err error
	p := func(format string, args ...any) {
		if err == nil {
			_, err = fmt.Fprintf(w, format+"\n", args...)
		}
	}
	p("content version %d, league version %d", r.DefinitionsVersion, r.LeagueVersion)
	p("clubs %d, senior squad %d (limit %d)", r.Clubs, r.SquadSize, r.SquadMax)
	p("")
	p("nations")
	for _, n := range r.Nations {
		p("  %s: %d first names, %d last names, %d combinations", n.Name, n.FirstNames, n.LastNames, n.Names)
		for i, dv := range n.Divisions {
			p("    division %d: %d clubs, %d spare towns", i+1, dv.Clubs, dv.SpareTowns)
		}
	}
	p("")
	p("roster (overall at the lowest, middle and highest of the ranges)")
	for _, q := range r.Roster {
		p("  %s: %d (min %d), senior %d/%d/%d, youth %d/%d/%d", q.Position, q.Count, q.Min,
			q.Overall[0], q.Overall[1], q.Overall[2], q.Youth[0], q.Youth[1], q.Youth[2])
	}
	p("")
	p("leagues")
	for _, l := range r.Leagues {
		where := "no matching division"
		if l.Tier > 0 {
			where = fmt.Sprintf("%s division %d", l.Nation, l.Tier)
		}
		p("  %d %s: %d entrants, %d rounds, %s", l.ID, l.Name, l.Entrants, l.Rounds, where)
		if l.Below != 0 {
			p("    exchanges %d places with league %d", l.Places, l.Below)
		}
	}
	p("")
	p("cups")
	for _, c := range r.Cups {
		p("  %d %s: %d entrants, %d rounds", c.ID, c.Name, c.Entrants, c.Rounds)
		for _, q := range c.Qualifiers {
			p("    top %d of league %d", q.Places, q.League)
		}
	}
	p("")
	p("weekly wages (demand, offer ceiling)")
	for _, wg := range r.Wages {
		p("  overall %d: %s, %s", wg.Overall, wg.Demand, wg.Ceiling)
	}
	p("")
	if len(r.Problems) == 0 {
		p("no problems")
	}
	for _, pr := range r.Problems {
		p("problem: %s", pr)
	}
	return err
}
