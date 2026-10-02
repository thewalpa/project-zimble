package main

import (
	"fmt"
	"net/http"

	"github.com/thewalpa/project-zimble/internal/app"
)

// openEdition is a competition the club is still in when its league season
// is reviewed.
type openEdition struct {
	Name string
	Link string
}

type reviewView struct {
	Open         bool // a review awaits: the league season has every result
	Season       clubSeasonRow
	Champion     string
	Table        *leagueTable
	Editions     []openEdition
	ContractsEnd string
	Expiring     int
}

// review shows the club's league season once it has every result and before
// it ends. The review is app's (SeasonReview), so it reads the same after a
// restart from a save made at the stop.
func (s *server) review(*http.Request) (string, any, error) {
	rv, ok, err := s.w.SeasonReview()
	if err != nil || !ok {
		return "review", reviewView{}, err
	}
	s.reviewShown = rv.Season
	v := reviewView{Open: true, ContractsEnd: s.w.Calendar().Format(rv.NextContractYearEnd), Expiring: len(s.expiring())}
	for _, row := range s.clubHistoryOf(s.club()).Rows {
		if row.Competition == rv.Season.Competition && row.Season == int(rv.Season.Season) {
			v.Season = row
		}
	}
	if t, ok := s.w.Table(rv.Season); ok {
		lt := s.leagueTableOf(t)
		v.Table = &lt
		if len(t.Rows) > 0 {
			v.Champion = t.Rows[0].Label.ClubName
		}
		// The season has not ended, so its history row has no note yet.
		for _, row := range t.Rows {
			if row.Label.Club != s.club() {
				continue
			}
			promoted, relegated := s.w.SeasonMove(rv.Season, row.Rank)
			switch {
			case row.Rank == 1:
				v.Season.Note = "Champions"
			case promoted:
				v.Season.Note = "Promoted"
			case relegated:
				v.Season.Note = "Relegated"
			}
		}
	}
	for _, ref := range rv.OpenEditions {
		if c, ok := s.w.Cup(ref); ok {
			v.Editions = append(v.Editions, openEdition{Name: fmt.Sprintf("%s %d", c.Name, c.Edition), Link: "/cup"})
		} else if _, ok := s.w.Playoff(ref); ok {
			v.Editions = append(v.Editions, openEdition{Name: s.w.PlayoffTitle(ref.Competition), Link: "/playoffs"})
		}
	}
	return "review", v, nil
}

// acknowledgeShownReview records that the manager has seen the open review,
// so the season ends on the way on. It does nothing unless the review was
// shown: a Continue that reaches a review unseen stops there first.
func (s *server) acknowledgeShownReview() (bool, error) {
	rv, ok, err := s.w.SeasonReview()
	if err != nil || !ok || rv.Season != s.reviewShown {
		return false, err
	}
	_, err = s.w.AcknowledgeSeasonReview(app.AcknowledgeSeasonReview{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Season: rv.Season})
	return err == nil, err
}

// endReviewedSeason acknowledges a shown review and runs the season end due
// now, which draws the play-offs and any cup and schedules the next season.
func (s *server) endReviewedSeason() error {
	if ok, err := s.acknowledgeShownReview(); err != nil || !ok {
		return err
	}
	_, err := s.continueTo(s.w.Now())
	return err
}
