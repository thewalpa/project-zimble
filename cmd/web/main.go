// Command web serves the career game as a small local web app: one managed
// club, played in the browser. Pages are rendered on the server from the
// same queries as any other client, and every change is one of the same
// commands (see server.go); the browser needs no JavaScript.
//
//	go run ./cmd/web                  # new career: random seed, choose a club in the browser
//	go run ./cmd/web -seed 42 -club 3
//	go run ./cmd/web -engine tick     # new career on the tick match engine
//	go run ./cmd/web -load career.json
//
// Then open http://127.0.0.1:8080. The server holds one career in memory;
// use the Save button to write it to disk.
package main

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, cryptoSeed, listen); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "web:", err)
		}
		os.Exit(2)
	}
}

func cryptoSeed() (uint64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("draw random seed: %w", err)
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

func listen(addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

// run parses the flags, builds the server and hands it to serve.
func run(args []string, out, errOut io.Writer, newSeed func() (uint64, error), serve func(string, http.Handler) error) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(errOut)
	addr := fs.String("addr", "127.0.0.1:8080", "`address` to listen on")
	seed := fs.Uint64("seed", 0, "world seed for a new career (default: random)")
	club := fs.Uint64("club", 0, "club `ID` to manage in a new career (default: choose in the browser)")
	load := fs.String("load", "", "continue the career saved in `FILE`")
	save := fs.String("save", "career.json", "`FILE` the Save button writes (default: the -load file)")
	saves := fs.String("saves", "saves", "`DIR` where multiple saves are stored")
	engine := fs.String("engine", app.Engines()[0], "match `ENGINE` for a new career: "+strings.Join(app.Engines(), ", "))
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["load"] && (set["seed"] || set["club"] || set["engine"]) {
		return errors.New("-seed, -club and -engine cannot be used with -load: a saved career keeps its own")
	}
	if !slices.Contains(app.Engines(), *engine) {
		return fmt.Errorf("-engine must be one of %s", strings.Join(app.Engines(), ", "))
	}
	cfg := config{savePath: *save, loadPath: *load, savesDir: *saves, engine: *engine}
	if set["load"] && !set["save"] {
		cfg.savePath = *load
	}
	if !set["load"] {
		if !set["seed"] {
			drawn, err := newSeed()
			if err != nil {
				return err
			}
			*seed = drawn
		}
		cfg.seed, cfg.club = random.Seed(*seed), ids.ClubID(*club)
	}
	s, err := newServer(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Serving the career at http://%s (seed %d, %s match engine). Stop with Ctrl+C; save first in the browser.\n", *addr, s.seed, s.engineID())
	return serve(*addr, s)
}
