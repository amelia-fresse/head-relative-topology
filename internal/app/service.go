// Package app is the Wails-bound service the frontend talks to. It wires the
// git scanner, GitHub client, config store and watcher into snapshots.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/config"
	"github.com/amelia-fresse/head-relative-topology/internal/ghclient"
	"github.com/amelia-fresse/head-relative-topology/internal/gitscan"
	"github.com/amelia-fresse/head-relative-topology/internal/model"
	"github.com/amelia-fresse/head-relative-topology/internal/snapshot"
	"github.com/amelia-fresse/head-relative-topology/internal/watch"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// EventSnapshot is emitted with a model.Snapshot whenever the map changes.
const EventSnapshot = "snapshot"

// ErrNoRepo is returned when no repository is open. The frontend shows an
// "Open a repository" prompt when it sees this message.
var ErrNoRepo = errors.New("no repository open")

const (
	// mergedLookup is how many recent merged PRs are fetched to detect
	// squash-merged leftover branches.
	mergedLookup = 100
	// localRescanEvery re-scans even without ref changes, so worktree dirty
	// state stays fresh (file edits don't touch .git/refs).
	localRescanEvery = 15 * time.Second
	tick             = 5 * time.Second
	watchDebounce    = 300 * time.Millisecond
)

func init() {
	application.RegisterEvent[model.Snapshot](EventSnapshot)
}

// Service is bound to the frontend; every exported method becomes a binding.
type Service struct {
	initialRepo  string
	fallbackRepo string

	// buildMu serialises snapshot builds (git + gh calls).
	buildMu sync.Mutex

	mu       sync.Mutex
	store    *config.Store
	settings model.Settings
	repo     *gitscan.Repo
	gh       *ghclient.Client
	watcher  *watch.Watcher
	last     *model.Snapshot

	// Cached GitHub data, refreshed on its own interval.
	openPRs     []model.PullRequest
	mergedPRs   []model.MergedPR
	prFetchedAt time.Time
	ghWarning   string

	fetchedAt   time.Time
	lastFetchAt time.Time // last attempt, success or not
	lastLocalAt time.Time
	fetchWarn   string

	trigger chan struct{}
	cancel  context.CancelFunc
}

// New creates the service. At startup it opens initialRepo (an explicit
// path), else the last repository used, else fallbackRepo (the working
// directory). Either may be empty.
func New(initialRepo, fallbackRepo string) *Service {
	return &Service{
		initialRepo:  initialRepo,
		fallbackRepo: fallbackRepo,
		settings:     model.DefaultSettings(),
		trigger:      make(chan struct{}, 1),
	}
}

// ServiceStartup is called by Wails when the app starts.
func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if path, err := config.DefaultPath(); err == nil {
		if store, err := config.Open(path); err == nil {
			s.store = store
			s.settings = store.Settings()
		} else {
			log.Printf("hrt: config: %v (using defaults)", err)
		}
	}

	loopCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	for _, candidate := range []string{s.initialRepo, s.settings.LastRepo, s.fallbackRepo} {
		if candidate == "" {
			continue
		}
		if err := s.openRepo(loopCtx, candidate); err == nil {
			break
		}
	}

	go s.loop(loopCtx)
	return nil
}

// ServiceShutdown stops background work.
func (s *Service) ServiceShutdown() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Lock()
	w := s.watcher
	s.watcher = nil
	s.mu.Unlock()
	if w != nil {
		return w.Close()
	}
	return nil
}

// Snapshot returns the latest snapshot, computing one if needed.
func (s *Service) Snapshot() (model.Snapshot, error) {
	s.mu.Lock()
	last := s.last
	s.mu.Unlock()
	if last != nil {
		return *last, nil
	}
	snap, err := s.rebuild(context.Background(), false, true)
	if errors.Is(err, ErrNoRepo) {
		return emptySnapshot(), nil
	}
	return snap, err
}

// emptySnapshot signals "no repository open" to the frontend (Repo.Path == "").
func emptySnapshot() model.Snapshot {
	return model.Snapshot{
		Main:     model.MainLine{Anchors: []model.Anchor{}},
		Branches: []model.Branch{},
		Warnings: []string{},
	}
}

// Refresh recomputes the snapshot; fetch=true runs `git fetch` first.
// GitHub data is always re-queried.
func (s *Service) Refresh(fetch bool) (model.Snapshot, error) {
	return s.rebuild(context.Background(), fetch, true)
}

// OpenRepo switches to the repository containing path.
func (s *Service) OpenRepo(path string) (model.Snapshot, error) {
	if err := s.openRepo(context.Background(), path); err != nil {
		return model.Snapshot{}, err
	}
	return s.rebuild(context.Background(), false, true)
}

// PickRepo shows a folder picker and opens the chosen repository. When the
// user cancels it returns the current snapshot unchanged.
func (s *Service) PickRepo() (model.Snapshot, error) {
	path, err := application.Get().Dialog.OpenFile().
		SetTitle("Open a git repository").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
	if err != nil {
		return model.Snapshot{}, err
	}
	if path == "" {
		return s.Snapshot()
	}
	return s.OpenRepo(path)
}

