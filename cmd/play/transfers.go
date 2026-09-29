package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

// --- transfers -------------------------------------------------------------

var positionNames = map[string]players.Position{
	"gk": players.Goalkeeper, "df": players.Defender, "mf": players.Midfielder, "fw": players.Forward,
}

// windowLine describes the transfer window under way or the next one.
func (s *session) windowLine() string {
	cal := s.w.Calendar()
	win := s.w.TransferWindow()
	if !win.Open {
		return fmt.Sprintf("The transfer window opens on %s.", cal.Format(win.Opens))
	}
	line := fmt.Sprintf("The transfer window is open until %s; clubs answer bids made before %s.", cal.Format(win.Closes), cal.Format(win.BidsClose))
	if s.w.Now() >= win.NeededClose && s.w.Now() < win.Closes {
		line += " The window is about to close: AI clubs now sell only listed players and players they can spare."
	}
	return line
}

// refusalText says why an AI club would refuse a bid at its price, or "".
func refusalText(r app.BidRefusal) string {
	switch r {
	case app.RefusalStar:
		return "won't join a weaker club"
	case app.RefusalSettling:
		return "just signed"
	case app.RefusalNeeded:
		return "needed until the window closes"
	}
	return ""
}

// bidsReceived returns the open bids for the club's players.
func (s *session) bidsReceived() []app.OfferView {
	var out []app.OfferView
	for _, o := range s.w.Offers() {
		if o.Status == transfers.StatusOpen && o.Seller == s.club() {
			out = append(out, o)
		}
	}
	return out
}

// transfers shows the window, the bids for the club's players awaiting an
// answer, the club's own bids and this window's transfers.
func (s *session) transfers(args []string) error {
	col := "date"
	desc := false
	if len(args) > 0 {
		col = strings.ToLower(args[0])
		switch col {
		case "fee":
			desc = true
		case "date", "player", "name", "from", "to", "id":
			desc = false
		default:
			return fmt.Errorf("unknown sort %q: choose date, player, from, to or fee", args[0])
		}
		if len(args) > 1 {
			switch strings.ToLower(args[1]) {
			case "asc":
				desc = false
			case "desc":
				desc = true
			default:
				return errors.New("usage: transfers [COLUMN [asc|desc]]")
			}
		}
		if len(args) > 2 {
			return errors.New("usage: transfers [COLUMN [asc|desc]]")
		}
	}
	cal := s.w.Calendar()
	win := s.w.TransferWindow()
	s.printf("\n%s\n", s.windowLine())
	var mine, done []app.OfferView
	for _, o := range s.w.Offers() {
		switch {
		case o.Status == transfers.StatusOpen && o.Buyer == s.club():
			mine = append(mine, o)
		case o.Status == transfers.StatusCompleted && o.ClosedAt >= win.Opens:
			done = append(done, o)
		}
	}
	sortOffers := func(offers []app.OfferView) {
		slices.SortStableFunc(offers, func(a, b app.OfferView) int {
			var diff int
			switch col {
			case "date":
				diff = cmp.Compare(a.Deadline, b.Deadline)
			case "player", "name":
				diff = strings.Compare(strings.ToLower(a.PlayerName), strings.ToLower(b.PlayerName))
			case "from":
				diff = strings.Compare(strings.ToLower(a.SellerName), strings.ToLower(b.SellerName))
			case "to":
				diff = strings.Compare(strings.ToLower(a.BuyerName), strings.ToLower(b.BuyerName))
			case "fee":
				diff = cmp.Compare(a.Fee, b.Fee)
			case "id":
				diff = cmp.Compare(a.ID, b.ID)
			}
			if desc {
				diff = -diff
			}
			return cmp.Or(diff, cmp.Compare(a.Deadline, b.Deadline), cmp.Compare(a.ID, b.ID))
		})
	}
	if received := s.bidsReceived(); len(received) > 0 {
		sortOffers(received)
		s.printf("\nBids for your players (accept OFFER or reject OFFER):\n")
		for _, o := range received {
			s.printf("  offer %-4d %-24s %-22s %14s  answer before %s\n", o.ID, o.PlayerName, o.BuyerName, o.Fee, cal.Format(o.Deadline))
		}
	}
	var listed []app.ListedPlayer
	for _, l := range s.w.TransferList() {
		if l.Club == s.club() {
			listed = append(listed, l)
		}
	}
	if len(listed) > 0 {
		s.printf("\nYour players on the transfer list (unlist ID takes one off):\n")
		for _, l := range listed {
			s.printf("  %4d  %-24s %-3s %14s\n", l.Player, l.Name, l.Position, l.Value)
		}
	}
	if len(mine) > 0 {
		sortOffers(mine)
		s.printf("\nYour bids awaiting an answer:\n")
		for _, o := range mine {
			s.printf("  offer %-4d %-24s %-22s %14s  answered %s\n", o.ID, o.PlayerName, o.SellerName, o.Fee, cal.Format(o.Deadline))
		}
	}
	if len(done) > 0 && win.Open {
		slices.SortStableFunc(done, func(a, b app.OfferView) int {
			var diff int
			switch col {
			case "date":
				diff = cmp.Compare(a.ClosedAt, b.ClosedAt)
			case "player", "name":
				diff = strings.Compare(strings.ToLower(a.PlayerName), strings.ToLower(b.PlayerName))
			case "from":
				diff = strings.Compare(strings.ToLower(a.SellerName), strings.ToLower(b.SellerName))
			case "to":
				diff = strings.Compare(strings.ToLower(a.BuyerName), strings.ToLower(b.BuyerName))
			case "fee":
				diff = cmp.Compare(a.Fee, b.Fee)
			case "id":
				diff = cmp.Compare(a.ID, b.ID)
			}
			if desc {
				diff = -diff
			}
			return cmp.Or(diff, cmp.Compare(a.ClosedAt, b.ClosedAt), cmp.Compare(a.ID, b.ID))
		})
		s.printf("\nTransfers in this window:\n")
		for _, o := range done {
			mark := "  "
			if o.Buyer == s.club() || o.Seller == s.club() {
				mark = "* "
			}
			s.printf("%s%-26s %-24s %-22s -> %-22s %14s\n", mark, cal.Format(o.ClosedAt), o.PlayerName, o.SellerName, o.BuyerName, o.Fee)
		}
	}
	s.printf("\nmarket GK|DF|MF|FW lists other clubs' players with their asking prices; bid ID [FEE [YEARS [WAGE]]] makes a bid.\n")
	s.printf("list shows the players clubs have put up for sale; list ID [PRICE] puts one of yours on it.\n")
	return nil
}

