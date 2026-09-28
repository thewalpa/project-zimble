package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

// --- transfers -----------------------------------------------------------------

type offerRow struct {
	app.OfferView
	When string // received bids: answer before; own bids: answered on; transfers: completed on
	Ours bool   // this window's transfers: the club bought or sold
}

type marketRow struct {
	app.SquadPlayer
	Club  string
	Ends  int
	Offer app.ContractOffer
}

type transfersView struct {
	Window       string
	Open         bool
	CanBid       bool // the window takes bids and no matchday is waiting
	Room         bool // the squad has room at the market's position
	Position     string
	Positions    []string
	Received     []offerRow
	Mine         []offerRow
	Done         []offerRow
	Market       []marketRow
	YearOptions  []int
	SortMarket   SortState
	SortReceived SortState
	SortMine     SortState
	SortDone     SortState
}

// windowText describes the transfer window under way or the next one.
func (s *server) windowText() string {
	cal := s.w.Calendar()
	win := s.w.TransferWindow()
	if !win.Open {
		return fmt.Sprintf("The transfer window opens on %s.", cal.Format(win.Opens))
	}
	return fmt.Sprintf("The transfer window is open until %s; clubs answer bids made before %s.", cal.Format(win.Closes), cal.Format(win.BidsClose))
}

// openBidsForUs counts the bids for the club's players awaiting an answer.
func (s *server) openBidsForUs() int {
	n := 0
	for _, o := range s.w.Offers() {
		if o.Status == transfers.StatusOpen && o.Seller == s.club() {
			n++
		}
	}
	return n
}

func (s *server) transfers(r *http.Request) (string, any, error) {
	cal := s.w.Calendar()
	win := s.w.TransferWindow()
	_, locked := s.w.Pending()
	defs := s.w.Content()
	pos := strings.ToUpper(r.URL.Query().Get("pos"))
	position, ok := positionByName(pos)
	if !ok {
		pos, position = "FW", players.Forward
	}
	counts := map[players.Position]int{}
	squad, _ := s.w.Squad(s.club())
	for _, p := range squad {
		counts[p.Position]++
	}
	v := transfersView{Window: s.windowText(), Open: win.Open, CanBid: win.Open && s.w.Now() < win.BidsClose && !locked,
		Room: counts[position] < defs.Quota(position).Count, Position: pos}
	for _, p := range players.Positions() {
		v.Positions = append(v.Positions, p.String())
	}
	for y := defs.Economy.ContractYears[0]; y <= defs.Economy.ContractYears[1]; y++ {
		v.YearOptions = append(v.YearOptions, y)
	}
	for _, o := range s.w.Offers() {
		switch {
		case o.Status == transfers.StatusOpen && o.Seller == s.club():
			v.Received = append(v.Received, offerRow{OfferView: o, When: cal.Format(o.Deadline)})
		case o.Status == transfers.StatusOpen && o.Buyer == s.club():
			v.Mine = append(v.Mine, offerRow{OfferView: o, When: cal.Format(o.Deadline)})
		case o.Status == transfers.StatusCompleted && win.Open && o.ClosedAt >= win.Opens:
			v.Done = append(v.Done, offerRow{OfferView: o, When: cal.Format(o.ClosedAt), Ours: o.Buyer == s.club() || o.Seller == s.club()})
		}
	}
	slices.Reverse(v.Done)
	for _, c := range s.w.Summary().ClubRows {
		if c.ID == s.club() {
			continue
		}
		players, _ := s.w.Squad(c.ID)
		for _, p := range players {
			if p.Position != position {
				continue
			}
			ends, _ := cal.Civil(p.Contract.Expires)
			o, _ := s.w.SuggestContract(p.Player)
			v.Market = append(v.Market, marketRow{SquadPlayer: p, Club: c.ShortName, Ends: ends.Year, Offer: o})
		}
	}
	v.SortMarket = newSortStatePrefixed(r, "m_", "ovr", "desc")
	if r.URL.Query().Has("sort") && !r.URL.Query().Has("m_sort") {
		v.SortMarket = newSortState(r, "ovr", "desc")
	}
	v.SortReceived = newSortStatePrefixed(r, "rec_", "id", "asc")
	v.SortMine = newSortStatePrefixed(r, "mine_", "id", "asc")
	v.SortDone = newSortStatePrefixed(r, "done_", "when", "desc")

	sortMarketRows(v.Market, v.SortMarket.Col, v.SortMarket.Dir)
	sortOfferRows(v.Received, v.SortReceived.Col, v.SortReceived.Dir)
	sortOfferRows(v.Mine, v.SortMine.Col, v.SortMine.Dir)
	sortOfferRows(v.Done, v.SortDone.Col, v.SortDone.Dir)
	return "transfers", v, nil
}

