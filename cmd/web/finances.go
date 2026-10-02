package main

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

type ledgerRow struct {
	When    string
	What    string
	Amount  money.Money
	Balance money.Money
	at      sim.GameInstant // When, for sorting
}

type financesView struct {
	Balance    money.Money
	WeeklyWage money.Money
	Entries    int
	Rows       []ledgerRow // newest first
	Sort       SortState
}

func (s *server) finances(r *http.Request) (string, any, error) {
	fin, ok := s.w.Finances(s.club())
	if !ok {
		return "", nil, errors.New("your club has no account")
	}
	sortState := newSortState(r, "when", "desc")
	v := financesView{Balance: fin.Balance, WeeklyWage: fin.WeeklyWage, Entries: len(fin.Entries), Sort: sortState}
	var running money.Money
	for _, e := range fin.Entries {
		running += e.Amount
		what := e.Kind.String()
		if e.Fixture != 0 {
			what = fmt.Sprintf("%s, fixture %d", what, e.Fixture)
		}
		if e.Offer != 0 {
			what = fmt.Sprintf("%s, offer %d", what, e.Offer)
		}
		if e.Player != 0 {
			what = fmt.Sprintf("%s, %s", what, s.name(e.Player))
		}
		v.Rows = append(v.Rows, ledgerRow{When: s.w.Calendar().Format(e.At), at: e.At, What: what, Amount: e.Amount, Balance: running})
	}
	slices.Reverse(v.Rows)
	v.Rows = v.Rows[:min(len(v.Rows), 40)]
	sortLedgerRows(v.Rows, sortState.Col, sortState.Dir)
	return "finances", v, nil
}
