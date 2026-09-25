// Package storage encodes world snapshots and writes them to disk safely.
// It decides no football outcomes and knows no module internals: it
// serializes app.WorldSnapshot and hands decoded snapshots back to
// app.Restore, which validates them.
//
// File format: one JSON object
//
//	{"Format": Format, "Schema": SchemaVersion, "PayloadSHA256": hex, "Payload": {...WorldSnapshot...}}
//
// The payload's field names are part of the schema: renaming a field in any
// snapshot type requires a new SchemaVersion (and, later, a migration).
package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/thewalpa/project-zimble/internal/app"
)

const (
	Format        = "project-zimble/save"
	SchemaVersion = 2
)

var (
	// ErrMalformedSave: not a complete, intact save file (truncated,
	// corrupted, trailing data, unknown fields, checksum mismatch).
	ErrMalformedSave = errors.New("storage: malformed save")
	// ErrUnsupportedSave: a save of another format or schema version.
	ErrUnsupportedSave = errors.New("storage: unsupported save")
)

type envelope struct {
	Format        string
	Schema        int
	PayloadSHA256 string
	Payload       json.RawMessage
}

// Encode serializes a snapshot with its envelope and checksum.
func Encode(snap app.WorldSnapshot) ([]byte, error) {
	payload, err := json.Marshal(snap)
	if err != nil {
		return nil, fmt.Errorf("storage: encode: %w", err)
	}
	sum := sha256.Sum256(payload)
	data, err := json.MarshalIndent(envelope{
		Format: Format, Schema: SchemaVersion, PayloadSHA256: hex.EncodeToString(sum[:]), Payload: payload,
	}, "", " ")
	if err != nil {
		return nil, fmt.Errorf("storage: encode: %w", err)
	}
	return append(data, '\n'), nil
}

// Decode parses and checks a save file. It verifies the format, schema and
// checksum and rejects unknown fields and trailing data. It does not
// validate world state; app.Restore does.
func Decode(data []byte) (app.WorldSnapshot, error) {
	var env envelope
	if err := strictUnmarshal(data, &env); err != nil {
		return app.WorldSnapshot{}, fmt.Errorf("%w: %v", ErrMalformedSave, err)
	}
	if env.Format != Format {
		return app.WorldSnapshot{}, fmt.Errorf("%w: format %q, want %q", ErrUnsupportedSave, env.Format, Format)
	}
	if env.Schema != SchemaVersion {
		return app.WorldSnapshot{}, fmt.Errorf("%w: schema version %d, this build reads %d", ErrUnsupportedSave, env.Schema, SchemaVersion)
	}
	// MarshalIndent re-indents the payload; compact it to the bytes that
	// were hashed.
	var compact bytes.Buffer
	if err := json.Compact(&compact, env.Payload); err != nil {
		return app.WorldSnapshot{}, fmt.Errorf("%w: %v", ErrMalformedSave, err)
	}
	sum := sha256.Sum256(compact.Bytes())
	if hex.EncodeToString(sum[:]) != env.PayloadSHA256 {
		return app.WorldSnapshot{}, fmt.Errorf("%w: payload checksum mismatch", ErrMalformedSave)
	}
	var snap app.WorldSnapshot
	if err := strictUnmarshal(compact.Bytes(), &snap); err != nil {
		return app.WorldSnapshot{}, fmt.Errorf("%w: payload: %v", ErrMalformedSave, err)
	}
	return snap, nil
}

func strictUnmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing data after save")
	}
	return nil
}

// Save writes the world's snapshot to path, replacing any existing file
// atomically: a failed save leaves the previous file intact.
func Save(path string, w *app.World) error {
	return saveWith(path, w, osFS)
}

func saveWith(path string, w *app.World, fs fileOps) error {
	data, err := Encode(w.Snapshot())
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, fs)
}

// Load reads, decodes and restores a world. It returns a world only after
// app.Restore has validated it completely; on any error it returns nil.
func Load(path string) (*app.World, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	snap, err := Decode(data)
	if err != nil {
		return nil, err
	}
	return app.Restore(snap)
}

// fileOps are the filesystem calls used for saving, replaceable in tests.
type fileOps struct {
	createTemp func(dir, pattern string) (*os.File, error)
	write      func(f *os.File, data []byte) error
	sync       func(f *os.File) error
	rename     func(oldpath, newpath string) error
}

var osFS = fileOps{
	createTemp: os.CreateTemp,
	write:      func(f *os.File, data []byte) error { _, err := f.Write(data); return err },
	sync:       func(f *os.File) error { return f.Sync() },
	rename:     os.Rename,
}

// writeFileAtomic writes data to a temporary file in path's directory,
// flushes it to stable storage, then renames it over path. Rename within a
// directory is atomic on POSIX filesystems, so readers see either the old
// file or the complete new one. On error the temporary file is removed and
// path is untouched.
func writeFileAtomic(path string, data []byte, fs fileOps) (err error) {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	f, err := fs.createTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return fmt.Errorf("storage: save %s: %w", path, err)
	}
	tmp := f.Name()
	closed := false
	defer func() {
		if err != nil {
			if !closed {
				f.Close()
			}
			os.Remove(tmp)
		}
	}()
	if err = fs.write(f, data); err != nil {
		return fmt.Errorf("storage: save %s: write: %w", path, err)
	}
	if err = f.Chmod(0o644); err != nil {
		return fmt.Errorf("storage: save %s: %w", path, err)
	}
	if err = fs.sync(f); err != nil {
		return fmt.Errorf("storage: save %s: sync: %w", path, err)
	}
	closed = true
	if err = f.Close(); err != nil {
		return fmt.Errorf("storage: save %s: close: %w", path, err)
	}
	if err = fs.rename(tmp, path); err != nil {
		return fmt.Errorf("storage: save %s: replace: %w", path, err)
	}
	// Persist the directory entry. Best effort: some platforms cannot sync
	// directories, and the new file is already complete and in place.
	if d, derr := os.Open(dir); derr == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
