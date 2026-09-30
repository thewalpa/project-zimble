package app

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

// taskContractYear ends the contract year: at 00:00 on day 1 of the epoch's
// month (1 July by default) every contract ending then is renewed or
// expires, and clubs sign free agents. It has no payload and reschedules
// itself a year later, so exactly one is always queued, due at the next
// contract-year end. It runs in the Expiries phase, before anything else due
// at that instant.
const taskContractYear sim.TaskKind = 5

var (
	ErrNotUserPlayer = errors.New("app: not a player of the user club")
	ErrNotFinalYear  = errors.New("app: the contract is not in its final year")
	ErrNotFreeAgent  = errors.New("app: the player is not a free agent")
	ErrOfferRejected = errors.New("app: contract offer rejected")
	ErrSquadFull     = errors.New("app: the squad is full")
	ErrSquadsLocked  = errors.New("app: squads cannot change while rounds await results")
)

// Contract years. Every contract ends at a contract-year end: 00:00 UTC on
// day 1 of the epoch's month. A contract of N contract years signed during a
// contract year ends at the Nth contract-year end from then; a renewal of N
// years moves the end N years on.

// contractYearEnd returns the first contract-year end strictly after t.
func (w *World) contractYearEnd(t sim.GameInstant) (sim.GameInstant, error) {
	c, err := w.calendar.Civil(t)
	if err != nil {
		return 0, err
	}
	month := w.calendar.Epoch().Month
	end, err := w.calendar.Instant(sim.CivilTime{Year: c.Year, Month: month, Day: 1})
	if err != nil || end > t {
		return end, err
	}
	return w.calendar.Instant(sim.CivilTime{Year: c.Year + 1, Month: month, Day: 1})
}

// addYears moves a contract-year end n years on.
func (w *World) addYears(end sim.GameInstant, n int) (sim.GameInstant, error) {
	c, err := w.calendar.Civil(end)
	if err != nil {
		return 0, err
	}
	c.Year += n
	return w.calendar.Instant(c)
}

func (w *World) isContractYearEnd(t sim.GameInstant) bool {
	c, err := w.calendar.Civil(t)
	return err == nil && c == (sim.CivilTime{Year: c.Year, Month: w.calendar.Epoch().Month, Day: 1})
}

func (w *World) yearOf(t sim.GameInstant) (int, error) {
	c, err := w.calendar.Civil(t)
	return c.Year, err
}

func (w *World) scheduleContractYear(at sim.GameInstant) error {
	if _, err := w.scheduler.Schedule(sim.TaskSpec{DueAt: at, Phase: sim.PhaseExpiries, Kind: taskContractYear}); err != nil {
		return fmt.Errorf("app: schedule contract year at %d: %w", at, err)
	}
	return nil
}

// ContractOffer is a contract's length in contract years and its weekly
// wage.
type ContractOffer struct {
	Years      int
	WeeklyWage money.Money
}

// aiOffer is what an AI club offers a player: ai.ContractLength years keyed
// by the calendar year the contract starts, at the player's demand.
func (w *World) aiOffer(player ids.PlayerID, startYear int) (ContractOffer, error) {
	p, ok := w.players.Profile(player)
	if !ok {
		return ContractOffer{}, fmt.Errorf("app: player %d has no profile", player)
	}
	e := w.defs.Economy
	return ContractOffer{
		Years:      ai.ContractLength(w.seed, player, startYear, e.ContractYears[0], e.ContractYears[1]),
		WeeklyWage: e.Demand(p.Overall()),
	}, nil
}

// checkOffer applies the contract rules: a length within the content's
// contract years and a wage from the player's demand up to the offer
// ceiling.
func (w *World) checkOffer(player ids.PlayerID, o ContractOffer) error {
	p, ok := w.players.Profile(player)
	if !ok {
		return fmt.Errorf("%w: player %d is unknown", ErrOfferRejected, player)
	}
	e := w.defs.Economy
	if o.Years < e.ContractYears[0] || o.Years > e.ContractYears[1] {
		return fmt.Errorf("%w: %d years, allowed %d..%d", ErrOfferRejected, o.Years, e.ContractYears[0], e.ContractYears[1])
	}
	if lo, hi := e.Demand(p.Overall()), e.OfferCeiling(p.Overall()); o.WeeklyWage < lo || o.WeeklyWage > hi {
		return fmt.Errorf("%w: weekly wage %s, player accepts %s..%s", ErrOfferRejected, o.WeeklyWage, lo, hi)
	}
	return nil
}

