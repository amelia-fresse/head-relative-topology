package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func emptyRepo() model.RepoConfig {
	return model.RepoConfig{HiddenBranches: []string{}, HidePatterns: []string{}}
}

func TestDefaultPath(t *testing.T) {
	got, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "hrt", "config.toml"); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestOpenMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hrt", "config.toml")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open on missing file: %v", err)
	}
	if got := s.Settings(); !reflect.DeepEqual(got, model.DefaultSettings()) {
		t.Errorf("Settings() = %+v, want defaults", got)
	}
	if got := s.RepoConfig("acme/my-app"); !reflect.DeepEqual(got, emptyRepo()) {
		t.Errorf("RepoConfig() = %+v, want empty rules with non-nil slices", got)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open must not create the file, stat err = %v", err)
	}
}

func TestOpenCorruptFileIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, "[settings\ntheme =")
	if _, err := Open(path); err == nil {
		t.Fatal("Open on corrupt file: want error, got nil")
	}
}

func TestOpenPartialFileKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, "[settings]\ntheme = \"dolly\"\n")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	want := model.DefaultSettings()
	want.Theme = "dolly"
	if got := s.Settings(); !reflect.DeepEqual(got, want) {
		t.Errorf("Settings() = %+v, want %+v", got, want)
	}
}

func TestOpenNormalisesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, `[settings]
theme = "neon"
fetch_interval_sec = -5
pr_poll_interval_sec = -1
merged_window_hours = -2

[repos."acme/app"]
stale_days = -3
`)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	want := model.DefaultSettings()
	if got := s.Settings(); !reflect.DeepEqual(got, want) {
		t.Errorf("Settings() = %+v, want %+v", got, want)
	}
	got := s.RepoConfig("acme/app")
	if got.StaleDays != 0 {
		t.Errorf("StaleDays = %d, want 0 for negative input", got.StaleDays)
	}
	if !reflect.DeepEqual(got, emptyRepo()) {
		t.Errorf("RepoConfig() = %+v, want empty slices", got)
	}
}

