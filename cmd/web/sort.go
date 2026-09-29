package main

import (
	"cmp"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/players"
)

// attributeColumns maps a sort column to the attribute it orders by.
var attributeColumns = map[string]players.Attribute{
	"gk": players.Goalkeeping, "def": players.Defending, "pas": players.Passing, "fin": players.Finishing,
	"pac": players.Pace, "sta": players.Stamina, "dri": players.Dribbling, "hea": players.Heading,
	"str": players.Strength, "acc": players.Acceleration, "psn": players.Positioning,
}

// SortState tracks the sorting column and direction for a table view,
// preserving all query parameters for navigation without JavaScript.
type SortState struct {
	BasePath string
	Query    url.Values
	Prefix   string
	Col      string
	Dir      string // "asc" or "desc"
}

// newSortState creates a SortState for the given default column and direction.
func newSortState(r *http.Request, defaultCol, defaultDir string) SortState {
	return newSortStatePrefixed(r, "", defaultCol, defaultDir)
}

// newSortStatePrefixed creates a SortState with a query parameter prefix.
func newSortStatePrefixed(r *http.Request, prefix, defaultCol, defaultDir string) SortState {
	sortKey := prefix + "sort"
	dirKey := prefix + "dir"
	col := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(sortKey)))
	dir := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(dirKey)))
	if col == "" {
		col = defaultCol
	}
	if dir != "asc" && dir != "desc" {
		dir = defaultDir
	}
	q := url.Values{}
	for k, vv := range r.URL.Query() {
		for _, v := range vv {
			q.Add(k, v)
		}
	}
	return SortState{
		BasePath: r.URL.Path,
		Query:    q,
		Prefix:   prefix,
		Col:      col,
		Dir:      dir,
	}
}

// URL returns a relative URL with the given sort column and toggled direction.
func (s SortState) URL(col, defaultDir string) string {
	q := url.Values{}
	sortKey := s.Prefix + "sort"
	dirKey := s.Prefix + "dir"
	for k, vv := range s.Query {
		if k == sortKey || k == dirKey {
			continue
		}
		for _, v := range vv {
			q.Add(k, v)
		}
	}
	nextDir := defaultDir
	if s.Col == col {
		if s.Dir == "asc" {
			nextDir = "desc"
		} else {
			nextDir = "asc"
		}
	}
	q.Set(sortKey, col)
	q.Set(dirKey, nextDir)
	qs := q.Encode()
	if qs != "" {
		return s.BasePath + "?" + qs
	}
	return s.BasePath
}

// Header generates a clickable sort link HTML with arrow indicator.
func (s SortState) Header(col, label, defaultDir string) template.HTML {
	href := s.URL(col, defaultDir)
	activeClass := ""
	indicator := `<span class="sort-icon muted">↕</span>`
	if s.Col == col {
		activeClass = " active " + s.Dir
		if s.Dir == "asc" {
			indicator = `<span class="sort-icon asc">▲</span>`
		} else {
			indicator = `<span class="sort-icon desc">▼</span>`
		}
	}
	return template.HTML(fmt.Sprintf(
		`<a class="sort-link%s" href="%s" data-col="%s" data-dir="%s" title="Sort by %s">%s %s</a>`,
		activeClass, template.HTMLEscapeString(href), template.HTMLEscapeString(col), s.Dir, template.HTMLEscapeString(label), template.HTMLEscapeString(label), indicator,
	))
}

// sortSquadRows sorts a slice of squadRow in place.
func sortSquadRows(rows []squadRow, col, dir string) {
	desc := dir == "desc"
	slices.SortStableFunc(rows, func(a, b squadRow) int {
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
		case "gk", "def", "pas", "fin", "pac", "sta", "dri", "hea", "str", "acc", "psn":
			at := attributeColumns[col]
			diff = cmp.Compare(a.Attributes[at], b.Attributes[at])
		case "nat":
			diff = strings.Compare(strings.ToLower(a.Nationality), strings.ToLower(b.Nationality))
		case "wage":
			diff = cmp.Compare(a.Contract.WeeklyWage, b.Contract.WeeklyWage)
		case "until":
			diff = cmp.Compare(a.Ends, b.Ends)
		case "contract":
			diff = cmp.Compare(a.Demand, b.Demand)
			if diff == 0 {
				diff = cmp.Compare(a.Value, b.Value)
			}
		case "payoff", "release":
			diff = cmp.Compare(a.Payoff, b.Payoff)
		default:
			diff = cmp.Compare(a.Position, b.Position)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(b.Overall, a.Overall), cmp.Compare(a.Player, b.Player))
	})
}

