package gitscan_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/gitscan"
	"github.com/amelia-fresse/head-relative-topology/internal/gitscan/gittest"
)

func TestParseGitHubSlug(t *testing.T) {
	tests := []struct{ url, want string }{
		{"https://github.com/owner/name.git", "owner/name"},
		{"https://github.com/owner/name", "owner/name"},
		{"https://github.com/owner/name/", "owner/name"},
		{"https://user:tok@github.com/owner/name.git", "owner/name"},
		{"https://www.github.com/owner/name", "owner/name"},
		{"http://GitHub.com/Owner/Name.git", "Owner/Name"},
		{"git@github.com:owner/name.git", "owner/name"},
		{"git@github.com:owner/name", "owner/name"},
		{"github.com:owner/name.git", "owner/name"},
		{"git@github.com-work:owner/name.git", "owner/name"},
		{"ssh://git@github.com/owner/name.git", "owner/name"},
		{"ssh://git@github.com:22/owner/name.git", "owner/name"},
		{"git://github.com/owner/name.git", "owner/name"},
		{"https://gitlab.com/owner/name.git", ""},
		{"git@gitlab.com:owner/name.git", ""},
		{"https://github.com/owner", ""},
		{"https://github.com/a/b/c", ""},
		{"/srv/git/repo.git", ""},
		{`C:\repos\origin.git`, ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := gitscan.ParseGitHubSlug(tt.url); got != tt.want {
			t.Errorf("ParseGitHubSlug(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

// setup creates a bare origin and a clone-like work repo with main pushed.
func setup(t *testing.T) (*gittest.Env, string, string) {
	env := gittest.New(t)
	origin := env.InitBare("origin.git")
	work := env.Init("work")
	env.Commit(work, "c0")
	env.Commit(work, "c1")
	env.Git(work, "remote", "add", "origin", origin)
	env.Git(work, "push", "-q", "origin", "main")
	return env, origin, work
}

func TestOpen(t *testing.T) {
	env, _, work := setup(t)
	env.Git(work, "remote", "set-head", "origin", "main")
	env.Git(work, "remote", "add", "upstream", "git@github.com:acme/widgets.git")
	sub := filepath.Join(work, "sub", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	env.Git(work, "branch", "feat/x")
	linked := env.Path("linked")
	env.Git(work, "worktree", "add", "-q", linked, "feat/x")

	for _, from := range []string{work, sub, linked, filepath.Join(work, "c0.txt")} {
		t.Run(filepath.Base(from), func(t *testing.T) {
			r, err := gitscan.Open(context.Background(), from)
			if err != nil {
				t.Fatal(err)
			}
			if r.Path != work {
				t.Errorf("Path = %q, want %q", r.Path, work)
			}
			if want := filepath.Join(work, ".git"); r.GitDir != want {
				t.Errorf("GitDir = %q, want %q", r.GitDir, want)
			}
			if r.Remote != "origin" || r.DefaultBranch != "main" || r.Slug != "" {
				t.Errorf("got remote=%q default=%q slug=%q", r.Remote, r.DefaultBranch, r.Slug)
			}
			if r.MainRef() != "refs/remotes/origin/main" || r.MainDisplayRef() != "origin/main" {
				t.Errorf("MainRef=%q display=%q", r.MainRef(), r.MainDisplayRef())
			}
		})
	}

	if _, err := gitscan.Open(context.Background(), env.Root); err == nil {
		t.Error("Open outside a repository should fail")
	}
}

func TestOpenRemoteAndDefaultBranch(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(env *gittest.Env, work, origin string)
		wantRemote string
		wantBranch string
		wantSlug   string
		wantErr    bool
	}{
		{
			name:       "origin HEAD set",
			prepare:    func(env *gittest.Env, work, _ string) { env.Git(work, "remote", "set-head", "origin", "main") },
			wantRemote: "origin", wantBranch: "main",
		},
		{
			name:       "fallback to main",
			prepare:    func(*gittest.Env, string, string) {},
			wantRemote: "origin", wantBranch: "main",
		},
		{
			name: "fallback to master",
			prepare: func(env *gittest.Env, work, _ string) {
				env.Git(work, "push", "-q", "origin", "main:master")
				env.Git(work, "update-ref", "-d", "refs/remotes/origin/main")
			},
			wantRemote: "origin", wantBranch: "master",
		},
		{
			name: "HEAD points to trunk",
			prepare: func(env *gittest.Env, work, _ string) {
				env.Git(work, "push", "-q", "origin", "main:trunk")
				env.Git(work, "fetch", "-q", "origin")
				env.Git(work, "remote", "set-head", "origin", "trunk")
			},
			wantRemote: "origin", wantBranch: "trunk",
		},
		{
			name: "no origin, first remote is github",
			prepare: func(env *gittest.Env, work, origin string) {
				env.Git(work, "remote", "rename", "origin", "gh")
				env.Git(work, "remote", "set-url", "gh", "https://github.com/acme/widgets.git")
			},
			wantRemote: "gh", wantBranch: "main", wantSlug: "acme/widgets",
		},
		{
			name: "no remote at all",
			prepare: func(env *gittest.Env, work, _ string) {
				env.Git(work, "remote", "remove", "origin")
			},
			wantRemote: "", wantBranch: "main",
		},
		{
			name: "remote without main or master",
			prepare: func(env *gittest.Env, work, _ string) {
				env.Git(work, "push", "-q", "origin", "main:dev")
				env.Git(work, "update-ref", "-d", "refs/remotes/origin/main")
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, origin, work := setup(t)
			tt.prepare(env, work, origin)
			r, err := gitscan.Open(context.Background(), work)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", r)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Remote != tt.wantRemote || r.DefaultBranch != tt.wantBranch || r.Slug != tt.wantSlug {
				t.Errorf("got remote=%q branch=%q slug=%q, want %q %q %q",
					r.Remote, r.DefaultBranch, r.Slug, tt.wantRemote, tt.wantBranch, tt.wantSlug)
			}
		})
	}
}

func TestScanAndHelpers(t *testing.T) {
	env, origin, work := setup(t)
	ctx := context.Background()
	c1 := env.SHA(work, "HEAD")
	env.Git(work, "remote", "set-head", "origin", "main")

	env.Git(work, "switch", "-q", "-c", "feat/a")
	a1 := env.Commit(work, "a1")
	a2 := env.Commit(work, "a2")
	env.Git(work, "push", "-q", "origin", "feat/a")
	env.Git(work, "switch", "-q", "main")
	m2 := env.Commit(work, "m2")
	env.Git(work, "push", "-q", "origin", "main")

	// Remote-only branch.
	env.Git(work, "switch", "-q", "-c", "feat/remote", c1)
	r1 := env.Commit(work, "r1")
	env.Git(work, "push", "-q", "origin", "feat/remote")
	env.Git(work, "switch", "-q", "main")
	env.Git(work, "branch", "-q", "-D", "feat/remote")

	// Local branch "main" must be excluded; so must origin/HEAD and origin/main.
	env.Git(work, "branch", "feat/wt", c1)
	wtDir := env.Path("wt")
	env.Git(work, "worktree", "add", "-q", wtDir, "feat/wt")
	if err := os.WriteFile(filepath.Join(wtDir, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.Git(work, "worktree", "add", "-q", "--detach", env.Path("detached"), c1)

	// Orphan branch with unrelated history.
	env.Git(work, "switch", "-q", "--orphan", "gh-pages")
	orphan := env.Commit(work, "pages")
	env.Git(work, "switch", "-q", "main")
	_ = origin

	r, err := gitscan.Open(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.MainRef != "origin/main" || s.MainHead.SHA != m2 || s.MainHead.Subject != "m2" {
		t.Errorf("main = %q %+v", s.MainRef, s.MainHead)
	}
	if s.MainHead.Time.IsZero() || len(s.MainHead.Parents) != 1 || s.MainHead.Parents[0] != c1 {
		t.Errorf("main head info = %+v", s.MainHead)
	}

	local := map[string]string{}
	for _, ref := range s.Local {
		local[ref.Name] = ref.SHA
		if ref.CommitTime.IsZero() {
			t.Errorf("local %s has zero time", ref.Name)
		}
	}
	wantLocal := map[string]string{"feat/a": a2, "feat/wt": c1, "gh-pages": orphan}
	if !equalMaps(local, wantLocal) {
		t.Errorf("local = %v, want %v", local, wantLocal)
	}
	remote := map[string]string{}
	for _, ref := range s.Remote {
		remote[ref.Name] = ref.SHA
	}
	wantRemote := map[string]string{"feat/a": a2, "feat/remote": r1}
	if !equalMaps(remote, wantRemote) {
		t.Errorf("remote = %v, want %v", remote, wantRemote)
	}

	if len(s.Worktrees) != 3 {
		t.Fatalf("worktrees = %+v", s.Worktrees)
	}
	main := s.Worktrees[0]
	var wt, det gitscan.Worktree
	for _, w := range s.Worktrees {
		switch w.Path {
		case wtDir:
			wt = w
		case env.Path("detached"):
			det = w
		}
	}
	if main.Path != work || !main.IsMain || main.Branch != "main" || main.Dirty {
		t.Errorf("main worktree = %+v", main)
	}
	if wt.Path != wtDir || wt.IsMain || wt.Branch != "feat/wt" || !wt.Dirty {
		t.Errorf("linked worktree = %+v", wt)
	}
	if det.Path == "" || det.Branch != "" || det.HEAD != c1 || det.Dirty {
		t.Errorf("detached worktree = %+v", det)
	}

	// Helpers.
	if mb, err := r.MergeBase(ctx, a2, m2); err != nil || mb != c1 {
		t.Errorf("MergeBase = %q, %v", mb, err)
	}
	if mb, err := r.MergeBase(ctx, orphan, m2); err != nil || mb != "" {
		t.Errorf("MergeBase(unrelated) = %q, %v", mb, err)
	}
	if n, err := r.Count(ctx, c1, a2); err != nil || n != 2 {
		t.Errorf("Count = %d, %v", n, err)
	}
	if n, err := r.CountAll(ctx, m2); err != nil || n != 3 {
		t.Errorf("CountAll = %d, %v", n, err)
	}
	if l, rr, err := r.LeftRight(ctx, a2, m2); err != nil || l != 2 || rr != 1 {
		t.Errorf("LeftRight = %d %d %v", l, rr, err)
	}
	for _, tc := range []struct {
		a, b string
		want bool
	}{{c1, m2, true}, {m2, m2, true}, {a1, m2, false}, {m2, c1, false}} {
		if got, err := r.IsAncestor(ctx, tc.a, tc.b); err != nil || got != tc.want {
			t.Errorf("IsAncestor(%s,%s) = %v %v", tc.a[:7], tc.b[:7], got, err)
		}
	}
	if _, err := r.IsAncestor(ctx, "0123456789012345678901234567890123456789", m2); err == nil {
		t.Error("IsAncestor(unknown) should fail")
	}
	if !r.HasCommit(ctx, a1) || r.HasCommit(ctx, "0123456789012345678901234567890123456789") {
		t.Error("HasCommit wrong")
	}
	infos, err := r.Commits(ctx, a2, c1, a2)
	if err != nil || len(infos) != 2 || infos[0].SHA != a2 || infos[1].SHA != c1 ||
		infos[0].Subject != "a2" || infos[0].ShortSHA == "" || !infos[0].Time.After(infos[1].Time) {
		t.Errorf("Commits = %+v, %v", infos, err)
	}
	merged, err := r.RefsMergedInto(ctx, m2)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := merged["refs/heads/feat/wt"]; !ok {
		t.Errorf("feat/wt should be merged into main: %v", merged)
	}
	if _, ok := merged["refs/heads/feat/a"]; ok {
		t.Errorf("feat/a should not be merged: %v", merged)
	}
	if _, ok := merged["refs/remotes/origin/HEAD"]; ok {
		t.Errorf("symbolic origin/HEAD must be excluded")
	}

	if err := r.Fetch(ctx); err != nil {
		t.Errorf("Fetch: %v", err)
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestForEach(t *testing.T) {
	var running, peak, calls int32
	err := gitscan.ForEach(context.Background(), 50, func(i int) error {
		n := atomic.AddInt32(&running, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		atomic.AddInt32(&calls, 1)
		return nil
	})
	if err != nil || calls != 50 || peak > gitscan.MaxParallel || peak < 2 {
		t.Errorf("err=%v calls=%d peak=%d", err, calls, peak)
	}

	boom := errors.New("boom")
	calls = 0
	err = gitscan.ForEach(context.Background(), 1000, func(i int) error {
		atomic.AddInt32(&calls, 1)
		if i == 3 {
			return boom
		}
		time.Sleep(time.Millisecond)
		return nil
	})
	if !errors.Is(err, boom) || calls >= 1000 {
		t.Errorf("err=%v calls=%d", err, calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := gitscan.ForEach(ctx, 5, func(int) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx: err=%v", err)
	}
}
