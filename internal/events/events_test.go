package events

import (
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

func valid() []Event {
	env := func(id ID, k Kind) Event {
		return Event{ID: id, Revision: 3, Sequence: 1, Cause: Cause{Kind: CauseTask, ID: 9}, Kind: k, SchemaVersion: SchemaVersion}
	}
	a := env(1, KindRoundStarted)
	a.RoundStarted = &RoundStarted{Competition: 1, Season: 1, Round: 1, Fixtures: []Pairing{{Fixture: 1, Home: 1, Away: 2}, {Fixture: 2, Home: 3, Away: 4}}}
	b := env(2, KindMatchCompleted)
	b.MatchCompleted = &MatchCompleted{Fixture: 1, Competition: 1, Season: 1, Round: 1, Home: 1, Away: 2, HomeGoals: 2}
	c := env(3, KindLineupSubmitted)
	c.Cause.Kind = CauseCommand
	c.LineupSubmitted = &LineupSubmitted{Fixture: 1, Team: 1}
	d := env(4, KindSeasonEnded)
	d.SeasonEnded = &SeasonEnded{Competition: 1, Season: 1, Ranking: []ids.TeamID{2, 1}}
	e := env(5, KindSeasonStarted)
	e.SeasonStarted = &SeasonStarted{Competition: 1, Season: 2, FirstKickoff: 100, Entrants: []ids.TeamID{1, 2}}
	f := env(6, KindLedgerPosted)
	f.LedgerPosted = &LedgerPosted{Entries: []LedgerEntry{{Entry: 9, Club: 1, Kind: 2, Amount: -500, Balance: 100}, {Entry: 10, Club: 2, Kind: 2, Amount: -700, Balance: -50}}}
	g := env(7, KindContractRenewed)
	g.ContractRenewed = &ContractRenewed{Player: 5, Club: 1, Team: 1, Expires: 500, WeeklyWage: 100}
	h := env(8, KindContractExpired)
	h.ContractExpired = &ContractExpired{Player: 6, Club: 1, Team: 1}
	i := env(9, KindPlayerSigned)
	i.PlayerSigned = &PlayerSigned{Player: 6, Club: 2, Team: 2, Expires: 500, WeeklyWage: 100}
	j := env(10, KindPlayerRetired)
	j.PlayerRetired = &PlayerRetired{Player: 7, Club: 1, Team: 1, Age: 35}
	k := env(11, KindYouthJoined)
	k.YouthJoined = &YouthJoined{Player: 161, Club: 1, Team: 1, Expires: 500, WeeklyWage: 100}
	l := env(12, KindPlayersDeveloped)
	l.PlayersDeveloped = &PlayersDeveloped{Players: []Development{{Player: 1, Team: 1, Before: 60, After: 62}, {Player: 2, Before: 50, After: 49}}}
	deal := Deal{Offer: 4, Player: 8, Seller: 1, SellerTeam: 1, Buyer: 2, BuyerTeam: 2, Fee: 500_000}
	m := env(13, KindTransferOffered)
	m.TransferOffered = &TransferOffered{Deal: deal, Deadline: 100}
	n := env(14, KindTransferCompleted)
	n.TransferCompleted = &TransferCompleted{Deal: deal, Expires: 500, WeeklyWage: 100}
	o := env(15, KindOfferClosed)
	o.OfferClosed = &OfferClosed{Deal: deal, Outcome: 3}
	r := env(16, KindPlayerReleased)
	r.Cause.Kind = CauseCommand
	r.PlayerReleased = &PlayerReleased{Player: 9, Club: 1, Team: 1, Compensation: 12_000}
	ls := env(17, KindPlayerListed)
	ls.PlayerListed = &PlayerListed{Player: 9, Club: 1, Team: 1, Asking: 400_000}
	ul := env(18, KindPlayerUnlisted)
	ul.Cause.Kind = CauseCommand
	ul.PlayerUnlisted = &PlayerUnlisted{Player: 9, Club: 1, Team: 1}
	rd := env(19, KindInboxRead)
	rd.Cause.Kind = CauseCommand
	rd.InboxRead = &InboxRead{Message: 2}
	return []Event{a, b, c, d, e, f, g, h, i, j, k, l, m, n, o, r, ls, ul, rd}
}

func TestValidate(t *testing.T) {
	for _, e := range valid() {
		if err := e.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]func([]Event) Event{
		"read zero message": func(v []Event) Event { v[18].InboxRead.Message = 0; return v[18] },
		"read itself":       func(v []Event) Event { v[18].InboxRead.Message = v[18].ID; return v[18] },
		"read future":       func(v []Event) Event { v[18].InboxRead.Message = v[18].ID + 1; return v[18] },
		"read from task":    func(v []Event) Event { v[18].Cause.Kind = CauseTask; return v[18] },
		"zero ID":           func(v []Event) Event { v[0].ID = 0; return v[0] },
		"zero revision":     func(v []Event) Event { v[0].Revision = 0; return v[0] },
		"zero sequence":     func(v []Event) Event { v[0].Sequence = 0; return v[0] },
		"other schema":      func(v []Event) Event { v[0].SchemaVersion = 2; return v[0] },
		"no cause":          func(v []Event) Event { v[0].Cause = Cause{}; return v[0] },
		"cause kind 3":      func(v []Event) Event { v[0].Cause.Kind = 3; return v[0] },
		"kind mismatch":     func(v []Event) Event { v[0].Kind = KindMatchCompleted; return v[0] },
		"no payload":        func(v []Event) Event { v[1].MatchCompleted = nil; return v[1] },
		"two payloads":      func(v []Event) Event { v[1].LineupSubmitted = v[2].LineupSubmitted; return v[1] },
		"unknown kind":      func(v []Event) Event { v[2].Kind = 99; return v[2] },
		"no fixtures":       func(v []Event) Event { v[0].RoundStarted.Fixtures = nil; return v[0] },
		"unordered fixture": func(v []Event) Event { v[0].RoundStarted.Fixtures[1].Fixture = 1; return v[0] },
		"self match":        func(v []Event) Event { v[1].MatchCompleted.Away = 1; return v[1] },
		"zero team":         func(v []Event) Event { v[2].LineupSubmitted.Team = 0; return v[2] },
		"empty ranking":     func(v []Event) Event { v[3].SeasonEnded.Ranking = nil; return v[3] },
		"zero entrant":      func(v []Event) Event { v[4].SeasonStarted.Entrants[0] = 0; return v[4] },
		"no ledger entries": func(v []Event) Event { v[5].LedgerPosted.Entries = nil; return v[5] },
		"entries unordered": func(v []Event) Event { v[5].LedgerPosted.Entries[1].Entry = 9; return v[5] },
		"entry kind zero":   func(v []Event) Event { v[5].LedgerPosted.Entries[0].Kind = 0; return v[5] },
		"renewal ended":     func(v []Event) Event { v[6].OccurredAt = 500; return v[6] },
		"renewal unpaid":    func(v []Event) Event { v[6].ContractRenewed.WeeklyWage = 0; return v[6] },
		"expiry zero team":  func(v []Event) Event { v[7].ContractExpired.Team = 0; return v[7] },
		"signing no player": func(v []Event) Event { v[8].PlayerSigned.Player = 0; return v[8] },
		"retired club only": func(v []Event) Event { v[9].PlayerRetired.Team = 0; return v[9] },
		"retired no player": func(v []Event) Event { v[9].PlayerRetired.Player = 0; return v[9] },
		"youth no team":     func(v []Event) Event { v[10].YouthJoined.Team = 0; return v[10] },
		"youth unpaid":      func(v []Event) Event { v[10].YouthJoined.WeeklyWage = 0; return v[10] },
		"nobody developed":  func(v []Event) Event { v[11].PlayersDeveloped.Players = nil; return v[11] },
		"developed twice":   func(v []Event) Event { v[11].PlayersDeveloped.Players[1].Player = 1; return v[11] },
		"overall zero":      func(v []Event) Event { v[11].PlayersDeveloped.Players[0].After = 0; return v[11] },
		"overall above 100": func(v []Event) Event { v[11].PlayersDeveloped.Players[0].Before = 101; return v[11] },
		"offer no offer":    func(v []Event) Event { v[12].TransferOffered.Offer = 0; return v[12] },
		"offer to itself":   func(v []Event) Event { v[12].TransferOffered.Buyer = 1; return v[12] },
		"offer same team":   func(v []Event) Event { v[12].TransferOffered.BuyerTeam = 1; return v[12] },
		"offer for nothing": func(v []Event) Event { v[12].TransferOffered.Fee = 0; return v[12] },
		"offer answered":    func(v []Event) Event { v[12].OccurredAt = 100; return v[12] },
		"done no player":    func(v []Event) Event { v[13].TransferCompleted.Player = 0; return v[13] },
		"done unpaid":       func(v []Event) Event { v[13].TransferCompleted.WeeklyWage = 0; return v[13] },
		"done expired":      func(v []Event) Event { v[13].OccurredAt = 500; return v[13] },
		"closed while open": func(v []Event) Event { v[14].OfferClosed.Outcome = 1; return v[14] },
		"closed completed":  func(v []Event) Event { v[14].OfferClosed.Outcome = 2; return v[14] },
		"closed as 6":       func(v []Event) Event { v[14].OfferClosed.Outcome = 6; return v[14] },
		"closed no seller":  func(v []Event) Event { v[14].OfferClosed.SellerTeam = 0; return v[14] },
		"released nobody":   func(v []Event) Event { v[15].PlayerReleased.Player = 0; return v[15] },
		"released no team":  func(v []Event) Event { v[15].PlayerReleased.Team = 0; return v[15] },
		"released refunded": func(v []Event) Event { v[15].PlayerReleased.Compensation = -1; return v[15] },
		"listed no club":    func(v []Event) Event { v[16].PlayerListed.Club = 0; return v[16] },
		"listed for free":   func(v []Event) Event { v[16].PlayerListed.Asking = 0; return v[16] },
		"unlisted nobody":   func(v []Event) Event { v[17].PlayerUnlisted.Player = 0; return v[17] },
		"unlisted no team":  func(v []Event) Event { v[17].PlayerUnlisted.Team = 0; return v[17] },
	}
	for name, mutate := range cases {
		if err := mutate(valid()).Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCloneSharesNothing(t *testing.T) {
	orig := valid()
	want := valid()
	c := CloneAll(orig)
	c[0].RoundStarted.Fixtures[0].Home = 9
	c[1].MatchCompleted.HomeGoals = 9
	c[2].LineupSubmitted.Team = 9
	c[3].SeasonEnded.Ranking[0] = 9
	c[4].SeasonStarted.Entrants[0] = 9
	c[5].LedgerPosted.Entries[0].Amount = 9
	c[6].ContractRenewed.Expires = 9
	c[7].ContractExpired.Club = 9
	c[8].PlayerSigned.Team = 9
	c[9].PlayerRetired.Age = 9
	c[10].YouthJoined.Expires = 9
	c[11].PlayersDeveloped.Players[0].After = 9
	c[12].TransferOffered.Fee = 9
	c[13].TransferCompleted.Expires = 9
	c[14].OfferClosed.Outcome = 9
	c[15].PlayerReleased.Compensation = 9
	c[16].PlayerListed.Asking = 9
	c[17].PlayerUnlisted.Club = 9
	c[18].InboxRead.Message = 9
	if !reflect.DeepEqual(orig, want) {
		t.Fatal("clone shares memory with the original")
	}
	if CloneAll(nil) != nil {
		t.Fatal("CloneAll(nil) is not nil")
	}
}
