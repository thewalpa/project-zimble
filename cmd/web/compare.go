package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
)

type compareView struct {
	A, B       app.PlayerProfile
	HasA, HasB bool
	Missing    string
	AID, BID   string
}

func (s *server) compare(r *http.Request) (string, any, error) {
	v := compareView{AID: r.URL.Query().Get("a"), BID: r.URL.Query().Get("b")}
	load := func(raw string) (app.PlayerProfile, bool, string) {
		if raw == "" {
			return app.PlayerProfile{}, false, ""
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || n == 0 {
			return app.PlayerProfile{}, false, fmt.Sprintf("%q is not a player ID.", raw)
		}
		p, ok := s.w.PlayerProfile(ids.PlayerID(n))
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
