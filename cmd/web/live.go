package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// The manager's match can be watched live: played to a stop (half time,
// full time or a chosen minute), with substitutions and mentality changes
// in between. Play match (continue) finishes it and confirms the result,
// as it does for a match not watched.

// frameEvery thins the pitch replay to one frame per second of match time.
const frameEvery = 1000

type liveView struct {
	Fixture      ids.FixtureID
	Title        string // the match's name, e.g. "Round 1"
	Home, Away   app.TeamLabel
	Minute       uint16
	Score        [2]uint16
	Penalties    string // " (4-3 on penalties)" after a shootout
	State        string // "Half time", "Full time" or "In play"
	Finished     bool
	Target       uint16 // the default next stop
	Mentality    string
	Mentalities  []string
	SubsLeft     int
	OnPitch      []livePlayer // the club's players on the pitch
	Bench        []livePlayer // the club's substitutes who can still come on
	Events       []eventView
	Stats        []app.StatLine
	Pitch        *pitchReplay
	EngineID     string
	StatsMissing bool // the engine keeps no statistics
}

type livePlayer struct {
	Player   ids.PlayerID
	Name     string
	Position string
	Role     string
	Overall  int
}

// pitchReplay is the play since the previous stop, drawn on a pitch in
// decimetres: the last frame as SVG (without JavaScript), and every frame
// for the script to replay.
type pitchReplay struct {
	Dots   []pitchDot
	Ball   [2]int32
	From   string // the replay's first and last match minute
	To     string
	Frames [][]int32 // per frame: seconds, ball x, y, carrier index (-1 none), then x, y per dot
}

type pitchDot struct {
	X, Y    int32
	Away    bool
	Mine    bool
	Label   string
	Name    string
	Carrier bool
}

// live shows the manager's match in progress; without one, the home page.
func (s *server) live(r *http.Request) (string, any, error) {
	l, ok := s.w.LiveMatch()
	if !ok {
		return s.home(r)
	}
	info, _ := s.fixtureInfo(l.Fixture)
	side := l.Side.Index()
	v := liveView{
		Fixture: l.Fixture, Title: matchName(info), Home: l.Home, Away: l.Away,
		Minute: l.Position.Minute, Score: l.View.Score, Penalties: penalties(l.View.Shootout),
		Mentality: l.View.Mentality[side].String(), SubsLeft: int(l.Rules.MaxSubstitutions) - int(l.View.SubstitutionsUsed[side]),
		Events: s.eventViews(l.Events, l.Home, l.Away), Stats: app.StatLines(l.View.Stats), EngineID: s.w.MatchEngine().ID,
	}
	v.StatsMissing = !s.w.MatchEngine().Capabilities.DetailedStats
	for m := matches.Defensive; m <= matches.Attacking; m++ {
		v.Mentalities = append(v.Mentalities, m.String())
	}
	switch {
	case l.Status == matches.MatchFinished:
		v.State, v.Finished = "Full time", true
	case l.Status == matches.MatchDecisionRequired && l.Position.Minute == matches.HalfTimeMinute:
		v.State = "Half time"
	default:
		v.State = "In play"
	}
	v.Target = matches.HalfTimeMinute
	if l.Position.Minute >= matches.HalfTimeMinute {
		v.Target = matches.RegulationMinutes
	}
	team := l.Teams[side]
	roles := map[ids.PlayerID]matches.Role{}
	for _, p := range append(append([]matches.PlayerInput(nil), team.Starters...), team.Bench...) {
		roles[p.Player] = p.Role
	}
	player := func(id ids.PlayerID) livePlayer {
		p := livePlayer{Player: id, Name: s.name(id), Role: roleLabel(roles[id])}
		if prof, ok := s.w.ObservedPlayerProfile(s.club(), id); ok {
			p.Position, p.Overall = prof.Position.String(), prof.Overall
		}
		return p
	}
	used := map[ids.PlayerID]bool{}
	for _, e := range l.Events {
		if e.Kind == matches.EventSubstitution && e.Side == l.Side {
			used[e.Player], used[e.Other] = true, true
		}
	}
	for _, id := range l.View.OnPitch[side] {
		if id != 0 {
			v.OnPitch = append(v.OnPitch, player(id))
		}
	}
	for _, b := range team.Bench {
		if !used[b.Player] {
			v.Bench = append(v.Bench, player(b.Player))
		}
	}
	if s.w.MatchEngine().Capabilities.PositionalFrames {
		frames, err := s.w.LiveFrames(frameEvery)
		if err != nil {
			return "", nil, err
		}
		v.Pitch = s.replayOf(l, frames)
	}
	return "live", v, nil
}