func positionByName(name string) (players.Position, bool) {
	for _, p := range players.Positions() {
		if p.String() == name {
			return p, true
		}
	}
	return 0, false
}

// bid makes a bid from the market: "player", "fee", "years" and "wage"
// (whole units).
func (s *server) bid(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	player, o, err := offer(form)
	if err != nil {
		return "", err
	}
	units, err := strconv.ParseInt(strings.ReplaceAll(form.Get("fee"), ",", ""), 10, 64)
	if err != nil || units <= 0 || units > 1_000_000_000_000 {
		return "", errors.New("the fee must be a positive whole amount")
	}
	res, err := s.w.MakeTransferOffer(app.MakeTransferOffer{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Player: player, Fee: money.Units(units), Offer: o})
	if err != nil {
		return "", err
	}
	s.say("You bid %s for %s. The club answers on %s: press Continue.", money.Units(units), s.name(player), s.w.Calendar().Format(res.Deadline))
	return "/transfers", nil
}

// answer accepts or rejects a bid for one of the club's players: "offer"
// and "accept" (yes or no).
func (s *server) answer(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	n, err := strconv.ParseUint(form.Get("offer"), 10, 64)
	if err != nil || n == 0 {
		return "", errors.New("unknown offer")
	}
	res, err := s.w.RespondToOffer(app.RespondToOffer{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Offer: ids.OfferID(n), Accept: form.Get("accept") == "yes"})
	if err != nil {
		return "", err
	}
	switch res.Status {
	case transfers.StatusCompleted:
		s.say("Accepted: the transfer is complete.")
	case transfers.StatusRejected:
		s.say("Bid rejected.")
	default:
		s.say("Accepted, but the buying club can no longer complete the transfer: the offer collapsed.")
	}
	return "/transfers", nil
}

// transferText renders a transfer inbox message.
func (s *server) transferText(m app.InboxItem) string {
	switch m.Kind {
	case inbox.KindBidReceived:
		return fmt.Sprintf("%s bid %s for %s; answer before %s on the Transfers page", m.ClubName, m.Fee, m.PlayerName, s.w.Calendar().Format(m.Deadline))
	case inbox.KindTransferIn:
		return fmt.Sprintf("%s joined from %s for %s, until %s at %s a week", m.PlayerName, m.ClubName, m.Fee, s.endDate(m.Expires), m.WeeklyWage)
	case inbox.KindTransferOut:
		return fmt.Sprintf("%s left for %s for %s", m.PlayerName, m.ClubName, m.Fee)
	case inbox.KindOfferClosed:
		how := "fell through"
		switch transfers.Status(m.Outcome) {
		case transfers.StatusRejected:
			how = "was rejected"
		case transfers.StatusExpired:
			how = "expired unanswered"
		}
		if m.Selling {
			return fmt.Sprintf("The bid of %s from %s for %s %s", m.Fee, m.ClubName, m.PlayerName, how)
		}
		return fmt.Sprintf("Your bid of %s for %s of %s %s", m.Fee, m.PlayerName, m.ClubName, how)
	}
	return fmt.Sprintf("Message kind %d", m.Kind)
}

// transferNews reports whether the inbox has a transfer message after
// event.
func (s *server) transferNews(after uint64) bool {
	for _, m := range s.w.Inbox() {
		if uint64(m.Event) > after && m.Kind >= inbox.KindBidReceived {
			return true
		}
	}
	return false
}

// lastInboxEvent is the newest inbox message's event, or 0.
func (s *server) lastInboxEvent() uint64 {
	items := s.w.Inbox()
	if len(items) == 0 {
		return 0
	}
	return uint64(items[len(items)-1].Event)
}
