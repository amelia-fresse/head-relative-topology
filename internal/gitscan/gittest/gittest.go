// Package gittest builds real, deterministic git repositories for tests.
// It is only imported from _test.go files.
package gittest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Env is an isolated git environment rooted in a temporary directory.
type Env struct {
	t    testing.TB
	Root string
	tick int64
}

// baseTime is 2026-01-01T00:00:00Z.
const baseTime = 1767225600

// New skips the test when git is missing and isolates git from the user's
// global/system configuration (signing, hooks, default branch...).
func New(t testing.TB) *Env {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	cfg := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(cfg, []byte("[user]\n\tname = Test\n\temail = test@example.com\n[advice]\n\tdetachedHead = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	return &Env{t: t, Root: root}
}

// Path returns an absolute path under Root.
func (e *Env) Path(elem ...string) string {
	return filepath.Join(append([]string{e.Root}, elem...)...)
}

// Git runs git in dir and returns trimmed stdout; it fails the test on error.
// Every call advances a fake clock so commit dates are fixed and increasing.
func (e *Env) Git(dir string, args ...string) string {
	e.t.Helper()
	out, err := e.TryGit(dir, args...)
	if err != nil {
		e.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// TryGit is Git without failing the test.
func (e *Env) TryGit(dir string, args ...string) (string, error) {
	e.tick++
	date := fmt.Sprintf("%d +0000", baseTime+e.tick*60)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Init creates a non-bare repository at Root/name on branch main.
func (e *Env) Init(name string) string {
	dir := e.Path(name)
	e.Git(e.Root, "init", "-q", dir)
	e.Git(dir, "symbolic-ref", "HEAD", "refs/heads/main")
	return dir
}

// InitBare creates a bare repository at Root/name whose HEAD is main.
func (e *Env) InitBare(name string) string {
	dir := e.Path(name)
	e.Git(e.Root, "init", "-q", "--bare", dir)
	e.Git(dir, "symbolic-ref", "HEAD", "refs/heads/main")
	return dir
}

// Commit writes a unique file, commits it with msg and returns the new SHA.
func (e *Env) Commit(dir, msg string) string {
	e.t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_", "#", "", "(", "", ")", "").Replace(msg) + ".txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(msg+"\n"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.Git(dir, "add", "--", name)
	e.Git(dir, "commit", "-q", "--no-verify", "-m", msg)
	return e.Git(dir, "rev-parse", "HEAD")
}

// SHA resolves rev in dir.
func (e *Env) SHA(dir, rev string) string {
	return e.Git(dir, "rev-parse", "--verify", rev+"^{commit}")
}