// sortFreeRows sorts a slice of freeRow in place.
func sortFreeRows(rows []freeRow, col, dir string) {
	desc := dir == "desc"
	slices.SortStableFunc(rows, func(a, b freeRow) int {
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
		case "asks":
			diff = cmp.Compare(a.Demand, b.Demand)
		case "nat":
			diff = strings.Compare(strings.ToLower(a.Nationality), strings.ToLower(b.Nationality))
		default:
			diff = cmp.Compare(b.Overall, a.Overall)
			return cmp.Or(diff, cmp.Compare(a.Player, b.Player))
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(b.Overall, a.Overall), cmp.Compare(a.Player, b.Player))
	})
}

// sortTableRows sorts a slice of app.TableRow in place.
func sortTableRows(rows []app.TableRow, col, dir string) {
	desc := dir == "desc"
	slices.SortStableFunc(rows, func(a, b app.TableRow) int {
		var diff int
		switch col {
		case "rank", "pos":
			diff = cmp.Compare(a.Rank, b.Rank)
		case "club":
			diff = strings.Compare(strings.ToLower(a.Label.ClubName), strings.ToLower(b.Label.ClubName))
		case "p", "played":
			diff = cmp.Compare(a.Played, b.Played)
		case "w", "won":
			diff = cmp.Compare(a.Won, b.Won)
		case "d", "drawn":
			diff = cmp.Compare(a.Drawn, b.Drawn)
		case "l", "lost":
			diff = cmp.Compare(a.Lost, b.Lost)
		case "gf":
			diff = cmp.Compare(a.GoalsFor, b.GoalsFor)
		case "ga":
			diff = cmp.Compare(a.GoalsAgainst, b.GoalsAgainst)
		case "gd":
			diff = cmp.Compare(a.GoalDifference(), b.GoalDifference())
		case "pts", "points":
			diff = cmp.Compare(a.Points, b.Points)
		default:
			diff = cmp.Compare(a.Rank, b.Rank)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(a.Rank, b.Rank))
	})
}

// sortFixtureRows sorts a slice of fixtureRow in place.
func sortFixtureRows(rows []fixtureRow, col, dir string) {
	desc := dir == "desc"
	slices.SortStableFunc(rows, func(a, b fixtureRow) int {
		var diff int
		switch col {
		case "round":
			diff = cmp.Compare(a.Round, b.Round)
		case "when":
			diff = strings.Compare(a.When, b.When)
		case "opp", "opponent":
			diff = strings.Compare(strings.ToLower(a.Opponent), strings.ToLower(b.Opponent))
		case "score", "result":
			diff = strings.Compare(a.Result, b.Result)
		case "outcome":
			diff = strings.Compare(a.Outcome, b.Outcome)
		default:
			diff = cmp.Compare(a.Round, b.Round)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(a.Round, b.Round), cmp.Compare(a.Fixture, b.Fixture))
	})
}

// sortLedgerRows sorts a slice of ledgerRow in place.
func sortLedgerRows(rows []ledgerRow, col, dir string) {
	desc := dir == "desc"
	slices.SortStableFunc(rows, func(a, b ledgerRow) int {
		var diff int
		switch col {
		case "when", "date":
			diff = strings.Compare(a.When, b.When)
		case "what":
			diff = strings.Compare(strings.ToLower(a.What), strings.ToLower(b.What))
		case "amount":
			diff = cmp.Compare(a.Amount, b.Amount)
		case "balance":
			diff = cmp.Compare(a.Balance, b.Balance)
		default:
			return 0
		}
		if desc {
			diff = -diff
		}
		return diff
	})
}

// sortMarketRows sorts a slice of marketRow in place.
func sortMarketRows(rows []marketRow, col, dir string) {
	desc := dir == "desc"
	slices.SortStableFunc(rows, func(a, b marketRow) int {
		var diff int
		switch col {
		case "club":
			diff = strings.Compare(strings.ToLower(a.Club), strings.ToLower(b.Club))
		case "name":
			diff = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "age":
			diff = cmp.Compare(a.Age, b.Age)
		case "ovr":
			diff = cmp.Compare(a.Overall, b.Overall)
		case "until", "ends":
			diff = cmp.Compare(a.Ends, b.Ends)
		case "nat":
			diff = strings.Compare(strings.ToLower(a.Nationality), strings.ToLower(b.Nationality))
		case "price", "value":
			diff = cmp.Compare(a.Value, b.Value)
		default:
			diff = cmp.Compare(b.Overall, a.Overall)
			return cmp.Or(diff, cmp.Compare(a.Player, b.Player))
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(b.Overall, a.Overall), cmp.Compare(a.Player, b.Player))
	})
}

