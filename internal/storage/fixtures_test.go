package storage

import (
	"bytes"
	"compress/gzip"
	"encoding"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

var writeFixture = flag.Bool("fixture", false, "write the save fixture for the current schema version (it must not exist yet)")

// Save fixtures: per schema version, a gzipped save file and the payload's
// JSON shape, written once by the build that introduced that version and
// never changed afterwards.
var fixtureName = regexp.MustCompile(`^schema-(\d+)\.(json\.gz|shape)$`)

func fixturePath(schema int) string {
	return filepath.Join("testdata", fmt.Sprintf("schema-%d.json.gz", schema))
}

func shapePath(schema int) string {
	return filepath.Join("testdata", fmt.Sprintf("schema-%d.shape", schema))
}

// Every save fixture either loads or is refused explicitly, never loaded
// silently wrong:
//
//   - a fixture of an older schema is refused with ErrUnsupportedSave (no
//     migrations exist yet; a migration would make it load instead);
//   - the current schema's shape lists every field path of the payload
//     with its JSON kind, and must match the snapshot types, so no field is
//     added, removed, renamed or retyped without a new SchemaVersion, even
//     where the fixture holds no value for it;
//   - the fixture of the current schema decodes and re-encodes to the same
//     bytes, and app.Restore accepts it or refuses it with
//     ErrIncompatibleSave (a simulation version moved since it was
//     written). Any other error means a rule change made existing saves
//     invalid: bump SchemaVersion or migrate.
//
// A new SchemaVersion needs its fixture:
//
//	go test ./internal/storage -run TestSaveFixtures -fixture
func TestSaveFixtures(t *testing.T) {
	if *writeFixture {
		writeCurrentFixture(t)
	}
	entries, err := os.ReadDir("testdata")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	var schemas []int
	for _, e := range entries {
		m := fixtureName.FindStringSubmatch(e.Name())
		if m == nil {
			t.Errorf("testdata/%s is not a save fixture (schema-N.json.gz or schema-N.shape)", e.Name())
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		if m[2] == "json.gz" {
			schemas = append(schemas, n)
		}
	}
	if !slices.Contains(schemas, SchemaVersion) {
		t.Fatalf("no save fixture for schema %d: write it with go test ./internal/storage -run TestSaveFixtures -fixture", SchemaVersion)
	}
	shape, err := os.ReadFile(shapePath(SchemaVersion))
	if err != nil {
		t.Fatal(err)
	}
	if want := payloadShape(); string(shape) != want {
		t.Errorf("the snapshot types no longer match %s (bump SchemaVersion and write its fixture):\n%s", shapePath(SchemaVersion), shapeDiff(string(shape), want))
	}
	for _, schema := range schemas {
		t.Run(fmt.Sprintf("schema-%d", schema), func(t *testing.T) {
			data := readFixture(t, fixturePath(schema))
			var env envelope
			if err := strictUnmarshal(data, &env); err != nil || env.Format != Format || env.Schema != schema {
				t.Fatalf("not a %s save of schema %d: format %q, schema %d, %v", Format, schema, env.Format, env.Schema, err)
			}
			path := filepath.Join(t.TempDir(), "career.json")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			loaded, loadErr := Load(path)
			switch {
			case schema > SchemaVersion:
				t.Fatalf("fixture from schema %d, newer than this build's %d", schema, SchemaVersion)
			case schema < SchemaVersion:
				if !errors.Is(loadErr, ErrUnsupportedSave) || loaded != nil {
					t.Fatalf("schema %d save: world %v, err = %v, want %v", schema, loaded != nil, loadErr, ErrUnsupportedSave)
				}
				return
			}
			snap, err := Decode(data)
			if err != nil {
				t.Fatalf("the current schema's fixture no longer decodes (a snapshot field was removed or renamed? bump SchemaVersion): %v", err)
			}
			again, err := Encode(snap)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, again) {
				var got envelope
				if err := strictUnmarshal(again, &got); err != nil {
					t.Fatal(err)
				}
				at, _ := firstDifference(env.Payload, got.Payload)
				t.Fatalf("the current schema's fixture re-encodes differently at payload byte %d (a snapshot field was added or changed? bump SchemaVersion):\n fixture: %s\n encoded: %s",
					at, excerpt(env.Payload, at), excerpt(got.Payload, at))
			}
			switch {
			case loadErr == nil:
				t.Logf("schema %d fixture loads (now %d, revision %d)", schema, loaded.Now(), loaded.Revision())
			case errors.Is(loadErr, app.ErrIncompatibleSave):
				t.Logf("schema %d fixture is refused: %v", schema, loadErr)
			default:
				t.Fatalf("a save of the current schema no longer loads (bump SchemaVersion or migrate): %v", loadErr)
			}
		})
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return data
}

// writeCurrentFixture writes the current schema's fixture. Fixtures are
// frozen: an existing one is never replaced, since regenerating it would
// hide exactly the changes it exists to catch.
func writeCurrentFixture(t *testing.T) {
	path := fixturePath(SchemaVersion)
	for _, p := range []string{path, shapePath(SchemaVersion)} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s exists; fixtures are never rewritten", p)
		}
	}
	data := encoded(t, fixtureCareer(t))
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shapePath(SchemaVersion), []byte(payloadShape()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%d bytes, %d uncompressed)", path, buf.Len(), len(data))
}

