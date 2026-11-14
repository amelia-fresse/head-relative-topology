package t3

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

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
	got := Attach(branches, ts)
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