// market lists other clubs' players at a position, best first, with their
// clubs' asking prices.
func (s *session) market(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: market GK|DF|MF|FW")
	}
	pos, ok := positionNames[strings.ToLower(args[0])]
	if !ok {
		return errors.New("usage: market GK|DF|MF|FW")
	}
	col := "ovr"
	desc := true
	if len(args) > 1 {
		col = strings.ToLower(args[1])
		switch col {
		case "ovr", "price", "value":
			desc = true
		case "id", "club", "clb", "name", "age", "ends", "contract":
			desc = false
		default:
			return fmt.Errorf("unknown sort %q: choose ovr, price, id, club, name, age or ends", args[1])
		}
		if len(args) > 2 {
			switch strings.ToLower(args[2]) {
			case "asc":
				desc = false
			case "desc":
				desc = true
			default:
				return errors.New("usage: market GK|DF|MF|FW [COLUMN [asc|desc]]")
			}
		}
		if len(args) > 3 {
			return errors.New("usage: market GK|DF|MF|FW [COLUMN [asc|desc]]")
		}
	}
	type row struct {
		app.SquadPlayer
		club   string
		nation string
	}
	var rows []row
	for _, c := range s.w.Summary().ClubRows {
		if c.ID == s.club() {
			continue
		}
		squad, _ := s.w.Squad(c.ID)
		for _, p := range squad {
			if p.Position == pos {
				rows = append(rows, row{p, c.ShortName, c.Nation})
			}
		}
	}
	slices.SortStableFunc(rows, func(a, b row) int {
		var diff int
		switch col {
		case "id":
			diff = cmp.Compare(a.Player, b.Player)
		case "club", "clb":
			diff = strings.Compare(strings.ToLower(a.club), strings.ToLower(b.club))
		case "name":
			diff = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "age":
			diff = cmp.Compare(a.Age, b.Age)
		case "ovr":
			diff = cmp.Compare(a.Overall, b.Overall)
		case "ends", "contract":
			diff = cmp.Compare(a.Contract.Expires, b.Contract.Expires)
		case "price", "value":
			diff = cmp.Compare(a.Value, b.Value)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(b.Overall, a.Overall), cmp.Compare(a.Player, b.Player))
	})
	cal := s.w.Calendar()
	s.printf("\n%4s  %-3s %-10s %-24s %-10s %3s %5s %8s %14s\n", "ID", "CLB", "CLUB NATION", "NAME", "NATIONALITY", "AGE", "OVR", "ENDS", "ASKING PRICE")
	for _, r := range rows[:min(len(rows), 20)] {
		ends, _ := cal.Civil(r.Contract.Expires)
		note := ""
		if r.Listed {
			note = "  listed"
		}
		if why := refusalText(s.w.BidRefusal(r.Player)); why != "" {
			note += "  (" + why + ")"
		}
		s.printf("%4d  %-3s %-11s %-24s %-11s %3d %5d %8d %14s%s\n", r.Player, r.club, r.nation, r.Name, r.Nationality, r.Age, r.Overall, ends.Year, r.Value, note)
	}
	if fin, ok := s.w.Finances(s.club()); ok {
		s.printf("Your balance is %s. %s\n", fin.Balance, s.windowLine())
	}
	return nil
}

