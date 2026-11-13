package gitscan

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

// MaxParallel bounds the number of concurrent git processes started by
// ForEach.
const MaxParallel = 8

// CommitInfo describes one commit.
type CommitInfo struct {
	SHA      string
	ShortSHA string
	Subject  string
	// Time is the committer time.
	Time    time.Time
	Parents []string
}

// Model converts to the frontend commit type.
func (c CommitInfo) Model() model.Commit {
	return model.Commit{SHA: c.SHA, ShortSHA: c.ShortSHA, Subject: c.Subject, Time: c.Time}
}

// commitsChunk keeps command lines well under the Windows 32K limit.
const commitsChunk = 200

// Commits returns info for each rev (SHA or ref), in the given order with
// duplicates removed. It fails if any rev does not resolve to a commit.
func (r *Repo) Commits(ctx context.Context, revs ...string) ([]CommitInfo, error) {
	var out []CommitInfo
	for start := 0; start < len(revs); start += commitsChunk {
		end := min(start+commitsChunk, len(revs))
		args := append([]string{"log", "--no-walk=unsorted",
			"--format=%H%x00%h%x00%ct%x00%P%x00%s", "--end-of-options"}, revs[start:end]...)
		args = append(args, "--")
		res, err := r.git(ctx, args...)
		if err != nil {
			return nil, err
		}
		for _, line := range splitLines(res) {
			f := strings.SplitN(line, "\x00", 5)
			if len(f) != 5 {
				return nil, fmt.Errorf("git log: unexpected line %q", line)
			}
			out = append(out, CommitInfo{
				SHA:      f[0],
				ShortSHA: f[1],
				Time:     parseUnix(f[2]),
				Parents:  strings.Fields(f[3]),
				Subject:  f[4],
			})
		}
	}
	return out, nil
}

// MergeBase returns the best common ancestor of a and b, or "" when the
// histories are unrelated.
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	out, err := r.git(ctx, "merge-base", "--end-of-options", a, b)
	if err != nil {
		if exitCode(err) == 1 && strings.TrimSpace(out) == "" {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Count returns the number of commits in from..to (reachable from to, not
// from from).
func (r *Repo) Count(ctx context.Context, from, to string) (int, error) {
	out, err := r.git(ctx, "rev-list", "--count", "--end-of-options", from+".."+to)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

// CountAll returns the number of commits reachable from rev.
func (r *Repo) CountAll(ctx context.Context, rev string) (int, error) {
	out, err := r.git(ctx, "rev-list", "--count", "--end-of-options", rev)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

// LeftRight returns the commits only in a and only in b (a...b).
func (r *Repo) LeftRight(ctx context.Context, a, b string) (left, right int, err error) {
	out, err := r.git(ctx, "rev-list", "--left-right", "--count", "--end-of-options", a+"..."+b)
	if err != nil {
		return 0, 0, err
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("rev-list --left-right: unexpected output %q", out)
	}
	if left, err = strconv.Atoi(f[0]); err != nil {
		return 0, 0, err
	}
	if right, err = strconv.Atoi(f[1]); err != nil {
		return 0, 0, err
	}
	return left, right, nil
}

// IsAncestor reports whether a is an ancestor of (or equal to) b. An unknown
// commit is an error.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	_, err := r.git(ctx, "merge-base", "--is-ancestor", "--end-of-options", a, b)
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

// HasCommit reports whether sha names a commit present in the repository.
func (r *Repo) HasCommit(ctx context.Context, sha string) bool {
	_, err := r.git(ctx, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// RefsMergedInto returns every branch ref (local and remote-tracking for
// Remote) whose tip is reachable from rev, as full ref name → tip SHA. This
// answers "is-ancestor" for all branches in a single git call.
func (r *Repo) RefsMergedInto(ctx context.Context, rev string) (map[string]string, error) {
	args := []string{"for-each-ref", "--merged=" + rev,
		"--format=%(refname)%00%(objectname)%00%(symref)", "refs/heads/"}
	if r.Remote != "" {
		args = append(args, "refs/remotes/"+r.Remote+"/")
	}
	out, err := r.git(ctx, args...)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for _, line := range splitLines(out) {
		f := strings.Split(line, "\x00")
		if len(f) == 3 && f[2] == "" {
			m[f[0]] = f[1]
		}
	}
	return m, nil
}

// ForEach calls fn(i) for i in [0,n) with at most MaxParallel calls running
// at once. It returns the first error; after an error (or ctx cancellation)
// no new calls are started.
func ForEach(ctx context.Context, n int, fn func(i int) error) error {
	if n == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, MaxParallel)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for i := 0; i < n; i++ {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(i); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				cancel()
			}
		}(i)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}