// sortOfferRows sorts a slice of offerRow in place.
func sortOfferRows(rows []offerRow, col, dir string) {
	desc := dir == "desc"
	slices.SortStableFunc(rows, func(a, b offerRow) int {
		var diff int
		switch col {
		case "id", "offer":
			diff = cmp.Compare(a.ID, b.ID)
		case "player":
			diff = strings.Compare(strings.ToLower(a.PlayerName), strings.ToLower(b.PlayerName))
		case "buyer", "from":
			diff = strings.Compare(strings.ToLower(a.BuyerName), strings.ToLower(b.BuyerName))
		case "seller", "to", "club":
			diff = strings.Compare(strings.ToLower(a.SellerName), strings.ToLower(b.SellerName))
		case "fee":
			diff = cmp.Compare(a.Fee, b.Fee)
		case "when", "date":
			diff = strings.Compare(a.When, b.When)
		default:
			diff = cmp.Compare(a.ID, b.ID)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(a.ID, b.ID))
	})
}

// sortChooseRows sorts the clubs of the club chooser in place. The default
// column groups clubs by league, in competition order.
func sortChooseRows(rows []chooseClubRow, col, dir string) {
	desc := dir == "desc"
	slices.SortStableFunc(rows, func(a, b chooseClubRow) int {
		var diff int
		switch col {
		case "league":
			diff = cmp.Compare(a.leagueOrder, b.leagueOrder)
			if diff == 0 {
				diff = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
			}
		case "nation":
			diff = strings.Compare(strings.ToLower(a.Nation), strings.ToLower(b.Nation))
		case "name", "club":
			diff = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "short":
			diff = strings.Compare(strings.ToLower(a.ShortName), strings.ToLower(b.ShortName))
		case "players":
			diff = cmp.Compare(a.Players, b.Players)
		case "ovr":
			diff = cmp.Compare(a.AverageOverall, b.AverageOverall)
		default:
			diff = cmp.Compare(a.ID, b.ID)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(a.ID, b.ID))
	})
}

// sortLineupRows sorts a slice of lineupRow in place.
func sortLineupRows(rows []lineupRow, col, dir string) {
	desc := dir == "desc"
	order := map[string]int{"gk": 0, "df": 1, "mf": 2, "fw": 3, "bench": 4, "out": 5}
	slices.SortStableFunc(rows, func(a, b lineupRow) int {
		var diff int
		switch col {
		case "selection", "slot":
			diff = cmp.Compare(order[a.Slot], order[b.Slot])
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
		case "gk", "def", "pas", "fin", "pac", "sta", "dri", "hea", "str", "acc", "psn":
			at := attributeColumns[col]
			diff = cmp.Compare(a.Attributes[at], b.Attributes[at])
		case "nat":
			diff = strings.Compare(strings.ToLower(a.Nationality), strings.ToLower(b.Nationality))
		default:
			diff = cmp.Compare(order[a.Slot], order[b.Slot])
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(a.Position, b.Position), cmp.Compare(b.Overall, a.Overall), cmp.Compare(a.Player, b.Player))
	})
}

// sortInboxMessages sorts a slice of messageView in place.
func sortInboxMessages(rows []messageView, col, dir string) {
	desc := dir == "desc"
	slices.SortStableFunc(rows, func(a, b messageView) int {
		var diff int
		switch col {
		case "when", "date":
			diff = strings.Compare(a.When, b.When)
		case "text":
			diff = strings.Compare(strings.ToLower(a.Text), strings.ToLower(b.Text))
		default:
			return 0
		}
		if desc {
			diff = -diff
		}
		return diff
	})
}

// sortCupTies sorts a slice of cupTieView in place.
func sortCupTies(rows []cupTieView, col, dir string) {
	desc := dir == "desc"
	slices.SortStableFunc(rows, func(a, b cupTieView) int {
		var diff int
		switch col {
		case "home":
			diff = strings.Compare(strings.ToLower(a.Home.ClubName), strings.ToLower(b.Home.ClubName))
		case "away":
			diff = strings.Compare(strings.ToLower(a.Away.ClubName), strings.ToLower(b.Away.ClubName))
		case "result":
			diff = strings.Compare(a.Result, b.Result)
		default:
			diff = cmp.Compare(a.Fixture, b.Fixture)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(a.Fixture, b.Fixture))
	})
}
