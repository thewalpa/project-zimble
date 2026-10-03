package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/cmd/internal/present"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/selection"
)

// The manager's match can be watched live. Kick off records its first
// minute, which locks the lineup as Play match would. From then on the page
// runs a match clock over a preview of the play to the next stop (half time,
// full time or a chosen minute): the score, the timeline and the pitch
// follow the clock, and the stop's state appears when the clock gets there.
// The preview records nothing; playing on is a page load. A change made
// with the clock at a minute records the play up to that minute, then the
// change, which takes effect from the next minute. Matches play the same
// however their minutes are split, so what was watched is what was played.
// Play match (continue) finishes it and confirms the result, as it does for
// a match not watched.

// frameEvery thins the pitch replay to one frame per second of match time.
const frameEvery = 1000

type liveView struct {
	Fixture      ids.FixtureID
	Title        string // the match's name, e.g. "Round 1"
	Home, Away   app.TeamLabel
	Minute       uint16 // the minute the page plays to: its stop
	Played       uint16 // the latest recorded minute: 0 before kickoff
	From         uint16 // the clock's first minute; Minute when there is nothing to play out
	Autoplay     bool   // run the clock as the page opens
	Speed        int    // match seconds per second of the clock by default
	Speeds       []int
	StopName     string // what the clock runs to, e.g. "half time"
	Score        [2]uint16
	Penalties    string // " (4-3 on penalties)" after a shootout
	State        string // "Kick-off", "Half time", "Full time" or "In play"
	KickOff      bool   // not kicked off yet
	Finished     bool
	Target       uint16 // the default next stop
	TargetName   string
	Mentality    string
	Mentalities  []string
	Formation    string   // the club's shape now, e.g. "4-4-2"
	Shapes       []string // the shapes offered, the current one among them
	Board        pitchView
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
	Dots       []pitchDot
	Ball       [2]int32
	From       string // the replay's first and last match minute
	To         string
	FromMinute uint16    // the minute the replay starts at
	Frames     [][]int32 // per frame: seconds, ball x, y, carrier index (-1 none), then x, y per dot
}

type pitchDot struct {
	X, Y    int32
	Away    bool
	Mine    bool
	Label   string
	Name    string
	Carrier bool
}

// live shows the manager's match: before kickoff, or played on to the
// minute "to" (default: the latest recorded one) with the clock from "from";
// "run" starts the clock. Without a match waiting, the home page.
func (s *server) live(r *http.Request) (string, any, error) {
	fixture, ok := s.pendingFixture()
	if !ok {
		return s.home(r)
	}
	q := r.URL.Query()
	frames := s.w.MatchEngine().Capabilities.PositionalFrames
	played := uint16(0)
	if l, live := s.w.LiveMatch(); live {
		played = l.Position.Minute
	}
	to := played
	if n, err := strconv.ParseUint(q.Get("to"), 10, 16); err == nil && n > uint64(played) && n <= matches.RegulationMinutes {
		to = uint16(n)
	}
	p, err := s.w.PreviewLive(fixture, to, frames, frameEvery)
	if err != nil {
		return "", nil, err
	}
	l := p.Live
	from := l.Position.Minute
	if n, err := strconv.ParseUint(q.Get("from"), 10, 16); err == nil && n < uint64(from) {
		from = uint16(n)
	}
	info, _ := s.fixtureInfo(l.Fixture)
	side := l.Side.Index()
	v := liveView{
		Fixture: l.Fixture, Title: present.MatchName(info), Home: l.Home, Away: l.Away,
		Minute: l.Position.Minute, Played: p.Played, From: from, Autoplay: q.Has("run") && from < l.Position.Minute,
		Speed: 60, Speeds: []int{5, 10, 30, 60, 90},
		Score: l.View.Score, Penalties: present.Penalties(l.View.Shootout),
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
	case p.Played == 0 && l.Position.Minute == 0:
		v.State, v.KickOff = "Kick-off", true
	default:
		v.State = "In play"
	}
	v.StopName = strings.ToLower(v.State)
	if v.State == "In play" {
		v.StopName = fmt.Sprintf("minute %d", l.Position.Minute)
	}
	v.Target, v.TargetName = matches.HalfTimeMinute, "half time"
	if l.Position.Minute >= matches.HalfTimeMinute {
		v.Target, v.TargetName = matches.RegulationMinutes, "full time"
	}
	team := l.Teams[side]
	roles := map[ids.PlayerID]matches.Role{}
	for _, p := range team.Bench {
		roles[p.Player] = p.Role
	}
	for slot, id := range l.View.OnPitch[side] {
		roles[id] = l.View.Roles[side][slot]
	}
	v.Formation = app.RolesLabel(l.View.Roles[side])
	v.Shapes = shapes(v.Formation)
	v.Board = s.formationBoard(l)
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
	if frames {
		shown := p.Frames
		if from < p.Played {
			// The clock starts before the latest recorded minute (the first
			// minute, recorded at kickoff): its play leads the preview.
			before, err := s.w.LiveFrames(frameEvery)
			if err != nil {
				return "", nil, err
			}
			shown = append(before, shown...)
		}
		v.Pitch = s.replayOf(l, sinceMinute(shown, from))
	}
	return "live", v, nil
}

// commonShapes are the formations the live match offers in one choice;
// any other is made by moving players between lines.
var commonShapes = []string{"4-4-2", "4-3-3", "4-5-1", "4-2-4", "3-5-2", "3-4-3", "5-3-2", "5-4-1"}

