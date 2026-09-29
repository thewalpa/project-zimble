// Package transfers owns transfer offers: one club's bid of a fixed fee for
// another club's player, with the personal terms offered to the player, and
// where each offer stands in the workflow (open, then completed, rejected,
// expired or collapsed). It also owns the transfer list: the players clubs
// have put up for sale, each at an asking price. Who employs the player and
// money belong to employment and finance; the application completes an
// accepted offer with them in one unit of work.
//
// Changes are two-step so an application workflow can commit several
// modules atomically: Plan validates closures and new bids and allocates
// offer IDs without touching the store; Apply commits a plan made from the
// store's current state and cannot fail otherwise.
package transfers

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// Status is where an offer stands. Values are durable; never reorder.
type Status uint8

const (
	StatusOpen      Status = 1 // awaiting the selling club's answer
	StatusCompleted Status = 2 // accepted; the player moved and the fee was paid
	StatusRejected  Status = 3 // the selling club declined
	StatusExpired   Status = 4 // no answer before the deadline
	// StatusCollapsed: accepted, but the transfer could no longer complete
	// (the player had moved, a squad limit, the buyer's funds).
	StatusCollapsed Status = 5
	// StatusRefused: accepted, but the player refused to join the buyer at
	// completion.
	StatusRefused Status = 6
)

func (s Status) Valid() bool { return s >= StatusOpen && s <= StatusRefused }

func (s Status) String() string {
	switch s {
	case StatusOpen:
		return "open"
	case StatusCompleted:
		return "completed"
	case StatusRejected:
		return "rejected"
	case StatusExpired:
		return "expired"
	case StatusCollapsed:
		return "collapsed"
	case StatusRefused:
		return "refused"
	}
	return fmt.Sprintf("Status(%d)", uint8(s))
}

// Terms are the personal terms the buying club offers the player: a new
// contract of Years contract years (the current one included) at WeeklyWage.
type Terms struct {
	Years      int
	WeeklyWage money.Money
}

// Offer is one bid and its outcome. An open offer is answered before
// Deadline; a closed one records when it closed.
type Offer struct {
	ID       ids.OfferID
	Player   ids.PlayerID
	Seller   ids.ClubID
	Buyer    ids.ClubID
	Fee      money.Money // positive
	Terms    Terms
	MadeAt   sim.GameInstant
	Deadline sim.GameInstant // after MadeAt
	Status   Status
	ClosedAt sim.GameInstant // zero while open
}

func (o Offer) validate() error {
	switch {
	case !o.ID.Valid() || !o.Player.Valid() || !o.Seller.Valid() || !o.Buyer.Valid() || o.Seller == o.Buyer:
		return fmt.Errorf("transfers: offer %d has invalid parties %+v", o.ID, o)
	case o.Fee <= 0 || o.Terms.Years < 1 || o.Terms.WeeklyWage <= 0:
		return fmt.Errorf("transfers: offer %d has invalid fee or terms %+v", o.ID, o)
	case !o.MadeAt.Valid() || !o.Deadline.Valid() || o.Deadline <= o.MadeAt:
		return fmt.Errorf("transfers: offer %d made at %d with deadline %d", o.ID, o.MadeAt, o.Deadline)
	case !o.Status.Valid():
		return fmt.Errorf("transfers: offer %d has status %d", o.ID, o.Status)
	case o.Status == StatusOpen && o.ClosedAt != 0, o.Status != StatusOpen && (o.ClosedAt < o.MadeAt || !o.ClosedAt.Valid()):
		return fmt.Errorf("transfers: offer %d is %s but closed at %d", o.ID, o.Status, o.ClosedAt)
	}
	return nil
}

// Bid is a new offer; Plan assigns its ID and time.
type Bid struct {
	Player   ids.PlayerID
	Seller   ids.ClubID
	Buyer    ids.ClubID
	Fee      money.Money
	Terms    Terms
	Deadline sim.GameInstant
}

// Closure closes an open offer with a final status.
type Closure struct {
	Offer  ids.OfferID
	Status Status
}

// Listing puts a club's player on the transfer list: the club offers him for
// sale at Asking. A player is listed at most once.
type Listing struct {
	Player   ids.PlayerID
	Club     ids.ClubID
	Asking   money.Money // positive
	ListedAt sim.GameInstant
}