// list shows the transfer list, or with "ID [PRICE]" puts one of the club's
// players on it at PRICE (whole units; default: his value).
func (s *session) list(args []string) error {
	const usage = "usage: list [ID [PRICE]]"
	if len(args) > 2 {
		return errors.New(usage)
	}
	if len(args) == 0 {
		list := s.w.TransferList()
		if len(list) == 0 {
			s.printf("No one is on the transfer list. %s\n", s.windowLine())
			return nil
		}
		s.printf("\n%4s  %-22s %-24s %-10s %-3s %3s %5s %14s\n", "ID", "CLUB", "NAME", "NATION", "POS", "AGE", "OVR", "ASKING PRICE")
		for _, l := range list {
			note := ""
			if l.Club != s.club() {
				if why := refusalText(s.w.BidRefusal(l.Player)); why != "" {
					note = "  (" + why + ")"
				}
			}
			s.printf("%4d  %-22s %-24s %-10s %-3s %3d %5d %14s%s\n", l.Player, l.ClubName, l.Name, l.Nationality, l.Position, l.Age, l.Overall, l.Value, note)
		}
		s.printf("bid ID buys another club's player at his asking price; list ID [PRICE] and unlist ID manage yours.\n")
		return nil
	}
	player, err := parsePlayer(args[0])
	if err != nil {
		return err
	}
	squad, _ := s.w.Squad(s.club())
	i := slices.IndexFunc(squad, func(p app.SquadPlayer) bool { return p.Player == player })
	if i < 0 {
		return fmt.Errorf("player %d is not in your squad", player)
	}
	p := squad[i]
	asking := p.Value
	if len(args) > 1 {
		units, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || units <= 0 || units > 1_000_000_000_000 {
			return errors.New(usage)
		}
		asking = money.Units(units)
	}
	res, err := s.w.ListPlayer(app.ListPlayer{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Player: player, Asking: asking})
	if err != nil {
		return err
	}
	role := map[players.Position]string{players.Goalkeeper: "goalkeeper", players.Defender: "defender", players.Midfielder: "midfielder", players.Forward: "forward"}[p.Position]
	s.printf("%s is on the transfer list at %s until the window closes; clubs that need a %s may bid.\n", p.Name, res.Asking, role)
	return nil
}

// unlist takes one of the club's players off the transfer list.
func (s *session) unlist(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: unlist ID")
	}
	player, err := parsePlayer(args[0])
	if err != nil {
		return err
	}
	if _, err := s.w.ListPlayer(app.ListPlayer{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Player: player}); err != nil {
		return err
	}
	s.printf("%s is off the transfer list.\n", s.name(player))
	return nil
}

