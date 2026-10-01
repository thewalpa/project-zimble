package competitions

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/thewalpa/project-zimble/internal/core/sim"
)

func TestTimingSetsRoundKickoffs(t *testing.T) {
	timing := Timing{FirstKickoff: 1000, RoundInterval: sim.Week}
	s := New()
	if err := s.CreateLeagueSeason(42, league1, eight(), timing); err != nil {
		t.Fatal(err)
	}
	rounds := s.Rounds(league1)
	if len(rounds) != 14 {
		t.Fatalf("got %d rounds", len(rounds))
	}
	for i, r := range rounds {
		want := sim.GameInstant(1000) + sim.GameInstant(i)*sim.GameInstant(sim.Week)
		if r.Ref != (RoundRef{league1, Round(i + 1)}) || r.Kickoff != want || r.Status != RoundScheduled || len(r.Fixtures) != 4 {
			t.Fatalf("round %d = %+v, want kickoff %d", i+1, r, want)
		}
		for _, id := range r.Fixtures {
			if f, _ := s.Fixture(id); f.Kickoff != want || f.Round != r.Ref.Round {
				t.Fatalf("fixture %d = %+v, want round %d kickoff %d", id, f, r.Ref.Round, want)
			}
		}
	}
}

// Timing only moves kickoffs; pairings and fixture IDs are unchanged.
func TestTimingDoesNotAffectPairings(t *testing.T) {
	a := New()
	if err := a.CreateLeagueSeason(42, league1, eight(), Timing{FirstKickoff: 5, RoundInterval: sim.Day}); err != nil {
		t.Fatal(err)
	}
	b := create(t, 42, league1, eight())
	fa, fb := a.Fixtures(league1), b.Fixtures(league1)
	for i := range fa {
		fa[i].Kickoff, fb[i].Kickoff = 0, 0
	}
	if !reflect.DeepEqual(fa, fb) {
		t.Fatal("timing changed pairings or fixture IDs")
	}
}

func TestCreateRejectsInvalidTiming(t *testing.T) {
	cases := map[string]Timing{
		"zero interval":      {FirstKickoff: 0, RoundInterval: 0},
		"negative interval":  {FirstKickoff: 0, RoundInterval: -sim.Week},
		"huge interval":      {FirstKickoff: 0, RoundInterval: 1 << 62},
		"first out of range": {FirstKickoff: sim.MaxInstant + 1, RoundInterval: sim.Week},
		"last out of range":  {FirstKickoff: sim.MaxInstant - sim.GameInstant(sim.Week), RoundInterval: sim.Week},
	}
	for name, timing := range cases {
		s := New()
		if err := s.CreateLeagueSeason(42, league1, eight(), timing); err == nil {
			t.Errorf("%s: CreateLeagueSeason succeeded", name)
		}
		if len(s.Seasons()) != 0 {
			t.Errorf("%s: failed create changed the store", name)
		}
	}
}

// Explicit kickoffs set each round's, also for the later rounds of a
// knockout created from results; a season's Kickoffs are those CreateSeasons
// schedules, in every format.
func TestExplicitKickoffs(t *testing.T) {
	kickoffs := []sim.GameInstant{100, 5000, 9000}
	s := New()
	spec := NewSeason{Ref: cup1, Format: FormatKnockout, Entrants: bracket(), Timing: Timing{Kickoffs: kickoffs}}
	if err := s.CreateSeasons(1, []NewSeason{spec}); err != nil {
		t.Fatal(err)
	}
	playRound(t, s, 1, Score{HomeGoals: 1}, Score{HomeGoals: 1}, Score{HomeGoals: 1}, Score{HomeGoals: 1})
	for i, r := range s.Rounds(cup1) {
		if r.Kickoff != kickoffs[i] {
			t.Fatalf("round %d kicks off at %d, want %d", i+1, r.Kickoff, kickoffs[i])
		}
	}
	for _, f := range s.Fixtures(cup1) {
		if f.Kickoff != kickoffs[f.Round-1] {
			t.Fatalf("fixture %+v, want kickoff %d", f, kickoffs[f.Round-1])
		}
	}
	if got, err := spec.Kickoffs(); err != nil || !reflect.DeepEqual(got, kickoffs) {
		t.Fatalf("knockout Kickoffs = %v, %v", got, err)
	}
	league := NewSeason{Ref: league1, Format: FormatLeague, Entrants: eight(), Timing: Timing{FirstKickoff: 1000, RoundInterval: sim.Week}}
	if got, err := league.Kickoffs(); err != nil || len(got) != 14 || got[13] != 1000+13*sim.GameInstant(sim.Week) {
		t.Fatalf("league Kickoffs = %v, %v", got, err)
	}
	ties := NewSeason{Ref: SeasonRef{Competition: 1000, Season: 1}, Format: FormatTies, Entrants: eight(), Timing: weekly}
	if got, err := ties.Kickoffs(); err != nil || !reflect.DeepEqual(got, []sim.GameInstant{weekly.FirstKickoff}) {
		t.Fatalf("ties Kickoffs = %v, %v", got, err)
	}

	for name, timing := range map[string]Timing{
		"too few":            {Kickoffs: kickoffs[:2]},
		"too many":           {Kickoffs: append(slices.Clone(kickoffs), 10000)},
		"not ascending":      {Kickoffs: []sim.GameInstant{100, 9000, 5000}},
		"repeated":           {Kickoffs: []sim.GameInstant{100, 100, 9000}},
		"out of range":       {Kickoffs: []sim.GameInstant{100, 5000, sim.MaxInstant + 1}},
		"and a first":        {Kickoffs: kickoffs, FirstKickoff: 100},
		"and an interval":    {Kickoffs: kickoffs, RoundInterval: sim.Week},
		"empty, no interval": {Kickoffs: []sim.GameInstant{}},
	} {
		s := New()
		bad := spec
		bad.Timing = timing
		if err := s.CreateSeasons(1, []NewSeason{bad}); err == nil || len(s.Seasons()) != 0 {
			t.Errorf("%s: created %v (%v)", name, s.Seasons(), err)
		}
		if _, err := bad.Kickoffs(); err == nil {
			t.Errorf("%s: Kickoffs accepted the timing", name)
		}
	}
}