func (l Listing) validate() error {
	if !l.Player.Valid() || !l.Club.Valid() || l.Asking <= 0 || !l.ListedAt.Valid() {
		return fmt.Errorf("transfers: invalid listing %+v", l)
	}
	return nil
}

// Changes is a set of changes made together at one instant: Close closes
// open offers (each at most once), then Bids adds new open offers; Unlist
// takes listed players off the transfer list (each at most once), then List
// lists players who are not listed (ListedAt is set by Plan). Unlisting and
// listing a player in one change replaces his listing.
type Changes struct {
	Close  []Closure
	Bids   []Bid
	Unlist []ids.PlayerID
	List   []Listing
}

// ErrStalePlan: the store changed after the plan was made.
var ErrStalePlan = errors.New("transfers: plan is stale")

// Snapshot is the store's authoritative state: every offer in ID order, the
// offer ID allocator, and the transfer list in player ID order.
type Snapshot struct {
	Offers    []Offer
	LastOffer ids.OfferID
	Listings  []Listing
}

// Store holds the offers and the transfer list. Queries return copies.
type Store struct {
	offers     []Offer // ascending ID
	lastOffer  ids.OfferID
	listings   []Listing // ascending player ID
	generation uint64
}

// New validates a snapshot: offer IDs ascending, non-zero and at most
// LastOffer; offers made in time order; each offer valid; listings valid, in
// ascending player order and at most one per player.
func New(snap Snapshot) (*Store, error) {
	for i, o := range snap.Offers {
		if err := o.validate(); err != nil {
			return nil, err
		}
		if o.ID > snap.LastOffer || (i > 0 && (o.ID <= snap.Offers[i-1].ID || o.MadeAt < snap.Offers[i-1].MadeAt)) {
			return nil, fmt.Errorf("transfers: offer %d out of order or above allocator %d", o.ID, snap.LastOffer)
		}
	}
	for i, l := range snap.Listings {
		if err := l.validate(); err != nil {
			return nil, err
		}
		if i > 0 && l.Player <= snap.Listings[i-1].Player {
			return nil, fmt.Errorf("transfers: listing of player %d out of order or listed twice", l.Player)
		}
	}
	return &Store{offers: slices.Clone(snap.Offers), lastOffer: snap.LastOffer, listings: slices.Clone(snap.Listings)}, nil
}

// Plan is a validated set of changes ready to apply.
type Plan struct {
	generation uint64
	offers     []Offer
	lastOffer  ids.OfferID
	listings   []Listing
	closed     []Offer
	made       []Offer
	unlisted   []Listing
	listed     []Listing
}

// Closed returns the offers the plan closes, with their final status.
func (p Plan) Closed() []Offer { return slices.Clone(p.closed) }

// Made returns the offers the plan adds, with their IDs.
func (p Plan) Made() []Offer { return slices.Clone(p.made) }

// Unlisted returns the listings the plan ends, in change order.
func (p Plan) Unlisted() []Listing { return slices.Clone(p.unlisted) }

// Listed returns the listings the plan adds, in change order.
func (p Plan) Listed() []Listing { return slices.Clone(p.listed) }

