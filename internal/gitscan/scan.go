package gitscan

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/execx"
)

// Ref is one branch ref (local or remote-tracking).
type Ref struct {
	// Name is the short branch name without refs/heads/ or the remote prefix.
	Name string
	// FullName is the full ref, e.g. refs/remotes/origin/feat/x.
	FullName string
	SHA      string
	// CommitTime is the committer time of the tip commit.
	CommitTime time.Time
}

// Worktree is one entry of `git worktree list --porcelain`.
type Worktree struct {
	Path string
	// HEAD is the checked-out commit ("" for a bare entry).
	HEAD string
	// Branch is the short name of the checked-out branch, "" when detached.
	Branch string
	// IsMain is true for the first (primary) worktree.
	IsMain bool
	Bare   bool
	// Prunable is true when git reports the worktree directory as gone.
	Prunable bool
	// Dirty is true when `git status --porcelain` printed anything.
	Dirty bool
}

// Scan is the raw state of a repository at one moment.
type Scan struct {
	// MainRef is the display ref of main, e.g. "origin/main".
	MainRef string
	// MainHead is the commit main points to.
	MainHead CommitInfo
	// Local are refs/heads/* branches, except the default branch.
	Local []Ref
	// LocalMain is refs/heads/<default> when main is read from a remote.
	LocalMain *Ref
	// Remote are refs/remotes/<remote>/* branches, except <remote>/HEAD and
	// the default branch.
	Remote    []Ref
	Worktrees []Worktree
}

// Scan reads main, every branch and every worktree. Dirty checks run
// concurrently (bounded by MaxParallel).
func (r *Repo) Scan(ctx context.Context) (*Scan, error) {
	head, err := r.Commits(ctx, r.MainRef())
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", r.MainDisplayRef(), err)
	}
	if len(head) != 1 {
		return nil, fmt.Errorf("reading %s: unexpected output", r.MainDisplayRef())
	}
	s := &Scan{MainRef: r.MainDisplayRef(), MainHead: head[0]}

	patterns := []string{"refs/heads/"}
	remotePrefix := ""
	if r.Remote != "" {
		remotePrefix = "refs/remotes/" + r.Remote + "/"
		patterns = append(patterns, remotePrefix)
	}
	out, err := r.git(ctx, append([]string{"for-each-ref",
		"--format=%(refname)%00%(objectname)%00%(committerdate:unix)%00%(symref)"}, patterns...)...)
	if err != nil {
		return nil, err
	}
	for _, line := range splitLines(out) {
		f := strings.Split(line, "\x00")
		if len(f) != 4 || f[3] != "" { // skip symbolic refs such as origin/HEAD
			continue
		}
		ref := Ref{FullName: f[0], SHA: f[1], CommitTime: parseUnix(f[2])}
		switch {
		case strings.HasPrefix(f[0], "refs/heads/"):
			ref.Name = strings.TrimPrefix(f[0], "refs/heads/")
			if ref.Name == r.DefaultBranch {
				if remotePrefix != "" {
					lm := ref
					s.LocalMain = &lm
				}
				continue
			}
			s.Local = append(s.Local, ref)
		case remotePrefix != "" && strings.HasPrefix(f[0], remotePrefix):
			ref.Name = strings.TrimPrefix(f[0], remotePrefix)
			if ref.Name == r.DefaultBranch || ref.Name == "HEAD" {
				continue
			}
			s.Remote = append(s.Remote, ref)
		}
	}

	s.Worktrees, err = listWorktrees(ctx, r.Path)
	if err != nil {
		return nil, err
	}
	err = ForEach(ctx, len(s.Worktrees), func(i int) error {
		wt := &s.Worktrees[i]
		if wt.Bare || wt.Prunable {
			return nil
		}
		st, err := execx.Run(ctx, wt.Path, "git", "status", "--porcelain")
		if err != nil {
			// A worktree we cannot inspect (deleted dir, permissions) is not
			// fatal for the map; treat it as clean.
			return ctx.Err()
		}
		wt.Dirty = strings.TrimSpace(st) != ""
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// listWorktrees parses `git worktree list --porcelain` run in dir.
func listWorktrees(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := execx.Run(ctx, dir, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

func parseWorktrees(out string) []Worktree {
	var wts []Worktree
	var cur *Worktree
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			wts = append(wts, Worktree{Path: filepath.Clean(filepath.FromSlash(val))})
			cur = &wts[len(wts)-1]
		case "HEAD":
			if cur != nil {
				cur.HEAD = val
			}
		case "branch":
			if cur != nil {
				cur.Branch = strings.TrimPrefix(val, "refs/heads/")
			}
		case "bare":
			if cur != nil {
				cur.Bare = true
			}
		case "prunable":
			if cur != nil {
				cur.Prunable = true
			}
		}
	}
	if len(wts) > 0 {
		wts[0].IsMain = true
	}
	return wts
}

func parseUnix(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0).UTC()
}