// A cup's rounds are spread over a season's matchdays as evenly as whole
// matchdays allow, the final after the last; a cup with more rounds than
// the season has matchdays does not fit.
func TestCupMatchdays(t *testing.T) {
	for _, c := range []struct {
		league, cup int
		want        []int
	}{
		{14, 3, []int{5, 10, 14}},
		{14, 1, []int{14}},
		{18, 3, []int{6, 12, 18}},
		{10, 4, []int{3, 5, 8, 10}},
		{2, 2, []int{1, 2}},
		{38, 5, []int{8, 16, 23, 31, 38}},
	} {
		got, err := CupMatchdays(c.league, c.cup)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("CupMatchdays(%d, %d) = %v, %v; want %v", c.league, c.cup, got, err, c.want)
		}
	}
	for league := 1; league <= 40; league++ {
		for cup := 1; cup <= league; cup++ {
			got, err := CupMatchdays(league, cup)
			if err != nil || got[len(got)-1] != league || got[0] < 1 {
				t.Fatalf("CupMatchdays(%d, %d) = %v, %v", league, cup, got, err)
			}
			for i := 1; i < len(got); i++ {
				if gap := got[i] - got[i-1]; gap < league/cup || gap > (league+cup-1)/cup {
					t.Fatalf("CupMatchdays(%d, %d) = %v: uneven gap %d", league, cup, got, gap)
				}
			}
		}
	}
	for _, c := range [][2]int{{2, 3}, {0, 1}, {14, 0}} {
		if got, err := CupMatchdays(c[0], c[1]); err == nil {
			t.Errorf("CupMatchdays(%d, %d) = %v", c[0], c[1], got)
		}
	}
}