// bid parses "ID [FEE [YEARS [WAGE]]]": FEE (whole units) defaults to the
// asking price, the terms to an AI club's.
func (s *session) bid(args []string) error {
	const usage = "usage: bid ID [FEE [YEARS [WAGE]]]"
	if len(args) < 1 || len(args) > 4 {
		return errors.New(usage)
	}
	player, err := parsePlayer(args[0])
	if err != nil {
		return err
	}
	p, ok := s.otherPlayer(player)
	if !ok {
		return fmt.Errorf("player %d is not at another club; type market POS for the list", player)
	}
	fee := p.Value
	if len(args) > 1 {
		units, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || units <= 0 || units > 1_000_000_000_000 {
			return errors.New(usage)
		}
		fee = money.Units(units)
	}
	terms := []string{args[0]}
	terms = append(terms, args[min(len(args), 2):]...)
	_, o, err := s.offer(terms, usage, []app.SquadPlayer{p}, "player %d is not at another club")
	if err != nil {
		return err
	}
	res, err := s.w.MakeTransferOffer(app.MakeTransferOffer{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Player: player, Fee: fee, Offer: o})
	if err != nil {
		return err
	}
	years := "years"
	if o.Years == 1 {
		years = "year"
	}
	s.printf("You bid %s for %s (offer %d), offering %d %s at %s a week. The club answers on %s.\n",
		fee, p.Name, res.Offer, o.Years, years, o.WeeklyWage, s.w.Calendar().Format(res.Deadline))
	return nil
}

// otherPlayer finds a player of another club.
func (s *session) otherPlayer(id ids.PlayerID) (app.SquadPlayer, bool) {
	for _, c := range s.w.Summary().ClubRows {
		if c.ID == s.club() {
			continue
		}
		squad, _ := s.w.Squad(c.ID)
		for _, p := range squad {
			if p.Player == id {
				return p, true
			}
		}
	}
	return app.SquadPlayer{}, false
}

// answer accepts or rejects a bid for one of the club's players.
func (s *session) answer(args []string, accept bool) error {
	usage := "usage: reject OFFER"
	if accept {
		usage = "usage: accept OFFER"
	}
	if len(args) != 1 {
		return errors.New(usage)
	}
	n, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil || n == 0 {
		return fmt.Errorf("%q is not an offer ID (type transfers)", args[0])
	}
	res, err := s.w.RespondToOffer(app.RespondToOffer{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Offer: ids.OfferID(n), Accept: accept})
	if err != nil {
		return err
	}
	switch res.Status {
	case transfers.StatusCompleted:
		s.printf("Accepted: the transfer is complete.\n")
	case transfers.StatusRejected:
		s.printf("Rejected.\n")
	case transfers.StatusRefused:
		for _, offer := range s.w.Offers() {
			if offer.ID == res.Offer {
				s.printf("%s\n", refusedOfferText(offer))
				break
			}
		}
	default:
		s.printf("Accepted, but the buying club can no longer complete the transfer: the offer collapsed.\n")
	}
	s.newMessages()
	return nil
}

func refusedOfferText(offer app.OfferView) string {
	return fmt.Sprintf("Accepted, but %s refused to join %s.", offer.PlayerName, offer.BuyerName)
}

// outcomeName says how a closed offer ended.
func outcomeName(outcome uint8) string {
	switch transfers.Status(outcome) {
	case transfers.StatusRejected:
		return "was rejected"
	case transfers.StatusExpired:
		return "expired unanswered"
	case transfers.StatusRefused:
		return "was refused; the player refused to join"
	}
	return "fell through"
}

// printTransferMessage prints a transfer inbox message.
func (s *session) printTransferMessage(m app.InboxItem) {
	cal := s.w.Calendar()
	switch m.Kind {
	case inbox.KindBidReceived:
		s.printf("bid: %s bid %s for %s (offer %d); answer before %s (accept/reject)\n", m.ClubName, m.Fee, m.PlayerName, m.Offer, cal.Format(m.Deadline))
	case inbox.KindTransferIn:
		s.printf("transfer: %s joined from %s for %s, until %s at %s a week\n", m.PlayerName, m.ClubName, m.Fee, s.endDate(m.Expires), m.WeeklyWage)
	case inbox.KindTransferOut:
		s.printf("transfer: %s left for %s for %s\n", m.PlayerName, m.ClubName, m.Fee)
	case inbox.KindOfferClosed:
		if m.Selling {
			s.printf("transfer: the bid of %s from %s for %s %s\n", m.Fee, m.ClubName, m.PlayerName, outcomeName(m.Outcome))
		} else {
			s.printf("transfer: your bid of %s for %s of %s %s\n", m.Fee, m.PlayerName, m.ClubName, outcomeName(m.Outcome))
		}
	}
}
