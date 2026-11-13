// Package ghclient reads pull request data through the GitHub CLI (`gh`).
// It never handles tokens: authentication is whatever `gh auth` already has.
package ghclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/amelia-fresse/head-relative-topology/internal/execx"
	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

// Runner executes a command in dir and returns its trimmed stdout.
// execx.Run has this shape; tests substitute a fake.
type Runner func(ctx context.Context, dir, name string, args ...string) (string, error)

// openPRLimit caps how many open PRs are fetched in one call.
const openPRLimit = 200

const (
	openPRFields   = "number,title,url,author,isDraft,baseRefName,headRefName,headRefOid,reviewDecision,statusCheckRollup,mergeable,updatedAt,isCrossRepository"
	mergedPRFields = "number,title,url,headRefName,author,mergedAt,mergeCommit"
)

// Client queries GitHub for the repository at repoDir. gh infers the
// repository from that directory's git remote.
type Client struct {
	repoDir  string
	run      Runner
	lookPath func(string) (string, error)
}

// New returns a Client that runs gh through execx.
func New(repoDir string) *Client {
	return NewWithRunner(repoDir, execx.Run)
}

// NewWithRunner returns a Client that runs gh through r.
func NewWithRunner(repoDir string, r Runner) *Client {
	return &Client{repoDir: repoDir, run: r, lookPath: execx.LookPath}
}

// Available reports whether gh is installed and logged in.
func (c *Client) Available(ctx context.Context) error {
	if _, err := c.lookPath("gh"); err != nil {
		return errors.New("gh not found on PATH")
	}
	if _, err := c.run(ctx, c.repoDir, "gh", "auth", "status"); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("gh is not logged in: run `gh auth login`: %w", err)
	}
	return nil
}

// OpenPRs lists open pull requests in the repository. Fork PRs (cross
// repository) are skipped because their head branch is not on origin.
func (c *Client) OpenPRs(ctx context.Context) ([]model.PullRequest, error) {
	out, err := c.run(ctx, c.repoDir, "gh",
		"pr", "list",
		"--state", "open",
		"--limit", fmt.Sprint(openPRLimit),
		"--json", openPRFields,
	)
	if err != nil {
		return nil, fmt.Errorf("list open PRs: %w", err)
	}
	return parseOpenPRs([]byte(out))
}

// MergedPRs lists up to limit merged pull requests, newest first.
func (c *Client) MergedPRs(ctx context.Context, limit int) ([]model.MergedPR, error) {
	if limit < 1 {
		return nil, fmt.Errorf("list merged PRs: limit must be positive, got %d", limit)
	}
	out, err := c.run(ctx, c.repoDir, "gh",
		"pr", "list",
		"--state", "merged",
		"--limit", fmt.Sprint(limit),
		"--json", mergedPRFields,
	)
	if err != nil {
		return nil, fmt.Errorf("list merged PRs: %w", err)
	}
	return parseMergedPRs([]byte(out))
}

// decodeJSON is shared by the parsers so empty output gives a clear error
// instead of a bare "unexpected end of JSON input".
func decodeJSON(data []byte, v any) error {
	if len(data) == 0 {
		return errors.New("gh returned no output")
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode gh output: %w", err)
	}
	return nil
}