// Plan validates changes made at instant at and returns the resulting state
// without changing the store. Closures must name open offers made at or
// before at, with a final status; new bids are made at at, which must not be
// before the latest offer or listing. Unlisting must name listed players;
// listing, players not listed.
func (s *Store) Plan(at sim.GameInstant, c Changes) (Plan, error) {
	if n := len(s.offers); n > 0 && at < s.offers[n-1].MadeAt {
		return Plan{}, fmt.Errorf("transfers: changes at %d before the latest offer at %d", at, s.offers[n-1].MadeAt)
	}
	for _, l := range s.listings {
		if at < l.ListedAt {
			return Plan{}, fmt.Errorf("transfers: changes at %d before player %d was listed at %d", at, l.Player, l.ListedAt)
		}
	}
	plan := Plan{generation: s.generation, offers: slices.Clone(s.offers), lastOffer: s.lastOffer, listings: slices.Clone(s.listings)}
	for _, cl := range c.Close {
		i, ok := s.index(cl.Offer)
		switch {
		case !ok:
			return Plan{}, fmt.Errorf("transfers: cannot close unknown offer %d", cl.Offer)
		case plan.offers[i].Status != StatusOpen:
			return Plan{}, fmt.Errorf("transfers: offer %d is already %s", cl.Offer, plan.offers[i].Status)
		case !cl.Status.Valid() || cl.Status == StatusOpen:
			return Plan{}, fmt.Errorf("transfers: offer %d cannot close as %s", cl.Offer, cl.Status)
		case at < plan.offers[i].MadeAt:
			return Plan{}, fmt.Errorf("transfers: offer %d closed at %d before it was made", cl.Offer, at)
		}
		plan.offers[i].Status, plan.offers[i].ClosedAt = cl.Status, at
		plan.closed = append(plan.closed, plan.offers[i])
	}
	for _, b := range c.Bids {
		plan.lastOffer++
		o := Offer{ID: plan.lastOffer, Player: b.Player, Seller: b.Seller, Buyer: b.Buyer, Fee: b.Fee, Terms: b.Terms,
			MadeAt: at, Deadline: b.Deadline, Status: StatusOpen}
		if err := o.validate(); err != nil {
			return Plan{}, err
		}
		plan.offers = append(plan.offers, o)
		plan.made = append(plan.made, o)
	}
	for _, player := range c.Unlist {
		i, ok := listingIndex(plan.listings, player)
		if !ok {
			return Plan{}, fmt.Errorf("transfers: player %d is not listed", player)
		}
		plan.unlisted = append(plan.unlisted, plan.listings[i])
		plan.listings = slices.Delete(plan.listings, i, i+1)
	}
	for _, l := range c.List {
		l.ListedAt = at
		if err := l.validate(); err != nil {
			return Plan{}, err
		}
		i, listed := listingIndex(plan.listings, l.Player)
		if listed {
			return Plan{}, fmt.Errorf("transfers: player %d is already listed", l.Player)
		}
		plan.listings = slices.Insert(plan.listings, i, l)
		plan.listed = append(plan.listed, l)
	}
	return plan, nil
}

// Apply commits a plan made from the store's current state. It fails only
// with ErrStalePlan; then nothing changes.
func (s *Store) Apply(p Plan) error {
	if p.generation != s.generation {
		return fmt.Errorf("%w: made at generation %d, store at %d", ErrStalePlan, p.generation, s.generation)
	}
	s.offers, s.lastOffer, s.listings = p.offers, p.lastOffer, p.listings
	s.generation++
	return nil
}

func (s *Store) index(id ids.OfferID) (int, bool) {
	return slices.BinarySearchFunc(s.offers, id, func(o Offer, id ids.OfferID) int { return cmp.Compare(o.ID, id) })
}

func listingIndex(listings []Listing, player ids.PlayerID) (int, bool) {
	return slices.BinarySearchFunc(listings, player, func(l Listing, id ids.PlayerID) int { return cmp.Compare(l.Player, id) })
}

// Offer returns an offer by ID.
func (s *Store) Offer(id ids.OfferID) (Offer, bool) {
	i, ok := s.index(id)
	if !ok {
		return Offer{}, false
	}
	return s.offers[i], true
}

// Offers returns every offer in ID order.
func (s *Store) Offers() []Offer { return slices.Clone(s.offers) }

// Open returns the open offers in ID order.
func (s *Store) Open() []Offer {
	var out []Offer
	for _, o := range s.offers {
		if o.Status == StatusOpen {
			out = append(out, o)
		}
	}
	return out
}

// LastOffer returns the offer ID allocator: the highest ID ever issued.
func (s *Store) LastOffer() ids.OfferID { return s.lastOffer }

// Listing returns a player's listing, or false if he is not listed.
func (s *Store) Listing(player ids.PlayerID) (Listing, bool) {
	i, ok := listingIndex(s.listings, player)
	if !ok {
		return Listing{}, false
	}
	return s.listings[i], true
}

// Listings returns the transfer list in player ID order.
func (s *Store) Listings() []Listing { return slices.Clone(s.listings) }

// Snapshot exports the store's authoritative state as a fresh copy.
func (s *Store) Snapshot() Snapshot {
	return Snapshot{Offers: slices.Clone(s.offers), LastOffer: s.lastOffer, Listings: slices.Clone(s.listings)}
}