func TestBeginRoundsMarksAwaitingResultsOnce(t *testing.T) {
	s := create(t, 42, league1, eight())
	r1 := RoundRef{league1, 1}
	kickoff := s.Rounds(league1)[0].Kickoff
	before := s.Fixtures(league1)

	if err := s.BeginRounds([]RoundRef{r1}, kickoff); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Round(r1)
	if got.Status != RoundAwaitingResults {
		t.Fatalf("status = %s", got.Status)
	}
	if !reflect.DeepEqual(s.Fixtures(league1), before) {
		t.Fatal("BeginRounds changed fixtures")
	}
	if p := s.PendingRounds(); len(p) != 1 || p[0].Ref != r1 {
		t.Fatalf("PendingRounds = %+v", p)
	}
	if err := s.BeginRounds([]RoundRef{r1}, kickoff); err == nil {
		t.Fatal("round dispatched twice")
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBeginRoundsIsAllOrNothing(t *testing.T) {
	s := create(t, 42, league1, eight())
	r1, r2 := RoundRef{league1, 1}, RoundRef{league1, 2}
	k1 := s.Rounds(league1)[0].Kickoff
	cases := map[string][]RoundRef{
		"empty":          nil,
		"unknown season": {r1, {SeasonRef{9, 1}, 1}},
		"round 0":        {r1, {league1, 0}},
		"round 15":       {r1, {league1, 15}},
		"duplicate":      {r1, r1},
		"wrong kickoff":  {r1, r2},
	}
	for name, refs := range cases {
		if err := s.BeginRounds(refs, k1); err == nil {
			t.Errorf("%s: BeginRounds succeeded", name)
		}
		if len(s.PendingRounds()) != 0 {
			t.Fatalf("%s: failed BeginRounds left a round pending", name)
		}
	}
}

func TestPendingRoundsOrderIsDeterministic(t *testing.T) {
	s := New()
	// Created out of competition order, all kicking off together.
	for _, comp := range []SeasonRef{{3, 1}, {1, 2}, {2, 1}, {1, 1}} {
		if err := s.CreateLeagueSeason(42, comp, eight(), weekly); err != nil {
			t.Fatal(err)
		}
	}
	k := sim.GameInstant(sim.Week)
	refs := []RoundRef{{SeasonRef{2, 1}, 1}, {SeasonRef{1, 2}, 1}, {SeasonRef{3, 1}, 1}, {SeasonRef{1, 1}, 1}}
	if err := s.BeginRounds(refs, k); err != nil {
		t.Fatal(err)
	}
	var got []SeasonRef
	for _, r := range s.PendingRounds() {
		got = append(got, r.Ref.Season)
	}
	want := []SeasonRef{{1, 1}, {1, 2}, {2, 1}, {3, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pending order = %v, want %v", got, want)
	}
}

func TestRoundQueriesReturnCopies(t *testing.T) {
	s := create(t, 42, league1, eight())
	rounds := s.Rounds(league1)
	rounds[0].Fixtures[0] = 999
	rounds[0].Status = RoundAwaitingResults
	if again := s.Rounds(league1); again[0].Fixtures[0] == 999 || again[0].Status != RoundScheduled {
		t.Fatal("Rounds exposed internal state")
	}
	if _, ok := s.Round(RoundRef{league1, 15}); ok {
		t.Fatal("Round found round 15")
	}
}

// Season kickoffs keep the first season's weekday and time and stay within
// three days of its anniversary for centuries, so the season never drifts
// against the civil calendar. Seasons are 52 or 53 weeks apart.
func TestSeasonKickoffFollowsTheAnniversary(t *testing.T) {
	cal, err := sim.NewCalendar(sim.CivilTime{Year: 2025, Month: 7, Day: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, first := range []sim.CivilTime{
		{Year: 2025, Month: 8, Day: 9, Hour: 15},               // the default leagues' Saturday
		{Year: 2025, Month: 12, Day: 31, Hour: 20, Minute: 45}, // the anniversary crosses the year end
		{Year: 2028, Month: 2, Day: 29, Hour: 19},              // a leap day
	} {
		t0 := time.Date(first.Year, time.Month(first.Month), first.Day, first.Hour, first.Minute, 0, 0, time.UTC)
		var prev sim.GameInstant
		for season := Season(1); season <= 400; season++ {
			k, err := SeasonKickoff(cal, first, season)
			if err != nil {
				t.Fatalf("%s season %d: %v", first, season, err)
			}
			c, _ := cal.Civil(k)
			got := time.Date(c.Year, time.Month(c.Month), c.Day, c.Hour, c.Minute, 0, 0, time.UTC)
			anniversary := time.Date(first.Year+int(season)-1, time.Month(first.Month), first.Day, first.Hour, first.Minute, 0, 0, time.UTC)
			if off := got.Sub(anniversary); got.Weekday() != t0.Weekday() || c.Hour != first.Hour || c.Minute != first.Minute || off < -3*24*time.Hour || off > 3*24*time.Hour {
				t.Fatalf("%s season %d kicks off %s, %v from %s", first, season, cal.Format(k), off, anniversary)
			}
			if season == 1 && got != t0 {
				t.Fatalf("%s season 1 kicks off %s", first, cal.Format(k))
			}
			if gap := k - prev; season > 1 && gap != 52*sim.GameInstant(sim.Week) && gap != 53*sim.GameInstant(sim.Week) {
				t.Fatalf("%s season %d starts %d minutes after season %d", first, season, gap, season-1)
			}
			prev = k
		}
	}
}

// For the default leagues the anchored calendar kept seasons 2 and 3 where
// 52-week seasons put them; season 4 is the first that moved.
func TestSeasonKickoffDefaultDates(t *testing.T) {
	cal, _ := sim.NewCalendar(sim.CivilTime{Year: 2025, Month: 7, Day: 1})
	first := sim.CivilTime{Year: 2025, Month: 8, Day: 9, Hour: 15}
	want := []string{"Sat 2025-08-09 15:00 UTC", "Sat 2026-08-08 15:00 UTC", "Sat 2027-08-07 15:00 UTC", "Sat 2028-08-12 15:00 UTC", "Sat 2053-08-09 15:00 UTC"}
	for i, season := range []Season{1, 2, 3, 4, 29} {
		k, err := SeasonKickoff(cal, first, season)
		if err != nil || cal.Format(k) != want[i] {
			t.Fatalf("season %d kicks off %s (%v), want %s", season, cal.Format(k), err, want[i])
		}
	}
	for name, f := range map[string]func() error{
		"season 0": func() error { _, err := SeasonKickoff(cal, first, 0); return err },
		"invalid civil": func() error {
			_, err := SeasonKickoff(cal, sim.CivilTime{Year: 2025, Month: 2, Day: 30}, 1)
			return err
		},
		"out of range":  func() error { _, err := SeasonKickoff(cal, first, 60000); return err },
		"zero calendar": func() error { _, err := SeasonKickoff(sim.Calendar{}, first, 1); return err },
	} {
		if f() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
