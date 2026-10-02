package main

import (
	"errors"

	"github.com/thewalpa/project-zimble/internal/app"
)

// review shows the club's league season that awaits its review: where
// continue stopped, or where a save was made.
func (s *session) review() error {
	rv, ok, err := s.w.SeasonReview()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no season awaits its review: continue stops there when your club's league season has every result")
	}
	s.printReview(rv)
	return nil
}

// acknowledgeShownReview records that the manager has seen the open review,
// so the season ends on the way on. It does nothing unless the review was
// shown: a continue that reaches a review unseen stops there first.
func (s *session) acknowledgeShownReview() (bool, error) {
	rv, ok, err := s.w.SeasonReview()
	if err != nil || !ok || rv.Season != s.reviewShown {
		return false, err
	}
	_, err = s.w.AcknowledgeSeasonReview(app.AcknowledgeSeasonReview{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Season: rv.Season})
	return err == nil, err
}

// endReviewedSeason acknowledges a shown review and runs the season end due
// now, which draws the play-offs and any cup and schedules the next season.
func (s *session) endReviewedSeason() error {
	if ok, err := s.acknowledgeShownReview(); err != nil || !ok {
		return err
	}
	_, err := s.continueTo(s.w.Now())
	return err
}

// printReview prints the league season's final table with the club's place,
// the competitions still to play and the renewal deadline.
func (s *session) printReview(rv app.SeasonReviewReady) {
	s.reviewShown = rv.Season
	cal := s.w.Calendar()
	s.printf("\nSEASON REVIEW, %s\n", cal.Format(rv.At))
	if t, ok := s.w.Table(rv.Season); ok && len(t.Rows) > 0 {
		s.printf("%s season %d: %s finish top.\n", t.CompetitionName, t.Season, t.Rows[0].Label.ClubName)
		for _, row := range t.Rows {
			if row.Label.Club != s.club() {
				continue
			}
			note := ""
			promoted, relegated := s.w.SeasonMove(rv.Season, row.Rank)
			switch {
			case row.Rank == 1:
				note = ", champions"
			case promoted:
				note = ", promoted"
			case relegated:
				note = ", relegated"
			}
			s.printf("You: %s season %d, %s of %d%s.\n", t.CompetitionName, t.Season, app.Ordinal(row.Rank), len(t.Rows), note)
		}
		s.printTable(t, t.Rows)
	}
	for _, ref := range rv.OpenEditions {
		if c, ok := s.w.Cup(ref); ok {
			s.printf("Still to play: %s %d (type cup).\n", c.Name, c.Edition)
		} else if p, ok := s.w.Playoff(ref); ok {
			s.printf("Still to play: %s %d (type playoffs).\n", s.w.PlayoffTitle(ref.Competition), p.Edition)
		}
	}
	s.printf("Contracts ending on %s can be renewed until then", cal.Format(rv.NextContractYearEnd))
	if n := len(s.expiring()); n > 0 {
		s.printf(": %d of your players' contracts end (type contracts).\n", n)
	} else {
		s.printf(".\n")
	}
	s.printf("Type continue to end the season and go on.\n")
}