// applyEmployment commits an employment plan made in the same operation;
// failure is a broken invariant.
func (w *World) applyEmployment(plan employment.Plan) {
	if err := w.employment.Apply(plan); err != nil {
		panic(fmt.Sprintf("app: unreachable: %v", err))
	}
}

// squadSize is the number of players in a squad's counts by position.
func squadSize(counts map[players.Position]int) int {
	n := 0
	for _, c := range counts {
		n += c
	}
	return n
}

// checkAdmission applies shared squad capacity rules to the staged counts
// before a player joins. Position targets are recruitment preferences, not
// legal caps; only positions absent from the roster are unavailable.
func (w *World) checkAdmission(counts map[players.Position]int, pos players.Position) error {
	q := w.defs.Quota(pos)
	if !slices.Contains(players.Positions(), pos) || q.Count == 0 {
		return fmt.Errorf("app: position %s is unavailable", pos)
	}
	if squadSize(counts) >= w.defs.SquadLimit {
		return ErrSquadFull
	}
	return nil
}

// squadCounts counts a team's players by position.
func (w *World) squadCounts(team ids.TeamID) map[players.Position]int {
	counts := map[players.Position]int{}
	for _, id := range w.employment.Squad(team) {
		p, _ := w.players.Profile(id)
		counts[p.Position]++
	}
	return counts
}

