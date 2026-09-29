package app

import (
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/careers"
	"github.com/thewalpa/project-zimble/internal/core/ids"
)

// CareerSpell is one spell of a player's career with its club's name. Format
// From and Until with Calendar.
type CareerSpell struct {
	careers.Spell
	ClubName string
}

// PlayerCareer returns the clubs a player has played for since the career
// began, oldest first: how and when he joined each (with the fee for a
// transfer) and how and when he left, the last spell being his current club
// unless it has ended. A player who has never had a club has no spells. False
// for an unknown player. Read-only.
func (w *World) PlayerCareer(id ids.PlayerID) ([]CareerSpell, bool) {
	if _, ok := w.registry.Player(id); !ok {
		return nil, false
	}
	spells, _ := w.careers.Career(id)
	out := make([]CareerSpell, len(spells))
	for i, s := range spells {
		out[i] = CareerSpell{Spell: s}
		if c, ok := w.registry.Club(s.Club); ok {
			out[i].ClubName = c.Name
		}
	}
	return out, true
}

// validateCareers checks the careers read model against the world:
//
//   - it has consumed every event;
//   - its players and clubs are registered, no spell is in the future, a
//     JoinedAtStart spell starts at the career's start, and a player whose
//     career ended in retirement has retired;
//   - a player has a current spell exactly when employed, at his employer:
//     every change of employment emitted its event;
//   - when the journal is complete (it still starts at event 1), the
//     careers equal a rebuild from their start and the journal.
func (w *World) validateCareers() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	if w.careers.Offset() != w.lastEvent {
		fail("careers consumed up to event %d, journal is at %d", w.careers.Offset(), w.lastEvent)
	}
	now := w.Now()
	current := map[ids.PlayerID]ids.ClubID{}
	for _, c := range w.careers.Careers() {
		if _, ok := w.registry.Player(c.Player); !ok {
			fail("career of unknown player %d", c.Player)
			continue
		}
		for _, s := range c.Spells {
			if _, ok := w.registry.Club(s.Club); !ok {
				fail("player %d career names unknown club %d", c.Player, s.Club)
			}
			if s.From > now || s.Until > now || (s.Joined == careers.JoinedAtStart && s.From != 0) {
				fail("player %d spell %+v is in the future or starts after the career", c.Player, s)
			}
		}
		last := c.Spells[len(c.Spells)-1]
		if p, _ := w.players.Profile(c.Player); last.Left == careers.LeftRetired && !p.Retired {
			fail("player %d retired from club %d but is active", c.Player, last.Club)
		}
		if last.Current() {
			current[c.Player] = last.Club
		}
	}
	assignments := w.employment.Assignments()
	for _, a := range assignments {
		if club, ok := current[a.Player]; !ok || club != a.Club {
			fail("player %d is employed by club %d but his career is at club %d", a.Player, a.Club, club)
		}
	}
	if len(current) != len(assignments) {
		fail("%d careers have a current club, %d players are employed", len(current), len(assignments))
	}

	if len(w.journal) == 0 && w.lastEvent == 0 || len(w.journal) > 0 && w.journal[0].ID == 1 {
		rebuilt, err := careers.New(w.careers.Initial())
		if err == nil {
			_, err = rebuilt.Apply(w.journal)
		}
		if err != nil || !slices.EqualFunc(rebuilt.Careers(), w.careers.Careers(), func(a, b careers.Career) bool {
			return a.Player == b.Player && slices.Equal(a.Spells, b.Spells)
		}) {
			fail("careers differ from a rebuild from the journal (%v)", err)
		}
	}
	return errs
}
