package config

import (
	"testing"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"dependabot/*", "dependabot/npm_and_yarn/vite-8", true},
		{"dependabot/*", "dependabot/", true},
		{"dependabot/*", "dependabot", false},
		{"dependabot/*", "feat/dependabot/x", false},
		{"*", "", true},
		{"*", "a/b/c", true},
		{"**", "x", true},
		{"", "", true},
		{"", "a", false},
		{"feat/?", "feat/a", true},
		{"feat/?", "feat/ab", false},
		{"feat/?", "feat/", false},
		{"feat?x", "feat/x", true}, // '?' matches '/' too
		{"a*b*c", "abc", true},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false},
		{"*a*", "bab", true},
		{"*ab", "aab", true},
		{"a*a", "a", false},
		{"héllo?", "héllo!", true}, // '?' matches a rune, not a byte
		{"héllo?", "hello!", false},
		{"*", "*", true},
		{"a*", "a*", true},
		{"a?c", "a*c", true},
		{"[ab]", "a", false}, // no character classes
		{"release/*", "release/1.2", true},
		{"release/*", "Release/1.2", false}, // case-sensitive
	}
	for _, tt := range tests {
		t.Run(tt.pattern+" vs "+tt.name, func(t *testing.T) {
			if got := MatchGlob(tt.pattern, tt.name); got != tt.want {
				t.Errorf("MatchGlob(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestHideReason(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	recent := now.AddDate(0, 0, -1)
	stale := now.AddDate(0, 0, -31)
	wt := &model.Worktree{Path: "/work/agent"}
	pr := &model.PullRequest{Number: 7}

	tests := []struct {
		name   string
		cfg    model.RepoConfig
		branch model.Branch
		want   string
	}{
		{
			name:   "no rules, visible",
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 3, LastCommitAt: stale},
			want:   "",
		},
		{
			name:   "hidden by hand",
			cfg:    model.RepoConfig{HiddenBranches: []string{"archive/old-spike"}},
			branch: model.Branch{Name: "archive/old-spike", Local: true, Ahead: 1},
			want:   "hidden by hand",
		},
		{
			name:   "exact name only, not prefix",
			cfg:    model.RepoConfig{HiddenBranches: []string{"archive/old"}},
			branch: model.Branch{Name: "archive/old-spike", Local: true, Ahead: 1},
			want:   "",
		},
		{
			name: "hidden by hand beats pattern",
			cfg: model.RepoConfig{
				HiddenBranches: []string{"dependabot/x"},
				HidePatterns:   []string{"dependabot/*"},
			},
			branch: model.Branch{Name: "dependabot/x", Local: true, Ahead: 1},
			want:   "hidden by hand",
		},
		{
			name:   "pattern",
			cfg:    model.RepoConfig{HidePatterns: []string{"dependabot/*"}},
			branch: model.Branch{Name: "dependabot/npm_and_yarn/vite-8", Local: true, Ahead: 1},
			want:   "pattern dependabot/*",
		},
		{
			name: "pattern beats automatic rules",
			cfg: model.RepoConfig{
				HidePatterns:  []string{"dependabot/*"},
				HideNoCommits: true,
				StaleDays:     30,
			},
			branch: model.Branch{Name: "dependabot/x", Local: true, Ahead: 0, LastCommitAt: stale},
			want:   "pattern dependabot/*",
		},
		{
			name:   "no commits of its own",
			cfg:    model.RepoConfig{HideNoCommits: true},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 0},
			want:   "no commits of its own",
		},
		{
			name:   "no commits rule off",
			cfg:    model.RepoConfig{HideNoCommits: false},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 0},
			want:   "",
		},
		{
			name:   "ahead branch not hidden by no-commits",
			cfg:    model.RepoConfig{HideNoCommits: true},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 1},
			want:   "",
		},
		{
			name:   "remote only without PR",
			cfg:    model.RepoConfig{HideRemoteOnly: true},
			branch: model.Branch{Name: "feat/x", Remote: true, Ahead: 1},
			want:   "remote only, no PR",
		},
		{
			name:   "remote only with PR stays visible",
			cfg:    model.RepoConfig{HideRemoteOnly: true},
			branch: model.Branch{Name: "feat/x", Remote: true, Ahead: 1, PR: pr},
			want:   "",
		},
		{
			name:   "remote only but also local stays visible",
			cfg:    model.RepoConfig{HideRemoteOnly: true},
			branch: model.Branch{Name: "feat/x", Remote: true, Local: true, Ahead: 1},
			want:   "",
		},
		{
			name:   "local only is not remote only",
			cfg:    model.RepoConfig{HideRemoteOnly: true},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 1},
			want:   "",
		},
		{
			name:   "stale",
			cfg:    model.RepoConfig{StaleDays: 30},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 1, LastCommitAt: stale},
			want:   "no commits in 30 days",
		},
		{
			name:   "stale boundary: exactly StaleDays old is visible",
			cfg:    model.RepoConfig{StaleDays: 30},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 1, LastCommitAt: now.AddDate(0, 0, -30)},
			want:   "",
		},
		{
			name:   "recent commit not stale",
			cfg:    model.RepoConfig{StaleDays: 30},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 1, LastCommitAt: recent},
			want:   "",
		},
		{
			name:   "unknown commit date never stale",
			cfg:    model.RepoConfig{StaleDays: 30},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 1},
			want:   "",
		},
		{
			name:   "stale rule off when StaleDays is 0",
			cfg:    model.RepoConfig{StaleDays: 0},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 1, LastCommitAt: stale},
			want:   "",
		},
		{
			name:   "worktree exempt from no-commits",
			cfg:    model.RepoConfig{HideNoCommits: true},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 0, Worktree: wt},
			want:   "",
		},
		{
			name:   "worktree exempt from remote-only",
			cfg:    model.RepoConfig{HideRemoteOnly: true},
			branch: model.Branch{Name: "feat/x", Remote: true, Worktree: wt},
			want:   "",
		},
		{
			name:   "worktree exempt from stale",
			cfg:    model.RepoConfig{StaleDays: 30},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 1, LastCommitAt: stale, Worktree: wt},
			want:   "",
		},
		{
			name:   "worktree does not exempt from hidden by hand",
			cfg:    model.RepoConfig{HiddenBranches: []string{"feat/x"}},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 1, Worktree: wt},
			want:   "hidden by hand",
		},
		{
			name:   "worktree does not exempt from pattern",
			cfg:    model.RepoConfig{HidePatterns: []string{"feat/*"}},
			branch: model.Branch{Name: "feat/x", Local: true, Ahead: 1, Worktree: wt},
			want:   "pattern feat/*",
		},
		{
			name: "automatic priority: no-commits before remote-only before stale",
			cfg: model.RepoConfig{
				HideNoCommits:  true,
				HideRemoteOnly: true,
				StaleDays:      30,
			},
			branch: model.Branch{Name: "feat/x", Remote: true, Ahead: 0, LastCommitAt: stale},
			want:   "no commits of its own",
		},
		{
			name: "automatic priority: remote-only before stale",
			cfg: model.RepoConfig{
				HideRemoteOnly: true,
				StaleDays:      30,
			},
			branch: model.Branch{Name: "feat/x", Remote: true, Ahead: 1, LastCommitAt: stale},
			want:   "remote only, no PR",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HideReason(tt.cfg, tt.branch, now); got != tt.want {
				t.Errorf("HideReason() = %q, want %q", got, tt.want)
			}
		})
	}
}
