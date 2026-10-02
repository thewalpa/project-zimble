package present

import (
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/inbox"
)

// Every inbox kind has words: no client falls back to a kind number, and
// no value is missing from its sentence.
func TestEveryInboxKindIsWorded(t *testing.T) {
	w, err := app.NewWorld(app.DefaultConfig(42))
	if err != nil {
		t.Fatal(err)
	}
	for kind := inbox.KindMatchday; kind <= inbox.KindRecovered; kind++ {
		m := app.InboxItem{Message: inbox.Message{Kind: kind}, PlayerName: "Ann Lee", ClubName: "Eldhaven United"}
		msg := InboxMessage(w, 3, m)
		if msg.Text == "" || strings.Contains(msg.Text, "message kind") || strings.Contains(msg.String(), "%!") {
			t.Errorf("kind %d: %q", kind, msg)
		}
	}
	if got := InboxMessage(w, 3, app.InboxItem{Message: inbox.Message{Kind: 200}}); got.Text != "message kind 200" {
		t.Errorf("unknown kind: %q", got)
	}
}

func TestMatchName(t *testing.T) {
	for _, c := range []struct {
		info app.FixtureInfo
		want string
	}{
		{app.FixtureInfo{CompetitionName: "Founders League", RoundName: "round 3"}, "Round 3"},
		{app.FixtureInfo{CompetitionName: "Continental Cup", RoundName: "quarter-final", Cup: true}, "Continental Cup quarter-final"},
		{app.FixtureInfo{CompetitionName: "Promotion Play-off", RoundName: "round 1", Playoff: true}, "Promotion Play-off"},
	} {
		if got := MatchName(c.info); got != c.want {
			t.Errorf("MatchName(%+v) = %q, want %q", c.info, got, c.want)
		}
	}
}

// A level knockout match is decided by its shootout, from either side.
func TestOutcomeAndPenalties(t *testing.T) {
	for _, c := range []struct {
		score, shootout [2]uint16
		home            bool
		want            string
	}{
		{[2]uint16{2, 1}, [2]uint16{}, true, "W"},
		{[2]uint16{2, 1}, [2]uint16{}, false, "L"},
		{[2]uint16{1, 1}, [2]uint16{}, true, "D"},
		{[2]uint16{1, 1}, [2]uint16{3, 4}, true, "L"},
		{[2]uint16{1, 1}, [2]uint16{3, 4}, false, "W"},
	} {
		if got := Outcome(c.score, c.shootout, c.home); got != c.want {
			t.Errorf("Outcome(%v, %v, %v) = %s, want %s", c.score, c.shootout, c.home, got, c.want)
		}
	}
	if Penalties([2]uint16{}) != "" || Penalties([2]uint16{4, 3}) != " (4-3 on penalties)" {
		t.Error("Penalties")
	}
}
