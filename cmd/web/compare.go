package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
)

type compareView struct {
	A, B       app.PlayerProfile
	HasA, HasB bool
	Missing    string
	AID, BID   string
	Squad      []app.SquadPlayer
}

func (s *server) compare(r *http.Request) (string, any, error) {
	v := compareView{AID: r.URL.Query().Get("a"), BID: r.URL.Query().Get("b")}
	if club, ok := s.w.UserClub(); ok {
		v.Squad, _ = s.w.ObservedSquad(s.club(), club)
	}
	load := func(raw string) (app.PlayerProfile, bool, string) {
		if raw == "" {
			return app.PlayerProfile{}, false, ""
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || n == 0 {
			var found ids.PlayerID
			for _, player := range v.Squad {
				if !strings.EqualFold(strings.TrimSpace(player.Name), strings.TrimSpace(raw)) {
					continue
				}
				if found != 0 {
					return app.PlayerProfile{}, false, fmt.Sprintf("%q matches more than one player in your squad; use a player ID.", raw)
				}
				found = player.Player
			}
			if found == 0 {
				for _, player := range v.Squad {
					parts := strings.Fields(player.Name)
					if len(parts) == 0 || !strings.EqualFold(parts[0], strings.TrimSpace(raw)) {
						continue
					}
					if found != 0 {
						return app.PlayerProfile{}, false, fmt.Sprintf("%q matches more than one player in your squad; use the full name or a player ID.", raw)
					}
					found = player.Player
				}
			}
			if found == 0 {
				return app.PlayerProfile{}, false, fmt.Sprintf("%q is not a player ID or a name in your squad.", raw)
			}
			n = uint64(found)
		}
		p, ok := s.w.ObservedPlayerProfile(s.club(), ids.PlayerID(n))
		if !ok {
			return app.PlayerProfile{}, false, fmt.Sprintf("There is no player %d.", n)
		}
		return p, true, ""
	}
	var err string
	v.A, v.HasA, err = load(v.AID)
	if err != "" {
		v.Missing = err
	}
	v.B, v.HasB, err = load(v.BID)
	if v.Missing == "" && err != "" {
		v.Missing = err
	}
	return "compare", v, nil
}