// GetSettings returns the global settings.
func (s *Service) GetSettings() model.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

// SaveSettings persists the global settings.
func (s *Service) SaveSettings(st model.Settings) error {
	s.mu.Lock()
	prevTop := s.settings.AlwaysOnTop
	st.LastRepo = s.settings.LastRepo // owned by the backend
	s.settings = st
	store := s.store
	s.mu.Unlock()

	if st.AlwaysOnTop != prevTop {
		applyAlwaysOnTop(st.AlwaysOnTop)
	}
	if store != nil {
		return store.SetSettings(st)
	}
	return nil
}

// GetRepoConfig returns the hide rules for the current repository.
func (s *Service) GetRepoConfig() model.RepoConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repoConfigLocked()
}

// SaveRepoConfig persists hide rules and returns the updated snapshot.
func (s *Service) SaveRepoConfig(cfg model.RepoConfig) (model.Snapshot, error) {
	s.mu.Lock()
	if s.repo == nil {
		s.mu.Unlock()
		return model.Snapshot{}, ErrNoRepo
	}
	key := s.repoKeyLocked()
	store := s.store
	s.mu.Unlock()

	if store != nil {
		if err := store.SetRepoConfig(key, cfg); err != nil {
			return model.Snapshot{}, err
		}
	}
	return s.reapplyHide(cfg)
}

// HideBranch hides one branch by name and returns the updated snapshot.
func (s *Service) HideBranch(name string) (model.Snapshot, error) {
	cfg := s.GetRepoConfig()
	for _, n := range cfg.HiddenBranches {
		if n == name {
			return s.SaveRepoConfig(cfg)
		}
	}
	cfg.HiddenBranches = append(cfg.HiddenBranches, name)
	return s.SaveRepoConfig(cfg)
}

// UnhideBranch removes a branch from the hand-hidden list.
func (s *Service) UnhideBranch(name string) (model.Snapshot, error) {
	cfg := s.GetRepoConfig()
	kept := make([]string, 0, len(cfg.HiddenBranches))
	for _, n := range cfg.HiddenBranches {
		if n != name {
			kept = append(kept, n)
		}
	}
	cfg.HiddenBranches = kept
	return s.SaveRepoConfig(cfg)
}

// OpenURL opens a link (PR page) in the default browser.
func (s *Service) OpenURL(url string) error {
	return application.Get().Browser.OpenURL(url)
}

// --- internals ---

func (s *Service) openRepo(ctx context.Context, path string) error {
	repo, err := gitscan.Open(ctx, path)
	if err != nil {
		return fmt.Errorf("%s is not a git repository: %w", path, err)
	}

	s.mu.Lock()
	s.repo = repo
	s.gh = ghclient.New(repo.Path)
	s.last = nil
	s.openPRs, s.mergedPRs, s.prFetchedAt, s.ghWarning = nil, nil, time.Time{}, ""
	s.fetchedAt, s.lastFetchAt, s.fetchWarn = time.Time{}, time.Time{}, ""
	s.settings.LastRepo = repo.Path
	settings := s.settings
	store := s.store
	w := s.watcher
	s.mu.Unlock()

	if store != nil {
		if err := store.SetSettings(settings); err != nil {
			log.Printf("hrt: saving settings: %v", err)
		}
	}

	paths, err := watch.GitPaths(repo.GitDir)
	if err != nil {
		log.Printf("hrt: watch paths: %v", err)
		return nil
	}
	if w != nil {
		if err := w.SetPaths(paths); err != nil {
			log.Printf("hrt: watcher: %v", err)
		}
		return nil
	}
	nw, err := watch.New(paths, watchDebounce, s.poke)
	if err != nil {
		log.Printf("hrt: watcher: %v", err)
		return nil
	}
	s.mu.Lock()
	s.watcher = nw
	s.mu.Unlock()
	return nil
}

// poke asks the loop for a local rebuild (coalesced).
func (s *Service) poke() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

func (s *Service) loop(ctx context.Context) {
	applyAlwaysOnTop(s.GetSettings().AlwaysOnTop)

	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.trigger:
			s.rebuildLogged(ctx, false, false)
		case now := <-t.C:
			s.mu.Lock()
			st := s.settings
			hasRepo := s.repo != nil
			dueFetch := st.FetchIntervalSec > 0 && now.Sub(s.lastFetchAt) >= time.Duration(st.FetchIntervalSec)*time.Second
			duePR := st.PRPollIntervalSec > 0 && now.Sub(s.prFetchedAt) >= time.Duration(st.PRPollIntervalSec)*time.Second
			dueLocal := now.Sub(s.lastLocalAt) >= localRescanEvery
			s.mu.Unlock()
			if hasRepo && (dueFetch || duePR || dueLocal) {
				s.rebuildLogged(ctx, dueFetch, duePR)
			}
		}
	}
}

