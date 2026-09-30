package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/thewalpa/project-zimble/internal/storage"
)

// recoverOffer is the explicit choice to replace a save with the previous one
// storage kept. Nothing is recovered unless the player submits its form.
type recoverOffer struct {
	Name     string // the selected file
	Path     string
	Previous string // the file that would replace it
	Rev      uint64 // of the open career, if any
	Unsaved  bool   // the open career has changes recovery would discard
}

// previousAvailable reports whether storage holds a previous save for path.
func previousAvailable(path string) bool {
	fi, err := os.Stat(storage.PreviousPath(path))
	return err == nil && fi.Mode().IsRegular()
}

// offerFor returns the recovery choice for a save that cannot be loaded, or
// nil when it loads or has no previous save.
func (s *server) offerFor(path string) *recoverOffer {
	if !previousAvailable(path) {
		return nil
	}
	if _, err := storage.Load(path); err == nil {
		return nil
	}
	o := &recoverOffer{Name: filepath.Base(path), Path: path, Previous: filepath.Base(storage.PreviousPath(path))}
	if s.w != nil {
		o.Rev = uint64(s.w.Revision())
		o.Unsaved = !s.saved || s.w.Revision() != s.savedRevision
	}
	return o
}

// offerRecovery shows the recovery choice for path on the next page.
func (s *server) offerRecovery(path string) {
	if o := s.offerFor(path); o != nil {
		s.offers = append(s.offers, *o)
	}
}

// resolveSave finds the file a form names: a relative path, or a name in the
// saves directory, as the first that satisfies exists. The file the server
// works with and the saves directory's own files are always allowed.
func (s *server) resolveSave(file string, exists func(string) bool) (string, error) {
	file = strings.TrimSpace(file)
	if file == "" {
		return "", errors.New("no save file specified")
	}
	clean := filepath.Clean(file)
	if (clean == filepath.Clean(s.savePath) || filepath.Dir(clean) == filepath.Clean(s.savesDir)) && exists(clean) {
		return clean, nil
	}
	if strings.Contains(clean, "..") || filepath.IsAbs(clean) {
		return "", errors.New("invalid save file path")
	}
	if exists(clean) {
		return clean, nil
	}
	if inSaves := filepath.Join(s.savesDir, filepath.Base(clean)); exists(inSaves) {
		return inSaves, nil
	}
	return "", fmt.Errorf("save file %s not found", file)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// recoverCareer replaces the selected save with its previous one and loads it.
// It runs only from the recovery form; a failed recovery changes no file.
func (s *server) recoverCareer(form url.Values) (string, error) {
	file := form.Get("file")
	path, err := s.resolveSave(file, previousAvailable)
	if err != nil {
		if strings.HasPrefix(err.Error(), "save file") {
			err = fmt.Errorf("%s has no previous save to recover", file)
		}
		return "/", err
	}
	w, err := storage.RecoverPrevious(path)
	if err != nil {
		s.offerRecovery(path)
		return "/", fmt.Errorf("recover %s: %w; no file was changed", file, err)
	}
	if err := s.adopt(w, path); err != nil {
		return "/", err
	}
	s.say("Recovered %s from the previous save %s.", filepath.Base(path), filepath.Base(storage.PreviousPath(path)))
	return "/", nil
}