// contractYear handles the contract-year end at `at`:
//
//  1. Every contract ending at `at` is decided. An AI club renews the
//     player when ai.Renew accepts them, by their age at `at`, against the
//     squad's average overall, on aiOffer terms; otherwise, and for every user-club player the manager
//     did not renew, the contract expires and the player becomes a free
//     agent.
//  2. Clubs sign free agents (everyone unemployed, including those who just
//     left) with ai.Signings: AI clubs fill every position up to the roster
//     count, the user club only up to the roster minimum (the safety net that
//     keeps a legal squad; the manager signs the rest). Terms are aiOffer's.
//     The best signings above a club's minimum are held back (see holdBack):
//     those players stay in the pool for the manager.
//  3. The changes are planned as one employment change and checked against
//     the squad minimums, next year's task is queued, then the plan is
//     applied and one event per change is emitted.
func (w *World) contractYear(at sim.GameInstant, cohort []sim.Task) error {
	if len(cohort) != 1 || cohort[0].PayloadID != 0 {
		return fmt.Errorf("app: contract-year cohort at %d has %d tasks, first payload %d", at, len(cohort), cohort[0].PayloadID)
	}
	year, err := w.yearOf(at)
	if err != nil {
		return err
	}
	var changes employment.Changes
	pool := w.freeAgentPool()
	counts := map[ids.ClubID]map[players.Position]int{}
	var needs []ai.ClubNeeds
	for _, c := range w.registry.Clubs() {
		team, _ := w.registry.SeniorTeam(c.ID)
		squad := w.employment.Squad(team)
		average := 0
		for _, id := range squad {
			p, _ := w.players.Profile(id)
			average += p.Overall()
		}
		if n := len(squad); n > 0 {
			average = (2*average + n) / (2 * n)
		}
		counts[c.ID] = map[players.Position]int{}
		kept, keptTotal := 0, 0
		for _, id := range squad {
			a, _ := w.employment.Assignment(id)
			p, _ := w.players.Profile(id)
			if a.Contract.Expires == at {
				age, err := w.age(id, at)
				if err != nil {
					return err
				}
				if c.ID == w.userClub || !ai.Renew(p.Overall(), average, age) {
					changes.Departures = append(changes.Departures, id)
					pool = append(pool, ai.FreeAgent{Player: id, Role: roleOf(p.Position), Overall: p.Overall(), ReleasedBy: c.ID})
					continue
				}
				offer, err := w.aiOffer(id, year)
				if err != nil {
					return err
				}
				expires, err := w.addYears(at, offer.Years)
				if err != nil {
					return err
				}
				changes.Renewals = append(changes.Renewals, employment.Renewal{Player: id, Contract: employment.Contract{Expires: expires, WeeklyWage: offer.WeeklyWage}})
			}
			counts[c.ID][p.Position]++
			kept++
			keptTotal += p.Overall()
		}
		need := ai.ClubNeeds{Club: c.ID}
		if kept > 0 {
			need.Average = (2*keptTotal + kept) / (2 * kept)
		}
		for _, q := range w.defs.Roster {
			want := q.Count
			if c.ID == w.userClub {
				want = q.Min
			}
			if n := want - counts[c.ID][q.Position]; n > 0 {
				need.Needs = append(need.Needs, ai.RoleCount{Role: roleOf(q.Position), Count: n})
			}
		}
		needs = append(needs, need)
	}
	signings, err := ai.Signings(needs, pool)
	if err != nil {
		return err
	}
	signings = w.holdBack(signings, counts)
	for _, s := range signings {
		offer, err := w.aiOffer(s.Player, year)
		if err != nil {
			return err
		}
		expires, err := w.addYears(at, offer.Years)
		if err != nil {
			return err
		}
		p, _ := w.players.Profile(s.Player)
		team, _ := w.registry.SeniorTeam(s.Club)
		if err := w.checkAdmission(counts[s.Club], p.Position); err != nil {
			return fmt.Errorf("app: contract year %d signing player %d for club %d: %w", year, s.Player, s.Club, err)
		}
		changes.Signings = append(changes.Signings, employment.Assignment{
			Player: s.Player, Club: s.Club, Team: team,
			Contract: employment.Contract{Expires: expires, WeeklyWage: offer.WeeklyWage},
		})
		counts[s.Club][p.Position]++
	}
	for _, c := range w.registry.Clubs() {
		for _, q := range w.defs.Roster {
			if n := counts[c.ID][q.Position]; n < q.Min {
				return fmt.Errorf("app: contract year %d leaves club %d with %d %s, minimum %d", year, c.ID, n, q.Position, q.Min)
			}
		}
	}
	plan, err := w.employment.Plan(changes)
	if err != nil {
		return fmt.Errorf("app: contract year %d: %w", year, err)
	}
	next, err := w.addYears(at, 1)
	if err != nil {
		return err
	}
	// The club and team of each departing player, read before applying.
	left := map[ids.PlayerID]employment.Assignment{}
	for _, id := range changes.Departures {
		left[id], _ = w.employment.Assignment(id)
	}
	if err := w.scheduleContractYear(next); err != nil {
		return err
	}
	w.applyEmployment(plan)

	cause := taskCause(cohort[0].ID)
	for _, r := range changes.Renewals {
		a, _ := w.employment.Assignment(r.Player)
		w.emit(at, cause, events.Event{Kind: events.KindContractRenewed, ContractRenewed: &events.ContractRenewed{
			Player: a.Player, Club: a.Club, Team: a.Team, Expires: a.Contract.Expires, WeeklyWage: a.Contract.WeeklyWage,
		}})
	}
	for _, id := range changes.Departures {
		a := left[id]
		w.emit(at, cause, events.Event{Kind: events.KindContractExpired, ContractExpired: &events.ContractExpired{Player: id, Club: a.Club, Team: a.Team}})
	}
	for _, a := range changes.Signings {
		w.emit(at, cause, events.Event{Kind: events.KindPlayerSigned, PlayerSigned: &events.PlayerSigned{
			Player: a.Player, Club: a.Club, Team: a.Team, Expires: a.Contract.Expires, WeeklyWage: a.Contract.WeeklyWage,
		}})
	}
	return nil
}

// freeAgentReserve is how many free agents AI clubs leave in the pool at
// the contract-year end, so that the manager has some to choose from.
const freeAgentReserve = 4

// holdBack drops from the contract-year signings up to freeAgentReserve of
// the best-rated ones (ties: lower player ID) that only fill an AI club's
// vacancy above its roster minimum, and returns the rest in order. counts
// holds each club's players by position, the signings not included. The
// clubs stay short until AI clubs sign free agents in the window, after the
// manager's first days (see freeAgentGrace), or at its close.
func (w *World) holdBack(signings []ai.Signing, counts map[ids.ClubID]map[players.Position]int) []ai.Signing {
	type pick struct {
		i       int
		overall int
	}
	added := map[ids.ClubID]map[players.Position]int{}
	var eligible []pick
	for i, s := range signings {
		p, _ := w.players.Profile(s.Player)
		if added[s.Club] == nil {
			added[s.Club] = map[players.Position]int{}
		}
		added[s.Club][p.Position]++
		if s.Club != w.userClub {
			eligible = append(eligible, pick{i, p.Overall()})
		}
	}
	slices.SortFunc(eligible, func(a, b pick) int {
		return cmp.Or(cmp.Compare(b.overall, a.overall), cmp.Compare(signings[a.i].Player, signings[b.i].Player))
	})
	dropped := map[int]bool{}
	for _, e := range eligible {
		if len(dropped) == freeAgentReserve {
			break
		}
		s := signings[e.i]
		p, _ := w.players.Profile(s.Player)
		if counts[s.Club][p.Position]+added[s.Club][p.Position]-1 < w.defs.Quota(p.Position).Min {
			continue
		}
		added[s.Club][p.Position]--
		dropped[e.i] = true
	}
	var out []ai.Signing
	for i, s := range signings {
		if !dropped[i] {
			out = append(out, s)
		}
	}
	return out
}