func (s *Service) rebuildLogged(ctx context.Context, fetch, prs bool) {
	if _, err := s.rebuild(ctx, fetch, prs); err != nil && !errors.Is(err, ErrNoRepo) && ctx.Err() == nil {
		log.Printf("hrt: refresh: %v", err)
	}
}

// rebuild scans the repo (optionally fetching and re-querying GitHub first),
// stores the snapshot and emits it to the frontend.
func (s *Service) rebuild(ctx context.Context, fetch, prs bool) (model.Snapshot, error) {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()

	s.mu.Lock()
	repo, gh := s.repo, s.gh
	st := s.settings
	s.mu.Unlock()
	if repo == nil {
		return model.Snapshot{}, ErrNoRepo
	}

	if fetch {
		err := repo.Fetch(ctx)
		s.mu.Lock()
		s.lastFetchAt = time.Now()
		if err != nil {
			s.fetchWarn = "git fetch failed: " + err.Error()
		} else {
			s.fetchedAt, s.fetchWarn = s.lastFetchAt, ""
		}
		s.mu.Unlock()
	}

	if prs {
		s.refreshPRs(ctx, repo, gh)
	}

	scan, err := repo.Scan(ctx)
	if err != nil {
		return model.Snapshot{}, err
	}

	now := time.Now()
	s.mu.Lock()
	repoCfg := s.repoConfigLocked()
	window := time.Duration(st.MergedWindowHours) * time.Hour
	merged := make([]model.MergedPR, 0, len(s.mergedPRs))
	mergedRefs := make(map[string]bool, len(s.mergedPRs))
	for _, m := range s.mergedPRs {
		mergedRefs[m.HeadRef] = true
		if window <= 0 || now.Sub(m.MergedAt) <= window {
			merged = append(merged, m)
		}
	}
	in := snapshot.Input{
		Repo:           repo,
		Scan:           scan,
		OpenPRs:        s.openPRs,
		MergedPRs:      merged,
		MergedHeadRefs: mergedRefs,
		Hide:           func(b model.Branch) string { return config.HideReason(repoCfg, b, now) },
		FetchedAt:      s.fetchedAt,
		Warnings:       nonEmpty(s.ghWarning, s.fetchWarn),
	}
	s.mu.Unlock()

	snap, err := snapshot.Build(ctx, in)
	if err != nil {
		return model.Snapshot{}, err
	}

	s.mu.Lock()
	if s.repo != repo { // repo switched mid-build; drop the stale result
		s.mu.Unlock()
		return snap, nil
	}
	s.last = &snap
	s.lastLocalAt = now
	s.mu.Unlock()

	emit(snap)
	return snap, nil
}

func (s *Service) refreshPRs(ctx context.Context, repo *gitscan.Repo, gh *ghclient.Client) {
	var (
		open   []model.PullRequest
		merged []model.MergedPR
		warn   string
	)
	switch {
	case repo.Slug == "":
		warn = "" // not a GitHub repo: nothing to fetch, nothing to warn about
	case gh == nil:
	default:
		if err := gh.Available(ctx); err != nil {
			warn = err.Error()
			break
		}
		var err error
		if open, err = gh.OpenPRs(ctx); err != nil {
			warn = "GitHub: " + err.Error()
			break
		}
		if merged, err = gh.MergedPRs(ctx, mergedLookup); err != nil {
			warn = "GitHub: " + err.Error()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.prFetchedAt = time.Now()
	s.ghWarning = warn
	if warn == "" {
		s.openPRs, s.mergedPRs = open, merged
	}
}

// reapplyHide recomputes hidden flags on the last snapshot without rescanning.
// Anchors already include hidden branches, so only the flags change.
func (s *Service) reapplyHide(cfg model.RepoConfig) (model.Snapshot, error) {
	s.mu.Lock()
	if s.last == nil {
		s.mu.Unlock()
		return s.rebuild(context.Background(), false, false)
	}
	snap := *s.last
	now := time.Now()
	branches := make([]model.Branch, len(snap.Branches))
	for i, b := range snap.Branches {
		reason := config.HideReason(cfg, b, now)
		b.Hidden, b.HiddenReason = reason != "", reason
		branches[i] = b
	}
	snap.Branches = branches
	s.last = &snap
	s.mu.Unlock()

	emit(snap)
	return snap, nil
}

func (s *Service) repoKeyLocked() string {
	if s.repo == nil {
		return ""
	}
	return config.RepoKey(s.repo.Slug, s.repo.Path)
}

func (s *Service) repoConfigLocked() model.RepoConfig {
	if s.store == nil || s.repo == nil {
		return model.RepoConfig{HiddenBranches: []string{}, HidePatterns: []string{}}
	}
	return s.store.RepoConfig(s.repoKeyLocked())
}

func emit(snap model.Snapshot) {
	if a := application.Get(); a != nil {
		a.Event.Emit(EventSnapshot, snap)
	}
}

func applyAlwaysOnTop(on bool) {
	a := application.Get()
	if a == nil {
		return
	}
	if w, ok := a.Window.GetByName("main"); ok {
		w.SetAlwaysOnTop(on)
	}
}

func nonEmpty(xs ...string) []string {
	out := []string{}
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}
