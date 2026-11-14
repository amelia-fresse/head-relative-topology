// Package app is the Wails-bound service the frontend talks to. It wires the
// git scanner, GitHub client, config store and watcher into snapshots.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/config"
	"github.com/amelia-fresse/head-relative-topology/internal/ghclient"
	"github.com/amelia-fresse/head-relative-topology/internal/gitscan"
	"github.com/amelia-fresse/head-relative-topology/internal/model"
	"github.com/amelia-fresse/head-relative-topology/internal/snapshot"
	"github.com/amelia-fresse/head-relative-topology/internal/watch"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// EventSnapshot is emitted with the active tab's model.Snapshot whenever it changes.
const EventSnapshot = "snapshot"

// EventTabs is emitted with the []model.Tab list whenever tabs or their
// attention state change.
const EventTabs = "tabs"

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
	application.RegisterEvent[[]model.Tab](EventTabs)
}

// repoState is one open repository (one tab). Fields below buildMu are
// guarded by Service.mu.
type repoState struct {
	repo    *gitscan.Repo
	gh      *ghclient.Client
	watcher *watch.Watcher

	// buildMu serialises snapshot builds (git + gh calls) for this repo.
	buildMu sync.Mutex
	// dirty is set by the watcher; inflight while a background build runs.
	dirty    atomic.Bool
	inflight atomic.Bool

	last *model.Snapshot

	// Cached GitHub data, refreshed on its own interval.
	openPRs     []model.PullRequest
	mergedPRs   []model.MergedPR
	prFetchedAt time.Time
	ghWarning   string

	fetchedAt   time.Time
	lastFetchAt time.Time // last attempt, success or not
	lastLocalAt time.Time
	fetchWarn   string
}

// Service is bound to the frontend; every exported method becomes a binding.
type Service struct {
	initialRepo  string
	fallbackRepo string

	mu       sync.Mutex
	store    *config.Store
	settings model.Settings
	tabs     []*repoState
	active   *repoState

	trigger chan struct{}
	cancel  context.CancelFunc
}

// New creates the service. At startup it reopens the tabs from last time
// and activates initialRepo (an explicit path), else the last active tab,
// else fallbackRepo (the working directory) when nothing else is open.
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

	s.mu.Lock()
	saved := append([]string(nil), s.settings.OpenRepos...)
	lastActive := s.settings.LastRepo
	s.mu.Unlock()
	if len(saved) == 0 && lastActive != "" {
		saved = []string{lastActive} // config from before tabs existed
	}

	var active *repoState
	for _, p := range saved {
		st, err := s.addRepo(loopCtx, p)
		if err != nil {
			log.Printf("hrt: reopening %s: %v", p, err)
			continue
		}
		if p == lastActive {
			active = st
		}
	}
	if s.initialRepo != "" {
		if st, err := s.addRepo(loopCtx, s.initialRepo); err == nil {
			active = st
		} else {
			log.Printf("hrt: %v", err)
		}
	}
	s.mu.Lock()
	empty := len(s.tabs) == 0
	if active == nil && !empty {
		active = s.tabs[0]
	}
	s.mu.Unlock()
	if empty && s.fallbackRepo != "" {
		if st, err := s.addRepo(loopCtx, s.fallbackRepo); err == nil {
			active = st
		}
	}
	if active != nil {
		s.activate(active)
	}
	s.persist()

	go s.loop(loopCtx)
	return nil
}

// ServiceShutdown stops background work.
func (s *Service) ServiceShutdown() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Lock()
	var watchers []*watch.Watcher
	for _, st := range s.tabs {
		if st.watcher != nil {
			watchers = append(watchers, st.watcher)
		}
	}
	s.mu.Unlock()
	var firstErr error
	for _, w := range watchers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Snapshot returns the latest snapshot of the active tab, computing one if needed.
func (s *Service) Snapshot() (model.Snapshot, error) {
	s.mu.Lock()
	st := s.active
	s.mu.Unlock()
	return s.snapshotOf(st)
}

func (s *Service) snapshotOf(st *repoState) (model.Snapshot, error) {
	if st == nil {
		return emptySnapshot(), nil
	}
	s.mu.Lock()
	last := st.last
	s.mu.Unlock()
	if last != nil {
		return *last, nil
	}
	return s.rebuild(context.Background(), st, false, true)
}

