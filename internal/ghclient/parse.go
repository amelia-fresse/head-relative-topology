package ghclient

import (
	"cmp"
	"slices"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

// Check verdicts, as documented on model.PullRequest.Checks.
const (
	checksPassing = "passing"
	checksFailing = "failing"
	checksPending = "pending"
	checksNone    = "none"
)

type rawAuthor struct {
	Login string `json:"login"`
}

// rawCheck is one element of statusCheckRollup. It is either a CheckRun
// (name/status/conclusion) or a StatusContext (context/state).
type rawCheck struct {
	TypeName   string `json:"__typename"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Context    string `json:"context"`
	State      string `json:"state"`
}

type rawOpenPR struct {
	Number            int        `json:"number"`
	Title             string     `json:"title"`
	URL               string     `json:"url"`
	Author            *rawAuthor `json:"author"`
	IsDraft           bool       `json:"isDraft"`
	BaseRefName       string     `json:"baseRefName"`
	HeadRefName       string     `json:"headRefName"`
	HeadRefOid        string     `json:"headRefOid"`
	ReviewDecision    string     `json:"reviewDecision"`
	StatusCheckRollup []rawCheck `json:"statusCheckRollup"`
	Mergeable         string     `json:"mergeable"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	IsCrossRepository bool       `json:"isCrossRepository"`
}

type rawMergedPR struct {
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	HeadRefName string     `json:"headRefName"`
	Author      *rawAuthor `json:"author"`
	MergedAt    time.Time  `json:"mergedAt"`
	MergeCommit *struct {
		Oid string `json:"oid"`
	} `json:"mergeCommit"`
}

// parseOpenPRs decodes `gh pr list --json ...` output for open PRs and drops
// cross-repository PRs.
func parseOpenPRs(data []byte) ([]model.PullRequest, error) {
	var raw []rawOpenPR
	if err := decodeJSON(data, &raw); err != nil {
		return nil, err
	}
	prs := make([]model.PullRequest, 0, len(raw))
	for _, r := range raw {
		if r.IsCrossRepository {
			continue
		}
		checks, failing := aggregateChecks(r.StatusCheckRollup)
		mergeable := r.Mergeable
		if mergeable == "" {
			mergeable = "UNKNOWN"
		}
		prs = append(prs, model.PullRequest{
			Number:         r.Number,
			Title:          r.Title,
			URL:            r.URL,
			Author:         authorLogin(r.Author),
			IsDraft:        r.IsDraft,
			BaseRef:        r.BaseRefName,
			HeadRef:        r.HeadRefName,
			HeadSHA:        r.HeadRefOid,
			ReviewDecision: r.ReviewDecision,
			Checks:         checks,
			FailingChecks:  failing,
			Mergeable:      mergeable,
			UpdatedAt:      r.UpdatedAt,
		})
	}
	return prs, nil
}

// parseMergedPRs decodes `gh pr list --state merged --json ...` output and
// returns the PRs sorted newest first (by merge time, then number).
func parseMergedPRs(data []byte) ([]model.MergedPR, error) {
	var raw []rawMergedPR
	if err := decodeJSON(data, &raw); err != nil {
		return nil, err
	}
	merged := make([]model.MergedPR, 0, len(raw))
	for _, r := range raw {
		var sha string
		if r.MergeCommit != nil {
			sha = r.MergeCommit.Oid
		}
		merged = append(merged, model.MergedPR{
			Number:         r.Number,
			Title:          r.Title,
			URL:            r.URL,
			HeadRef:        r.HeadRefName,
			Author:         authorLogin(r.Author),
			MergedAt:       r.MergedAt,
			MergeCommitSHA: sha,
		})
	}
	slices.SortStableFunc(merged, func(a, b model.MergedPR) int {
		if c := b.MergedAt.Compare(a.MergedAt); c != 0 {
			return c
		}
		return cmp.Compare(b.Number, a.Number)
	})
	return merged, nil
}

// aggregateChecks collapses a statusCheckRollup into a single verdict and the
// names of the failing checks. Precedence: failing, then pending, then
// passing, otherwise none. Items with an unrecognised __typename are ignored.
// failing is never nil.
func aggregateChecks(rollup []rawCheck) (string, []string) {
	failing := []string{}
	seen := map[string]bool{}
	var anyFailing, anyPending bool
	recognised := 0
	allPassing := true

	fail := func(name string) {
		anyFailing = true
		if !seen[name] {
			seen[name] = true
			failing = append(failing, name)
		}
	}

	for _, c := range rollup {
		switch c.TypeName {
		case "CheckRun":
			recognised++
			if c.Status != "COMPLETED" {
				anyPending = true
				allPassing = false
				break
			}
			switch c.Conclusion {
			case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
				fail(c.Name)
			case "SUCCESS", "NEUTRAL", "SKIPPED":
			default:
				allPassing = false
			}
		case "StatusContext":
			recognised++
			switch c.State {
			case "FAILURE", "ERROR":
				fail(c.Context)
			case "PENDING", "EXPECTED":
				anyPending = true
			}
			if c.State != "SUCCESS" {
				allPassing = false
			}
		}
	}

	switch {
	case anyFailing:
		return checksFailing, failing
	case anyPending:
		return checksPending, failing
	case recognised > 0 && allPassing:
		return checksPassing, failing
	default:
		return checksNone, failing
	}
}

func authorLogin(a *rawAuthor) string {
	if a == nil {
		return ""
	}
	return a.Login
}