func TestSetSettingsNormalises(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st := model.DefaultSettings()
	st.Theme = "bogus"
	st.PRPollIntervalSec = -1
	if err := s.SetSettings(st); err != nil {
		t.Fatal(err)
	}
	want := model.DefaultSettings()
	if got := s.Settings(); !reflect.DeepEqual(got, want) {
		t.Errorf("Settings() after SetSettings = %+v, want %+v", got, want)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Settings(); !reflect.DeepEqual(got, want) {
		t.Errorf("reopened Settings() = %+v, want %+v", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "hrt", "config.toml")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	st := model.Settings{
		Theme:             "babydoll",
		Compact:           true,
		ShowMerged:        false,
		ShowHidden:        true,
		OnlyPRs:           true,
		AlwaysOnTop:       true,
		FetchIntervalSec:  0,
		PRPollIntervalSec: 300,
		MergedWindowHours: 12,
		LastRepo:          filepath.Join(t.TempDir(), "repo"),
	}
	st.OpenRepos = []string{st.LastRepo, "/other/repo"}
	if err := s.SetSettings(st); err != nil {
		t.Fatal(err)
	}

	app := model.RepoConfig{
		HiddenBranches: []string{"archive/old-spike"},
		HidePatterns:   []string{"dependabot/*"},
		HideNoCommits:  true,
		HideRemoteOnly: false,
		StaleDays:      30,
	}
	if err := s.SetRepoConfig("acme/my-app", app); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRepoConfig("/some/local/repo", emptyRepo()); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := reopened.Settings(); !reflect.DeepEqual(got, st) {
		t.Errorf("Settings() = %+v, want %+v", got, st)
	}
	if got := reopened.RepoConfig("acme/my-app"); !reflect.DeepEqual(got, app) {
		t.Errorf("RepoConfig(acme/my-app) = %+v, want %+v", got, app)
	}
	if got := reopened.RepoConfig("/some/local/repo"); !reflect.DeepEqual(got, emptyRepo()) {
		t.Errorf("RepoConfig(local) = %+v, want empty", got)
	}
	if got := reopened.RepoConfig("unknown/repo"); !reflect.DeepEqual(got, emptyRepo()) {
		t.Errorf("RepoConfig(unknown) = %+v, want empty", got)
	}
}

func TestRepoConfigReturnsCopies(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := model.RepoConfig{HiddenBranches: []string{"a"}, HidePatterns: []string{"b/*"}}
	if err := s.SetRepoConfig("acme/app", cfg); err != nil {
		t.Fatal(err)
	}
	cfg.HiddenBranches[0] = "mutated-input"
	got := s.RepoConfig("acme/app")
	got.HiddenBranches[0] = "mutated-output"

	if again := s.RepoConfig("acme/app"); again.HiddenBranches[0] != "a" {
		t.Errorf("store was affected by mutation: %+v", again)
	}
}

func TestSetRepoConfigRejectsEmptyKey(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRepoConfig("", emptyRepo()); err == nil {
		t.Error("SetRepoConfig with empty key: want error, got nil")
	}
}

func TestFailedSaveKeepsPreviousState(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	st := model.DefaultSettings()
	st.Theme = "dolly"
	if err := s.SetSettings(st); err != nil {
		t.Fatal(err)
	}

	// A regular file where the directory should be makes every write fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	writeFile(t, blocker, "not a directory")
	s.path = filepath.Join(blocker, "config.toml")

	if err := s.SetRepoConfig("acme/app", model.RepoConfig{HideNoCommits: true}); err == nil {
		t.Fatal("SetRepoConfig: want error, got nil")
	}
	if got := s.RepoConfig("acme/app"); !reflect.DeepEqual(got, emptyRepo()) {
		t.Errorf("failed SetRepoConfig changed state: %+v", got)
	}
	if err := s.SetSettings(model.DefaultSettings()); err == nil {
		t.Fatal("SetSettings: want error, got nil")
	}
	if got := s.Settings().Theme; got != "dolly" {
		t.Errorf("failed SetSettings changed state: theme = %q", got)
	}
}

func TestSaveFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on Windows")
	}
	dir := filepath.Join(t.TempDir(), "hrt")
	path := filepath.Join(dir, "config.toml")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSettings(model.DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir perm = %o, want 700", perm)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file perm = %o, want 600", perm)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("config dir has leftovers after save: %v", names)
	}
}

func TestConcurrentUse(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	const writers, rounds = 8, 10
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("acme/repo-%d", i)
			for j := range rounds {
				if err := s.SetRepoConfig(key, model.RepoConfig{StaleDays: j}); err != nil {
					errs <- err
					return
				}
				_ = s.RepoConfig(key)
				_ = s.Settings()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for i := range writers {
		key := fmt.Sprintf("acme/repo-%d", i)
		if got := s.RepoConfig(key).StaleDays; got != rounds-1 {
			t.Errorf("%s StaleDays = %d, want %d", key, got, rounds-1)
		}
	}
}

func TestRepoKey(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")

	tests := []struct {
		name, slug, path, want string
	}{
		{"slug wins over path", "acme/my-app", repo, "acme/my-app"},
		{"no slug uses path", "", repo, repo},
		{"path is cleaned", "", filepath.Join(dir, "a", "..", "repo"), repo},
		{"trailing separator dropped", "", repo + string(filepath.Separator), repo},
		{"nothing given", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepoKey(tt.slug, tt.path); got != tt.want {
				t.Errorf("RepoKey(%q, %q) = %q, want %q", tt.slug, tt.path, got, tt.want)
			}
		})
	}
}

func TestRepoKeyRelativePathIsAbsolute(t *testing.T) {
	want, err := filepath.Abs("repo")
	if err != nil {
		t.Fatal(err)
	}
	if got := RepoKey("", filepath.Join("sub", "..", "repo")); got != want {
		t.Errorf("RepoKey with relative path = %q, want %q", got, want)
	}
}