// replayOf draws frames on the pitch. Frames are indexed like the players on
// the pitch when the stop was reached, before any change made at it.
func (s *server) replayOf(l app.LiveMatch, frames []matches.Frame) *pitchReplay {
	if len(frames) == 0 {
		return nil
	}
	onPitch := l.View.OnPitch
	if s.frameLabels.fixture == l.Fixture && s.frameLabels.minute == l.Position.Minute {
		onPitch = s.frameLabels.onPitch
	}
	var slots []ids.PlayerID
	p := &pitchReplay{From: minuteOf(frames[0].Millis), To: minuteOf(frames[len(frames)-1].Millis)}
	for sd := range onPitch {
		for _, id := range onPitch[sd] {
			slots = append(slots, id)
			name := s.name(id)
			label := name
			if i := strings.LastIndex(name, " "); i >= 0 {
				label = name[i+1:]
			}
			p.Dots = append(p.Dots, pitchDot{Away: sd == 1, Mine: sd == l.Side.Index(), Label: label, Name: name})
		}
	}
	for _, f := range frames {
		row := []int32{int32(f.Millis / 1000), f.Ball.X / 10, f.Ball.Y / 10, -1}
		for i, id := range slots {
			if id != 0 && id == f.Carrier {
				row[3] = int32(i)
			}
		}
		for sd := range f.Players {
			for _, pt := range f.Players[sd] {
				row = append(row, pt.X/10, pt.Y/10)
			}
		}
		p.Frames = append(p.Frames, row)
	}
	last := p.Frames[len(p.Frames)-1]
	p.Ball = [2]int32{last[1], last[2]}
	for i := range p.Dots {
		p.Dots[i].X, p.Dots[i].Y = last[4+2*i], last[5+2*i]
		p.Dots[i].Carrier = last[3] == int32(i)
	}
	return p
}

func minuteOf(millis uint32) string {
	return fmt.Sprintf("%d:%02d", millis/60_000, millis/1000%60)
}

func roleLabel(r matches.Role) string {
	for name, role := range slotRoles {
		if role == r {
			return strings.ToUpper(name)
		}
	}
	return ""
}

// watch plays the club's waiting match live to the minute asked (by default
// half time, then full time).
func (s *server) watch(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	fixture, ok := s.pendingFixture()
	if !ok {
		return "/", errors.New("no match is waiting; Continue goes to your next matchday")
	}
	target := uint16(matches.HalfTimeMinute)
	if l, live := s.w.LiveMatch(); live {
		if l.Status == matches.MatchFinished {
			return "/live", errors.New("full time: Play match confirms the result")
		}
		if l.Position.Minute >= matches.HalfTimeMinute {
			target = matches.RegulationMinutes
		}
	}
	if m := strings.TrimSpace(form.Get("minute")); m != "" {
		n, err := strconv.ParseUint(m, 10, 16)
		if err != nil {
			return "/live", errors.New("enter the minute to play to")
		}
		target = uint16(n)
	}
	res, err := s.w.PlayMatch(app.PlayMatch{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Fixture: fixture, ToMinute: target})
	if err != nil {
		return "/live", err
	}
	s.frameLabels = frameLabels{fixture: fixture, minute: res.Live.Position.Minute, onPitch: res.Live.View.OnPitch}
	return "/live", nil
}

// decide makes a substitution or a mentality change in the live match.
func (s *server) decide(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	l, live := s.w.LiveMatch()
	if !live {
		return "/", errors.New("your match has not kicked off")
	}
	c := matches.MatchCommand{Side: l.Side}
	switch form.Get("kind") {
	case "sub":
		out, err1 := strconv.ParseUint(form.Get("out"), 10, 64)
		in, err2 := strconv.ParseUint(form.Get("in"), 10, 64)
		if err1 != nil || err2 != nil {
			return "/live", errors.New("choose the player to take off and the one to bring on")
		}
		c.Kind, c.Out, c.In = matches.CommandSubstitute, ids.PlayerID(out), ids.PlayerID(in)
	case "mentality":
		m, ok := parseMentality(form.Get("mentality"))
		if !ok {
			return "/live", errors.New("choose a mentality")
		}
		c.Kind, c.Mentality = matches.CommandSetMentality, m
	default:
		return "/live", errors.New("unknown match decision")
	}
	if _, err := s.w.MatchDecision(app.MatchDecision{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Fixture: l.Fixture, Command: c}); err != nil {
		return "/live", err
	}
	return "/live", nil
}

// frameLabels are the players on the pitch when the live match reached its
// latest stop: the frames up to it are indexed by them, not by changes made
// at the stop.
type frameLabels struct {
	fixture ids.FixtureID
	minute  uint16
	onPitch [2][matches.StartersPerTeam]ids.PlayerID
}