// freeAgentPool lists every active player without an employer.
func (w *World) freeAgentPool() []ai.FreeAgent {
	var out []ai.FreeAgent
	for _, id := range w.activePlayers() {
		if _, employed := w.employment.Assignment(id); employed {
			continue
		}
		profile, _ := w.players.Profile(id)
		out = append(out, ai.FreeAgent{Player: id, Role: roleOf(profile.Position), Overall: profile.Overall()})
	}
	return out
}

// checkFreeAgent reports whether a player is an active player without a
// club.
func (w *World) checkFreeAgent(player ids.PlayerID) error {
	p, ok := w.players.Profile(player)
	if _, employed := w.employment.Assignment(player); !ok || p.Retired || employed {
		return fmt.Errorf("%w: player %d is unknown, retired or employed", ErrNotFreeAgent, player)
	}
	return nil
}

// RenewContract offers one of the user club's players, whose contract ends
// at the next contract-year end (its final year), a new contract: Offer.Years
// more contract years at Offer.WeeklyWage. The player accepts a wage from
// their demand up to the offer ceiling. The new wage is paid from the next
// weekly wage run.
type RenewContract struct {
	ID               CommandID
	ExpectedRevision Revision
	Player           ids.PlayerID
	Offer            ContractOffer
}

// ContractRenewed is the recorded result of RenewContract.
type ContractRenewed struct {
	Command  CommandID
	Revision Revision
	Player   ids.PlayerID
	Contract employment.Contract
}

type RenewRecord struct {
	Request RenewContract
	Result  ContractRenewed
}

// SignPlayer signs a free agent to the user club's senior team for
// Offer.Years contract years (the current one included) at
// Offer.WeeklyWage. The squad must be below the squad limit, and squads
// cannot change while rounds await results.
type SignPlayer struct {
	ID               CommandID
	ExpectedRevision Revision
	Player           ids.PlayerID
	Offer            ContractOffer
}

// PlayerSigned is the recorded result of SignPlayer.
type PlayerSigned struct {
	Command  CommandID
	Revision Revision
	Player   ids.PlayerID
	Team     ids.TeamID
	Contract employment.Contract
}

type SignRecord struct {
	Request SignPlayer
	Result  PlayerSigned
}

// checkCommand handles the ID and revision rules shared by the contract
// commands: it returns the recorded record for a retry (found), or an error.
func (w *World) checkCommand(id CommandID, expected Revision, recorded func(commandRecord) bool) (commandRecord, bool, error) {
	if !validCommandID(id) {
		return commandRecord{}, false, fmt.Errorf("%w: zero command ID", ErrInvalidCommand)
	}
	if rec, ok := w.commands[id]; ok {
		if !recorded(rec) {
			return commandRecord{}, false, fmt.Errorf("%w: command %d", ErrCommandIDReused, id)
		}
		return rec, true, nil
	}
	if expected != w.revision {
		return commandRecord{}, false, fmt.Errorf("%w: expected %d, world is at %d", ErrStaleRevision, expected, w.revision)
	}
	if w.userClub == 0 {
		return commandRecord{}, false, ErrNoUserClub
	}
	return commandRecord{}, false, nil
}

