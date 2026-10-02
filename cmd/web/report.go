package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/cmd/internal/present"
	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/selection"
)

// --- report -----------------------------------------------------------------

type goalView struct {
	Minute     uint16
	Team       string
	PlayerName string
}

type eventView struct {
	Minute uint16
	Text   string
	Score  string // after a goal, e.g. "2 - 1", for the live match clock
}

type reportView struct {
	Fixture      ids.FixtureID
	Context      string // e.g. "Founders League season 1 · Round 3", "Continental Cup 1 · Final"
	Penalties    string // e.g. "Won 4-3 on penalties" (home first)
	Competition  string
	Season       competitions.Season
	Round        competitions.Round
	Kickoff      string
	Played       bool
	HasDetails   bool // replay data is available; otherwise this is score-only
	Home         app.TeamLabel
	Away         app.TeamLabel
	Score        [2]uint16
	Outcome      string
	HomeSelected string
	AwaySelected string
	Goals        []goalView
	Events       []eventView
	Stats        []app.StatLine // none when the engine keeps no statistics
	Lineups      []*reportPitch // what each side started with, home first
}

// reportPitch is a lineup drawn read-only.
type reportPitch struct {
	Title     string
	Note      string // a caveat shown under the title
	Formation string
	Lines     []reportLine
	Bench     []reportChip
}

type reportLine struct {
	Label   string
	Players []reportChip
}

type reportChip struct {
	Player   ids.PlayerID
	Name     string
	Short    string
	Position string
	Overall  int // the player's overall now; 0 when unknown
}

func selectedText(b app.SelectedBy) string {
	switch b {
	case app.SelectedByManager:
		return "Your lineup"
	case app.SelectedByAI:
		return "The assistant's suggestion"
	default:
		return ""
	}
}

func (s *server) reportPage(r *http.Request) (string, any, error) {
	var fixID ids.FixtureID
	if v := r.URL.Query().Get("fixture"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			fixID = ids.FixtureID(n)
		}
	} else if v := r.URL.Query().Get("id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			fixID = ids.FixtureID(n)
		}
	}
	if fixID == 0 {
		if s.report != nil && s.report.Fixture != 0 {
			fixID = s.report.Fixture
		} else {
			for _, sc := range s.w.Schedules() {
				for _, rd := range sc.Rounds {
					for _, f := range rd.Fixtures {
						if f.Played {
							fixID = f.ID
						}
					}
				}
			}
		}
	}
	if fixID == 0 {
		return "report", reportView{}, nil
	}

	rep, ok := s.w.MatchReport(fixID)
	info, hasF := s.fixtureInfo(fixID)
	f := info.FixtureLine
	cal := s.w.Calendar()

	v := reportView{
		Fixture: fixID,
	}

	if hasF {
		v.Competition = info.CompetitionName
		v.Round = info.Round.Round
		v.Season = info.Round.Season.Season
		v.Kickoff = cal.Format(info.Kickoff)
		v.Home = f.Home
		v.Away = f.Away
		v.Played = f.Played
		v.Score = f.Score
		if info.Playoff {
			v.Context = fmt.Sprintf("%s season %d", s.w.PlayoffTitle(info.Round.Season.Competition), info.Round.Season.Season)
		} else if info.Cup {
			v.Context = fmt.Sprintf("%s %d · %s", info.CompetitionName, info.Round.Season.Season, present.Capitalize(info.RoundName))
		} else {
			v.Context = fmt.Sprintf("%s season %d · %s", info.CompetitionName, info.Round.Season.Season, present.Capitalize(info.RoundName))
		}
		if f.Shootout != [2]uint16{} {
			v.Penalties = fmt.Sprintf("%d-%d on penalties", f.Shootout[0], f.Shootout[1])
		}
	}

	if ok {
		v.Played = true
		v.Home = rep.Home
		v.Away = rep.Away
		v.Score = rep.Score
		v.Round = rep.Round.Round
		v.Season = rep.Round.Season.Season
		if rep.Home.Club == s.club() || rep.Away.Club == s.club() {
			v.Outcome = present.Outcome(rep.Score, rep.Shootout, rep.Home.Club == s.club())
		}
		v.HomeSelected = selectedText(rep.Selected[0])
		v.AwaySelected = selectedText(rep.Selected[1])
		v.Lineups = s.reportPitches(rep)

		for _, g := range rep.Goals {
			team := rep.Home.ShortName
			if g.Side == matches.Away {
				team = rep.Away.ShortName
			}
			pName, _ := s.w.PlayerName(g.Scorer)
			if pName == "" {
				pName = s.name(g.Scorer)
			}
			v.Goals = append(v.Goals, goalView{Minute: g.Minute, Team: team, PlayerName: pName})
		}
		v.Events = s.eventViews(rep.Events, rep.Home, rep.Away)
		v.Stats = app.StatLines(rep.Stats)
		v.HasDetails = len(v.Events) > 0 || len(v.Goals) > 0 || len(v.Lineups) > 0 || len(v.Stats) > 0
	} else if hasF && f.Played {
		if f.Home.Club == s.club() || f.Away.Club == s.club() {
			v.Outcome = present.Outcome(f.Score, f.Shootout, f.Home.Club == s.club())
		}
	} else if !hasF {
		return "", nil, errors.New("match not found")
	}

	return "report", v, nil
}

