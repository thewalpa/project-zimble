package main

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/cmd/internal/present"
	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/selection"
)

// --- lineup -----------------------------------------------------------------

type slotOption struct {
	Value string
	Label string
}

var slotOptions = []slotOption{
	{"gk", "Start in goal"}, {"df", "Start in defence"}, {"mf", "Start in midfield"}, {"fw", "Start in attack"},
	{"bench", "Bench"}, {"out", "Not selected"},
}

type lineupRow struct {
	app.SquadPlayer
	Slot         string
	Availability string
	Selectable   bool
}

type hiddenLineupSlot struct {
	Player ids.PlayerID
	Slot   string
}

// pitchChip is one player on the lineup pitch, the bench or among the
// unselected squad.
type pitchChip struct {
	Player        ids.PlayerID
	Name          string
	Short         string // the last part of the name, for the pitch
	Position      string
	Overall       int
	Condition     uint8
	DaysOut       uint16
	Natural       string // slot value of the player's natural role
	OutOfPosition bool   // starts in another role than his natural one
	Unavailable   bool   // may not be named for this match
	Tired         bool   // a tired starter of a planned or carried-over lineup
	Fixed         bool   // may not be moved (the goalkeeper during a match)
}

// pitchLine is one line of starters, in slot order: the match spreads a
// line's players across the width in this order.
type pitchLine struct {
	Slot    string
	Label   string
	Fixed   bool // no player may move into or out of it
	Players []pitchChip
}

// pitchView draws the lineup: lines from attack down to goal, then the bench
// and the rest of the squad. Order lists the lineup's player IDs as the
// form's "order" field expects them.
type pitchView struct {
	Lines     []pitchLine
	Bench     []pitchChip
	Reserves  []pitchChip
	Formation string
	Starters  int
	Want      int
	Order     string
}

type lineupView struct {
	Fixture          ids.FixtureID
	Plan             bool
	HasFixture       bool
	Unavailable      []string
	Title            string
	State            string
	Mentality        string
	Mentalities      []string
	Rows             []lineupRow
	HiddenSlots      []hiddenLineupSlot
	Slots            []slotOption
	Sort             SortState
	DroppedNotes     []string
	TiredNote        string // the tired starters of a planned or carried-over lineup
	IsSuggested      bool
	AvailableOnly    bool
	AvailabilityNote string
	Pitch            pitchView
}

// newPitch places l's players on the pitch. Players who have left the squad
// are not drawn; the page's notes name them. tired marks starters
// (MatchdayLineup.Tired).
func newPitch(l selection.Lineup, squad []app.SquadPlayer, byPlayer map[ids.PlayerID]app.LineupEligibility, availableOnly bool, tired []ids.PlayerID) pitchView {
	bySquad := make(map[ids.PlayerID]app.SquadPlayer, len(squad))
	for _, p := range squad {
		bySquad[p.Player] = p
	}
	chip := func(p app.SquadPlayer, slot string) pitchChip {
		natural := slotName(app.NaturalRole(p.Position))
		short := p.Name
		if i := strings.LastIndex(short, " "); i >= 0 {
			short = short[i+1:]
		}
		return pitchChip{
			Player: p.Player, Name: p.Name, Short: short, Position: p.Position.String(), Overall: p.Overall,
			Condition: p.Condition, DaysOut: p.DaysOut, Natural: natural,
			OutOfPosition: slot != "bench" && slot != "out" && slot != natural,
			Unavailable:   !byPlayer[p.Player].Eligibility.Selectable(),
			Tired:         slot != "bench" && slot != "out" && slices.Contains(tired, p.Player),
		}
	}
	v := pitchView{Formation: app.FormationLabel(l), Want: matches.StartersPerTeam}
	var order []string
	for _, line := range []struct {
		role  matches.Role
		label string
	}{{matches.Forward, "Attack"}, {matches.Midfielder, "Midfield"}, {matches.Defender, "Defence"}, {matches.Goalkeeper, "Goal"}} {
		pl := pitchLine{Slot: slotName(line.role), Label: line.label}
		for _, st := range l.Starters {
			if p, ok := bySquad[st.Player]; ok && st.Role == line.role {
				pl.Players = append(pl.Players, chip(p, pl.Slot))
			}
		}
		v.Lines = append(v.Lines, pl)
	}
	selected := map[ids.PlayerID]bool{}
	for _, st := range l.Starters {
		selected[st.Player] = true
		if _, ok := bySquad[st.Player]; ok {
			v.Starters++
			order = append(order, strconv.FormatUint(uint64(st.Player), 10))
		}
	}
	for _, id := range l.Bench {
		selected[id] = true
		if p, ok := bySquad[id]; ok {
			v.Bench = append(v.Bench, chip(p, "bench"))
			order = append(order, strconv.FormatUint(uint64(id), 10))
		}
	}
	for _, p := range squad {
		if selected[p.Player] || availableOnly && !byPlayer[p.Player].Eligibility.Selectable() {
			continue
		}
		v.Reserves = append(v.Reserves, chip(p, "out"))
	}
	slices.SortStableFunc(v.Reserves, func(a, b pitchChip) int {
		return cmp.Or(cmp.Compare(slotRank(a.Natural), slotRank(b.Natural)), cmp.Compare(b.Overall, a.Overall))
	})
	v.Order = strings.Join(order, " ")
	return v
}