// RenewContract records a renewal (see the RenewContract type). Retries
// follow ResolveRounds. On error nothing changes.
func (w *World) RenewContract(cmd RenewContract) (ContractRenewed, error) {
	rec, retry, err := w.checkCommand(cmd.ID, cmd.ExpectedRevision, func(r commandRecord) bool { return r.renew != nil && r.renew.Request == cmd })
	if err != nil {
		return ContractRenewed{}, err
	}
	if retry {
		return rec.renew.Result, nil
	}
	a, ok := w.employment.Assignment(cmd.Player)
	if !ok || a.Club != w.userClub {
		return ContractRenewed{}, fmt.Errorf("%w: player %d", ErrNotUserPlayer, cmd.Player)
	}
	end, err := w.contractYearEnd(w.Now())
	if err != nil {
		return ContractRenewed{}, err
	}
	if a.Contract.Expires != end {
		return ContractRenewed{}, fmt.Errorf("%w: player %d's contract ends at %s", ErrNotFinalYear, cmd.Player, w.calendar.Format(a.Contract.Expires))
	}
	if err := w.checkOffer(cmd.Player, cmd.Offer); err != nil {
		return ContractRenewed{}, err
	}
	expires, err := w.addYears(end, cmd.Offer.Years)
	if err != nil {
		return ContractRenewed{}, err
	}
	contract := employment.Contract{Expires: expires, WeeklyWage: cmd.Offer.WeeklyWage}
	plan, err := w.employment.Plan(employment.Changes{Renewals: []employment.Renewal{{Player: cmd.Player, Contract: contract}}})
	if err != nil {
		return ContractRenewed{}, err
	}

	// Committed. Nothing below can fail.
	w.applyEmployment(plan)
	w.revision++
	res := ContractRenewed{Command: cmd.ID, Revision: w.revision, Player: cmd.Player, Contract: contract}
	w.commands[cmd.ID] = commandRecord{renew: &RenewRecord{Request: cmd, Result: res}}
	w.emit(w.Now(), commandCause(cmd.ID), events.Event{Kind: events.KindContractRenewed, ContractRenewed: &events.ContractRenewed{
		Player: cmd.Player, Club: a.Club, Team: a.Team, Expires: expires, WeeklyWage: contract.WeeklyWage,
	}})
	w.publish()
	return res, nil
}

// SignPlayer records a signing (see the SignPlayer type). Retries follow
// ResolveRounds. On error nothing changes.
func (w *World) SignPlayer(cmd SignPlayer) (PlayerSigned, error) {
	rec, retry, err := w.checkCommand(cmd.ID, cmd.ExpectedRevision, func(r commandRecord) bool { return r.sign != nil && r.sign.Request == cmd })
	if err != nil {
		return PlayerSigned{}, err
	}
	if retry {
		return rec.sign.Result, nil
	}
	if _, pending := w.pendingRounds(); pending {
		return PlayerSigned{}, ErrSquadsLocked
	}
	if err := w.checkFreeAgent(cmd.Player); err != nil {
		return PlayerSigned{}, err
	}
	team, _ := w.userTeam()
	p, _ := w.players.Profile(cmd.Player)
	counts := w.squadCounts(team)
	if err := w.checkAdmission(counts, p.Position); err != nil {
		return PlayerSigned{}, fmt.Errorf("%w: %d players of %d", err, squadSize(counts), w.defs.SquadLimit)
	}
	if err := w.checkOffer(cmd.Player, cmd.Offer); err != nil {
		return PlayerSigned{}, err
	}
	end, err := w.contractYearEnd(w.Now())
	if err != nil {
		return PlayerSigned{}, err
	}
	expires, err := w.addYears(end, cmd.Offer.Years-1)
	if err != nil {
		return PlayerSigned{}, err
	}
	a := employment.Assignment{Player: cmd.Player, Club: w.userClub, Team: team, Contract: employment.Contract{Expires: expires, WeeklyWage: cmd.Offer.WeeklyWage}}
	plan, err := w.employment.Plan(employment.Changes{Signings: []employment.Assignment{a}})
	if err != nil {
		return PlayerSigned{}, err
	}

	// Committed. Nothing below can fail.
	w.applyEmployment(plan)
	w.revision++
	res := PlayerSigned{Command: cmd.ID, Revision: w.revision, Player: cmd.Player, Team: team, Contract: a.Contract}
	w.commands[cmd.ID] = commandRecord{sign: &SignRecord{Request: cmd, Result: res}}
	w.emit(w.Now(), commandCause(cmd.ID), events.Event{Kind: events.KindPlayerSigned, PlayerSigned: &events.PlayerSigned{
		Player: cmd.Player, Club: w.userClub, Team: team, Expires: expires, WeeklyWage: a.Contract.WeeklyWage,
	}})
	w.publish()
	return res, nil
}