// emptySnapshot signals "no repository open" to the frontend (Repo.Path == "").
func emptySnapshot() model.Snapshot {
	return model.Snapshot{
		Main:     model.MainLine{Anchors: []model.Anchor{}},
		Branches: []model.Branch{},
		Warnings: []string{},
	}
}

// Refresh recomputes the active tab; fetch=true runs `git fetch` first.
// GitHub data is always re-queried.
func (s *Service) Refresh(fetch bool) (model.Snapshot, error) {
	s.mu.Lock()
	st := s.active
	s.mu.Unlock()
	if st == nil {
		return emptySnapshot(), nil
	}
	return s.rebuild(context.Background(), st, fetch, true)
}

// OpenRepo opens the repository containing path in a tab (reusing its tab
// when already open) and makes it active.
func (s *Service) OpenRepo(path string) (model.Snapshot, error) {
	st, err := s.addRepo(context.Background(), path)
	if err != nil {
		return model.Snapshot{}, err
	}
	s.activate(st)
	s.persist()
	return s.snapshotOf(st)
}

// PickRepo shows a folder picker and opens the chosen repository in a tab.
// When the user cancels it returns the current snapshot unchanged.
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

// Tabs lists the open repositories in tab order.
func (s *Service) Tabs() []model.Tab {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tabsLocked()
}

// SwitchRepo makes the tab for path active and returns its snapshot.
func (s *Service) SwitchRepo(path string) (model.Snapshot, error) {
	s.mu.Lock()
	st := s.findLocked(path)
	s.mu.Unlock()
	if st == nil {
		return model.Snapshot{}, fmt.Errorf("%s is not open", path)
	}
	s.activate(st)
	s.persist()
	return s.snapshotOf(st)
}

