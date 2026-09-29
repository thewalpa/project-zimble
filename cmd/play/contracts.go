package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/players"
)

// --- contracts and free agents -------------------------------------------

// expiring returns the club's players whose contracts end at the next
// contract-year end, in ascending ID order.
func (s *session) expiring() []app.SquadPlayer {
	squad, _ := s.w.Squad(s.club())
	end := s.w.ContractYearEnd()
	var out []app.SquadPlayer
	for _, p := range squad {
		if p.Contract.Expires == end {
			out = append(out, p)
		}
	}
	return out
}

// contracts lists the squad's contracts, soonest end first, with what each
// player asks for in a new one.
func (s *session) contracts(args []string) error {
	col := "ends"
	desc := false
	if len(args) > 0 {
		col = strings.ToLower(args[0])
		switch col {
		case "ends", "contract":
			desc = false
		case "wage", "ovr":
			desc = true
		case "age", "name", "pos", "asks", "id":
			desc = false
		default:
			return fmt.Errorf("unknown sort %q: choose ends, wage, ovr, age, name, pos or asks", args[0])
		}
		if len(args) > 1 {
			switch strings.ToLower(args[1]) {
			case "asc":
				desc = false
			case "desc":
				desc = true
			default:
				return errors.New("usage: contracts [COLUMN [asc|desc]]")
			}
		}
		if len(args) > 2 {
			return errors.New("usage: contracts [COLUMN [asc|desc]]")
		}
	}
	squad, _ := s.w.Squad(s.club())
	slices.SortStableFunc(squad, func(a, b app.SquadPlayer) int {
		var diff int
		switch col {
		case "ends", "contract":
			diff = cmp.Compare(a.Contract.Expires, b.Contract.Expires)
		case "wage":
			diff = cmp.Compare(a.Contract.WeeklyWage, b.Contract.WeeklyWage)
		case "ovr":
			diff = cmp.Compare(a.Overall, b.Overall)
		case "age":
			diff = cmp.Compare(a.Age, b.Age)
		case "name":
			diff = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "pos":
			diff = cmp.Compare(a.Position, b.Position)
		case "asks":
			diff = cmp.Compare(a.Demand, b.Demand)
		default:
			diff = cmp.Compare(a.Contract.Expires, b.Contract.Expires)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(a.Contract.Expires, b.Contract.Expires), cmp.Compare(a.Player, b.Player))
	})
	cal := s.w.Calendar()
	end := s.w.ContractYearEnd()
	s.printf("\n%4s  %-3s %-24s %3s %5s %12s %8s %12s\n", "ID", "POS", "NAME", "AGE", "OVR", "WAGE/WEEK", "ENDS", "ASKS FOR")
	for _, p := range squad {
		ends, _ := cal.Civil(p.Contract.Expires)
		mark := ""
		if p.Contract.Expires == end {
			mark = "  <- final year"
		}
		s.printf("%4d  %-3s %-24s %3d %5d %12s %8d %12s%s\n", p.Player, p.Position, p.Name, p.Age, p.Overall, p.Contract.WeeklyWage, ends.Year, p.Demand, mark)
	}
	s.printf("Contracts in their final year end on %s. Players accept from their asking wage up to double it.\n", cal.Format(end))
	s.printf("renew ID [YEARS [WAGE]] offers a new contract; without YEARS and WAGE it offers the usual terms.\n")
	return nil
}

// freeAgents lists the unemployed players, best first.
func (s *session) freeAgents(args []string) error {
	col := "ovr"
	desc := true
	if len(args) > 0 {
		col = strings.ToLower(args[0])
		switch col {
		case "ovr", "cond", "wage", "asks":
			desc = true
		case "pos", "name", "age", "id":
			desc = false
		default:
			return fmt.Errorf("unknown sort %q: choose ovr, pos, name, age, cond, asks or id", args[0])
		}
		if len(args) > 1 {
			switch strings.ToLower(args[1]) {
			case "asc":
				desc = false
			case "desc":
				desc = true
			default:
				return errors.New("usage: free [COLUMN [asc|desc]]")
			}
		}
		if len(args) > 2 {
			return errors.New("usage: free [COLUMN [asc|desc]]")
		}
	}
	agents := s.w.FreeAgents()
	if len(agents) == 0 {
		s.printf("There are no free agents. Players whose contracts end on %s and are not renewed become free agents.\n",
			s.w.Calendar().Format(s.w.ContractYearEnd()))
		return nil
	}
	slices.SortStableFunc(agents, func(a, b app.SquadPlayer) int {
		var diff int
		switch col {
		case "pos":
			diff = cmp.Compare(a.Position, b.Position)
		case "name":
			diff = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "age":
			diff = cmp.Compare(a.Age, b.Age)
		case "ovr":
			diff = cmp.Compare(a.Overall, b.Overall)
		case "cond":
			diff = cmp.Compare(a.Condition, b.Condition)
		case "wage", "asks":
			diff = cmp.Compare(a.Demand, b.Demand)
		case "id":
			diff = cmp.Compare(a.Player, b.Player)
		default:
			diff = cmp.Compare(b.Overall, a.Overall)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(b.Overall, a.Overall), cmp.Compare(a.Player, b.Player))
	})
	s.printf("\n%4s  %-3s %-24s %-10s %3s %5s %-12s %12s\n", "ID", "POS", "NAME", "NATION", "AGE", "OVR", "COND", "ASKS FOR")
	for _, p := range agents {
		s.printf("%4d  %-3s %-24s %-10s %3d %5d %-12s %12s\n", p.Player, p.Position, p.Name, p.Nationality, p.Age, p.Overall, fitness(p), p.Demand)
	}
	if s.w.TransferWindow().Open {
		win := s.w.TransferWindow()
		s.printf("Until %s, only you may sign free agents; AI clubs can sign them from then.\n", s.w.Calendar().Format(win.FreeAgentsOpen))
	}
	s.printf("Free agents retire on the eve of %s once they are %d.\n", monthDay(s.w.Calendar().Epoch()), players.FreeAgentRetirementAge)
	s.printf("sign ID [YEARS [WAGE]] signs a player; without YEARS and WAGE it offers the usual terms.\n")
	return nil
}

