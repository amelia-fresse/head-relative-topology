// Package t3 reads T3 Code agent threads through the `t3` CLI, so the map can
// show which branches an agent is working on and what state it is in.
//
// `t3 thread list --json` has no branch, so each thread is looked up once
// with `t3 thread show --json` and cached until its updatedAt changes.
package t3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
	// Settled: the user marked the thread done in T3 Code.
	Settled bool
	// AwaitingWork: the agent paused its turn until a background task it
	// started, or another thread, finishes. It resumes on its own.
	AwaitingWork bool
	UpdatedAt    time.Time
}

// State folds T3's statuses into what the map shows.
func (t Thread) State() string {
	s := strings.ToLower(t.Status)
	switch {
	case t.PendingCount > 0 || strings.Contains(s, "input") || strings.Contains(s, "approval"):
		return model.AgentWaiting
	case t.ActiveRun || s == "running" || s == "starting" || s == "queued":
		return model.AgentRunning
	case strings.Contains(s, "waiting"):
		// T3's "waiting" is usually the agent waiting on its own background
		// work; only with nothing to wait on is it waiting for you.
		if t.AwaitingWork {
			return model.AgentRunning
		}
		return model.AgentWaiting
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
	ThreadID            string `json:"threadId"`
	Settled             bool   `json:"settled"`
	BlockedOnThreadWait bool   `json:"blockedOnThreadWait"`
	BackgroundTasks     []struct {
		Awaited bool `json:"awaited"`
	} `json:"pendingBackgroundTasks"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// awaitingWork reports whether the agent paused its turn on background work.
func (it listItem) awaitingWork() bool {
	if it.BlockedOnThreadWait {
		return true
	}
	for _, bt := range it.BackgroundTasks {
		if bt.Awaited {
			return true
		}
	}
	return false
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
		// Fresh on every list, even for a cached show.
		t.Settled = items[i].Settled
		t.AwaitingWork = items[i].awaitingWork()
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

// Start starts a thread in project (a path inside it) with message as its
// first prompt, in a new worktree based on baseRef, and returns its id. T3
// puts the worktree on a temporary branch, since baseRef's branch may be
// checked out elsewhere.
func (c *Client) Start(ctx context.Context, project, title, baseRef, message string) (string, error) {
	out, err := c.run(ctx, "thread", "start", project, message, "--title", title, "--worktree", baseRef, "--json")
	if err != nil {
		// The command line holds the whole prompt; T3's own message is enough.
		var xe *execx.Error
		if errors.As(err, &xe) {
			return "", fmt.Errorf("t3: %s", xe.Stderr)
		}
		return "", err
	}
	var r struct {
		ThreadID string `json:"threadId"`
	}
	if err := json.Unmarshal(out, &r); err != nil || r.ThreadID == "" {
		return "", fmt.Errorf("t3 thread start: unexpected output %q", strings.TrimSpace(string(out)))
	}
	return r.ThreadID, nil
}

// Attach returns branches with Thread set to the most recently updated
// thread working on each branch: same branch name, same worktree, or started
// for it (owners maps thread id to branch name, for threads hrt started in a
// worktree of their own).
func Attach(branches []model.Branch, threads []Thread, owners map[string]string) []model.Branch {
	out := make([]model.Branch, len(branches))
	for i, b := range branches {
		b.Thread = nil
		var best *Thread
		for j := range threads {
			t := &threads[j]
			match := (t.Branch != "" && t.Branch == b.Name) || ownerOf(t, owners) == b.Name
			if !match && t.WorktreePath != "" && b.Worktree != nil {
				match = samePath(t.WorktreePath, b.Worktree.Path)
			}
			if match && (best == nil || t.UpdatedAt.After(best.UpdatedAt)) {
				best = t
			}
		}
		if best != nil {
			b.Thread = best.agent()
		}
		b.CopyOf, b.Copy = "", nil
		out[i] = b
	}
	linkCopies(out, threads, owners)
	return out
}

// ownerOf is the branch a thread was started for: from owners (this
// session) or, after a restart, from the title hrt gives rebase threads.
func ownerOf(t *Thread, owners map[string]string) string {
	if b := owners[t.ID]; b != "" {
		return b
	}
	if m := rebaseTitle.FindStringSubmatch(t.Title); m != nil {
		return m[1]
	}
	return ""
}

// rebaseTitle matches the title of threads started by Service.RebaseOnMain.
var rebaseTitle = regexp.MustCompile(`^Rebase (\S+) onto \S+$`)

// linkCopies marks the branch checked out in each owned thread's worktree as
// a working copy of the branch the thread was started for.
func linkCopies(branches []model.Branch, threads []Thread, owners map[string]string) {
	index := make(map[string]int, len(branches))
	for i, b := range branches {
		index[b.Name] = i
	}
	for _, t := range threads {
		owner, ok := index[ownerOf(&t, owners)]
		if !ok {
			continue
		}
		for i := range branches {
			c := &branches[i]
			if i == owner || c.CopyOf != "" {
				continue
			}
			match := t.Branch != "" && t.Branch == c.Name
			if !match && t.WorktreePath != "" && c.Worktree != nil {
				match = samePath(t.WorktreePath, c.Worktree.Path)
			}
			if !match {
				continue
			}
			c.CopyOf = branches[owner].Name
			branches[owner].Copy = &model.BranchCopy{Name: c.Name, Ahead: c.Ahead, Behind: c.Behind, Dirty: c.Worktree != nil && c.Worktree.Dirty}
			break
		}
	}
}

// AttachMain returns a copy of local with Thread set to the most urgent
// unsettled thread in the main checkout and ActiveThreads to how many there
// are. Settled threads are left out: every thread ever started on main would
// match otherwise.
func AttachMain(local *model.LocalMain, threads []Thread) *model.LocalMain {
	if local == nil {
		return nil
	}
	l := *local
	l.Thread, l.ActiveThreads = nil, 0
	var best *Thread
	for j := range threads {
		t := &threads[j]
		if t.Settled {
			continue
		}
		match := t.Branch != "" && t.Branch == l.Name
		if !match && t.WorktreePath != "" && l.Worktree != nil {
			match = samePath(t.WorktreePath, l.Worktree.Path)
		}
		if !match {
			continue
		}
		l.ActiveThreads++
		if best == nil || urgentFirst(t, best) {
			best = t
		}
	}
	if best != nil {
		l.Thread = best.agent()
	}
	return &l
}

// urgency orders agent states for AttachMain: needs you, working, failed, idle.
var urgency = map[string]int{model.AgentWaiting: 0, model.AgentRunning: 1, model.AgentFailed: 2, model.AgentIdle: 3}

// urgentFirst reports whether a should be shown before b: the more urgent
// state, then the most recently updated.
func urgentFirst(a, b *Thread) bool {
	ua, ub := urgency[a.State()], urgency[b.State()]
	if ua != ub {
		return ua < ub
	}
	return a.UpdatedAt.After(b.UpdatedAt)
}

func (t *Thread) agent() *model.AgentThread {
	link := t.Link
	if app := AppLink(link); app != "" && hasDesktopApp() {
		link = app
	}
	return &model.AgentThread{
		ID:        t.ID,
		Title:     t.Title,
		State:     t.State(),
		Link:      link,
		UpdatedAt: t.UpdatedAt,
	}
}

// AppLink turns a thread's web link (https://app.t3.codes/threads/<env>/<id>)
// into the T3 Code desktop app's deep link (t3code://threads/<env>/<id>): the
// URL the web page's "Open in desktop app" button opens. It returns "" when
// link is not a thread link.
func AppLink(link string) string {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "threads" || parts[1] == "" || parts[2] == "" {
		return ""
	}
	return "t3code://threads/" + parts[1] + "/" + parts[2]
}

// hasDesktopApp reports whether the T3 Code desktop app handles t3code://
// links. On Linux that is a registered x-scheme-handler; macOS and Windows
// register the scheme when the app is installed, so they are assumed to.
var hasDesktopApp = sync.OnceValue(func() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	out, err := execx.RunRaw(context.Background(), "", "xdg-mime", "query", "default", "x-scheme-handler/t3code")
	return err == nil && strings.TrimSpace(string(out)) != ""
})

func samePath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}
