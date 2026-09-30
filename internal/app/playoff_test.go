package app

import (
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// After a linked league season ends its play-off follows: one FormatTies
// round, the bottom of the upper division at home to the top of the lower
// (competitions.PlayoffPairings), competitions.PlayoffDelay after the
// linked leagues' last kickoffs. The next league seasons wait for every
// play-off of the group and take the decided movement
// (competitions.NextEntrantsAfterPlayoffs); the cup edition is drawn only
// once the qualifying leagues have moved on.
func TestPlayoffFollowsLinkedSeasons(t *testing.T) {
	w := newWorld(t, 42)
	playLeagues(t, w)

	rankings := map[ids.CompetitionID][]ids.TeamID{}
	ends := map[ids.CompetitionID]sim.GameInstant{}
	for _, l := range w.leagues {
		ref := competitions.SeasonRef{Competition: l.def.ID, Season: 1}
		rankings[l.def.ID] = w.competitions.Ranking(ref)
		rounds := w.competitions.Rounds(ref)
		ends[l.def.ID] = rounds[len(rounds)-1].Kickoff
	}
	for _, link := range w.movementLinks() {
		comp, ok := w.playoffCompetition(link)
		if !ok {
			t.Fatalf("link %+v has no play-off competition", link)
		}
		ref := competitions.SeasonRef{Competition: comp, Season: 1}
		if f, _ := w.competitions.Format(ref); f != competitions.FormatTies {
			t.Fatalf("%s is %s, want ties", ref, f)
		}
		pairs, err := competitions.PlayoffPairings(link, rankings)
		if err != nil {
			t.Fatal(err)
		}
		var want []ids.TeamID
		for _, p := range pairs {
			want = append(want, p[0], p[1])
		}
		if got, _ := w.competitions.Entrants(ref); !slices.Equal(got, want) {
			t.Fatalf("%s entrants %v, want %v", ref, got, want)
		}
		last := max(ends[link.Upper], ends[link.Lower])
		wantKickoff, err := last.Add(competitions.PlayoffDelay)
		if err != nil {
			t.Fatal(err)
		}
		if rounds := w.competitions.Rounds(ref); len(rounds) != 1 || rounds[0].Kickoff != wantKickoff {
			t.Fatalf("%s rounds %v, want one at %d", ref, rounds, wantKickoff)
		}
	}
	// The next seasons wait for the play-offs: every default league is linked.
	for _, l := range w.leagues {
		if _, exists := w.competitions.Entrants(competitions.SeasonRef{Competition: l.def.ID, Season: 2}); exists {
			t.Fatalf("league %d season 2 created before the play-offs", l.def.ID)
		}
	}

	playPlayoffs(t, w)
	for _, l := range w.leagues {
		next := competitions.SeasonRef{Competition: l.def.ID, Season: 2}
		got, ok := w.competitions.Entrants(next)
		if !ok {
			t.Fatalf("%s not created after the play-offs", next)
		}
		want, err := w.entrantsAfter(l.def.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s entrants %v, want %v", next, got, want)
		}
	}
	// The cup is drawn only once the qualifying leagues have moved on, and
	// its first round leaves the play-off's week free.
	cups := w.Cups()
	if len(cups) != 1 || cups[0].Edition != 1 || len(cups[0].Rounds) == 0 {
		t.Fatalf("cups after the play-offs: %+v", cups)
	}
	var last sim.GameInstant
	for _, at := range ends {
		last = max(last, at)
	}
	wantFirst, err := last.Add(w.cups[0].FirstRoundDelay)
	if err != nil || cups[0].Rounds[0].Kickoff != wantFirst {
		t.Fatalf("cup first round %d, want %d (%v)", cups[0].Rounds[0].Kickoff, wantFirst, err)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// A save taken with the play-offs waiting continues identically through the
// ties, the movement and the cup draw.
func TestPlayoffSaveContinuesIdentically(t *testing.T) {
	w := newWorld(t, 42)
	playLeagues(t, w)
	saved := roundTrip(t, w)
	for _, x := range []*World{w, saved} {
		playPlayoffs(t, x)
		playCup(t, x)
	}
	if !reflect.DeepEqual(snapshot(w), snapshot(saved)) {
		t.Fatal("saving before the play-offs changed the career")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// positionOf returns the team's 1-based place in the league's season-1 table.
func positionOf(t *testing.T, w *World, comp ids.CompetitionID, team ids.TeamID) (competitions.SeasonRef, int) {
	t.Helper()
	ref := competitions.SeasonRef{Competition: comp, Season: 1}
	tab, ok := w.Table(ref)
	if !ok {
		t.Fatalf("no table for %s", ref)
	}
	for i, row := range tab.Rows {
		if row.Team == team {
			return ref, i + 1
		}
	}
	t.Fatalf("team %d not in %s", team, ref)
	return ref, 0
}

// InPlayoff marks a linked table's play-off places from the start;
// SeasonMove reports a team's destination only once the play-offs decide it.
func TestPlayoffViewsDecideMovement(t *testing.T) {
	w := newWorld(t, 42)
	playLeagues(t, w)

	for _, link := range w.movementLinks() {
		if up, down := w.PromotionPlaces(link.Upper); up != 0 || down != link.Places {
			t.Fatalf("PromotionPlaces(%d) = %d, %d want 0, %d", link.Upper, up, down, link.Places)
		}
		if up, down := w.PromotionPlaces(link.Lower); up != link.Places || down != 0 {
			t.Fatalf("PromotionPlaces(%d) = %d, %d want %d, 0", link.Lower, up, down, link.Places)
		}
		rankings := map[ids.CompetitionID][]ids.TeamID{
			link.Upper: w.competitions.Ranking(competitions.SeasonRef{Competition: link.Upper, Season: 1}),
			link.Lower: w.competitions.Ranking(competitions.SeasonRef{Competition: link.Lower, Season: 1}),
		}
		// The zones are the play-off places, every zone team is in one, and
		// nothing has a destination until the ties are decided.
		n := len(rankings[link.Upper])
		for i := range n {
			upTeam := rankings[link.Upper][i]
			ref, pos := positionOf(t, w, link.Upper, upTeam)
			if want := pos > n-link.Places; want != w.InPlayoff(ref, pos) {
				t.Fatalf("InPlayoff(%s, %d) = %t, want %t", ref, pos, w.InPlayoff(ref, pos), want)
			}
			if promoted, relegated := w.SeasonMove(ref, pos); promoted || relegated {
				t.Fatalf("SeasonMove(%s, %d) before the play-offs = %t, %t", ref, pos, promoted, relegated)
			}
			lowTeam := rankings[link.Lower][i]
			ref, pos = positionOf(t, w, link.Lower, lowTeam)
			if want := pos <= link.Places; want != w.InPlayoff(ref, pos) {
				t.Fatalf("InPlayoff(%s, %d) = %t, want %t", ref, pos, w.InPlayoff(ref, pos), want)
			}
			if promoted, relegated := w.SeasonMove(ref, pos); promoted || relegated {
				t.Fatalf("SeasonMove(%s, %d) before the play-offs = %t, %t", ref, pos, promoted, relegated)
			}
		}
	}

	playPlayoffs(t, w)
	for _, link := range w.movementLinks() {
		comp, _ := w.playoffCompetition(link)
		results := w.competitions.Results(competitions.SeasonRef{Competition: comp, Season: 1})
		rankings := map[ids.CompetitionID][]ids.TeamID{
			link.Upper: w.competitions.Ranking(competitions.SeasonRef{Competition: link.Upper, Season: 1}),
			link.Lower: w.competitions.Ranking(competitions.SeasonRef{Competition: link.Lower, Season: 1}),
		}
		pairs, err := competitions.PlayoffPairings(link, rankings)
		if err != nil {
			t.Fatal(err)
		}
		for i, r := range results {
			winner, ok := r.Winner()
			if !ok {
				t.Fatalf("play-off fixture %d undecided", r.Fixture)
			}
			// Tie i is the deepest upper team at home to the best lower one.
			// The winner takes the upper place next season, the loser the
			// lower one.
			upTeam, lowTeam := pairs[i][0], pairs[i][1]
			upWon := winner == upTeam
			refU, posU := positionOf(t, w, link.Upper, upTeam)
			promU, relU := w.SeasonMove(refU, posU)
			if promU || relU != !upWon {
				t.Fatalf("SeasonMove(%s, %d) upper team %d = %t, %t want false, %t", refU, posU, upTeam, promU, relU, !upWon)
			}
			refL, posL := positionOf(t, w, link.Lower, lowTeam)
			promL, relL := w.SeasonMove(refL, posL)
			if promL != !upWon || relL {
				t.Fatalf("SeasonMove(%s, %d) lower team %d = %t, %t want %t, false", refL, posL, lowTeam, promL, relL, !upWon)
			}
		}
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// A play-off match is a knockout match with the link's lower division's
// rules, which needs an engine that can take penalties.
func TestPlayoffMatchRules(t *testing.T) {
	w := newWorld(t, 42)
	for _, link := range w.movementLinks() {
		comp, ok := w.playoffCompetition(link)
		if !ok {
			t.Fatalf("link %+v has no play-off competition", link)
		}
		rules, err := w.matchRules(comp)
		if err != nil {
			t.Fatal(err)
		}
		li, _ := w.leagueIndex(link.Lower)
		d := w.leagues[li].def
		want := matches.Rules{MaxSubstitutions: d.MaxSubstitutions, MaxBench: d.MaxBench, Knockout: true}
		if rules != want {
			t.Fatalf("matchRules(%d) = %+v, want %+v", comp, rules, want)
		}
	}
	if got, err := w.matchRules(99); err == nil || got != (matches.Rules{}) {
		t.Fatalf("matchRules(99) = %+v, %v", got, err)
	}
}