// payloadShape lists every JSON path of the save payload with its kind, one
// per line: what encoding/json reads and writes for app.WorldSnapshot. A
// field's tag options and pointers follow its kind. Type names are left
// out, so renaming a Go type changes nothing.
func payloadShape() string {
	var lines []string
	describe(reflect.TypeFor[app.WorldSnapshot](), "", "", &lines, map[reflect.Type]bool{})
	return strings.Join(lines, "\n") + "\n"
}

var (
	jsonMarshaler = reflect.TypeFor[json.Marshaler]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
)

// describe appends the lines for a value of type t at path; note is what
// the enclosing field adds (tag options, pointers).
func describe(t reflect.Type, path, note string, lines *[]string, active map[reflect.Type]bool) {
	line := func(kind string) { *lines = append(*lines, path+" "+kind+note) }
	if t.Implements(jsonMarshaler) || t.Implements(textMarshaler) ||
		reflect.PointerTo(t).Implements(jsonMarshaler) || reflect.PointerTo(t).Implements(textMarshaler) {
		line("custom " + t.String())
		return
	}
	switch t.Kind() {
	case reflect.Pointer:
		describe(t.Elem(), path, note+" pointer", lines, active)
	case reflect.Slice:
		describe(t.Elem(), path+"[]", note, lines, active)
	case reflect.Array:
		describe(t.Elem(), fmt.Sprintf("%s[%d]", path, t.Len()), note, lines, active)
	case reflect.Map:
		describe(t.Elem(), fmt.Sprintf("%s{%s}", path, t.Key().Kind()), note, lines, active)
	case reflect.Struct:
		if active[t] {
			line("recursive " + t.String())
			return
		}
		active[t] = true
		defer delete(active, t)
		if path != "" {
			line("object")
		}
		for i := range t.NumField() {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			if tag == "-" || (!f.IsExported() && !f.Anonymous) {
				continue
			}
			name, opts, _ := strings.Cut(tag, ",")
			if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
				describe(f.Type, path, "", lines, active) // promoted fields
				continue
			}
			if name == "" {
				name = f.Name
			}
			var fieldNote string
			if opts != "" {
				fieldNote = " " + strings.ReplaceAll(opts, ",", " ")
			}
			describe(f.Type, strings.TrimPrefix(path+"."+name, "."), fieldNote, lines, active)
		}
	default:
		line(t.Kind().String())
	}
}

// shapeDiff lists the lines only one of the shapes has.
func shapeDiff(saved, now string) string {
	a, b := strings.Split(saved, "\n"), strings.Split(now, "\n")
	var out []string
	for _, l := range a {
		if !slices.Contains(b, l) {
			out = append(out, "- "+l)
		}
	}
	for _, l := range b {
		if !slices.Contains(a, l) {
			out = append(out, "+ "+l)
		}
	}
	return strings.Join(out, "\n")
}

func firstDifference(a, b []byte) (int, bool) {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i, true
		}
	}
	return min(len(a), len(b)), len(a) != len(b)
}

func excerpt(data []byte, at int) string {
	return strconv.Quote(string(data[max(0, at-60):min(len(data), at+60)]))
}

const fixtureClub ids.ClubID = 3