// shapes lists commonShapes with current among them.
func shapes(current string) []string {
	if slices.Contains(commonShapes, current) {
		return commonShapes
	}
	return append([]string{current}, commonShapes...)
}

// formationBoard draws the club's players on the pitch by their roles now,
// for moving them between lines. The goalkeeper stays in goal.
func (s *server) formationBoard(l app.LiveMatch) pitchView {
	side := l.Side.Index()
	var lineup selection.Lineup
	fit := map[ids.PlayerID]app.LineupEligibility{}
	for slot, id := range l.View.OnPitch[side] {
		if id != 0 {
			lineup.Starters = append(lineup.Starters, selection.Slot{Player: id, Role: l.View.Roles[side][slot]})
			fit[id] = app.LineupEligibility{Player: id, Eligibility: app.EligibleFit}
		}
	}
	squad, _ := s.w.ObservedSquad(s.club(), s.club())
	b := newPitch(lineup, squad, fit, false, nil)
	b.Bench, b.Reserves, b.Order = nil, nil, ""
	for i := range b.Lines {
		if b.Lines[i].Slot == slotName(matches.Goalkeeper) {
			b.Lines[i].Fixed = true
			for j := range b.Lines[i].Players {
				b.Lines[i].Players[j].Fixed = true
			}
		}
	}
	return b
}

// sinceMinute keeps the frames from minute on, or the last one if none is.
func sinceMinute(frames []matches.Frame, minute uint16) []matches.Frame {
	for i, f := range frames {
		if f.Millis >= uint32(minute)*60_000 {
			return frames[i:]
		}
	}
	if len(frames) > 0 {
		return frames[len(frames)-1:]
	}
	return nil
}

// replayOf draws frames on the pitch. Frames are indexed like the players on
// the pitch at the page's stop: no change is made between the clock's start
// and the stop.
func (s *server) replayOf(l app.LiveMatch, frames []matches.Frame) *pitchReplay {
	if len(frames) == 0 {
		return nil
	}
	onPitch := l.View.OnPitch
	var slots []ids.PlayerID
	p := &pitchReplay{From: minuteOf(frames[0].Millis), To: minuteOf(frames[len(frames)-1].Millis), FromMinute: uint16(frames[0].Millis / 60_000)}
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

// watch kicks the club's waiting match off live: it records the first
// minute, which locks the lineup, and runs the clock to half time.
func (s *server) watch(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	fixture, ok := s.pendingFixture()
	if !ok {
		return "/", errors.New("no match is waiting; Continue goes to your next matchday")
	}
	if _, live := s.w.LiveMatch(); live {
		return "/live", nil
	}
	if _, err := s.w.PlayMatch(app.PlayMatch{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Fixture: fixture, ToMinute: 1}); err != nil {
		return "/live", err
	}
	return fmt.Sprintf("/live?from=0&to=%d&run", matches.HalfTimeMinute), nil
}

// decide makes a substitution, a mentality or formation change in the live match at
// the clock's minute: it records the play up to that minute first. The page
// then waits there, paused, on the way to the stop it was playing to.
func (s *server) decide(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	fixture, ok := s.pendingFixture()
	if !ok {
		return "/", errors.New("no match is waiting; Continue goes to your next matchday")
	}
	l, live := s.w.LiveMatch()
	if !live {
		return "/live", errors.New("kick off first; before kickoff, change the lineup instead")
	}
	minute, err := strconv.ParseUint(form.Get("minute"), 10, 16)
	if err != nil || minute > matches.RegulationMinutes {
		return "/live", errors.New("the minute of the change is missing")
	}
	if uint16(minute) < l.Position.Minute {
		return "/live", fmt.Errorf("the match has already been played to minute %d", l.Position.Minute)
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
	case "formation":
		roles, err := app.FormationRoles(l.View.Roles[l.Side.Index()], form.Get("shape"))
		if err != nil {
			return "/live", err
		}
		c.Kind, c.Roles = matches.CommandSetRoles, roles
	case "roles":
		// Each player on the pitch is in his line's field, as on the
		// lineup editor; a player without one keeps his role.
		side := l.Side.Index()
		c.Kind, c.Roles = matches.CommandSetRoles, l.View.Roles[side]
		for slot, id := range l.View.OnPitch[side] {
			if r, ok := slotRoles[form.Get(fmt.Sprintf("slot-%d", id))]; ok && id != 0 {
				c.Roles[slot] = r
			}
		}
	default:
		return "/live", errors.New("unknown match decision")
	}
	// Play up to the minute; a play stops at half time on the way.
	for l.Position.Minute < uint16(minute) {
		res, err := s.w.PlayMatch(app.PlayMatch{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Fixture: fixture, ToMinute: uint16(minute)})
		if err != nil {
			return "/live", err
		}
		if res.Live.Position.Minute <= l.Position.Minute {
			break
		}
		l = res.Live
	}
	back := fmt.Sprintf("/live?from=%d", l.Position.Minute)
	if to, err := strconv.ParseUint(form.Get("to"), 10, 16); err == nil && to > uint64(l.Position.Minute) {
		back += fmt.Sprintf("&to=%d", to)
	}
	if _, err := s.w.MatchDecision(app.MatchDecision{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Fixture: fixture, Command: c}); err != nil {
		s.notes = append(s.notes, note{Text: strings.TrimPrefix(err.Error(), "app: "), Error: true})
	}
	return back, nil
}