// slotRank orders slot values goalkeeper first.
func slotRank(slot string) matches.Role { return slotRoles[slot] }

func (s *server) lineup(r *http.Request) (string, any, error) {
	fixture, hasFixture := s.pendingFixture()
	planMode := r.URL.Query().Get("plan") == "1" || !hasFixture
	if _, live := s.w.LiveMatch(); live && !planMode {
		// The match has kicked off: changes are substitutions now.
		return s.live(r)
	}
	suggest := r.URL.Query().Get("suggest") == "1"
	var l selection.Lineup
	var state string
	var droppedNotes []string
	var ml app.MatchdayLineup
	var eligibility []app.LineupEligibility
	var plan app.TeamPlan
	if planMode {
		var err error
		plan, err = s.w.TeamPlan()
		if err != nil {
			return "", nil, err
		}
		l, eligibility = plan.Lineup, plan.Squad
		state = "Starting point for matches without a submitted lineup"
		if plan.Saved {
			state = "Saved team plan; used for matches without a submitted lineup"
		}
	} else {
		if suggest {
			var err error
			if l, err = s.w.SuggestLineup(fixture); err != nil {
				return "", nil, err
			}
			state = "The assistant's suggestion (used unless you save changes)"
		} else {
			var err error
			ml, err = s.w.MatchdayLineup(fixture)
			if err != nil {
				return "", nil, err
			}
			l = ml.Lineup
			state = s.w.LineupSourceLabel(ml)
			droppedNotes = s.w.LineupDroppedMessages(ml)
		}
		var err error
		eligibility, err = s.w.SquadEligibility(fixture)
		if err != nil {
			return "", nil, err
		}
	}
	byPlayer := make(map[ids.PlayerID]app.LineupEligibility, len(eligibility))
	emergency := false
	for _, e := range eligibility {
		byPlayer[e.Player] = e
		if e.Eligibility == app.EligibleInjured {
			emergency = true
		}
	}
	sortState := newSortState(r, "selection", "asc")
	v := lineupView{
		Fixture: fixture, Plan: planMode, HasFixture: hasFixture,
		State: state, Mentality: l.Tactics.Mentality.String(), Slots: slotOptions,
		Title:         "Team plan",
		Sort:          sortState,
		DroppedNotes:  droppedNotes,
		IsSuggested:   suggest,
		AvailableOnly: r.URL.Query().Get("available") == "1",
	}
	if !planMode {
		info, _ := s.fixtureInfo(fixture)
		v.Title = fmt.Sprintf("%s v %s, %s", present.MatchName(info), s.opponent(info.FixtureLine), s.w.Calendar().Format(info.Kickoff))
	}
	for _, id := range plan.Unavailable {
		if name, ok := s.w.PlayerName(id); ok {
			v.Unavailable = append(v.Unavailable, name)
		}
	}
	if emergency {
		v.AvailabilityNote = "Not enough fit players for a legal eleven; injured players are selectable under the emergency rule."
	}
	for m := matches.Defensive; m <= matches.Attacking; m++ {
		v.Mentalities = append(v.Mentalities, m.String())
	}
	slot := map[ids.PlayerID]string{}
	for _, st := range l.Starters {
		slot[st.Player] = slotName(st.Role)
	}
	for _, p := range l.Bench {
		slot[p] = "bench"
	}
	squad, _ := s.w.ObservedSquad(s.club(), s.club())
	for _, p := range squad {
		e := byPlayer[p.Player]
		if v.AvailableOnly && !e.Eligibility.Selectable() {
			v.HiddenSlots = append(v.HiddenSlots, hiddenLineupSlot{Player: p.Player, Slot: cmp.Or(slot[p.Player], "out")})
			continue
		}
		row := lineupRow{SquadPlayer: p, Slot: cmp.Or(slot[p.Player], "out"), Selectable: e.Eligibility.Selectable()}
		switch e.Eligibility {
		case app.EligibleFit:
			row.Availability = "Available"
		case app.EligibleInjured:
			row.Availability = "Injured, selectable: not enough fit players"
		case app.IneligibleInjured:
			row.Availability = fmt.Sprintf("Injured: %d days out", e.DaysOut)
		}
		v.Rows = append(v.Rows, row)
	}
	sortLineupRows(v.Rows, sortState.Col, sortState.Dir)
	v.TiredNote = present.TiredStarters(ml, squad)
	v.Pitch = newPitch(l, squad, byPlayer, v.AvailableOnly, ml.Tired)
	return "lineup", v, nil
}