// fixtureCareer is a managed career (seed 42, club 3) that fills as much of
// the snapshot as the public API can: the manager releases, signs, renews,
// bids for, lists and answers bids for players in the first transfer window,
// reads the inbox and submits every lineup through the first season, its
// review and end, the contract and player years and into the second window. It stops
// in the middle of the manager's live match, with a decision made.
func fixtureCareer(t *testing.T) *app.World {
	t.Helper()
	cfg := app.DefaultConfig(random.Seed(42))
	cfg.UserClub = fixtureClub
	w, err := app.NewWorld(cfg)
	if err != nil {
		t.Fatal(err)
	}
	squad := func() []app.SquadPlayer {
		s, _ := w.Squad(fixtureClub)
		return s
	}
	weakest := func(pos players.Position) ids.PlayerID {
		var out app.SquadPlayer
		for _, p := range squad() {
			if p.Position == pos && (out.Player == 0 || p.Overall < out.Overall) {
				out = p
			}
		}
		return out.Player
	}

	if _, err := w.ReleasePlayer(app.ReleasePlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: weakest(players.Forward)}); err != nil {
		t.Fatal(err)
	}
	firstThat(t, "signing", w.FreeAgents(), func(p app.SquadPlayer) error {
		offer, err := w.SuggestContract(p.Player)
		if err != nil {
			return err
		}
		_, err = w.SignPlayer(app.SignPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: p.Player, Offer: offer})
		return err
	})
	firstThat(t, "renewal", squad(), func(p app.SquadPlayer) error {
		offer, err := w.SuggestContract(p.Player)
		if err != nil {
			return err
		}
		_, err = w.RenewContract(app.RenewContract{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: p.Player, Offer: offer})
		return err
	})
	var others []app.SquadPlayer
	for _, c := range w.Summary().ClubRows {
		if s, _ := w.Squad(c.ID); c.ID != fixtureClub {
			others = append(others, s...)
		}
	}
	firstThat(t, "bid", others, func(p app.SquadPlayer) error {
		offer, err := w.SuggestContract(p.Player)
		if err != nil {
			return err
		}
		_, err = w.MakeTransferOffer(app.MakeTransferOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: p.Player, Fee: p.Value, Offer: offer})
		return err
	})
	for _, pos := range []players.Position{players.Defender, players.Midfielder} {
		p := weakest(pos)
		for _, s := range squad() {
			if s.Player == p {
				if _, err := w.ListPlayer(app.ListPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: p, Asking: s.Value}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if _, err := w.MarkInboxRead(app.MarkInboxRead{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Message: w.Inbox()[0].Event}); err != nil {
		t.Fatal(err)
	}
	plan, err := w.TeamPlan()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.SetTeamPlan(app.SetTeamPlan{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Lineup: plan.Lineup}); err != nil {
		t.Fatal(err)
	}

	// Day by day to the second window, answering every bid for the club's
	// players and playing every batch with submitted lineups.
	const day = sim.GameInstant(24 * 60)
	secondWindow := w.ContractYearEnd() + 2*day
	answered := 0
	for w.Now() < secondWindow {
		until := w.Now() + day
		for {
			res, err := w.ContinueWith(until, app.ContinueOptions{StopAtSeasonReview: true})
			if err != nil {
				t.Fatal(err)
			}
			if review, ok := res.(app.SeasonReviewReady); ok {
				if _, err := w.AcknowledgeSeasonReview(app.AcknowledgeSeasonReview{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Season: review.Season}); err != nil {
					t.Fatal(err)
				}
				continue
			}
			ready, ok := res.(app.FixtureRoundReady)
			if !ok {
				break
			}
			submitLineups(t, w, ready)
			resolve(t, w, ready)
		}
		for _, o := range w.Offers() {
			if o.Seller == fixtureClub && o.Status == transfers.StatusOpen {
				if _, err := w.RespondToOffer(app.RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: o.ID}); err != nil {
					t.Fatal(err)
				}
				answered++
			}
		}
	}
	if answered == 0 {
		t.Fatal("no club bid for a listed player")
	}

	// On to the manager's next match, played live to minute 30 with a
	// change of mentality, and left in progress.
	for {
		res, err := w.Continue(w.Now() + 60*day)
		if err != nil {
			t.Fatal(err)
		}
		ready, ok := res.(app.FixtureRoundReady)
		if !ok {
			t.Fatal("no match for the managed club within 60 days")
		}
		submitLineups(t, w, ready)
		if len(ready.UserFixtures) == 0 {
			resolve(t, w, ready)
			continue
		}
		f := ready.UserFixtures[0]
		stepped, err := w.PlayMatch(app.PlayMatch{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: f, ToMinute: 30})
		if err != nil {
			t.Fatal(err)
		}
		cmd := matches.MatchCommand{Kind: matches.CommandSetMentality, Side: stepped.Live.Side, Mentality: matches.Attacking}
		if _, err := w.MatchDecision(app.MatchDecision{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: f, Command: cmd}); err != nil {
			t.Fatal(err)
		}
		return w
	}
}

// firstThat applies try to candidates in order until one succeeds.
func firstThat[T any](t *testing.T, what string, candidates []T, try func(T) error) {
	t.Helper()
	var last error
	for _, c := range candidates {
		if last = try(c); last == nil {
			return
		}
	}
	t.Fatalf("no %s succeeded among %d candidates: %v", what, len(candidates), last)
}

func submitLineups(t *testing.T, w *app.World, ready app.FixtureRoundReady) {
	t.Helper()
	for _, f := range ready.UserFixtures {
		l, err := w.SuggestLineup(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.SubmitLineup(app.SubmitLineup{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: f, Lineup: l}); err != nil {
			t.Fatal(err)
		}
	}
}

func resolve(t *testing.T, w *app.World, ready app.FixtureRoundReady) {
	t.Helper()
	cmd := app.ResolveRounds{ID: w.NextCommandID(), ExpectedRevision: w.Revision()}
	for _, r := range ready.Rounds {
		cmd.Rounds = append(cmd.Rounds, r.Round)
	}
	if _, err := w.ResolveRounds(cmd); err != nil {
		t.Fatal(err)
	}
}
