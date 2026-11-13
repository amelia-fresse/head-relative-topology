package ghclient

import (
	"reflect"
	"testing"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

// openPRsFixture mirrors `gh pr list --state open --json ...` output: CheckRun
// and StatusContext items mixed, a null reviewDecision, a fork PR, an empty
// rollup and a PR whose mergeable field is missing.
const openPRsFixture = `[
  {
    "number": 412,
    "title": "Add SSO login",
    "url": "https://github.com/acme/app/pull/412",
    "author": {"login": "alice", "id": "MDQ6VXNlcjE=", "is_bot": false, "name": "Alice"},
    "isDraft": false,
    "baseRefName": "main",
    "headRefName": "feat/auth-sso",
    "headRefOid": "4f1c2e9a0b7d3c5e8f6a1b2c3d4e5f60718293a4",
    "reviewDecision": "APPROVED",
    "statusCheckRollup": [
      {"__typename": "CheckRun", "name": "unit", "status": "COMPLETED", "conclusion": "SUCCESS", "workflowName": "CI", "detailsUrl": "https://github.com/acme/app/actions/runs/1", "startedAt": "2026-11-12T09:00:00Z", "completedAt": "2026-11-12T09:04:00Z"},
      {"__typename": "StatusContext", "context": "ci/circleci: build", "state": "SUCCESS", "targetUrl": "https://circleci.com/gh/acme/app/1", "startedAt": "2026-11-12T09:00:00Z"}
    ],
    "mergeable": "MERGEABLE",
    "updatedAt": "2026-11-12T09:14:02Z",
    "isCrossRepository": false
  },
  {
    "number": 418,
    "title": "Rework billing export",
    "url": "https://github.com/acme/app/pull/418",
    "author": {"login": "bob", "is_bot": false, "name": "Bob"},
    "isDraft": true,
    "baseRefName": "main",
    "headRefName": "feat/billing-export",
    "headRefOid": "9b8a7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b",
    "reviewDecision": null,
    "statusCheckRollup": [
      {"__typename": "CheckRun", "name": "lint", "status": "COMPLETED", "conclusion": "FAILURE", "workflowName": "CI"},
      {"__typename": "CheckRun", "name": "e2e", "status": "COMPLETED", "conclusion": "TIMED_OUT", "workflowName": "CI"},
      {"__typename": "CheckRun", "name": "lint", "status": "COMPLETED", "conclusion": "FAILURE", "workflowName": "CI"},
      {"__typename": "StatusContext", "context": "deploy-preview", "state": "ERROR"},
      {"__typename": "CheckRun", "name": "docs", "status": "IN_PROGRESS", "conclusion": ""}
    ],
    "mergeable": "CONFLICTING",
    "updatedAt": "2026-11-11T17:02:45Z",
    "isCrossRepository": false
  },
  {
    "number": 421,
    "title": "Retry flaky webhook test",
    "url": "https://github.com/acme/app/pull/421",
    "author": {"login": "carol", "is_bot": false, "name": "Carol"},
    "isDraft": false,
    "baseRefName": "feat/auth-sso",
    "headRefName": "fix/webhook-retry",
    "headRefOid": "1111222233334444555566667777888899990000",
    "reviewDecision": "CHANGES_REQUESTED",
    "statusCheckRollup": [
      {"__typename": "CheckRun", "name": "unit", "status": "COMPLETED", "conclusion": "CANCELLED", "workflowName": "CI"},
      {"__typename": "StatusContext", "context": "ci/circleci: build", "state": "PENDING"}
    ],
    "mergeable": "UNKNOWN",
    "updatedAt": "2026-11-10T08:30:00Z",
    "isCrossRepository": false
  },
  {
    "number": 425,
    "title": "Pending integration run",
    "url": "https://github.com/acme/app/pull/425",
    "author": {"login": "dave", "is_bot": false, "name": "Dave"},
    "isDraft": false,
    "baseRefName": "main",
    "headRefName": "feat/integration",
    "headRefOid": "aaaabbbbccccddddeeeeffff0000111122223333",
    "reviewDecision": "REVIEW_REQUIRED",
    "statusCheckRollup": [
      {"__typename": "CheckRun", "name": "unit", "status": "COMPLETED", "conclusion": "SUCCESS", "workflowName": "CI"},
      {"__typename": "CheckRun", "name": "integration", "status": "QUEUED", "conclusion": ""},
      {"__typename": "StatusContext", "context": "deploy-preview", "state": "EXPECTED"}
    ],
    "mergeable": "MERGEABLE",
    "updatedAt": "2026-11-12T10:00:00Z",
    "isCrossRepository": false
  },
  {
    "number": 430,
    "title": "Fork contribution",
    "url": "https://github.com/acme/app/pull/430",
    "author": {"login": "eve", "is_bot": false, "name": "Eve"},
    "isDraft": false,
    "baseRefName": "main",
    "headRefName": "patch-1",
    "headRefOid": "5555666677778888999900001111222233334444",
    "reviewDecision": "",
    "statusCheckRollup": [],
    "mergeable": "MERGEABLE",
    "updatedAt": "2026-11-09T12:00:00Z",
    "isCrossRepository": true
  },
  {
    "number": 433,
    "title": "Docs only, no CI configured",
    "url": "https://github.com/acme/app/pull/433",
    "author": null,
    "isDraft": false,
    "baseRefName": "main",
    "headRefName": "docs/readme",
    "headRefOid": "7777888899990000aaaabbbbccccddddeeeeffff",
    "reviewDecision": "",
    "statusCheckRollup": [],
    "mergeable": "",
    "updatedAt": "2026-11-08T07:45:00Z",
    "isCrossRepository": false
  }
]`

// mergedPRsFixture mirrors `gh pr list --state merged --json ...` output. It
// is deliberately unsorted and includes a tie on mergedAt, a null author and
// a null mergeCommit.
const mergedPRsFixture = `[
  {
    "number": 405,
    "title": "Fix timezone parsing",
    "url": "https://github.com/acme/app/pull/405",
    "headRefName": "fix/tz",
    "author": {"login": "bob", "is_bot": false, "name": "Bob"},
    "mergedAt": "2026-11-10T16:20:00Z",
    "mergeCommit": {"oid": "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"}
  },
  {
    "number": 399,
    "title": "Old migration",
    "url": "https://github.com/acme/app/pull/399",
    "headRefName": "chore/migrate",
    "author": null,
    "mergedAt": "2026-11-01T09:00:00Z",
    "mergeCommit": null
  },
  {
    "number": 410,
    "title": "Squash-merged feature",
    "url": "https://github.com/acme/app/pull/410",
    "headRefName": "feat/export",
    "author": {"login": "alice", "is_bot": false, "name": "Alice"},
    "mergedAt": "2026-11-12T10:00:00Z",
    "mergeCommit": {"oid": "ffffeeeeddddccccbbbbaaaa9999888877776666"}
  },
  {
    "number": 408,
    "title": "Same-minute merge, lower number",
    "url": "https://github.com/acme/app/pull/408",
    "headRefName": "fix/lower",
    "author": {"login": "carol", "is_bot": false, "name": "Carol"},
    "mergedAt": "2026-11-10T16:20:00Z",
    "mergeCommit": {"oid": "0123456789abcdef0123456789abcdef01234567"}
  }
]`

func TestParseOpenPRs(t *testing.T) {
	prs, err := parseOpenPRs([]byte(openPRsFixture))
	if err != nil {
		t.Fatalf("parseOpenPRs: %v", err)
	}

	// The fork PR (430) must be dropped; the other five kept in order.
	wantNumbers := []int{412, 418, 421, 425, 433}
	got := make([]int, 0, len(prs))
	for _, p := range prs {
		got = append(got, p.Number)
	}
	if !reflect.DeepEqual(got, wantNumbers) {
		t.Fatalf("numbers = %v, want %v", got, wantNumbers)
	}

	byNumber := map[int]model.PullRequest{}
	for _, p := range prs {
		byNumber[p.Number] = p
	}

	t.Run("approved and passing", func(t *testing.T) {
		p := byNumber[412]
		want := model.PullRequest{
			Number:         412,
			Title:          "Add SSO login",
			URL:            "https://github.com/acme/app/pull/412",
			Author:         "alice",
			IsDraft:        false,
			BaseRef:        "main",
			HeadRef:        "feat/auth-sso",
			HeadSHA:        "4f1c2e9a0b7d3c5e8f6a1b2c3d4e5f60718293a4",
			ReviewDecision: "APPROVED",
			Checks:         "passing",
			FailingChecks:  []string{},
			Mergeable:      "MERGEABLE",
			UpdatedAt:      time.Date(2026, 11, 12, 9, 14, 2, 0, time.UTC),
		}
		if !reflect.DeepEqual(p, want) {
			t.Fatalf("got %+v\nwant %+v", p, want)
		}
	})

	t.Run("draft with failing checks and null reviewDecision", func(t *testing.T) {
		p := byNumber[418]
		if !p.IsDraft {
			t.Error("IsDraft = false, want true")
		}
		if p.ReviewDecision != "" {
			t.Errorf("ReviewDecision = %q, want empty for null", p.ReviewDecision)
		}
		if p.Checks != "failing" {
			t.Errorf("Checks = %q, want failing", p.Checks)
		}
		want := []string{"lint", "e2e", "deploy-preview"}
		if !reflect.DeepEqual(p.FailingChecks, want) {
			t.Errorf("FailingChecks = %v, want %v", p.FailingChecks, want)
		}
		if p.Mergeable != "CONFLICTING" {
			t.Errorf("Mergeable = %q, want CONFLICTING", p.Mergeable)
		}
	})

	t.Run("cancelled check counts as failing, pending stays behind", func(t *testing.T) {
		p := byNumber[421]
		if p.Checks != "failing" {
			t.Errorf("Checks = %q, want failing", p.Checks)
		}
		if !reflect.DeepEqual(p.FailingChecks, []string{"unit"}) {
			t.Errorf("FailingChecks = %v, want [unit]", p.FailingChecks)
		}
		if p.ReviewDecision != "CHANGES_REQUESTED" {
			t.Errorf("ReviewDecision = %q, want CHANGES_REQUESTED", p.ReviewDecision)
		}
		if p.BaseRef != "feat/auth-sso" {
			t.Errorf("BaseRef = %q, want stacked base feat/auth-sso", p.BaseRef)
		}
	})

	t.Run("pending when nothing failed", func(t *testing.T) {
		p := byNumber[425]
		if p.Checks != "pending" {
			t.Errorf("Checks = %q, want pending", p.Checks)
		}
		if p.FailingChecks == nil || len(p.FailingChecks) != 0 {
			t.Errorf("FailingChecks = %#v, want empty non-nil", p.FailingChecks)
		}
		if p.ReviewDecision != "REVIEW_REQUIRED" {
			t.Errorf("ReviewDecision = %q", p.ReviewDecision)
		}
	})

	t.Run("empty rollup, null author, missing mergeable", func(t *testing.T) {
		p := byNumber[433]
		if p.Checks != "none" {
			t.Errorf("Checks = %q, want none", p.Checks)
		}
		if p.FailingChecks == nil || len(p.FailingChecks) != 0 {
			t.Errorf("FailingChecks = %#v, want empty non-nil", p.FailingChecks)
		}
		if p.Author != "" {
			t.Errorf("Author = %q, want empty for null author", p.Author)
		}
		if p.Mergeable != "UNKNOWN" {
			t.Errorf("Mergeable = %q, want UNKNOWN default", p.Mergeable)
		}
	})
}

func TestParseOpenPRsEmptyList(t *testing.T) {
	prs, err := parseOpenPRs([]byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if prs == nil || len(prs) != 0 {
		t.Fatalf("got %#v, want empty non-nil slice", prs)
	}
}

func TestParseMergedPRs(t *testing.T) {
	merged, err := parseMergedPRs([]byte(mergedPRsFixture))
	if err != nil {
		t.Fatalf("parseMergedPRs: %v", err)
	}

	wantNumbers := []int{410, 408, 405, 399}
	got := make([]int, 0, len(merged))
	for _, m := range merged {
		got = append(got, m.Number)
	}
	if !reflect.DeepEqual(got, wantNumbers) {
		t.Fatalf("order = %v, want newest first %v", got, wantNumbers)
	}

	if merged[0].MergeCommitSHA != "ffffeeeeddddccccbbbbaaaa9999888877776666" {
		t.Errorf("MergeCommitSHA = %q", merged[0].MergeCommitSHA)
	}
	if merged[0].HeadRef != "feat/export" || merged[0].Author != "alice" {
		t.Errorf("unexpected fields: %+v", merged[0])
	}
	if want := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC); !merged[0].MergedAt.Equal(want) {
		t.Errorf("MergedAt = %v, want %v", merged[0].MergedAt, want)
	}

	last := merged[3]
	if last.Author != "" || last.MergeCommitSHA != "" {
		t.Errorf("null author/mergeCommit should give empty strings, got %+v", last)
	}
}

func TestParseMergedPRsEmptyList(t *testing.T) {
	merged, err := parseMergedPRs([]byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if merged == nil || len(merged) != 0 {
		t.Fatalf("got %#v, want empty non-nil slice", merged)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	if _, err := parseOpenPRs(nil); err == nil {
		t.Error("parseOpenPRs(nil): want error")
	}
	if _, err := parseMergedPRs([]byte(`{"not":"a list"}`)); err == nil {
		t.Error("parseMergedPRs(object): want error")
	}
}

func TestAggregateChecks(t *testing.T) {
	cr := func(name, status, conclusion string) rawCheck {
		return rawCheck{TypeName: "CheckRun", Name: name, Status: status, Conclusion: conclusion}
	}
	sc := func(context, state string) rawCheck {
		return rawCheck{TypeName: "StatusContext", Context: context, State: state}
	}

	tests := []struct {
		name        string
		rollup      []rawCheck
		wantChecks  string
		wantFailing []string
	}{
		{
			name:       "empty rollup",
			rollup:     nil,
			wantChecks: "none",
		},
		{
			name:       "all success mix",
			rollup:     []rawCheck{cr("unit", "COMPLETED", "SUCCESS"), sc("ci/build", "SUCCESS")},
			wantChecks: "passing",
		},
		{
			name: "neutral and skipped count as passing",
			rollup: []rawCheck{
				cr("docs", "COMPLETED", "NEUTRAL"),
				cr("optional", "COMPLETED", "SKIPPED"),
				cr("unit", "COMPLETED", "SUCCESS"),
			},
			wantChecks: "passing",
		},
		{
			name: "failure with duplicate names deduplicated in order",
			rollup: []rawCheck{
				cr("lint", "COMPLETED", "FAILURE"),
				cr("e2e", "COMPLETED", "TIMED_OUT"),
				cr("lint", "COMPLETED", "FAILURE"),
				sc("deploy", "ERROR"),
				sc("deploy", "FAILURE"),
			},
			wantChecks:  "failing",
			wantFailing: []string{"lint", "e2e", "deploy"},
		},
		{
			name: "each failing conclusion is a failure",
			rollup: []rawCheck{
				cr("a", "COMPLETED", "CANCELLED"),
				cr("b", "COMPLETED", "ACTION_REQUIRED"),
				cr("c", "COMPLETED", "STARTUP_FAILURE"),
			},
			wantChecks:  "failing",
			wantFailing: []string{"a", "b", "c"},
		},
		{
			name: "failure beats pending",
			rollup: []rawCheck{
				cr("integration", "IN_PROGRESS", ""),
				sc("deploy", "PENDING"),
				cr("lint", "COMPLETED", "FAILURE"),
			},
			wantChecks:  "failing",
			wantFailing: []string{"lint"},
		},
		{
			name: "queued CheckRun is pending",
			rollup: []rawCheck{
				cr("unit", "COMPLETED", "SUCCESS"),
				cr("integration", "QUEUED", ""),
			},
			wantChecks: "pending",
		},
		{
			name:       "expected StatusContext is pending",
			rollup:     []rawCheck{sc("deploy-preview", "EXPECTED")},
			wantChecks: "pending",
		},
		{
			name: "completed with unrecognised conclusion is none, not passing",
			rollup: []rawCheck{
				cr("unit", "COMPLETED", "SUCCESS"),
				cr("legacy", "COMPLETED", "STALE"),
			},
			wantChecks: "none",
		},
		{
			name: "unknown __typename items are ignored",
			rollup: []rawCheck{
				{TypeName: "Mystery", Name: "x", Status: "IN_PROGRESS"},
			},
			wantChecks: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks, failing := aggregateChecks(tt.rollup)
			if checks != tt.wantChecks {
				t.Errorf("checks = %q, want %q", checks, tt.wantChecks)
			}
			if failing == nil {
				t.Fatal("failing is nil, want non-nil slice")
			}
			want := tt.wantFailing
			if want == nil {
				want = []string{}
			}
			if !reflect.DeepEqual(failing, want) {
				t.Errorf("failing = %v, want %v", failing, want)
			}
		})
	}
}