// offer parses "ID [YEARS [WAGE]]" for a player in pool, taking missing
// terms from the AI's suggestion. WAGE is in whole units.
func (s *session) offer(args []string, usage string, pool []app.SquadPlayer, notInPool string) (ids.PlayerID, app.ContractOffer, error) {
	if len(args) < 1 || len(args) > 3 {
		return 0, app.ContractOffer{}, errors.New(usage)
	}
	player, err := parsePlayer(args[0])
	if err != nil {
		return 0, app.ContractOffer{}, err
	}
	if !slices.ContainsFunc(pool, func(p app.SquadPlayer) bool { return p.Player == player }) {
		return 0, app.ContractOffer{}, fmt.Errorf(notInPool, player)
	}
	o, err := s.w.SuggestContract(player)
	if err != nil {
		return 0, app.ContractOffer{}, err
	}
	if len(args) > 1 {
		if o.Years, err = strconv.Atoi(args[1]); err != nil {
			return 0, app.ContractOffer{}, errors.New(usage)
		}
	}
	if len(args) > 2 {
		units, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil || units <= 0 || units > 1_000_000_000 {
			return 0, app.ContractOffer{}, errors.New(usage)
		}
		o.WeeklyWage = money.Units(units)
	}
	return player, o, nil
}

func (s *session) renew(args []string) error {
	squad, _ := s.w.Squad(s.club())
	player, o, err := s.offer(args, "usage: renew ID [YEARS [WAGE]]", squad, "player %d is not in your squad")
	if err != nil {
		return err
	}
	res, err := s.w.RenewContract(app.RenewContract{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Player: player, Offer: o})
	if err != nil {
		return err
	}
	s.printf("%s signed a new contract until %s at %s a week.\n", s.name(player), s.endDate(res.Contract.Expires), res.Contract.WeeklyWage)
	s.markInboxRead()
	return nil
}

func (s *session) sign(args []string) error {
	player, o, err := s.offer(args, "usage: sign ID [YEARS [WAGE]]", s.w.FreeAgents(), "player %d is not a free agent; type free for the list")
	if err != nil {
		return err
	}
	res, err := s.w.SignPlayer(app.SignPlayer{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Player: player, Offer: o})
	if err != nil {
		return err
	}
	s.printf("%s joined until %s at %s a week.\n", s.name(player), s.endDate(res.Contract.Expires), res.Contract.WeeklyWage)
	s.markInboxRead()
	return nil
}

// release releases one of the club's players: "release ID" shows what it
// costs, "release ID yes" does it.
func (s *session) release(args []string) error {
	if len(args) < 1 || len(args) > 2 || (len(args) == 2 && strings.ToLower(args[1]) != "yes") {
		return errors.New("usage: release ID [yes]")
	}
	player, err := parsePlayer(args[0])
	if err != nil {
		return err
	}
	squad, _ := s.w.Squad(s.club())
	i := slices.IndexFunc(squad, func(p app.SquadPlayer) bool { return p.Player == player })
	if i < 0 {
		return fmt.Errorf("player %d is not in your squad", player)
	}
	p := squad[i]
	if len(args) == 1 {
		s.printf("Releasing %s costs %s: his wages until his contract ends on %s. He becomes a free agent.\n",
			p.Name, p.Payoff, s.endDate(p.Contract.Expires))
		s.printf("Type release %d yes to release him.\n", player)
		return nil
	}
	res, err := s.w.ReleasePlayer(app.ReleasePlayer{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Player: player})
	if err != nil {
		return err
	}
	s.printf("%s was released and is now a free agent. You paid %s.\n", p.Name, res.Compensation)
	s.markInboxRead()
	return nil
}

// endDate renders a contract end, e.g. "1 July 2029".
func (s *session) endDate(at sim.GameInstant) string {
	cal := s.w.Calendar()
	c, _ := cal.Civil(at)
	return fmt.Sprintf("%s %d", monthDay(cal.Epoch()), c.Year)
}

// name is a player's name, from the squad or the free agents.
func (s *session) name(id ids.PlayerID) string {
	squad, _ := s.w.Squad(s.club())
	for _, p := range append(squad, s.w.FreeAgents()...) {
		if p.Player == id {
			return p.Name
		}
	}
	return fmt.Sprintf("player %d", id)
}

// warnBeforeContractYear stops advance just before the contract-year end
// while final-year players would leave, once per contract-year end. It
// returns the target to continue to and whether it is that stop.
func (s *session) warnBeforeContractYear(target sim.GameInstant) (sim.GameInstant, bool) {
	end := s.w.ContractYearEnd()
	stop := end - sim.GameInstant(sim.Day)
	if s.warnedYearEnd == end || target < end || stop <= s.w.Now() || len(s.expiring()) == 0 {
		return target, false
	}
	return stop, true
}
