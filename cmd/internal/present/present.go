// Package present is the wording the clients share: the names of matches,
// results, career spells and inbox messages. Each client lays the text out
// its own way (a table cell, a terminal line) but never words it again.
//
// It only phrases what app queries return; it owns no football rules.
package present

import (
	"fmt"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/careers"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

// Capitalize upper-cases the first letter, for text that starts a line or
// a cell.
func Capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// MatchName names a fixture's round: "Round 3" in a league, "Continental
// Cup quarter-final" in a cup, "Promotion Play-off" in a play-off (one
// round).
func MatchName(info app.FixtureInfo) string {
	return matchName(info.Playoff, info.Cup, info.CompetitionName, info.RoundName)
}

// ItemMatchName names an inbox message's round like MatchName.
func ItemMatchName(w *app.World, m app.InboxItem) string {
	return matchName(w.PlayoffTitle(m.Competition) != "", m.Cup, m.CompetitionName, m.RoundName)
}

func matchName(playoff, cup bool, competition, round string) string {
	switch {
	case playoff:
		return competition
	case cup:
		return competition + " " + round
	}
	return Capitalize(round)
}

// Penalties renders a shootout, e.g. " (4-3 on penalties)", or nothing.
func Penalties(p [2]uint16) string {
	if p == [2]uint16{} {
		return ""
	}
	return fmt.Sprintf(" (%d-%d on penalties)", p[0], p[1])
}

// Outcome is W, D or L for one side of a played fixture; a shootout
// decides a level knockout match.
func Outcome(score, shootout [2]uint16, home bool) string {
	if score[0] == score[1] {
		score = shootout
	}
	us, them := score[0], score[1]
	if !home {
		us, them = them, us
	}
	switch {
	case us > them:
		return "W"
	case us < them:
		return "L"
	}
	return "D"
}

// FixtureOutcome is Outcome for a club in a played fixture.
func FixtureOutcome(f app.FixtureLine, club ids.ClubID) string {
	return Outcome(f.Score, f.Shootout, f.Home.Club == club)
}

// Date renders a game day, e.g. "1 July 2029", such as a contract's end.
func Date(cal sim.Calendar, at sim.GameInstant) string {
	c, _ := cal.Civil(at)
	months := [...]string{"January", "February", "March", "April", "May", "June", "July",
		"August", "September", "October", "November", "December"}
	return fmt.Sprintf("%d %s %d", c.Day, months[c.Month-1], c.Year)
}

// Joined says how a career spell began, e.g. "signed by transfer".
func Joined(joined careers.Joined) string {
	switch joined {
	case careers.JoinedAtStart:
		return "at career start"
	case careers.JoinedYouth:
		return "joined through youth"
	case careers.JoinedFree:
		return "signed as a free agent"
	case careers.JoinedTransfer:
		return "signed by transfer"
	}
	return "joined"
}

// Left says how a career spell ended, or "current club".
func Left(left careers.Left) string {
	switch left {
	case careers.LeftNot:
		return "current club"
	case careers.LeftTransfer:
		return "sold"
	case careers.LeftExpired:
		return "contract expired"
	case careers.LeftReleased:
		return "released"
	case careers.LeftRetired:
		return "retired"
	}
	return "left"
}

// OfferOutcome says how a closed offer ended, e.g. "was rejected".
func OfferOutcome(outcome uint8) string {
	switch transfers.Status(outcome) {
	case transfers.StatusRejected:
		return "was rejected"
	case transfers.StatusExpired:
		return "expired unanswered"
	case transfers.StatusRefused:
		return "was refused; the player refused to join"
	}
	return "fell through"
}

// Message is an inbox message in words. Topic is a one-word heading, e.g.
// "contract", or empty when Text speaks for itself.
type Message struct {
	Topic string
	Text  string
}

// String is the message on one line, e.g. "contract: Ann Lee renewed ...".
func (m Message) String() string {
	if m.Topic == "" {
		return m.Text
	}
	return m.Topic + ": " + m.Text
}

// InboxMessage words an inbox message for the manager of club.
func InboxMessage(w *app.World, club ids.ClubID, m app.InboxItem) Message {
	cal := w.Calendar()
	venue := "away"
	if m.Home {
		venue = "home"
	}
	switch m.Kind {
	case inbox.KindMatchday:
		return Message{"matchday", fmt.Sprintf("%s v %s (%s)", ItemMatchName(w, m), m.OpponentLabel.ClubName, venue)}
	case inbox.KindResult:
		return Message{"result", fmt.Sprintf("%d-%d%s v %s (%s), %s", m.Goals[0], m.Goals[1], Penalties(m.Shootout), m.OpponentLabel.ClubName, venue, ItemMatchName(w, m))}
	case inbox.KindSeasonEnded:
		return Message{"", seasonEnded(w, club, m)}
	case inbox.KindSeasonStarted:
		if title := w.PlayoffTitle(m.Competition); title != "" {
			return Message{"", fmt.Sprintf("%s season %d drawn: kickoff %s", title, m.Season, cal.Format(m.Kickoff))}
		}
		if m.Cup {
			return Message{"", fmt.Sprintf("%s %d drawn: first kickoff %s", m.CompetitionName, m.Season, cal.Format(m.Kickoff))}
		}
		return Message{"", fmt.Sprintf("%s season %d scheduled: first kickoff %s", m.CompetitionName, m.Season, cal.Format(m.Kickoff))}
	case inbox.KindRenewed:
		return Message{"contract", fmt.Sprintf("%s renewed until %s at %s a week", m.PlayerName, Date(cal, m.Expires), m.WeeklyWage)}
	case inbox.KindPlayerLeft:
		return Message{"contract", fmt.Sprintf("%s left the club as a free agent", m.PlayerName)}
	case inbox.KindPlayerJoined:
		return Message{"signing", fmt.Sprintf("%s joined until %s at %s a week", m.PlayerName, Date(cal, m.Expires), m.WeeklyWage)}
	case inbox.KindRetired:
		return Message{"retirement", fmt.Sprintf("%s retired at %d", m.PlayerName, m.Age)}
	case inbox.KindYouthJoined:
		return Message{"youth", fmt.Sprintf("%s joined from the youth ranks until %s at %s a week", m.PlayerName, Date(cal, m.Expires), m.WeeklyWage)}
	case inbox.KindDeveloped:
		return Message{"development", fmt.Sprintf("%d of your players improved and %d declined over the year", m.Improved, m.Declined)}
	case inbox.KindReleased:
		return Message{"release", fmt.Sprintf("%s left the club as a free agent; you paid %s", m.PlayerName, m.Compensation)}
	case inbox.KindInjured:
		return Message{"injury", fmt.Sprintf("%s is out for %d days", m.PlayerName, m.Days)}
	case inbox.KindRecovered:
		return Message{"injury", fmt.Sprintf("%s is fit again", m.PlayerName)}
	case inbox.KindBidReceived:
		return Message{"bid", fmt.Sprintf("%s bid %s for %s; answer before %s", m.ClubName, m.Fee, m.PlayerName, cal.Format(m.Deadline))}
	case inbox.KindTransferIn:
		return Message{"transfer", fmt.Sprintf("%s joined from %s for %s, until %s at %s a week", m.PlayerName, m.ClubName, m.Fee, Date(cal, m.Expires), m.WeeklyWage)}
	case inbox.KindTransferOut:
		return Message{"transfer", fmt.Sprintf("%s left for %s for %s", m.PlayerName, m.ClubName, m.Fee)}
	case inbox.KindOfferClosed:
		if m.Selling {
			return Message{"transfer", fmt.Sprintf("the bid of %s from %s for %s %s", m.Fee, m.ClubName, m.PlayerName, OfferOutcome(m.Outcome))}
		}
		return Message{"transfer", fmt.Sprintf("your bid of %s for %s of %s %s", m.Fee, m.PlayerName, m.ClubName, OfferOutcome(m.Outcome))}
	}
	return Message{"", fmt.Sprintf("message kind %d", m.Kind)}
}

// seasonEnded words the end of a league season, a cup edition or a
// play-off, with how club did in it.
func seasonEnded(w *app.World, club ids.ClubID, m app.InboxItem) string {
	ref := competitions.SeasonRef{Competition: m.Competition, Season: competitions.Season(m.Season)}
	if title := w.PlayoffTitle(m.Competition); title != "" {
		upper, _, _ := w.PlayoffDivisions(m.Competition)
		text := fmt.Sprintf("%s season %d decided: each tie's winner plays in %s next season", title, m.Season, upper)
		if tie, ok := PlayoffTie(w, ref, club); ok {
			if FixtureOutcome(tie, club) == "W" {
				return text + "; you won your tie"
			}
			return text + "; you lost your tie"
		}
		return text
	}
	if m.Cup {
		text := fmt.Sprintf("%s %d won by %s", m.CompetitionName, m.Season, m.ChampionLabel.ClubName)
		switch m.Stage {
		case "":
		case "winner":
			text += ": your club won it!"
		default:
			text += fmt.Sprintf("; you went out in the %s", m.Stage)
		}
		return text
	}
	text := fmt.Sprintf("%s season %d ended: champion %s", m.CompetitionName, m.Season, m.ChampionLabel.ClubName)
	if m.Position > 0 {
		text += fmt.Sprintf("; you finished %s", app.Ordinal(m.Position))
		switch up, down := w.SeasonMove(ref, m.Position); {
		case up:
			text += ": promoted to the division above"
		case down:
			text += ": relegated to the division below"
		}
	}
	return text
}

// PlayoffTie is club's played tie in a play-off season, if it played one.
func PlayoffTie(w *app.World, ref competitions.SeasonRef, club ids.ClubID) (app.FixtureLine, bool) {
	p, ok := w.Playoff(ref)
	if !ok {
		return app.FixtureLine{}, false
	}
	for _, r := range p.Rounds {
		for _, f := range r.Ties {
			if f.Played && (f.Home.Club == club || f.Away.Club == club) {
				return f, true
			}
		}
	}
	return app.FixtureLine{}, false
}

// Forecast and emergency wording of the medical view.
const (
	MedicalForecastNote  = "Return dates are forecasts: a later match can injure a player again, and a sale or release ends the forecast."
	MedicalEmergencyNote = "The fit players cannot field an eleven, so injured players are admitted to a lineup. They are still injured."
)

// MedicalCells is one medical row's wording.
type MedicalCells struct {
	Injury      string // "Fit", or the days left and the forecast return
	Eligibility string // whether the player can be put in a lineup today
}

// Medical words a player's medical row from app's view (World.SquadMedical):
// canField is the view's CanField, false when selection admits the injured.
func Medical(cal sim.Calendar, canField bool, p app.MedicalPlayer) MedicalCells {
	c := MedicalCells{Injury: "Fit", Eligibility: "Available"}
	if p.DaysOut > 0 {
		c.Injury = fmt.Sprintf("Out %d %s", p.DaysOut, plural(int(p.DaysOut), "day", "days"))
		if p.FitFrom != 0 {
			c.Injury += ", fit about " + cal.Format(p.FitFrom)
		}
		c.Eligibility = "Unavailable (injured)"
		if !canField {
			c.Eligibility = "Emergency only (injured)"
		}
	}
	return c
}

// Supply words a position's fit players against its roster quota.
func Supply(p app.PositionAvailability) string {
	switch {
	case p.Critical:
		return "critical: below the minimum"
	case p.Short:
		return "short"
	}
	return ""
}

// TiredStarters words the tired starters of a planned or carried-over
// lineup (MatchdayLineup.Tired), each with his condition from squad, or ""
// when there are none.
func TiredStarters(ml app.MatchdayLineup, squad []app.SquadPlayer) string {
	if len(ml.Tired) == 0 {
		return ""
	}
	cond := make(map[ids.PlayerID]app.SquadPlayer, len(squad))
	for _, p := range squad {
		cond[p.Player] = p
	}
	var names []string
	for _, id := range ml.Tired {
		if p, ok := cond[id]; ok {
			names = append(names, fmt.Sprintf("%s (%d)", p.Name, p.Condition))
		}
	}
	return fmt.Sprintf("%d %s below %d condition: %s. The assistant's suggestion weighs condition.",
		len(names), plural(len(names), "starter is", "starters are"), app.TiredCondition, strings.Join(names, ", "))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
