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
	Club    string
	Nation  string // the nation the club plays in
	Ends    int
	Offer   app.ContractOffer
	Refusal string // why the club would refuse a bid at its price now; empty when it sells
}

type listedRow struct {
	app.ListedPlayer
	Ours    bool
	Offer   app.ContractOffer
	Refusal string
}

type transfersView struct {
	Window       string
	NeededNote   string // from the days before the close: what AI clubs still sell
	Open         bool
	CanBid       bool // the window takes bids and no matchday is waiting
	Room         bool // the squad has room for more players
	SquadLimit   int
	Position     string
	Positions    []string
	Received     []offerRow
	Mine         []offerRow
	Done         []offerRow
	Listed       []listedRow
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

// neededNote is the notice from the day AI clubs stop selling the players they need.
func (s *server) neededNote() string {
	win := s.w.TransferWindow()
	if !win.Open || s.w.Now() < win.NeededClose || s.w.Now() >= win.Closes {
		return ""
	}
	return "The window is about to close: AI clubs now sell only listed players and players they can spare."
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
	squad, _ := s.w.ObservedSquad(s.club(), s.club())
	v := transfersView{
		Window:     s.windowText(),
		NeededNote: s.neededNote(),
		Open:       win.Open,
		CanBid:     win.Open && s.w.Now() < win.BidsClose && !locked,
		Room:       len(squad) < defs.SquadLimit,
		SquadLimit: defs.SquadLimit,
		Position:   pos,
	}
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
	for _, l := range s.transferList() {
		row := listedRow{ListedPlayer: l, Ours: l.Club == s.club()}
		if !row.Ours {
			row.Offer, _ = s.w.SuggestContract(l.Player)
			row.Refusal = refusalText(s.w.BidRefusal(l.Player))
		}
		v.Listed = append(v.Listed, row)
	}
	for _, c := range s.w.Summary().ClubRows {
		if c.ID == s.club() {
			continue
		}
		players, _ := s.w.ObservedSquad(s.club(), c.ID)
		for _, p := range players {
			if p.Position != position {
				continue
			}
			ends, _ := cal.Civil(p.Contract.Expires)
			o, _ := s.w.SuggestContract(p.Player)
			v.Market = append(v.Market, marketRow{SquadPlayer: p, Club: c.ShortName, Nation: c.Nation, Ends: ends.Year, Offer: o, Refusal: refusalText(s.w.BidRefusal(p.Player))})
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

// listPlayer puts one of the manager's players on the transfer list, changes
// his asking price, or takes him off it. Prices are entered in whole units.
func (s *server) listPlayer(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	playerID, err := strconv.ParseUint(form.Get("player"), 10, 64)
	if err != nil || playerID == 0 {
		return "", errors.New("unknown player")
	}
	player := ids.PlayerID(playerID)
	asking := money.Money(0)
	if form.Get("unlist") != "yes" {
		units, err := strconv.ParseInt(strings.ReplaceAll(form.Get("asking"), ",", ""), 10, 64)
		if err != nil || units <= 0 || units > 1_000_000_000_000 {
			return "", errors.New("the asking price must be a positive whole amount")
		}
		asking = money.Units(units)
	}
	name := s.name(player)
	res, err := s.w.ListPlayer(app.ListPlayer{
		ID:               s.w.NextCommandID(),
		ExpectedRevision: s.w.Revision(),
		Player:           player,
		Asking:           asking,
	})
	if err != nil {
		return "", err
	}
	if res.Asking == 0 {
		s.say("%s is off the transfer list.", name)
	} else {
		s.say("%s is on the transfer list at %s until the window closes; clubs that need him may bid.", name, res.Asking)
	}
	if form.Get("back") == "/transfers" {
		return "/transfers", nil
	}
	return "/squad", nil
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
	case transfers.StatusRefused:
		for _, offer := range s.w.Offers() {
			if offer.ID == res.Offer {
				s.say("%s", refusedOfferText(offer))
				break
			}
		}
	default:
		s.say("Accepted, but the buying club can no longer complete the transfer: the offer collapsed.")
	}
	return "/transfers", nil
}

func refusedOfferText(offer app.OfferView) string {
	return fmt.Sprintf("Accepted, but %s refused to join %s.", offer.PlayerName, offer.BuyerName)
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
		case transfers.StatusRefused:
			how = "was refused; the player refused to join"
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
		if uint64(m.Event) > after && m.Kind >= inbox.KindBidReceived && m.Kind <= inbox.KindOfferClosed {
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
