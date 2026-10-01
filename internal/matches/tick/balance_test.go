package tick

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
	"github.com/thewalpa/project-zimble/internal/matches/simple"
)

// The balance sweep plays the tick and simple engines on the same inputs
// and random seeds and prints a profile of each for docs/balance.md. It
// skips unless ZIMBLE_BALANCE=1:
//
//	ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalance -v -count=1

var (
	balanceSeeds = []random.Seed{1, 42, 2026}
	// balanceFixtures is the number of fixtures per seed and scenario.
	balanceFixtures = 1000
)

type balanceScenario struct {
	name         string
	home, away   int // enginetest strength, 1..100
	homeM, awayM matches.Mentality
}

func (sc balanceScenario) input(f ids.FixtureID) *matches.MatchInput {
	in := enginetest.Input(f, sc.home, sc.away)
	in.Home.Tactics.Mentality, in.Away.Tactics.Mentality = sc.homeM, sc.awayM
	// Every match is a knockout: the knockout rule leaves the 90 minutes
	// unchanged (enginetest's Knockouts check), so one run measures both
	// the regulation result and the shootout rate.
	in.Rules.Knockout = true
	return in
}

func balanced(name string, home, away int) balanceScenario {
	return balanceScenario{name, home, away, matches.Balanced, matches.Balanced}
}

func withMentality(name string, home, away matches.Mentality) balanceScenario {
	return balanceScenario{name, 60, 60, home, away}
}

var balanceScenarios = []balanceScenario{
	balanced("60 v 60", 60, 60),
	balanced("70 v 50", 70, 50),
	balanced("65 v 55", 65, 55),
	balanced("62 v 58", 62, 58),
	balanced("58 v 62", 58, 62),
	balanced("55 v 65", 55, 65),
	balanced("50 v 70", 50, 70),
	balanced("40 v 40", 40, 40),
	balanced("80 v 80", 80, 80),
	withMentality("both attacking", matches.Attacking, matches.Attacking),
	withMentality("both defensive", matches.Defensive, matches.Defensive),
	withMentality("home attacking", matches.Attacking, matches.Balanced),
	withMentality("home defensive", matches.Defensive, matches.Balanced),
	withMentality("away attacking", matches.Balanced, matches.Attacking),
	withMentality("away defensive", matches.Balanced, matches.Defensive),
	withMentality("attacking v defensive", matches.Attacking, matches.Defensive),
	withMentality("defensive v attacking", matches.Defensive, matches.Attacking),
}

// maxTally is the last bucket of the goal histograms: that many or more.
const maxTally = 6

// sideStatsSum accumulates one side's match statistics over a sweep.
type sideStatsSum struct {
	shots, onTarget, passes, completed, tackles, saves, offsides int
	possession                                                   int // permille summed over the matches
}

func (s *sideStatsSum) add(t matches.TeamStats) {
	s.shots += int(t.Shots)
	s.onTarget += int(t.ShotsOnTarget)
	s.passes += int(t.Passes)
	s.completed += int(t.PassesCompleted)
	s.tackles += int(t.Tackles)
	s.saves += int(t.Saves)
	s.offsides += int(t.Offsides)
	s.possession += int(t.PossessionPermille)
}

type matchProfile struct {
	n, home, draw, away int
	goals               [2]int
	total               [maxTally + 1]int    // matches by total goals
	perSide             [2][maxTally + 1]int // matches by one side's goals
	shootouts, penHome  int                  // shootouts, and those the home side won
	penKicks            int                  // penalties scored in shootouts
	stats               [2]sideStatsSum      // home and away, when the engine reports them
	statsN              int                  // matches whose statistics are available
}

func (p *matchProfile) add(o matches.MatchOutcome) {
	h, a := int(o.Score[0]), int(o.Score[1])
	p.n++
	p.goals[0] += h
	p.goals[1] += a
	p.total[min(h+a, maxTally)]++
	p.perSide[0][min(h, maxTally)]++
	p.perSide[1][min(a, maxTally)]++
	switch {
	case h > a:
		p.home++
	case a > h:
		p.away++
	default:
		p.draw++
	}
	if o.Resolution == matches.ResolutionPenalties {
		p.shootouts++
		p.penKicks += int(o.Shootout[0] + o.Shootout[1])
		if w, _ := o.Winner(); w == matches.Home {
			p.penHome++
		}
	}
	if o.Stats.Available {
		p.statsN++
		p.stats[0].add(o.Stats.Teams[0])
		p.stats[1].add(o.Stats.Teams[1])
	}
}

func pct(k, n int) float64 {
	if n == 0 {
		return 0
	}
	return 100 * float64(k) / float64(n)
}

// independentDraws is the draw percentage the two sides' goal counts would
// give if they were independent: a draw rate below it means the scores are
// anticorrelated (a leading side concedes more, or a trailing one scores
// more), not just that goals are high.
func (p *matchProfile) independentDraws() float64 {
	var sum float64
	for k := range p.perSide[0] {
		if k == maxTally {
			break // the open bucket is not a single score
		}
		sum += float64(p.perSide[0][k]) * float64(p.perSide[1][k])
	}
	return 100 * sum / float64(p.n) / float64(p.n)
}

