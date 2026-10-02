package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/cmd/internal/present"
	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/careers"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/players"
)

// --- squad and contracts --------------------------------------------------------

type clubOption struct {
	ID        ids.ClubID
	Name      string
	ShortName string
	Nation    string
	Selected  bool
	IsUser    bool
}

type squadRow struct {
	app.SquadPlayer
	Ends      int
	FinalYear bool
	Offer     app.ContractOffer // the usual terms, for final-year players
}

type squadView struct {
	Club        app.TeamLabel
	IsUserClub  bool
	Clubs       []clubOption
	Rows        []squadRow
	ContractEnd string
	SquadText   string
	YearOptions []int
	Sort        SortState
	Probable    *reportPitch // another club's lineup if it played today
}

func (s *server) squad(r *http.Request) (string, any, error) {
	viewClub := s.club()
	if cStr := r.URL.Query().Get("club"); cStr != "" {
		if n, err := strconv.ParseUint(cStr, 10, 64); err == nil && n != 0 {
			if _, ok := s.w.ObservedSquad(s.club(), ids.ClubID(n)); ok {
				viewClub = ids.ClubID(n)
			}
		}
	}
	squad, _ := s.w.ObservedSquad(s.club(), viewClub)
	clubLabel, _ := s.w.ClubLabel(viewClub)
	isUserClub := (viewClub == s.club())
	end := s.w.ContractYearEnd()
	defs := s.w.Content()
	var mins []string
	for _, q := range defs.Roster {
		mins = append(mins, fmt.Sprintf("%d %s", q.Min, q.Position))
	}
	squadText := fmt.Sprintf("You have %d players; a squad holds at most %d, and at least %s.", len(squad), defs.SquadLimit, strings.Join(mins, ", "))
	e := defs.Economy
	sortState := newSortState(r, "pos", "asc")
	v := squadView{
		Club:        clubLabel,
		IsUserClub:  isUserClub,
		ContractEnd: present.Date(s.w.Calendar(), end),
		SquadText:   squadText,
		Sort:        sortState,
	}
	for y := e.ContractYears[0]; y <= e.ContractYears[1]; y++ {
		v.YearOptions = append(v.YearOptions, y)
	}
	for _, c := range s.w.Summary().ClubRows {
		v.Clubs = append(v.Clubs, clubOption{
			ID:        c.ID,
			Name:      c.Name,
			ShortName: c.ShortName,
			Nation:    c.Nation,
			Selected:  c.ID == viewClub,
			IsUser:    c.ID == s.club(),
		})
		if c.ID == viewClub && !isUserClub {
			if l, err := s.w.ProbableLineup(c.SeniorTeam); err == nil {
				v.Probable = s.pitchOf(c.Name, l)
				v.Probable.Title = "Probable lineup"
				v.Probable.Note = "What the club would field if it played today; the squad changes by kickoff."
			}
		}
	}
	for _, p := range squad {
		c, _ := s.w.Calendar().Civil(p.Contract.Expires)
		row := squadRow{SquadPlayer: p, Ends: c.Year, FinalYear: isUserClub && p.Contract.Expires == end}
		if row.FinalYear {
			row.Offer, _ = s.w.SuggestContract(p.Player)
		}
		v.Rows = append(v.Rows, row)
	}
	sortSquadRows(v.Rows, sortState.Col, sortState.Dir)
	return "squad", v, nil
}

type freeRow struct {
	app.SquadPlayer
	Offer app.ContractOffer
	Room  bool // the squad has room at the player's position
}

type freeView struct {
	Rows        []freeRow
	Locked      bool // a matchday is waiting: squads cannot change
	YearOptions []int
	RetireAge   int
	WindowNote  string // while a transfer window is open
	Sort        SortState
}

func (s *server) free(r *http.Request) (string, any, error) {
	defs := s.w.Content()
	squad, _ := s.w.ObservedSquad(s.club(), s.club())
	hasRoom := len(squad) < defs.SquadLimit
	_, locked := s.w.Pending()
	sortState := newSortState(r, "ovr", "desc")
	v := freeView{Locked: locked, RetireAge: players.FreeAgentRetirementAge, Sort: sortState}
	if s.w.TransferWindow().Open {
		win := s.w.TransferWindow()
		v.WindowNote = fmt.Sprintf("Until %s, only you may sign free agents; AI clubs can sign them from then.", s.w.Calendar().Format(win.FreeAgentsOpen))
	}
	for y := defs.Economy.ContractYears[0]; y <= defs.Economy.ContractYears[1]; y++ {
		v.YearOptions = append(v.YearOptions, y)
	}
	for _, p := range s.observedFreeAgents() {
		o, _ := s.w.SuggestContract(p.Player)
		v.Rows = append(v.Rows, freeRow{SquadPlayer: p, Offer: o, Room: hasRoom})
	}
	sortFreeRows(v.Rows, sortState.Col, sortState.Dir)
	return "free", v, nil
}

type playerCareerView struct {
	ClubID, Club, From, Until, Joined, Left string
	Appearances, Goals                      uint16
}

type careerTotalView struct{ Appearances, Goals int }

type playerView struct {
	app.PlayerProfile
	Ends   int
	IsMine bool
	Career []playerCareerView
	// Total is set when the career has more than one spell.
	Total   *careerTotalView
	Missing string // set when the ID names no player
}

func (s *server) player(r *http.Request) (string, any, error) {
	arg := r.URL.Query().Get("id")
	n, err := strconv.ParseUint(arg, 10, 64)
	if err != nil || n == 0 {
		return "player", playerView{Missing: fmt.Sprintf("%q is not a player ID.", arg)}, nil
	}
	p, ok := s.w.ObservedPlayerProfile(s.club(), ids.PlayerID(n))
	if !ok {
		return "player", playerView{Missing: fmt.Sprintf("There is no player %d.", n)}, nil
	}
	c, _ := s.w.Calendar().Civil(p.Contract.Expires)
	spells, _ := s.w.PlayerCareer(ids.PlayerID(n))
	history := make([]playerCareerView, 0, len(spells))
	for _, spell := range spells {
		from := s.w.Calendar().Format(spell.From)
		if spell.Joined == careers.JoinedAtStart {
			from = "before " + from
		}
		until := "Present"
		if !spell.Current() {
			until = s.w.Calendar().Format(spell.Until)
		}
		joined := present.Capitalize(present.Joined(spell.Joined))
		if spell.Joined == careers.JoinedTransfer {
			joined += " for " + spell.Fee.String()
		}
		history = append(history, playerCareerView{ClubID: fmt.Sprint(spell.Club), Club: spell.ClubName, From: from, Until: until, Joined: joined, Left: present.Capitalize(present.Left(spell.Left)),
			Appearances: spell.Appearances, Goals: spell.Goals})
	}
	var total *careerTotalView
	if len(spells) > 1 {
		apps, goals := app.CareerTotals(spells)
		total = &careerTotalView{Appearances: apps, Goals: goals}
	}
	return "player", playerView{PlayerProfile: p, Ends: c.Year, IsMine: p.Club != 0 && p.Club == s.club(), Career: history, Total: total}, nil
}
