package config

import (
	"fmt"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

// HideReason returns why b should be hidden under cfg, or "" if it stays
// visible. Manual rules (exact name, then glob pattern) apply to every branch.
// The automatic rules never hide a branch that has a worktree, since an agent
// may be working in it.
func HideReason(cfg model.RepoConfig, b model.Branch, now time.Time) string {
	for _, name := range cfg.HiddenBranches {
		if name == b.Name {
			return "hidden by hand"
		}
	}
	for _, pattern := range cfg.HidePatterns {
		if MatchGlob(pattern, b.Name) {
			return "pattern " + pattern
		}
	}

	if b.Worktree != nil {
		return ""
	}
	if cfg.HideNoCommits && b.Ahead == 0 {
		return "no commits of its own"
	}
	if cfg.HideRemoteOnly && b.Remote && !b.Local && b.PR == nil {
		return "remote only, no PR"
	}
	// A zero LastCommitAt means the date is unknown; an unknown date never hides.
	if cfg.StaleDays > 0 && !b.LastCommitAt.IsZero() &&
		b.LastCommitAt.Before(now.AddDate(0, 0, -cfg.StaleDays)) {
		return fmt.Sprintf("no commits in %d days", cfg.StaleDays)
	}
	return ""
}

// MatchGlob reports whether name matches pattern. '*' matches any run of
// characters, including '/'; '?' matches exactly one character. Everything
// else is literal, and there are no character classes or escapes.
func MatchGlob(pattern, name string) bool {
	p := []rune(pattern)
	s := []rune(name)
	pi, si := 0, 0
	// starPi is the most recent '*' in p; starSi is the position in s it
	// was last tried against. On a mismatch we let that '*' absorb one more
	// rune and retry, which keeps the match linear-ish without recursion.
	starPi, starSi := -1, 0

	for si < len(s) {
		switch {
		case pi < len(p) && p[pi] == '*':
			starPi, starSi = pi, si
			pi++
		case pi < len(p) && (p[pi] == '?' || p[pi] == s[si]):
			pi++
			si++
		case starPi >= 0:
			pi = starPi + 1
			starSi++
			si = starSi
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