// eventViews words match events for a timeline, in order.
func (s *server) eventViews(events []matches.MatchEvent, home, away app.TeamLabel) []eventView {
	playerName := func(id ids.PlayerID) string {
		name, _ := s.w.PlayerName(id)
		if name == "" {
			name = s.name(id)
		}
		return name
	}
	var out []eventView
	var score [2]uint16
	for _, e := range events {
		team := home.ShortName
		if e.Side == matches.Away {
			team = away.ShortName
		}
		var text, after string
		switch e.Kind {
		case matches.EventGoal:
			text = fmt.Sprintf("%s · Goal · %s", team, playerName(e.Player))
			score[e.Side.Index()]++
			after = fmt.Sprintf("%d - %d", score[0], score[1])
		case matches.EventSubstitution:
			text = fmt.Sprintf("%s · Substitution · %s on for %s", team, playerName(e.Player), playerName(e.Other))
		case matches.EventMentalityChange:
			text = fmt.Sprintf("%s · Mentality changed to %s", team, e.Mentality.String())
		case matches.EventPeriodEnd:
			if e.Period == matches.FirstHalf {
				text = "Half time"
			} else if e.Period == matches.SecondHalf {
				text = "Full time"
			}
		}
		if text != "" {
			out = append(out, eventView{Minute: e.Minute, Text: text, Score: after})
		}
	}
	return out
}

// pitchOf draws a lineup read-only: starters by line, then the bench.
func (s *server) pitchOf(club string, l selection.Lineup) *reportPitch {
	chip := func(id ids.PlayerID) reportChip {
		name, _ := s.w.PlayerName(id)
		if name == "" {
			name = s.name(id)
		}
		c := reportChip{Player: id, Name: name, Short: name}
		if i := strings.LastIndex(name, " "); i >= 0 {
			c.Short = name[i+1:]
		}
		if p, ok := s.w.ObservedPlayerProfile(s.club(), id); ok {
			c.Position = p.Position.String()
			c.Overall = p.Overall
		}
		return c
	}
	v := &reportPitch{Title: club + " lineup", Formation: app.FormationLabel(l)}
	for _, line := range []struct {
		role  matches.Role
		label string
	}{{matches.Forward, "Attack"}, {matches.Midfielder, "Midfield"}, {matches.Defender, "Defence"}, {matches.Goalkeeper, "Goal"}} {
		rl := reportLine{Label: line.label}
		for _, st := range l.Starters {
			if st.Role == line.role {
				rl.Players = append(rl.Players, chip(st.Player))
			}
		}
		v.Lines = append(v.Lines, rl)
	}
	for _, id := range l.Bench {
		v.Bench = append(v.Bench, chip(id))
	}
	return v
}

// reportPitches draws what each side started the match with, home first;
// none for a result recorded without a report.
func (s *server) reportPitches(rep app.MatchReport) []*reportPitch {
	var out []*reportPitch
	for i, label := range []app.TeamLabel{rep.Home, rep.Away} {
		if l := rep.Lineups[i]; len(l.Starters) > 0 {
			out = append(out, s.pitchOf(label.ClubName, l))
		}
	}
	return out
}
