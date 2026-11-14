// Package config loads and saves hrt's private settings: global preferences
// and per-repository hide rules. The file lives in the user's config
// directory and is never written inside a repository.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

const (
	dirName  = "hrt"
	fileName = "config.toml"
)

const fileHeader = "# hrt configuration. Private to this user; never stored in a repository.\n"

// validThemes are the theme names the frontend knows about.
var validThemes = []string{"default", "babydoll", "dolly"}

// fileFormat is the on-disk layout of config.toml.
type fileFormat struct {
	Settings model.Settings              `toml:"settings"`
	Repos    map[string]model.RepoConfig `toml:"repos"`
}

// DefaultPath returns <user config dir>/hrt/config.toml.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: locate user config dir: %w", err)
	}
	return filepath.Join(dir, dirName, fileName), nil
}

// Store is the in-memory view of the config file. It is safe for concurrent
// use. Every write goes to disk before it becomes visible through the Store,
// so a failed write leaves the previous state in place.
type Store struct {
	mu       sync.Mutex
	path     string
	settings model.Settings
	repos    map[string]model.RepoConfig
}

// Open reads the config file at path. A missing file yields defaults and no
// error; the file is only created on the first write. A file that exists but
// cannot be parsed is an error, so a broken file is never silently replaced.
func Open(path string) (*Store, error) {
	s := &Store{
		path:     path,
		settings: model.DefaultSettings(),
		repos:    map[string]model.RepoConfig{},
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	// Decoding over the defaults keeps every key the file omits.
	ff := fileFormat{Settings: model.DefaultSettings()}
	if _, err := toml.Decode(string(data), &ff); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	s.settings = normalizeSettings(ff.Settings)
	for key, rc := range ff.Repos {
		s.repos[key] = normalizeRepo(rc)
	}
	return s, nil
}

// Settings returns the global settings.
func (s *Store) Settings() model.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

// SetSettings normalises st, saves it and makes it current.
func (s *Store) SetSettings(st model.Settings) error {
	st = normalizeSettings(st)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.save(st, s.repos); err != nil {
		return err
	}
	s.settings = st
	return nil
}

// RepoConfig returns the hide rules stored under key. Unknown keys yield the
// zero rules. Slices are never nil and are copies, safe to modify.
func (s *Store) RepoConfig(key string) model.RepoConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneRepo(s.repos[key])
}

// SetRepoConfig normalises cfg, saves it under key and makes it current.
// key is normally produced by RepoKey.
func (s *Store) SetRepoConfig(key string, cfg model.RepoConfig) error {
	if key == "" {
		return errors.New("config: empty repo key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]model.RepoConfig, len(s.repos)+1)
	maps.Copy(next, s.repos)
	next[key] = normalizeRepo(cfg)
	if err := s.save(s.settings, next); err != nil {
		return err
	}
	s.repos = next
	return nil
}

// RepoKey is the identity a repository's rules are stored under: the GitHub
// slug ("owner/name") when there is one, otherwise the cleaned absolute path.
func RepoKey(slug, path string) string {
	if slug != "" {
		return slug
	}
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}

// save encodes the given state and writes it atomically. The caller holds s.mu.
func (s *Store) save(st model.Settings, repos map[string]model.RepoConfig) error {
	var buf bytes.Buffer
	buf.WriteString(fileHeader)
	if err := toml.NewEncoder(&buf).Encode(fileFormat{Settings: st, Repos: repos}); err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	return writeFileAtomic(s.path, buf.Bytes())
}

// writeFileAtomic writes data to a temp file beside path and renames it over
// path, so readers see either the old or the new file, never a partial one.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+fileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("config: chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("config: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("config: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: replace %s: %w", path, err)
	}
	committed = true
	return nil
}

// normalizeSettings replaces invalid values with defaults.
func normalizeSettings(st model.Settings) model.Settings {
	def := model.DefaultSettings()
	if !slices.Contains(validThemes, st.Theme) {
		st.Theme = def.Theme
	}
	if st.FetchIntervalSec < 0 {
		st.FetchIntervalSec = def.FetchIntervalSec
	}
	if st.PRPollIntervalSec < 0 {
		st.PRPollIntervalSec = def.PRPollIntervalSec
	}
	if st.MergedWindowHours < 0 {
		st.MergedWindowHours = def.MergedWindowHours
	}
	if st.OpenRepos == nil {
		st.OpenRepos = []string{}
	}
	return st
}

// normalizeRepo returns a copy of c with non-nil slices and no negative StaleDays.
func normalizeRepo(c model.RepoConfig) model.RepoConfig {
	c = cloneRepo(c)
	if c.StaleDays < 0 {
		c.StaleDays = 0
	}
	return c
}

// cloneRepo copies c, turning nil slices into empty ones.
func cloneRepo(c model.RepoConfig) model.RepoConfig {
	return model.RepoConfig{
		HiddenBranches: append([]string{}, c.HiddenBranches...),
		HidePatterns:   append([]string{}, c.HidePatterns...),
		HideNoCommits:  c.HideNoCommits,
		HideRemoteOnly: c.HideRemoteOnly,
		StaleDays:      c.StaleDays,
	}
}
