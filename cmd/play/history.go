package main

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
)

// history lists every league season and cup edition with its champion, or,
// given a competition and a season, shows that season's table or bracket.
func (s *session) history(args []string) error {
	if len(args) != 0 && len(args) != 2 {
		return errors.New("usage: history [COMPETITION SEASON]")
	}
	records := s.w.History()
	if len(args) == 0 {
		s.printf("\n%-4s %-20s %6s  %s\n", "COMP", "NAME", "SEASON", "CHAMPION")
		for _, h := range records {
			champion := "in progress"
			switch {
			case h.Champion != nil:
				champion = h.Champion.ClubName
				if h.Champion.Club == s.club() {
					champion += " *"
				}
			case h.Complete:
				champion = "none"
			}
			s.printf("%-4d %-20s %6d  %s\n", h.Season.Competition, h.CompetitionName, h.Season.Season, champion)
		}
		s.printf("\nType history COMP SEASON for a season's final table or bracket.\n")
		return nil
	}
	comp, err1 := strconv.ParseUint(args[0], 10, 64)
	season, err2 := strconv.ParseUint(args[1], 10, 16)
	if err1 != nil || err2 != nil {
		return errors.New("usage: history [COMPETITION SEASON]")
	}
	ref := competitions.SeasonRef{Competition: ids.CompetitionID(comp), Season: competitions.Season(season)}
	for _, h := range records {
		if h.Season != ref {
			continue
		}
		if h.Format == competitions.FormatKnockout {
			if c, ok := s.w.Cup(ref); ok {
				s.printEdition(c, false, "id", false)
				return nil
			}
		} else if t, ok := s.w.Table(ref); ok {
			s.printTable(t, t.Rows)
			return nil
		}
	}
	return fmt.Errorf("no season %d of competition %d: type history for the list", season, comp)
}
