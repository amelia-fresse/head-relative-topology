// Package gitscan talks to git by shelling out to the `git` executable (via
// execx, so no console windows flash on Windows). It resolves a repository,
// lists its branches and worktrees, and answers the graph questions the
// snapshot builder needs (merge-bases, counts, ancestry).
package gitscan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/amelia-fresse/head-relative-topology/internal/execx"
)

// Repo is a resolved repository. All fields are filled by Open.
type Repo struct {
	// Path is the absolute path of the main worktree (or of the bare
	// repository when the main "worktree" is bare).
	Path string
	// GitDir is the absolute git common dir (shared by all worktrees).
	GitDir string
	// Bare is true when the main worktree entry is a bare repository.
	Bare bool
	// Remote is the remote used for main ("origin" when present, else the
	// first remote). Empty when the repository has no remote at all.
	Remote string
	// RemoteURL is the fetch URL of Remote ("" without a remote).
	RemoteURL string
	// DefaultBranch is the short name of main, e.g. "main".
	DefaultBranch string
	// Slug is "owner/name" when RemoteURL points to GitHub, else "".
	Slug string
}

// MainRef is the full ref of main: refs/remotes/<remote>/<default>, or
// refs/heads/<default> when the repository has no remote.
func (r *Repo) MainRef() string {
	if r.Remote == "" {
		return "refs/heads/" + r.DefaultBranch
	}
	return "refs/remotes/" + r.Remote + "/" + r.DefaultBranch
}

// MainDisplayRef is the short display form of MainRef, e.g. "origin/main".
func (r *Repo) MainDisplayRef() string {
	if r.Remote == "" {
		return r.DefaultBranch
	}
	return r.Remote + "/" + r.DefaultBranch
}

// git runs a git command in the repository and returns trimmed stdout.
func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	return execx.Run(ctx, r.Path, "git", args...)
}

// Open resolves the repository containing path. path may be anywhere inside
// the main worktree or inside any linked worktree; the returned Repo always
// describes the main worktree.
func Open(ctx context.Context, path string) (*Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(abs); err != nil {
		return nil, err
	} else if !fi.IsDir() {
		abs = filepath.Dir(abs)
	}

	commonDir, err := execx.Run(ctx, abs, "git", "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %s: %w", abs, err)
	}
	commonDir = filepath.FromSlash(commonDir)
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(abs, commonDir)
	}

	wts, err := listWorktrees(ctx, abs)
	if err != nil {
		return nil, err
	}
	if len(wts) == 0 {
		return nil, fmt.Errorf("git worktree list returned nothing for %s", abs)
	}
	r := &Repo{
		Path:   filepath.Clean(wts[0].Path),
		GitDir: filepath.Clean(commonDir),
		Bare:   wts[0].Bare,
	}

	remotesOut, err := r.git(ctx, "remote")
	if err != nil {
		return nil, err
	}
	r.Remote = pickRemote(splitLines(remotesOut))
	if r.Remote != "" {
		url, err := r.git(ctx, "remote", "get-url", r.Remote)
		if err == nil {
			r.RemoteURL = url
			r.Slug = ParseGitHubSlug(url)
		}
	}

	r.DefaultBranch, err = r.detectDefaultBranch(ctx)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func pickRemote(remotes []string) string {
	for _, r := range remotes {
		if r == "origin" {
			return r
		}
	}
	if len(remotes) > 0 {
		return remotes[0]
	}
	return ""
}

// detectDefaultBranch reads refs/remotes/<remote>/HEAD, falling back to
// <remote>/main then <remote>/master (or local main/master without remote).
func (r *Repo) detectDefaultBranch(ctx context.Context) (string, error) {
	if r.Remote != "" {
		prefix := "refs/remotes/" + r.Remote + "/"
		if out, err := r.git(ctx, "symbolic-ref", "-q", prefix+"HEAD"); err == nil {
			if name, ok := strings.CutPrefix(out, prefix); ok && name != "" {
				return name, nil
			}
		}
		for _, name := range []string{"main", "master"} {
			if r.refExists(ctx, prefix+name) {
				return name, nil
			}
		}
		return "", fmt.Errorf("cannot find the default branch of remote %q (no %sHEAD, main or master); try `git remote set-head %s -a`", r.Remote, prefix, r.Remote)
	}
	for _, name := range []string{"main", "master"} {
		if r.refExists(ctx, "refs/heads/"+name) {
			return name, nil
		}
	}
	return "", errors.New("repository has no remote and no local main or master branch")
}

func (r *Repo) refExists(ctx context.Context, ref string) bool {
	_, err := r.git(ctx, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

// Fetch runs `git fetch --prune <remote>`. It is a no-op without a remote.
func (r *Repo) Fetch(ctx context.Context) error {
	if r.Remote == "" {
		return nil
	}
	_, err := r.git(ctx, "fetch", "--prune", "--quiet", r.Remote)
	return err
}

// ParseGitHubSlug extracts "owner/name" from a GitHub remote URL. Supported
// forms: https://[user@]github.com/o/n[.git], git@github.com:o/n[.git],
// ssh://git@github.com[:port]/o/n[.git], git://github.com/o/n. SSH host
// aliases of the form "github.com-<suffix>" (a common ~/.ssh/config trick for
// multiple accounts) are accepted too. Returns "" for anything else.
func ParseGitHubSlug(url string) string {
	u := strings.TrimSpace(url)
	var host, path string
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			return ""
		}
		host, path = rest[:slash], rest[slash+1:]
		if at := strings.LastIndexByte(host, '@'); at >= 0 {
			host = host[at+1:]
		}
		if colon := strings.IndexByte(host, ':'); colon >= 0 {
			host = host[:colon]
		}
	} else {
		// scp-like: [user@]host:path
		colon := strings.IndexByte(u, ':')
		if colon < 0 {
			return ""
		}
		host, path = u[:colon], u[colon+1:]
		if at := strings.LastIndexByte(host, '@'); at >= 0 {
			host = host[at+1:]
		}
	}
	host = strings.ToLower(host)
	if host != "github.com" && host != "www.github.com" && !strings.HasPrefix(host, "github.com-") {
		return ""
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// exitCode returns the process exit code wrapped in err, or -1.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