type outcomeOrErr struct {
	o   matches.MatchOutcome
	err error
}

// sweep plays every fixture of every seed for sc on e, in parallel, and
// folds the outcomes in (seed, fixture) order.
func sweep(e matches.Engine, sc balanceScenario) (matchProfile, error) {
	n := len(balanceSeeds) * balanceFixtures
	results := make([]outcomeOrErr, n)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			var dst matches.MatchStepResult
			for i := range jobs {
				seed := balanceSeeds[i/balanceFixtures]
				f := ids.FixtureID(i%balanceFixtures + 1)
				results[i] = playOnce(e, sc.input(f), matches.FixtureRandom(seed, e.ID(), e.Version(), f), &dst)
			}
		})
	}
	for i := range n {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	var p matchProfile
	for i, r := range results {
		if r.err != nil {
			return p, fmt.Errorf("%s, %s, job %d: %w", e.ID(), sc.name, i, r.err)
		}
		p.add(r.o)
	}
	return p, nil
}

func playOnce(e matches.Engine, in *matches.MatchInput, rs matches.RandomState, dst *matches.MatchStepResult) outcomeOrErr {
	s, err := e.Start(in, rs)
	if err != nil {
		return outcomeOrErr{err: err}
	}
	for range 2 {
		if err := s.Advance(matches.AdvanceRequest{ToMinute: 90}, dst); err != nil {
			return outcomeOrErr{err: err}
		}
	}
	if dst.Status != matches.MatchFinished {
		return outcomeOrErr{err: fmt.Errorf("stopped at %+v", dst.Position)}
	}
	o := dst.Outcome
	o.Goals, o.Participants = nil, nil // dst's buffers are reused
	return outcomeOrErr{o: o}
}

