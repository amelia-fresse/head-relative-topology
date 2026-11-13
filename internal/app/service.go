// Package app is the Wails-bound service the frontend talks to. It wires the
// git scanner, GitHub client, config store and watcher into snapshots.
package app

import (
	"context"
	"errors"
	"sync"

	"github.com/amelia-fresse/head-relative-topology/internal/model"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// EventSnapshot is emitted with a model.Snapshot whenever the map changes.
const EventSnapshot = "snapshot"

func init() {
	application.RegisterEvent[model.Snapshot](EventSnapshot)
}

// Service is bound to the frontend; every exported method becomes a binding.
type Service struct {
	mu       sync.Mutex
	repoPath string
	settings model.Settings
	repoCfg  model.RepoConfig
	last     *model.Snapshot
}

// New creates the service. initialRepo may be empty.
func New(initialRepo string) *Service {
	return &Service{repoPath: initialRepo, settings: model.DefaultSettings()}
}

// ServiceStartup is called by Wails when the app starts.
func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	return nil
}

// Snapshot returns the latest snapshot, computing one if needed.
func (s *Service) Snapshot() (model.Snapshot, error) {
	return model.Snapshot{}, errors.New("not implemented yet")
}

// Refresh recomputes the snapshot; fetch=true runs `git fetch` first.
func (s *Service) Refresh(fetch bool) (model.Snapshot, error) {
	return model.Snapshot{}, errors.New("not implemented yet")
}

// OpenRepo switches to the repository containing path.
func (s *Service) OpenRepo(path string) (model.Snapshot, error) {
	return model.Snapshot{}, errors.New("not implemented yet")
}

// PickRepo shows a folder picker and opens the chosen repository.
func (s *Service) PickRepo() (model.Snapshot, error) {
	return model.Snapshot{}, errors.New("not implemented yet")
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
	defer s.mu.Unlock()
	s.settings = st
	return nil
}

// GetRepoConfig returns the hide rules for the current repository.
func (s *Service) GetRepoConfig() model.RepoConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repoCfg
}

// SaveRepoConfig persists hide rules and returns the updated snapshot.
func (s *Service) SaveRepoConfig(cfg model.RepoConfig) (model.Snapshot, error) {
	return model.Snapshot{}, errors.New("not implemented yet")
}

// HideBranch hides one branch by name and returns the updated snapshot.
func (s *Service) HideBranch(name string) (model.Snapshot, error) {
	return model.Snapshot{}, errors.New("not implemented yet")
}

// UnhideBranch removes a branch from the hand-hidden list.
func (s *Service) UnhideBranch(name string) (model.Snapshot, error) {
	return model.Snapshot{}, errors.New("not implemented yet")
}

// OpenURL opens a link (PR page) in the default browser.
func (s *Service) OpenURL(url string) error {
	return application.Get().Browser.OpenURL(url)
}
