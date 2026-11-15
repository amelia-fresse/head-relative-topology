package t3

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

type fake struct {
	mu    sync.Mutex
	list  string
	shows map[string]string
	calls map[string]int
}

func (f *fake) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.Join(args[:2], " ")
	if key == "thread list" {
		return []byte(f.list), nil
	}
	id := args[2]
	f.calls[id]++
	if s, ok := f.shows[id]; ok {
		return []byte(s), nil
	}
	return nil, errors.New("no such thread")
}

func show(id, status, branch, wt string, active bool, pending int, updated string) string {
	opt := func(v string) string {
		if v == "" {
			return "null"
		}
		return fmt.Sprintf("%q", v)
	}
	run := "null"
	if active {
		run = `"run:1"`
	}
	return fmt.Sprintf(`{"thread":{"threadId":%q,"title":"T %s","link":"https://t3/%s","status":%q,"activeRunId":%s,"branch":%s,"worktreePath":%s,"pendingRequestCount":%d,"archived":false,"updatedAt":%q},"messages":[]}`,
		id, id, id, status, run, opt(branch), opt(wt), pending, updated)
}

func TestThreadsCachesAndAttaches(t *testing.T) {
	f := &fake{
		list: `[{"threadId":"a","updatedAt":"2026-11-14T10:00:00Z"},{"threadId":"b","updatedAt":"2026-11-14T11:00:00Z"},{"threadId":"gone","updatedAt":"2026-11-14T09:00:00Z"},{"threadId":"c","updatedAt":"2026-11-14T08:00:00Z"}]`,
		shows: map[string]string{
			"a": show("a", "completed", "feat/x", "", false, 0, "2026-11-14T10:00:00Z"),
			"b": show("b", "completed", "", "/wt/y", true, 0, "2026-11-14T11:00:00Z"),
			"c": show("c", "completed", "feat/x", "", false, 2, "2026-11-14T08:00:00Z"),
		},
		calls: map[string]int{},
	}
	c := NewWithRunner(f.run)
	ts, err := c.Threads(context.Background(), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 3 || ts[0].ID != "b" {
		t.Fatalf("threads = %+v", ts)
	}
	if _, err := c.Threads(context.Background(), "/repo"); err != nil {
		t.Fatal(err)
	}
	if f.calls["a"] != 1 || f.calls["b"] != 1 {
		t.Errorf("unchanged threads re-fetched: %v", f.calls)
	}

	branches := []model.Branch{
		{Name: "feat/x"},
		{Name: "feat/y", Worktree: &model.Worktree{Path: "/wt/y/"}},
		{Name: "feat/z", Thread: &model.AgentThread{ID: "stale"}},
	}
	got := Attach(branches, ts, nil)
	if got[0].Thread == nil || got[0].Thread.ID != "a" || got[0].Thread.State != model.AgentIdle {
		t.Errorf("feat/x thread = %+v (want the newer thread a)", got[0].Thread)
	}
	if got[1].Thread == nil || got[1].Thread.ID != "b" || got[1].Thread.State != model.AgentRunning {
		t.Errorf("feat/y thread = %+v", got[1].Thread)
	}
	if got[2].Thread != nil {
		t.Errorf("feat/z thread = %+v, want nil", got[2].Thread)
	}
	if branches[2].Thread == nil {
		t.Error("Attach modified its input")
	}
	owned := Attach(branches, ts, map[string]string{"c": "feat/z"})
	if owned[2].Thread == nil || owned[2].Thread.ID != "c" {
		t.Errorf("feat/z thread = %+v, want c (started for it)", owned[2].Thread)
	}
}

func TestState(t *testing.T) {
	cases := []struct {
		t    Thread
		want string
	}{
		{Thread{Status: "running"}, model.AgentRunning},
		{Thread{Status: "completed", ActiveRun: true}, model.AgentRunning},
		{Thread{Status: "completed", PendingCount: 1}, model.AgentWaiting},
		{Thread{Status: "waiting_for_input"}, model.AgentWaiting},
		{Thread{Status: "waiting"}, model.AgentWaiting},
		{Thread{Status: "waiting", AwaitingWork: true}, model.AgentRunning},
		{Thread{Status: "waiting", AwaitingWork: true, PendingCount: 1}, model.AgentWaiting},
		{Thread{Status: "failed"}, model.AgentFailed},
		{Thread{Status: "interrupted"}, model.AgentFailed},
		{Thread{Status: "completed"}, model.AgentIdle},
		{Thread{Status: "something_new"}, model.AgentIdle},
	}
	for _, c := range cases {
		if got := c.t.State(); got != c.want {
			t.Errorf("%+v: State() = %s, want %s", c.t, got, c.want)
		}
	}
}

func TestAttachMain(t *testing.T) {
	at := func(s string) time.Time {
		tm, _ := time.Parse(time.RFC3339, s)
		return tm
	}
	ts := []Thread{
		{ID: "settled", Branch: "main", Status: "running", Settled: true, UpdatedAt: at("2026-11-14T14:00:00Z")},
		{ID: "idle", Branch: "main", Status: "completed", UpdatedAt: at("2026-11-14T12:00:00Z")},
		{ID: "run", Branch: "main", Status: "running", UpdatedAt: at("2026-11-14T11:00:00Z")},
		{ID: "wait", WorktreePath: "/repo", Status: "completed", PendingCount: 1, UpdatedAt: at("2026-11-14T09:00:00Z")},
		{ID: "elsewhere", Branch: "feat/x", Status: "running", UpdatedAt: at("2026-11-14T13:00:00Z")},
	}
	local := &model.LocalMain{Name: "main", Worktree: &model.Worktree{Path: "/repo/"}}
	got := AttachMain(local, ts)
	if got.Thread == nil || got.Thread.ID != "wait" || got.ActiveThreads != 3 {
		t.Errorf("main = %+v / %d, want the waiting thread of 3 unsettled", got.Thread, got.ActiveThreads)
	}
	if local.Thread != nil {
		t.Error("AttachMain modified its input")
	}
	if got := AttachMain(local, ts[:2]); got.Thread == nil || got.Thread.ID != "idle" || got.ActiveThreads != 1 {
		t.Errorf("idle main = %+v / %d, want the unsettled idle thread", got.Thread, got.ActiveThreads)
	}
	if got := AttachMain(local, ts[:1]); got.Thread != nil || got.ActiveThreads != 0 {
		t.Errorf("settled-only main = %+v, want no thread", got.Thread)
	}
	if AttachMain(nil, ts) != nil {
		t.Error("nil local main gained a value")
	}
}

func TestAppLink(t *testing.T) {
	cases := map[string]string{
		"https://app.t3.codes/threads/env-1/thread-2":  "t3code://threads/env-1/thread-2",
		"https://app.t3.codes/threads/env-1/thread-2/": "t3code://threads/env-1/thread-2",
		"https://app.t3.codes/threads/env-1":           "",
		"https://app.t3.codes/settings":                "",
		"t3code://threads/env-1/thread-2":              "",
		"":                                             "",
	}
	for in, want := range cases {
		if got := AppLink(in); got != want {
			t.Errorf("AppLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStart(t *testing.T) {
	var got []string
	c := NewWithRunner(func(_ context.Context, args ...string) ([]byte, error) {
		got = args
		return []byte(`{"threadId":"t-1","runId":"r-1"}`), nil
	})
	id, err := c.Start(context.Background(), "/repo", "Rebase x", "feat/x", "do it")
	if err != nil || id != "t-1" {
		t.Fatalf("Start = %q, %v", id, err)
	}
	want := "thread start /repo do it --title Rebase x --worktree feat/x --json"
	if strings.Join(got, " ") != want {
		t.Errorf("args = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestAttachLinksWorkingCopy(t *testing.T) {
	branches := []model.Branch{
		{Name: "feat/x", PR: &model.PullRequest{Number: 7}},
		{Name: "t3/rebase-x", Ahead: 3, Worktree: &model.Worktree{Path: "/wt/r", Dirty: true}},
		{Name: "other"},
	}
	ts := []Thread{{ID: "r", Branch: "t3/rebase-x", WorktreePath: "/wt/r", Status: "running"}}
	got := Attach(branches, ts, map[string]string{"r": "feat/x"})
	if got[1].CopyOf != "feat/x" {
		t.Errorf("copy's CopyOf = %q, want feat/x", got[1].CopyOf)
	}
	if c := got[0].Copy; c == nil || c.Name != "t3/rebase-x" || c.Ahead != 3 || !c.Dirty {
		t.Errorf("feat/x Copy = %+v", c)
	}
	if got[0].Thread == nil || got[0].Thread.ID != "r" {
		t.Errorf("feat/x thread = %+v, want r", got[0].Thread)
	}
	if got[2].CopyOf != "" || got[2].Copy != nil {
		t.Errorf("unrelated branch linked: %+v", got[2])
	}
	if again := Attach(got, nil, nil); again[1].CopyOf != "" || again[0].Copy != nil {
		t.Error("links survive once the thread is gone")
	}
	ts[0].Title = "Rebase feat/x onto origin/main"
	if byTitle := Attach(branches, ts, nil); byTitle[1].CopyOf != "feat/x" {
		t.Error("a rebase thread's title doesn't link its copy after a restart")
	}
}
