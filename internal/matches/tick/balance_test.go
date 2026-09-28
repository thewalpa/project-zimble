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

type matchProfile struct {
	n, home, draw, away int
	goals               [2]int
	total               [maxTally + 1]int    // matches by total goals
	perSide             [2][maxTally + 1]int // matches by one side's goals
	shootouts, penHome  int                  // shootouts, and those the home side won
	penKicks            int                  // penalties scored in shootouts
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

	var b strings.Builder
	b.WriteString("\n| Scenario | Engine | Goals | Home–away goals | Home % | Draw % | Away % | Draw % if independent | 0–0 % | 4+ goals % | Shootout home win % | Penalties per shootout |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for i, sc := range balanceScenarios {
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
	t.Log(b.String())

	b.Reset()
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