func TestBalanceEngineComparison(t *testing.T) {
	if os.Getenv("ZIMBLE_BALANCE") != "1" {
		t.Skip("set ZIMBLE_BALANCE=1 to run the engine comparison sweep")
	}
	simpleEngine, err := simple.New(simple.DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	engines := []matches.Engine{simpleEngine, engine(t)}
	for _, e := range engines {
		t.Logf("%s v%d", e.ID(), e.Version())
	}
	t.Logf("seeds %v, %d fixtures each: %d matches per scenario and engine", balanceSeeds, balanceFixtures, len(balanceSeeds)*balanceFixtures)

	profiles := make([][]matchProfile, len(balanceScenarios))
	for i, sc := range balanceScenarios {
		for _, e := range engines {
			p, err := sweep(e, sc)
			if err != nil {
				t.Fatal(err)
			}
			profiles[i] = append(profiles[i], p)
		}
	}

	t.Log(profileTable(balanceScenarios, engines, profiles))

	var b strings.Builder
	b.WriteString("\nTotal goals per match, 60 v 60 (% of matches):\n\n| Engine | 0 | 1 | 2 | 3 | 4 | 5 | 6+ |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for j, p := range profiles[0] {
		fmt.Fprintf(&b, "| %s |", engines[j].ID())
		for _, k := range p.total {
			fmt.Fprintf(&b, " %.1f |", pct(k, p.n))
		}
		b.WriteString("\n")
	}
	t.Log(b.String())

	// Shootouts in knockouts are exactly the level matches, and a
	// shootout always has a winner.
	for i := range profiles {
		for j, p := range profiles[i] {
			if p.shootouts != p.draw {
				t.Errorf("%s, %s: %d shootouts, %d level matches", balanceScenarios[i].name, engines[j].ID(), p.shootouts, p.draw)
			}
		}
	}
}

// profileTable formats one table row per scenario and engine.
func profileTable(scenarios []balanceScenario, engines []matches.Engine, profiles [][]matchProfile) string {
	var b strings.Builder
	b.WriteString("\n| Scenario | Engine | Goals | Home–away goals | Home % | Draw % | Away % | Draw % if independent | 0–0 % | 4+ goals % | Shootout home win % | Penalties per shootout |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for i, sc := range scenarios {
		for j, p := range profiles[i] {
			fourPlus := 0
			for k := 4; k <= maxTally; k++ {
				fourPlus += p.total[k]
			}
			fmt.Fprintf(&b, "| %s | %s | %.2f | %.2f–%.2f | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f | %.0f | %.1f |\n",
				sc.name, engines[j].ID(), float64(p.goals[0]+p.goals[1])/float64(p.n),
				float64(p.goals[0])/float64(p.n), float64(p.goals[1])/float64(p.n),
				pct(p.home, p.n), pct(p.draw, p.n), pct(p.away, p.n), p.independentDraws(),
				pct(p.total[0], p.n), pct(fourPlus, p.n), pct(p.penHome, p.shootouts),
				float64(p.penKicks)/float64(max(p.shootouts, 1)))
		}
	}
	return b.String()
}

// statsScenarios profile tick's match statistics at the gaps a career
// produces and under mentality, for docs/balance.md's comparison with real
// football. The home side is listed first.
var statsScenarios = []balanceScenario{
	balanced("60 v 60", 60, 60),
	balanced("65 v 55", 65, 55),
	balanced("55 v 65", 55, 65),
	balanced("70 v 50", 70, 50),
	balanced("50 v 70", 50, 70),
	balanced("80 v 80", 80, 80),
	withMentality("both attacking", matches.Attacking, matches.Attacking),
	withMentality("both defensive", matches.Defensive, matches.Defensive),
	withMentality("home attacking", matches.Attacking, matches.Balanced),
}

// statsTable formats one row per scenario and side. n is the number of
// matches behind each row's means.
func statsTable(scenarios []balanceScenario, profiles []matchProfile) string {
	var b strings.Builder
	b.WriteString("\n| Scenario | Side | n | Shots | On target | Saves | Passes | Completion % | Tackles | Offsides | Possession % |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for i, sc := range scenarios {
		for side, name := range []string{"home", "away"} {
			s := &profiles[i].stats[side]
			n := float64(profiles[i].statsN)
			fmt.Fprintf(&b, "| %s | %s | %d | %.1f | %.1f | %.1f | %.0f | %.0f | %.1f | %.1f | %.1f |\n",
				sc.name, name, profiles[i].statsN,
				float64(s.shots)/n, float64(s.onTarget)/n, float64(s.saves)/n,
				float64(s.passes)/n, 100*float64(s.completed)/float64(max(s.passes, 1)),
				float64(s.tackles)/n, float64(s.offsides)/n, float64(s.possession)/n/10)
		}
	}
	return b.String()
}

// TestBalanceMatchStats plays tick on the stats scenarios and logs the
// profile table for docs/balance.md.
//
//	ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalanceMatchStats -v -count=1
func TestBalanceMatchStats(t *testing.T) {
	if os.Getenv("ZIMBLE_BALANCE") != "1" {
		t.Skip("set ZIMBLE_BALANCE=1 to run the match-statistics sweep")
	}
	e := engine(t)
	t.Logf("%s v%d: seeds %v, %d fixtures each: %d matches per scenario", e.ID(), e.Version(), balanceSeeds, balanceFixtures, len(balanceSeeds)*balanceFixtures)
	profiles := make([]matchProfile, len(statsScenarios))
	for i, sc := range statsScenarios {
		p, err := sweep(e, sc)
		if err != nil {
			t.Fatal(err)
		}
		if p.statsN != p.n {
			t.Fatalf("%s: statistics for %d of %d matches", sc.name, p.statsN, p.n)
		}
		profiles[i] = p
	}
	t.Log(statsTable(statsScenarios, profiles))
}

// mentalityByGapScenarios put a mentality on the underdog and on the
// favourite. The equal-team rows of balanceScenarios cannot show whether
// defensive is a tool for the weaker side.
var mentalityByGapScenarios = []balanceScenario{
	{"55 v 65, home balanced", 55, 65, matches.Balanced, matches.Balanced},
	{"55 v 65, underdog defensive", 55, 65, matches.Defensive, matches.Balanced},
	{"55 v 65, underdog attacking", 55, 65, matches.Attacking, matches.Balanced},
	{"65 v 55, favourite balanced", 65, 55, matches.Balanced, matches.Balanced},
	{"65 v 55, favourite attacking", 65, 55, matches.Attacking, matches.Balanced},
	{"65 v 55, favourite defensive", 65, 55, matches.Defensive, matches.Balanced},
	{"65 v 55, underdog defensive", 65, 55, matches.Balanced, matches.Defensive},
	{"65 v 55, underdog attacking", 65, 55, matches.Balanced, matches.Attacking},
	{"65 v 55, both attacking", 65, 55, matches.Attacking, matches.Attacking},
	{"65 v 55, fav attacking, underdog defensive", 65, 55, matches.Attacking, matches.Defensive},
}

// TestBalanceMentalityByGap: ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalanceMentalityByGap -v -count=1
func TestBalanceMentalityByGap(t *testing.T) {
	if os.Getenv("ZIMBLE_BALANCE") != "1" {
		t.Skip("set ZIMBLE_BALANCE=1 to run the mentality-by-gap sweep")
	}
	simpleEngine, err := simple.New(simple.DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	engines := []matches.Engine{simpleEngine, engine(t)}
	profiles := make([][]matchProfile, len(mentalityByGapScenarios))
	for i, sc := range mentalityByGapScenarios {
		for _, e := range engines {
			p, err := sweep(e, sc)
			if err != nil {
				t.Fatal(err)
			}
			profiles[i] = append(profiles[i], p)
		}
	}
	t.Log(profileTable(mentalityByGapScenarios, engines, profiles))
}