// ReleasePlayer ends a user-club player's contract early. The club pays the
// rest of the contract at once, a contract payoff (see releaseCost), and
// the player becomes a free agent whom any club may sign; every open bid
// for him collapses and his listing ends. The squad must keep its minimum at his position and
// the club must have the payoff in hand; squads cannot change while rounds
// await results.
type ReleasePlayer struct {
	ID               CommandID
	ExpectedRevision Revision
	Player           ids.PlayerID
}

// PlayerReleased is the recorded result of ReleasePlayer.
type PlayerReleased struct {
	Command      CommandID
	Revision     Revision
	Player       ids.PlayerID
	Compensation money.Money // the contract payoff; zero when no wage was still due
}

type ReleaseRecord struct {
	Request ReleasePlayer
	Result  PlayerReleased
}

// releaseCost is what ending a contract at t costs: its weekly wage for
// every weekly wage run still due under it, after t and before it expires
// (the contract year runs before the wages at the expiry instant).
func releaseCost(c employment.Contract, t sim.GameInstant) (money.Money, error) {
	week := sim.GameInstant(sim.Week)
	runs := int64((c.Expires-1)/week - t/week)
	if runs <= 0 {
		return 0, nil
	}
	if c.WeeklyWage > money.Money(math.MaxInt64)/money.Money(runs) {
		return 0, fmt.Errorf("app: a payoff of %d weeks at %s overflows", runs, c.WeeklyWage)
	}
	return c.WeeklyWage * money.Money(runs), nil
}

// ReleasePlayer records a release (see the ReleasePlayer type). Retries
// follow ResolveRounds. On error nothing changes.
func (w *World) ReleasePlayer(cmd ReleasePlayer) (PlayerReleased, error) {
	rec, retry, err := w.checkCommand(cmd.ID, cmd.ExpectedRevision, func(r commandRecord) bool { return r.release != nil && r.release.Request == cmd })
	if err != nil {
		return PlayerReleased{}, err
	}
	if retry {
		return rec.release.Result, nil
	}
	a, ok := w.employment.Assignment(cmd.Player)
	if !ok || a.Club != w.userClub {
		return PlayerReleased{}, fmt.Errorf("%w: player %d", ErrNotUserPlayer, cmd.Player)
	}
	if _, pending := w.pendingRounds(); pending {
		return PlayerReleased{}, ErrSquadsLocked
	}
	m, err := w.newMarket(w.Now())
	if err != nil {
		return PlayerReleased{}, err
	}
	pos := m.position[cmd.Player]
	if n, q := m.counts[w.userClub][pos], w.defs.Quota(pos); n <= q.Min {
		return PlayerReleased{}, fmt.Errorf("%w: %d %s, minimum %d", ErrSquadMinimum, n, pos, q.Min)
	}
	cost, err := releaseCost(a.Contract, w.Now())
	if err != nil {
		return PlayerReleased{}, err
	}
	if cost > m.balances[w.userClub] {
		return PlayerReleased{}, fmt.Errorf("%w: payoff %s, balance %s", ErrCannotAfford, cost, m.balances[w.userClub])
	}
	m.jobs.Departures = append(m.jobs.Departures, cmd.Player)
	if cost > 0 {
		m.postings = append(m.postings, finance.Posting{Club: w.userClub, Kind: finance.KindPayoff, Amount: -cost, Player: cmd.Player})
	}
	for _, id := range slices.Sorted(maps.Keys(m.offers)) {
		if o := m.offers[id]; o.Player == cmd.Player {
			m.closeOffer(o, transfers.StatusCollapsed)
		}
	}
	m.unlist(cmd.Player, false)
	offerPlan, moneyPlan, err := m.commit(nil)
	if err != nil {
		return PlayerReleased{}, err
	}

	// Committed. Nothing below can fail.
	w.revision++
	res := PlayerReleased{Command: cmd.ID, Revision: w.revision, Player: cmd.Player, Compensation: cost}
	w.commands[cmd.ID] = commandRecord{release: &ReleaseRecord{Request: cmd, Result: res}}
	cause := commandCause(cmd.ID)
	w.emit(w.Now(), cause, events.Event{Kind: events.KindPlayerReleased, PlayerReleased: &events.PlayerReleased{
		Player: cmd.Player, Club: a.Club, Team: a.Team, Compensation: cost,
	}})
	m.emit(cause, offerPlan, moneyPlan)
	w.publish()
	return res, nil
}

