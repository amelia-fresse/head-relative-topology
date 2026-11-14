// Package t3 reads T3 Code agent threads through the `t3` CLI, so the map can
// show which branches an agent is working on and what state it is in.
//
// `t3 thread list --json` has no branch, so each thread is looked up once
// with `t3 thread show --json` and cached until its updatedAt changes.
package t3

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/execx"
	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

// maxThreads bounds how many of a project's most recent threads are looked up.
const maxThreads = 40

// Find returns the t3 CLI to use, or "" when T3 Code isn't installed.
func Find() string {
	if p := os.Getenv("T3CODE_CLI_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := execx.LookPath("t3"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		name := "t3"
		if runtime.GOOS == "windows" {
			name = "t3.cmd"
		}
		p := filepath.Join(home, ".t3", "bin", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Thread is the part of a T3 thread the map uses.
type Thread struct {
	ID           string
	Title        string
	Status       string
	Link         string
	Branch       string
	WorktreePath string
	ActiveRun    bool
	PendingCount int
	Archived     bool
	UpdatedAt    time.Time
}

// State folds T3's statuses into what the map shows.
func (t Thread) State() string {
	s := strings.ToLower(t.Status)
	switch {
	case t.PendingCount > 0 || strings.Contains(s, "waiting") || strings.Contains(s, "input") || strings.Contains(s, "approval"):
		return model.AgentWaiting
	case t.ActiveRun || s == "running" || s == "starting" || s == "queued":
		return model.AgentRunning
	case s == "failed" || s == "error" || s == "interrupted":
		return model.AgentFailed
	default:
		return model.AgentIdle
	}
}

// Runner runs the CLI with args and returns stdout.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// Client lists threads and caches per-thread details.
type Client struct {
	run   Runner
	mu    sync.Mutex
	cache map[string]Thread
}

// New returns a client for the CLI at path.
func New(path string) *Client {
	return NewWithRunner(func(ctx context.Context, args ...string) ([]byte, error) {
		return execx.RunRaw(ctx, "", path, args...)
	})
}

// NewWithRunner is New with an injected runner (tests).
func NewWithRunner(run Runner) *Client {
	return &Client{run: run, cache: map[string]Thread{}}
}

type listItem struct {
	ThreadID  string    `json:"threadId"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type showOut struct {
	Thread struct {
		ThreadID            string    `json:"threadId"`
		Title               string    `json:"title"`
		Link                string    `json:"link"`
		Status              string    `json:"status"`
		ActiveRunID         *string   `json:"activeRunId"`
		Branch              *string   `json:"branch"`
		WorktreePath        *string   `json:"worktreePath"`
		PendingRequestCount int       `json:"pendingRequestCount"`
		Archived            bool      `json:"archived"`
		UpdatedAt           time.Time `json:"updatedAt"`
	} `json:"thread"`
}

// Threads returns the project's recent threads. project is a path inside the
// project (the repository path). A folder that is not a T3 project yields an
// error from the CLI.
func (c *Client) Threads(ctx context.Context, project string) ([]Thread, error) {
	out, err := c.run(ctx, "thread", "list", "--project", project, "--json")
	if err != nil {
		return nil, err
	}
	var items []listItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("t3 thread list: %w", err)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if len(items) > maxThreads {
		items = items[:maxThreads]
	}

	threads := make([]Thread, len(items))
	errs := make([]error, len(items))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, it := range items {
		c.mu.Lock()
		cached, ok := c.cache[it.ThreadID]
		c.mu.Unlock()
		if ok && cached.UpdatedAt.Equal(it.UpdatedAt) {
			threads[i] = cached
			continue
		}
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			threads[i], errs[i] = c.show(ctx, id)
		}(i, it.ThreadID)
	}
	wg.Wait()

	kept := threads[:0]
	for i, t := range threads {
		if errs[i] != nil || t.ID == "" || t.Archived {
			continue // a thread deleted between list and show, or archived
		}
		kept = append(kept, t)
	}
	return kept, ctx.Err()
}

func (c *Client) show(ctx context.Context, id string) (Thread, error) {
	out, err := c.run(ctx, "thread", "show", id, "--messages", "0", "--json")
	if err != nil {
		return Thread{}, err
	}
	var s showOut
	if err := json.Unmarshal(out, &s); err != nil {
		return Thread{}, fmt.Errorf("t3 thread show: %w", err)
	}
	r := s.Thread
	t := Thread{
		ID:           r.ThreadID,
		Title:        r.Title,
		Status:       r.Status,
		Link:         r.Link,
		ActiveRun:    r.ActiveRunID != nil && *r.ActiveRunID != "",
		PendingCount: r.PendingRequestCount,
		Archived:     r.Archived,
		UpdatedAt:    r.UpdatedAt,
	}
	if r.Branch != nil {
		t.Branch = *r.Branch
	}
	if r.WorktreePath != nil {
		t.WorktreePath = *r.WorktreePath
	}
	c.mu.Lock()
	c.cache[id] = t
	c.mu.Unlock()
	return t, nil
}

// Attach returns branches with Thread set to the most recently updated
// thread working on each branch (same branch name, or same worktree).
func Attach(branches []model.Branch, threads []Thread) []model.Branch {
	out := make([]model.Branch, len(branches))
	for i, b := range branches {
		b.Thread = nil
		var best *Thread
		for j := range threads {
			t := &threads[j]
			match := t.Branch != "" && t.Branch == b.Name
			if !match && t.WorktreePath != "" && b.Worktree != nil {
				match = samePath(t.WorktreePath, b.Worktree.Path)
			}
			if match && (best == nil || t.UpdatedAt.After(best.UpdatedAt)) {
				best = t
			}
		}
		if best != nil {
			b.Thread = &model.AgentThread{
				ID:        best.ID,
				Title:     best.Title,
				State:     best.State(),
				Link:      best.Link,
				UpdatedAt: best.UpdatedAt,
			}
		}
		out[i] = b
	}
	return out
}

func samePath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}
