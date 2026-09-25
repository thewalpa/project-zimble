package app

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/thewalpa/project-zimble/"

// allowedImports lists the internal packages each package may import.
// Domain modules depend only on core; only app composes modules.
var allowedImports = map[string][]string{
	"internal/core/ids":    {},
	"internal/core/random": {},
	"internal/core/sim":    {},
	"internal/players":     {"internal/core/ids"},
	"internal/registry":    {"internal/core/ids"},
	"internal/core/money":  {},
	"internal/employment":  {"internal/core/ids", "internal/core/money", "internal/core/sim"},
	// Finance owns club ledgers; balances are derived from its entries.
	"internal/finance": {"internal/core/ids", "internal/core/money", "internal/core/sim"},
	"internal/medical": {"internal/core/ids"},
	// Events are a contract: typed facts other packages consume.
	"internal/events": {"internal/core/ids", "internal/core/money", "internal/core/sim"},
	// The inbox is a read model built only from events.
	"internal/inbox":        {"internal/core/ids", "internal/core/sim", "internal/events"},
	"internal/competitions": {"internal/core/ids", "internal/core/random", "internal/core/sim"},
	// Match engines depend only on the contract and core; never on app,
	// the scheduler or module stores.
	"internal/matches":        {"internal/core/ids", "internal/core/random"},
	"internal/matches/simple": {"internal/core/ids", "internal/core/random", "internal/matches"},
	// AI decides from detached match-contract data; it reads no module state.
	"internal/ai": {"internal/core/ids", "internal/matches"},
	// Selection stores lineups in the match contract's vocabulary (roles,
	// tactics) so they reach an engine untranslated; it reads no module.
	"internal/selection": {"internal/core/ids", "internal/matches"},
	"internal/content":   {"internal/core/ids", "internal/core/money", "internal/core/sim", "internal/players"},
	"internal/worldgen": {
		"internal/content", "internal/core/ids", "internal/core/money", "internal/core/random",
		"internal/employment", "internal/players", "internal/registry",
	},
	"internal/app": {
		"internal/competitions", "internal/content", "internal/core/ids", "internal/core/random",
		"internal/core/sim", "internal/employment", "internal/players", "internal/registry", "internal/worldgen",
		"internal/ai", "internal/matches", "internal/matches/simple", "internal/selection",
		"internal/medical", "internal/events", "internal/inbox", "internal/core/money", "internal/finance",
	},
	// Storage is an adapter: it encodes app snapshots and never reaches
	// into modules.
	"internal/storage": {"internal/app"},
	// The interactive client uses app queries and commands, plus the
	// contract types they return.
	"cmd/play": {
		"internal/app", "internal/competitions", "internal/core/ids", "internal/core/money", "internal/core/random", "internal/core/sim",
		"internal/events", "internal/inbox", "internal/matches", "internal/players", "internal/selection", "internal/storage",
	},
	"cmd/simulate": {
		"internal/app", "internal/competitions", "internal/core/ids", "internal/core/random", "internal/core/sim",
		"internal/inbox", "internal/matches", "internal/players", "internal/storage",
	},
}

func TestPackageImportBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	for pkg, allowed := range allowedImports {
		files, err := filepath.Glob(filepath.Join(root, pkg, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: no Go files found (%v)", pkg, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				rel, ok := strings.CutPrefix(path, modulePath)
				if ok && !slices.Contains(allowed, rel) {
					t.Errorf("%s imports %s, which is not allowed", file, rel)
				}
			}
		}
	}
}

// Every package must appear in allowedImports so new packages get a
// deliberate dependency decision.
func TestEveryPackageHasImportRules(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			if files, _ := filepath.Glob(filepath.Join(path, "*.go")); len(files) == 0 {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if _, ok := allowedImports[filepath.ToSlash(rel)]; !ok {
				t.Errorf("package %s has no entry in allowedImports", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Game time must never depend on the host clock or time zone.
func TestNoWallClockOrLocalTime(t *testing.T) {
	root := filepath.Join("..", "..")
	forbidden := []string{"time.Now(", "time.Since(", "time.Until(", "time.Local", ".Local()"}
	for pkg := range allowedImports {
		files, _ := filepath.Glob(filepath.Join(root, pkg, "*.go"))
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range forbidden {
				if strings.Contains(string(src), f) {
					t.Errorf("%s uses %s", file, f)
				}
			}
		}
	}
}