// restoreRelease validates a recorded ReleasePlayer: a fresh ID, a result
// within the revision range for the same registered player, a user club,
// and a payoff that is not negative. validateReleases matches payoffs with
// the ledger.
func (w *World) restoreRelease(c ReleaseRecord, revision Revision) error {
	q, r := c.Request, c.Result
	if err := w.checkRecordID(q.ID, r.Command); err != nil {
		return err
	}
	if r.Revision <= q.ExpectedRevision || r.Revision > revision {
		return fmt.Errorf("result revision %d outside (%d, %d]", r.Revision, q.ExpectedRevision, revision)
	}
	if w.userClub == 0 || r.Player != q.Player || r.Compensation < 0 {
		return fmt.Errorf("result %+v for player %d, user club %d", r, q.Player, w.userClub)
	}
	if _, ok := w.registry.Player(q.Player); !ok {
		return fmt.Errorf("unknown player %d", q.Player)
	}
	return nil
}

// validateReleases checks that contract payoffs and releases match: every
// payoff entry is the user club's and belongs to a recorded release of that
// player for that amount, and every release that paid something posted
// exactly one such entry.
func (w *World) validateReleases() []error {
	var errs []error
	type payoff struct {
		player ids.PlayerID
		amount money.Money
	}
	want := map[payoff]int{}
	for _, rec := range w.commands {
		if r := rec.release; r != nil && r.Result.Compensation > 0 {
			want[payoff{r.Result.Player, -r.Result.Compensation}]++
		}
	}
	for _, e := range w.finance.All() {
		if e.Kind != finance.KindPayoff {
			continue
		}
		key := payoff{e.Player, e.Amount}
		if e.Club != w.userClub || want[key] == 0 {
			errs = append(errs, fmt.Errorf("app: contract payoff %d of %s to player %d by club %d matches no release", e.ID, e.Amount, e.Player, e.Club))
			continue
		}
		want[key]--
	}
	for _, k := range slices.SortedFunc(maps.Keys(want), func(a, b payoff) int {
		return cmp.Or(cmp.Compare(a.player, b.player), cmp.Compare(a.amount, b.amount))
	}) {
		if want[k] > 0 {
			errs = append(errs, fmt.Errorf("app: the release of player %d paid no payoff of %s", k.player, -k.amount))
		}
	}
	return errs
}

// SuggestContract returns the terms an AI club would offer: for a user-club
// player in the final contract year, the renewal it would make at the
// contract-year end; for a free agent, the signing it would make now; for
// another club's player, the contract it would offer with a transfer bid
// now. They are the defaults a client offers. Read-only.
func (w *World) SuggestContract(player ids.PlayerID) (ContractOffer, error) {
	if w.userClub == 0 {
		return ContractOffer{}, ErrNoUserClub
	}
	end, err := w.contractYearEnd(w.Now())
	if err != nil {
		return ContractOffer{}, err
	}
	year, err := w.yearOf(end)
	if err != nil {
		return ContractOffer{}, err
	}
	a, employed := w.employment.Assignment(player)
	switch {
	case !employed:
		if err := w.checkFreeAgent(player); err != nil {
			return ContractOffer{}, err
		}
		return w.aiOffer(player, year-1) // the contract year under way
	case a.Club != w.userClub:
		return w.aiOffer(player, year-1) // as windowTerms
	case a.Contract.Expires != end:
		return ContractOffer{}, fmt.Errorf("%w: player %d", ErrNotFinalYear, player)
	}
	return w.aiOffer(player, year)
}

// FreeAgents returns every active player without a club in ascending ID
// order, with a zero contract. Read-only.
func (w *World) FreeAgents() []SquadPlayer {
	var out []SquadPlayer
	for _, a := range w.freeAgentPool() {
		out = append(out, w.squadPlayer(a.Player))
	}
	return out
}

// ContractYearEnd returns the next contract-year end: the instant every
// contract in its final year expires.
func (w *World) ContractYearEnd() sim.GameInstant {
	end, _ := w.contractYearEnd(w.Now()) // Now is always in range
	return end
}