// CloseRepo closes the tab for path. When it was active, the next tab (or
// the previous one) becomes active. Returns the active snapshot afterwards.
func (s *Service) CloseRepo(path string) (model.Snapshot, error) {
	s.mu.Lock()
	idx := -1
	for i, st := range s.tabs {
		if st.repo.Path == path {
			idx = i
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return s.Snapshot()
	}
	closed := s.tabs[idx]
	closedWatcher := closed.watcher
	s.tabs = append(s.tabs[:idx:idx], s.tabs[idx+1:]...)
	var next *repoState
	if s.active == closed {
		s.active = nil
		if len(s.tabs) > 0 {
			next = s.tabs[min(idx, len(s.tabs)-1)]
		} else {
			s.settings.LastRepo = ""
		}
	}
	s.mu.Unlock()

	if closedWatcher != nil {
		if err := closedWatcher.Close(); err != nil {
			log.Printf("hrt: watcher: %v", err)
		}
	}
	if next != nil {
		s.activate(next)
	} else {
		emitTabs(s.Tabs())
	}
	s.persist()
	return s.Snapshot()
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
	// Owned by the backend.
	st.LastRepo = s.settings.LastRepo
	st.OpenRepos = s.openPathsLocked()
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

// GetRepoConfig returns the hide rules for the active repository.
func (s *Service) GetRepoConfig() model.RepoConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repoConfigLocked(s.active)
}

// SaveRepoConfig persists hide rules for the active repository and returns
// the updated snapshot.
func (s *Service) SaveRepoConfig(cfg model.RepoConfig) (model.Snapshot, error) {
	s.mu.Lock()
	st := s.active
	if st == nil {
		s.mu.Unlock()
		return model.Snapshot{}, ErrNoRepo
	}
	key := repoKey(st)
	store := s.store
	s.mu.Unlock()

	if store != nil {
		if err := store.SetRepoConfig(key, cfg); err != nil {
			return model.Snapshot{}, err
		}
	}
	return s.reapplyHide(st, cfg)
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

// addRepo opens path as a tab, or returns the existing tab for that repository.
func (s *Service) addRepo(ctx context.Context, path string) (*repoState, error) {
	repo, err := gitscan.Open(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("%s is not a git repository: %w", path, err)
	}

	s.mu.Lock()
	if st := s.findLocked(repo.Path); st != nil {
		s.mu.Unlock()
		return st, nil
	}
	st := &repoState{repo: repo, gh: ghclient.New(repo.Path)}
	s.tabs = append(s.tabs, st)
	s.mu.Unlock()

	paths, err := watch.GitPaths(repo.GitDir)
	if err != nil {
		log.Printf("hrt: watch paths: %v", err)
		return st, nil
	}
	w, err := watch.New(paths, watchDebounce, func() {
		st.dirty.Store(true)
		s.poke()
	})
	if err != nil {
		log.Printf("hrt: watcher: %v", err)
		return st, nil
	}
	s.mu.Lock()
	st.watcher = w
	s.mu.Unlock()
	return st, nil
}

// activate makes st the drawn tab and pushes its snapshot (if any) and the tabs.
func (s *Service) activate(st *repoState) {
	s.mu.Lock()
	s.active = st
	s.settings.LastRepo = st.repo.Path
	last := st.last
	tabs := s.tabsLocked()
	s.mu.Unlock()
	if last != nil {
		emit(*last)
	}
	emitTabs(tabs)
}

// persist saves the open tabs and the active one.
func (s *Service) persist() {
	s.mu.Lock()
	s.settings.OpenRepos = s.openPathsLocked()
	settings, store := s.settings, s.store
	s.mu.Unlock()
	if store != nil {
		if err := store.SetSettings(settings); err != nil {
			log.Printf("hrt: saving settings: %v", err)
		}
	}
}

func (s *Service) findLocked(path string) *repoState {
	for _, st := range s.tabs {
		if st.repo.Path == path {
			return st
		}
	}
	return nil
}

func (s *Service) openPathsLocked() []string {
	out := make([]string, len(s.tabs))
	for i, st := range s.tabs {
		out[i] = st.repo.Path
	}
	return out
}

func (s *Service) tabsLocked() []model.Tab {
	out := make([]model.Tab, len(s.tabs))
	for i, st := range s.tabs {
		out[i] = model.Tab{
			Path:      st.repo.Path,
			Name:      tabName(st.repo),
			Slug:      st.repo.Slug,
			Active:    st == s.active,
			Attention: attention(st.last),
		}
	}
	return out
}

func tabName(r *gitscan.Repo) string {
	if r.Slug != "" {
		if i := strings.LastIndex(r.Slug, "/"); i >= 0 {
			return r.Slug[i+1:]
		}
		return r.Slug
	}
	return filepath.Base(r.Path)
}

// attention is the most urgent status among visible branches.
func attention(snap *model.Snapshot) string {
	if snap == nil {
		return ""
	}
	out := ""
	for _, b := range snap.Branches {
		if b.Hidden {
			continue
		}
		switch b.Status {
		case model.StatusConflicts:
			return string(model.StatusConflicts)
		case model.StatusCIFailing:
			out = string(model.StatusCIFailing)
		}
	}
	return out
}

// poke wakes the loop to rebuild tabs marked dirty (coalesced).
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
			s.mu.Lock()
			tabs := append([]*repoState(nil), s.tabs...)
			s.mu.Unlock()
			for _, st := range tabs {
				if st.dirty.Swap(false) {
					s.rebuildAsync(ctx, st, false, false)
				}
			}
		case now := <-t.C:
			s.mu.Lock()
			cfg := s.settings
			type due struct {
				st         *repoState
				fetch, prs bool
			}
			var todo []due
			for _, st := range s.tabs {
				dueFetch := cfg.FetchIntervalSec > 0 && now.Sub(st.lastFetchAt) >= time.Duration(cfg.FetchIntervalSec)*time.Second
				duePR := cfg.PRPollIntervalSec > 0 && now.Sub(st.prFetchedAt) >= time.Duration(cfg.PRPollIntervalSec)*time.Second
				dueLocal := now.Sub(st.lastLocalAt) >= localRescanEvery || st.dirty.Load()
				if dueFetch || duePR || dueLocal {
					todo = append(todo, due{st, dueFetch, duePR})
				}
			}
			s.mu.Unlock()
			for _, d := range todo {
				d.st.dirty.Store(false)
				s.rebuildAsync(ctx, d.st, d.fetch, d.prs)
			}
		}
	}
}

// rebuildAsync rebuilds st in the background unless a background build is
// already running for it (then it is marked dirty and retried later).
func (s *Service) rebuildAsync(ctx context.Context, st *repoState, fetch, prs bool) {
	if st.inflight.Swap(true) {
		st.dirty.Store(true)
		return
	}
	go func() {
		defer st.inflight.Store(false)
		if _, err := s.rebuild(ctx, st, fetch, prs); err != nil && ctx.Err() == nil {
			log.Printf("hrt: refresh %s: %v", st.repo.Path, err)
		}
	}()
}

