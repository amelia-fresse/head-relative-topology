package app

import (
	"context"
	"testing"

	"github.com/amelia-fresse/head-relative-topology/internal/gitscan/gittest"
)

func newRepo(t *testing.T, env *gittest.Env, name string) string {
	t.Helper()
	origin := env.InitBare(name + ".git")
	w := env.Init(name)
	env.Commit(w, "c0")
	env.Git(w, "remote", "add", "origin", origin)
	env.Git(w, "push", "-q", "origin", "main")
	env.Git(w, "remote", "set-head", "origin", "main")
	return w
}

func activePath(s *Service) string {
	for _, t := range s.Tabs() {
		if t.Active {
			return t.Path
		}
	}
	return ""
}

func TestTabs(t *testing.T) {
	env := gittest.New(t)
	a, b, c := newRepo(t, env, "a"), newRepo(t, env, "b"), newRepo(t, env, "c")
	s := New("", "")
	t.Cleanup(func() { _ = s.ServiceShutdown() })

	for _, p := range []string{a, b, c} {
		snap, err := s.OpenRepo(p)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Repo.Path != p {
			t.Fatalf("OpenRepo(%s) returned %s", p, snap.Repo.Path)
		}
	}
	// Reopening an open repo reuses its tab.
	if _, err := s.OpenRepo(a); err != nil {
		t.Fatal(err)
	}
	tabs := s.Tabs()
	if len(tabs) != 3 || activePath(s) != a || tabs[0].Name != "a" {
		t.Fatalf("tabs = %+v", tabs)
	}
	if got := s.GetSettings().OpenRepos; len(got) != 3 || got[2] != c {
		t.Errorf("OpenRepos = %v", got)
	}

	snap, err := s.SwitchRepo(b)
	if err != nil || snap.Repo.Path != b || activePath(s) != b {
		t.Fatalf("switch: %v %s %s", err, snap.Repo.Path, activePath(s))
	}
	if _, err := s.SwitchRepo("/nope"); err == nil {
		t.Error("switching to an unknown tab should fail")
	}

	// Closing the active tab activates the next one.
	snap, err = s.CloseRepo(b)
	if err != nil || snap.Repo.Path != c || activePath(s) != c {
		t.Fatalf("close b: %v %s", err, activePath(s))
	}
	// Closing the last tab in the list activates the previous one.
	if snap, _ = s.CloseRepo(c); snap.Repo.Path != a {
		t.Fatalf("close c: active %s", snap.Repo.Path)
	}
	if snap, _ = s.CloseRepo(a); snap.Repo.Path != "" || len(s.Tabs()) != 0 || s.GetSettings().LastRepo != "" {
		t.Fatalf("close a: %+v", s.Tabs())
	}
}

func TestBackgroundRebuild(t *testing.T) {
	env := gittest.New(t)
	a, b := newRepo(t, env, "a"), newRepo(t, env, "b")
	s := New("", "")
	t.Cleanup(func() { _ = s.ServiceShutdown() })
	ctx := context.Background()
	stA, err := s.addRepo(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	stB, _ := s.addRepo(ctx, b)
	s.activate(stA)

	env.Git(b, "switch", "-q", "-c", "feat/x")
	env.Commit(b, "x1")
	snap, err := s.rebuild(ctx, stB, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Branches) != 1 || stB.last == nil || activePath(s) != a {
		t.Fatalf("background rebuild: %+v", snap.Branches)
	}
}