// validateContracts checks employment against the rules of this file:
//
//   - the roster minimums allow a legal lineup: a goalkeeper and enough
//     outfield players;
//   - every club's senior squad holds at least Min players of each roster
//     position, no one else, and at most SquadLimit in all; players are
//     employed only by their club's senior team;
//   - every contract ends at a contract-year end after now and no more than
//     the longest renewal beyond the next one;
//   - exactly one contract-year task is queued, in the Expiries phase with
//     no payload, due at the next contract-year end.
func (w *World) validateContracts() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	keepers, outfield := 0, 0
	for _, q := range w.defs.Roster {
		if q.Position == players.Goalkeeper {
			keepers += q.Min
		} else {
			outfield += q.Min
		}
	}
	if keepers < 1 || keepers+outfield < matches.StartersPerTeam {
		fail("roster minimums (%d goalkeepers, %d outfield) do not allow a legal lineup", keepers, outfield)
	}

	for _, c := range w.registry.Clubs() {
		team, _ := w.registry.SeniorTeam(c.ID)
		counts := w.squadCounts(team)
		for _, pos := range players.Positions() {
			if n, q := counts[pos], w.defs.Quota(pos); n < q.Min || (q.Count == 0 && n > 0) {
				fail("club %d senior squad has %d %s, minimum %d", c.ID, n, pos, q.Min)
			}
		}
		if n := squadSize(counts); n > w.defs.SquadLimit {
			fail("club %d senior squad has %d players, limit %d", c.ID, n, w.defs.SquadLimit)
		}
	}

	next, err := w.contractYearEnd(w.Now())
	if err != nil {
		return append(errs, err)
	}
	latest, err := w.addYears(next, w.defs.Economy.ContractYears[1])
	if err != nil {
		return append(errs, err)
	}
	for _, a := range w.employment.Assignments() {
		if senior, ok := w.registry.SeniorTeam(a.Club); !ok || senior != a.Team {
			fail("player %d is assigned to team %d, not club %d's senior team", a.Player, a.Team, a.Club)
		}
		if e := a.Contract.Expires; !w.isContractYearEnd(e) || e < next || e > latest {
			fail("player %d's contract ends at %d, not a contract-year end in [%d, %d]", a.Player, e, next, latest)
		}
	}

	tasks := 0
	for _, t := range w.scheduler.Pending() {
		if t.Kind != taskContractYear {
			continue
		}
		tasks++
		if t.PayloadID != 0 || t.Phase != sim.PhaseExpiries || t.DueAt != next {
			fail("contract-year task %d due %d phase %d payload %d, want due %d", t.ID, t.DueAt, t.Phase, t.PayloadID, next)
		}
	}
	if tasks != 1 {
		fail("%d contract-year tasks queued, want 1", tasks)
	}
	return errs
}

// checkPlayerEvent checks a contract event's identities: a registered
// player, and a senior team of the club.
func (w *World) checkPlayerEvent(player ids.PlayerID, club ids.ClubID, team ids.TeamID) error {
	if _, ok := w.registry.Player(player); !ok {
		return fmt.Errorf("unknown player %d", player)
	}
	if senior, ok := w.registry.SeniorTeam(club); !ok || senior != team {
		return fmt.Errorf("team %d is not club %d's senior team", team, club)
	}
	return nil
}

// restoreContractCommand validates a recorded RenewContract or SignPlayer:
// a fresh ID, a result within the revision range for the same player, an
// offer of an allowed length with a positive wage, and a contract matching
// it that ends at a contract-year end. Later changes may have replaced the
// contract, so it is not compared with the current one; and development may
// have changed the player's demand since, so the wage is not compared with
// it.
func (w *World) restoreContractCommand(id CommandID, expected Revision, player ids.PlayerID, offer ContractOffer, resID CommandID, resRevision Revision, resPlayer ids.PlayerID, contract employment.Contract, revision Revision) error {
	if err := w.checkRecordID(id, resID); err != nil {
		return err
	}
	if resRevision <= expected || resRevision > revision {
		return fmt.Errorf("result revision %d outside (%d, %d]", resRevision, expected, revision)
	}
	if w.userClub == 0 || resPlayer != player {
		return fmt.Errorf("result for player %d, request for %d, user club %d", resPlayer, player, w.userClub)
	}
	if e := w.defs.Economy; offer.Years < e.ContractYears[0] || offer.Years > e.ContractYears[1] || offer.WeeklyWage <= 0 {
		return fmt.Errorf("offer %+v is outside the contract rules", offer)
	}
	if _, ok := w.registry.Player(player); !ok {
		return fmt.Errorf("unknown player %d", player)
	}
	if contract.WeeklyWage != offer.WeeklyWage || !w.isContractYearEnd(contract.Expires) {
		return fmt.Errorf("contract %+v does not match offer %+v", contract, offer)
	}
	return nil
}