// rebuild scans st (optionally fetching and re-querying GitHub first),
// stores the snapshot, emits it when st is the active tab, and emits the tabs.
func (s *Service) rebuild(ctx context.Context, st *repoState, fetch, prs bool) (model.Snapshot, error) {
	st.buildMu.Lock()
	defer st.buildMu.Unlock()

	repo := st.repo
	s.mu.Lock()
	cfg := s.settings
	s.mu.Unlock()

	if fetch {
		err := repo.Fetch(ctx)
		s.mu.Lock()
		st.lastFetchAt = time.Now()
		if err != nil {
			st.fetchWarn = "git fetch failed: " + err.Error()
		} else {
			st.fetchedAt, st.fetchWarn = st.lastFetchAt, ""
		}
		s.mu.Unlock()
	}

	if prs {
		s.refreshPRs(ctx, st)
	}

	scan, err := repo.Scan(ctx)
	if err != nil {
		return model.Snapshot{}, err
	}

	now := time.Now()
	s.mu.Lock()
	repoCfg := s.repoConfigLocked(st)
	window := time.Duration(cfg.MergedWindowHours) * time.Hour
	merged := make([]model.MergedPR, 0, len(st.mergedPRs))
	mergedRefs := make(map[string]bool, len(st.mergedPRs))
	for _, m := range st.mergedPRs {
		mergedRefs[m.HeadRef] = true
		if window <= 0 || now.Sub(m.MergedAt) <= window {
			merged = append(merged, m)
		}
	}
	in := snapshot.Input{
		Repo:           repo,
		Scan:           scan,
		OpenPRs:        st.openPRs,
		MergedPRs:      merged,
		MergedHeadRefs: mergedRefs,
		Hide:           func(b model.Branch) string { return config.HideReason(repoCfg, b, now) },
		FetchedAt:      st.fetchedAt,
		Warnings:       nonEmpty(st.ghWarning, st.fetchWarn),
	}
	s.mu.Unlock()

	snap, err := snapshot.Build(ctx, in)
	if err != nil {
		return model.Snapshot{}, err
	}

	s.mu.Lock()
	st.last = &snap
	st.lastLocalAt = now
	isActive := s.active == st
	tabs := s.tabsLocked()
	s.mu.Unlock()

	if isActive {
		emit(snap)
	}
	emitTabs(tabs)
	return snap, nil
}

func (s *Service) refreshPRs(ctx context.Context, st *repoState) {
	repo, gh := st.repo, st.gh
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
	st.prFetchedAt = time.Now()
	st.ghWarning = warn
	if warn == "" {
		st.openPRs, st.mergedPRs = open, merged
	}
}

// reapplyHide recomputes hidden flags on st's last snapshot without
// rescanning. Anchors already include hidden branches, so only the flags change.
func (s *Service) reapplyHide(st *repoState, cfg model.RepoConfig) (model.Snapshot, error) {
	s.mu.Lock()
	if st.last == nil {
		s.mu.Unlock()
		return s.rebuild(context.Background(), st, false, false)
	}
	snap := *st.last
	now := time.Now()
	branches := make([]model.Branch, len(snap.Branches))
	for i, b := range snap.Branches {
		reason := config.HideReason(cfg, b, now)
		b.Hidden, b.HiddenReason = reason != "", reason
		branches[i] = b
	}
	snap.Branches = branches
	st.last = &snap
	isActive := s.active == st
	tabs := s.tabsLocked()
	s.mu.Unlock()

	if isActive {
		emit(snap)
	}
	emitTabs(tabs)
	return snap, nil
}

func repoKey(st *repoState) string {
	return config.RepoKey(st.repo.Slug, st.repo.Path)
}

func (s *Service) repoConfigLocked(st *repoState) model.RepoConfig {
	if s.store == nil || st == nil {
		return model.RepoConfig{HiddenBranches: []string{}, HidePatterns: []string{}}
	}
	return s.store.RepoConfig(repoKey(st))
}

func emit(snap model.Snapshot) {
	if a := application.Get(); a != nil {
		a.Event.Emit(EventSnapshot, snap)
	}
}

func emitTabs(tabs []model.Tab) {
	if a := application.Get(); a != nil {
		a.Event.Emit(EventTabs, tabs)
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
