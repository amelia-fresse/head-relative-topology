package snapshot_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/gitscan"
	"github.com/amelia-fresse/head-relative-topology/internal/gitscan/gittest"
	"github.com/amelia-fresse/head-relative-topology/internal/model"
	"github.com/amelia-fresse/head-relative-topology/internal/snapshot"
)

// fixture is a repository with a known topology.
//
//	main: c0 - c1 - c2 - c2b - c3 - c4(squash #5) - m1(#6) - c5 - c5b - c6
//	feat/old      from c1: o1                      (local, hidden by rule)
//	feat/a        from c2: a1 a2                   (pushed, PR #1)
//	feat/e        from a2: e1                      (pushed, PR #2 base feat/a)
//	feat/b        from c3: b1                      (local, checked out in main worktree)
//	feat/b-child  from b1: bc1 bc2                 (local, stacked by ancestry)
//	feat/squashed from c3: s1                      (pushed, squash-merged as c4)
//	feat/merged   from c4: m1                      (fast-forwarded into main)
//	feat/d        from c5: d1 (pushed) d2 (local)  (PR #3, CI failing)
//	feat/f        from c5: f1 (local) f2 (remote)  (PR #4, approved)
//	feat/wt       from c5: w1                      (linked worktree, dirty)
//	feat/c        from c6: k1                      (remote only, draft PR #9)
//	feat/x, feat/y from c6: x1 / y1                (PRs whose bases form a cycle)
//	gh-pages      orphan                           (unrelated history)
type fixture struct {
	env                                      *gittest.Env
	work, wtDir                              string
	c0, c1, c2, c2b, c3, c4, m1, c5, c5b, c6 string
	o1, a2, e1, b1, bc2, s1, d2, f1, w1, k1  string
	x1, y1                                   string
}

func newFixture(t *testing.T) *fixture {
	env := gittest.New(t)
	f := &fixture{env: env}
	origin := env.InitBare("origin.git")
	w := env.Init("work")
	f.work = w
	g := func(args ...string) string { return env.Git(w, args...) }
	commit := func(msg string) string { return env.Commit(w, msg) }

	f.c0 = commit("c0")
	f.c1 = commit("c1")
	g("remote", "add", "origin", origin)

	g("switch", "-q", "-c", "feat/old")
	f.o1 = commit("o1")
	g("switch", "-q", "main")
	f.c2 = commit("c2")

	g("switch", "-q", "-c", "feat/a")
	commit("a1")
	f.a2 = commit("a2")
	g("switch", "-q", "-c", "feat/e")
	f.e1 = commit("e1")
	g("push", "-q", "origin", "feat/a", "feat/e")

	g("switch", "-q", "main")
	f.c2b = commit("c2b")
	f.c3 = commit("c3")

	g("switch", "-q", "-c", "feat/b")
	f.b1 = commit("b1")
	g("switch", "-q", "-c", "feat/b-child")
	commit("bc1")
	f.bc2 = commit("bc2")

	g("switch", "-q", "-c", "feat/squashed", f.c3)
	f.s1 = commit("s1")
	g("push", "-q", "origin", "feat/squashed")

	g("switch", "-q", "main")
	f.c4 = commit("feat: squashed (#5)")

	g("switch", "-q", "-c", "feat/merged")
	f.m1 = commit("m1")
	g("switch", "-q", "main")
	g("merge", "-q", "--ff-only", "feat/merged")
	f.c5 = commit("c5")

	g("switch", "-q", "-c", "feat/d")
	commit("d1")
	g("push", "-q", "origin", "feat/d")
	f.d2 = commit("d2")

	g("switch", "-q", "-c", "feat/f", f.c5)
	f.f1 = commit("f1")
	commit("f2")
	g("push", "-q", "origin", "feat/f")
	g("reset", "-q", "--hard", f.f1)

	g("switch", "-q", "-c", "feat/wt", f.c5)
	f.w1 = commit("w1")

	g("switch", "-q", "main")
	f.c5b = commit("c5b")
	f.c6 = commit("c6")
	g("push", "-q", "origin", "main")
	g("remote", "set-head", "origin", "main")

	g("switch", "-q", "-c", "feat/c")
	f.k1 = commit("k1")
	g("push", "-q", "origin", "feat/c")
	g("switch", "-q", "-c", "feat/x", f.c6)
	f.x1 = commit("x1")
	g("switch", "-q", "-c", "feat/y", f.c6)
	f.y1 = commit("y1")

	g("switch", "-q", "--orphan", "gh-pages")
	commit("pages")

	g("switch", "-q", "feat/b")
	g("branch", "-q", "-D", "feat/c")

	f.wtDir = env.Path("wt")
	g("worktree", "add", "-q", f.wtDir, "feat/wt")
	if err := os.WriteFile(filepath.Join(f.wtDir, "dirty.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) open(t *testing.T) (*gitscan.Repo, *gitscan.Scan) {
	t.Helper()
	ctx := context.Background()
	// Open from inside the linked worktree on purpose.
	r, err := gitscan.Open(ctx, f.wtDir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return r, s
}

var updated = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

func openPRs(f *fixture) []model.PullRequest {
	return []model.PullRequest{
		{Number: 1, HeadRef: "feat/a", BaseRef: "main", HeadSHA: f.a2, Checks: "passing", Mergeable: "MERGEABLE", ReviewDecision: "REVIEW_REQUIRED", UpdatedAt: updated},
		{Number: 2, HeadRef: "feat/e", BaseRef: "feat/a", HeadSHA: f.e1, Checks: "failing", Mergeable: "CONFLICTING", UpdatedAt: updated},
		{Number: 3, HeadRef: "feat/d", BaseRef: "main", Checks: "failing", FailingChecks: []string{"lint"}, ReviewDecision: "APPROVED", Mergeable: "MERGEABLE", UpdatedAt: updated},
		{Number: 4, HeadRef: "feat/f", BaseRef: "main", Checks: "passing", ReviewDecision: "APPROVED", Mergeable: "MERGEABLE", UpdatedAt: updated},
		{Number: 9, HeadRef: "feat/c", BaseRef: "main", IsDraft: true, Mergeable: "CONFLICTING", Checks: "failing", UpdatedAt: updated},
		{Number: 10, HeadRef: "feat/x", BaseRef: "feat/y", Checks: "pending", UpdatedAt: updated},
		{Number: 11, HeadRef: "feat/y", BaseRef: "feat/x", Checks: "none", UpdatedAt: updated},
		// A fork PR with the same head name but another SHA, updated more
		// recently: the SHA match must win.
		{Number: 12, HeadRef: "feat/a", BaseRef: "main", HeadSHA: "ffffffffffffffffffffffffffffffffffffffff", IsDraft: true, UpdatedAt: updated.Add(time.Hour)},
	}
}

func mergedPRs(f *fixture) []model.MergedPR {
	return []model.MergedPR{
		{Number: 5, HeadRef: "feat/squashed", MergeCommitSHA: f.c4, MergedAt: updated},
		{Number: 6, HeadRef: "feat/merged", MergeCommitSHA: f.m1, MergedAt: updated},
		// Merge commit not fetched locally: skipped.
		{Number: 7, HeadRef: "feat/ghost", MergeCommitSHA: "0123456789012345678901234567890123456789", MergedAt: updated},
		// Commit exists but is not on main: skipped.
		{Number: 8, HeadRef: "feat/elsewhere", MergeCommitSHA: f.s1, MergedAt: updated},
		// No merge commit at all.
		{Number: 13, HeadRef: "feat/none", MergedAt: updated},
	}
}

func hideOld(b model.Branch) string {
	// Only hide when the branch is fully computed (proves Hide runs last).
	if b.Name == "feat/old" && b.Behind == 8 && b.Ahead == 1 && b.Status == model.StatusNoPR {
		return "stale"
	}
	return ""
}

type wantBranch struct {
	local, remote      bool
	parent, base       string
	ahead, behind      int
	unpushed, unpulled int
	status             model.Status
	pr                 int
	wt                 string // "", "main", "dirty"
	hidden             string
}

type wantAnchor struct {
	sha      string
	behind   int
	head     bool
	branches []string
	merged   []int
}

func TestBuild(t *testing.T) {
	f := newFixture(t)
	repo, scan := f.open(t)

	tests := []struct {
		name         string
		in           snapshot.Input
		wantBranches map[string]wantBranch
		wantAnchors  []wantAnchor
	}{
		{
			name: "with GitHub data",
			in: snapshot.Input{
				OpenPRs:        openPRs(f),
				MergedPRs:      mergedPRs(f),
				MergedHeadRefs: map[string]bool{"feat/squashed": true, "feat/merged": true, "feat/a": true},
				Hide:           hideOld,
				Warnings:       []string{"gh: rate limited"},
			},
			wantBranches: map[string]wantBranch{
				"feat/old":     {local: true, base: f.c1, ahead: 1, behind: 8, status: model.StatusNoPR, hidden: "stale"},
				"feat/a":       {local: true, remote: true, base: f.c2, ahead: 2, behind: 7, status: model.StatusReview, pr: 1},
				"feat/e":       {local: true, remote: true, parent: "feat/a", base: f.a2, ahead: 1, behind: 7, status: model.StatusConflicts, pr: 2},
				"feat/b":       {local: true, base: f.c3, ahead: 1, behind: 5, status: model.StatusNoPR, wt: "main"},
				"feat/b-child": {local: true, parent: "feat/b", base: f.b1, ahead: 2, behind: 5, status: model.StatusNoPR},
				"feat/d":       {local: true, remote: true, base: f.c5, ahead: 2, behind: 2, unpushed: 1, status: model.StatusCIFailing, pr: 3},
				"feat/f":       {local: true, remote: true, base: f.c5, ahead: 1, behind: 2, unpulled: 1, status: model.StatusApproved, pr: 4},
				"feat/wt":      {local: true, base: f.c5, ahead: 1, behind: 2, status: model.StatusNoPR, wt: "dirty"},
				"feat/c":       {remote: true, base: f.c6, ahead: 1, behind: 0, status: model.StatusDraft, pr: 9},
				// PR bases form a cycle x→y→x; it is broken at feat/x.
				"feat/x": {local: true, base: f.c6, ahead: 1, behind: 0, status: model.StatusReview, pr: 10},
				"feat/y": {local: true, parent: "feat/x", base: f.c6, ahead: 1, behind: 0, status: model.StatusReview, pr: 11},
			},
			wantAnchors: []wantAnchor{
				{sha: f.c6, behind: 0, head: true, branches: []string{"feat/c", "feat/x"}},
				{sha: f.c5, behind: 2, branches: []string{"feat/d", "feat/f", "feat/wt"}},
				{sha: f.m1, behind: 3, merged: []int{6}},
				{sha: f.c4, behind: 4, merged: []int{5}},
				{sha: f.c3, behind: 5, branches: []string{"feat/b"}},
				{sha: f.c2, behind: 7, branches: []string{"feat/a"}},
				{sha: f.c1, behind: 8, branches: []string{"feat/old"}},
			},
		},
		{
			name: "without GitHub data",
			in:   snapshot.Input{},
			wantBranches: map[string]wantBranch{
				"feat/old": {local: true, base: f.c1, ahead: 1, behind: 8, status: model.StatusNoPR},
				"feat/a":   {local: true, remote: true, base: f.c2, ahead: 2, behind: 7, status: model.StatusNoPR},
				// Stacking found by ancestry alone.
				"feat/e":       {local: true, remote: true, parent: "feat/a", base: f.a2, ahead: 1, behind: 7, status: model.StatusNoPR},
				"feat/b":       {local: true, base: f.c3, ahead: 1, behind: 5, status: model.StatusNoPR, wt: "main"},
				"feat/b-child": {local: true, parent: "feat/b", base: f.b1, ahead: 2, behind: 5, status: model.StatusNoPR},
				// Squash-merged leftover cannot be detected without GitHub.
				"feat/squashed": {local: true, remote: true, base: f.c3, ahead: 1, behind: 5, status: model.StatusNoPR},
				"feat/d":        {local: true, remote: true, base: f.c5, ahead: 2, behind: 2, unpushed: 1, status: model.StatusNoPR},
				"feat/f":        {local: true, remote: true, base: f.c5, ahead: 1, behind: 2, unpulled: 1, status: model.StatusNoPR},
				"feat/wt":       {local: true, base: f.c5, ahead: 1, behind: 2, status: model.StatusNoPR, wt: "dirty"},
				"feat/c":        {remote: true, base: f.c6, ahead: 1, behind: 0, status: model.StatusNoPR},
				"feat/x":        {local: true, base: f.c6, ahead: 1, behind: 0, status: model.StatusNoPR},
				"feat/y":        {local: true, base: f.c6, ahead: 1, behind: 0, status: model.StatusNoPR},
			},
			wantAnchors: []wantAnchor{
				{sha: f.c6, behind: 0, head: true, branches: []string{"feat/c", "feat/x", "feat/y"}},
				{sha: f.c5, behind: 2, branches: []string{"feat/d", "feat/f", "feat/wt"}},
				{sha: f.c3, behind: 5, branches: []string{"feat/b", "feat/squashed"}},
				{sha: f.c2, behind: 7, branches: []string{"feat/a"}},
				{sha: f.c1, behind: 8, branches: []string{"feat/old"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			in.Repo, in.Scan = repo, scan
			in.FetchedAt = updated
			snap, err := snapshot.Build(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}

			// Repo & main.
			if snap.Repo.Path != f.work || snap.Repo.Remote != "origin" || snap.Repo.DefaultBranch != "main" {
				t.Errorf("repo = %+v", snap.Repo)
			}
			if snap.Main.Ref != "origin/main" || snap.Main.Head.SHA != f.c6 || snap.Main.Head.Subject != "c6" {
				t.Errorf("main = %q %+v", snap.Main.Ref, snap.Main.Head)
			}
			if !snap.Main.OlderHidden {
				t.Error("OlderHidden should be true (c1 has parent c0)")
			}
			if !snap.FetchedAt.Equal(updated) || snap.GeneratedAt.IsZero() {
				t.Errorf("times: fetched=%v generated=%v", snap.FetchedAt, snap.GeneratedAt)
			}
			if !reflect.DeepEqual(snap.Warnings, append([]string{}, in.Warnings...)) {
				t.Errorf("warnings = %q", snap.Warnings)
			}

			// Branches.
			got := map[string]model.Branch{}
			for _, b := range snap.Branches {
				got[b.Name] = b
			}
			for name := range got {
				if _, ok := tt.wantBranches[name]; !ok {
					t.Errorf("unexpected branch %q", name)
				}
			}
			for name, w := range tt.wantBranches {
				b, ok := got[name]
				if !ok {
					t.Errorf("missing branch %q", name)
					continue
				}
				checkBranch(t, f, b, w)
			}
			for i := 1; i < len(snap.Branches); i++ {
				a, b := snap.Branches[i-1], snap.Branches[i]
				if a.Behind > b.Behind || (a.Behind == b.Behind && a.Name >= b.Name) {
					t.Errorf("branches not sorted: %s(%d) before %s(%d)", a.Name, a.Behind, b.Name, b.Behind)
				}
			}

			// Anchors.
			var gotA []wantAnchor
			for _, a := range snap.Main.Anchors {
				nums := []int{}
				for _, m := range a.Merged {
					nums = append(nums, m.Number)
				}
				gotA = append(gotA, wantAnchor{sha: a.Commit.SHA, behind: a.Behind, head: a.IsHead, branches: a.BranchNames, merged: nums})
				if a.Commit.ShortSHA == "" || a.Commit.Time.IsZero() || a.Commit.Subject == "" {
					t.Errorf("anchor commit incomplete: %+v", a.Commit)
				}
			}
			norm := func(ws []wantAnchor) []wantAnchor {
				out := make([]wantAnchor, len(ws))
				for i, w := range ws {
					if w.branches == nil {
						w.branches = []string{}
					}
					if w.merged == nil {
						w.merged = []int{}
					}
					out[i] = w
				}
				return out
			}
			if !reflect.DeepEqual(norm(gotA), norm(tt.wantAnchors)) {
				t.Errorf("anchors:\n got  %s\n want %s", fmtAnchors(f, gotA), fmtAnchors(f, tt.wantAnchors))
			}

			// Invariant: collapsed commits between consecutive anchors ≥ 0.
			for i := 1; i < len(snap.Main.Anchors); i++ {
				newer, older := snap.Main.Anchors[i-1], snap.Main.Anchors[i]
				if c := older.Behind - newer.Behind - 1; c < 0 {
					t.Errorf("negative collapsed count %d between %s and %s", c, newer.Commit.ShortSHA, older.Commit.ShortSHA)
				}
			}
			// Every main-based branch has a divergence anchor at its BaseSHA.
			anchorBranches := map[string]string{}
			for _, a := range snap.Main.Anchors {
				for _, n := range a.BranchNames {
					anchorBranches[n] = a.Commit.SHA
				}
			}
			for _, b := range snap.Branches {
				if b.Parent == "" && anchorBranches[b.Name] != b.BaseSHA {
					t.Errorf("branch %s base %s has no matching anchor", b.Name, b.BaseSHA)
				}
				if b.Parent != "" && anchorBranches[b.Name] != "" {
					t.Errorf("stacked branch %s listed on an anchor", b.Name)
				}
			}

			// JSON never has null slices.
			js, err := json.Marshal(snap)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"branches", "warnings", "anchors", "branchNames", "merged", "failingChecks"} {
				if strings.Contains(string(js), `"`+key+`":null`) {
					t.Errorf("JSON has null %s", key)
				}
			}
		})
	}
}

func checkBranch(t *testing.T, f *fixture, b model.Branch, w wantBranch) {
	t.Helper()
	gotWT := ""
	if b.Worktree != nil {
		switch {
		case b.Worktree.IsMain && !b.Worktree.Dirty && b.Worktree.Path == f.work:
			gotWT = "main"
		case !b.Worktree.IsMain && b.Worktree.Dirty && b.Worktree.Path == f.wtDir:
			gotWT = "dirty"
		default:
			gotWT = "unexpected"
		}
	}
	pr := 0
	if b.PR != nil {
		pr = b.PR.Number
	}
	got := wantBranch{
		local: b.Local, remote: b.Remote, parent: b.Parent, base: b.BaseSHA,
		ahead: b.Ahead, behind: b.Behind, unpushed: b.Unpushed, unpulled: b.Unpulled,
		status: b.Status, pr: pr, wt: gotWT, hidden: b.HiddenReason,
	}
	if got != w {
		t.Errorf("branch %s:\n got  %+v\n want %+v", b.Name, got, w)
	}
	if b.Hidden != (w.hidden != "") {
		t.Errorf("branch %s: Hidden=%v", b.Name, b.Hidden)
	}
	if b.TipSHA == "" || b.LastCommitAt.IsZero() {
		t.Errorf("branch %s: tip=%q last=%v", b.Name, b.TipSHA, b.LastCommitAt)
	}
	if b.Name == "feat/f" && b.TipSHA != f.f1 {
		t.Errorf("feat/f tip should be the local SHA")
	}
	if b.Name == "feat/c" && b.TipSHA != f.k1 {
		t.Errorf("feat/c tip should be the remote SHA")
	}
}

func fmtAnchors(f *fixture, as []wantAnchor) string {
	names := map[string]string{f.c0: "c0", f.c1: "c1", f.c2: "c2", f.c2b: "c2b", f.c3: "c3", f.c4: "c4", f.m1: "m1", f.c5: "c5", f.c5b: "c5b", f.c6: "c6"}
	var sb strings.Builder
	for _, a := range as {
		n := names[a.sha]
		if n == "" {
			n = a.sha[:7]
		}
		sb.WriteString(n)
		sb.WriteString("{")
		sb.WriteString(strings.Join(a.branches, ","))
		sb.WriteString("}")
		if a.head {
			sb.WriteString("H")
		}
		sb.WriteString(" ")
	}
	return sb.String()
}

func TestBuildRequiresInput(t *testing.T) {
	if _, err := snapshot.Build(context.Background(), snapshot.Input{}); err == nil {
		t.Error("expected error without Repo/Scan")
	}
}

// TestBuildEmpty: only main exists → a single HEAD anchor, empty slices.
func TestBuildEmpty(t *testing.T) {
	env := gittest.New(t)
	origin := env.InitBare("origin.git")
	w := env.Init("work")
	c0 := env.Commit(w, "c0")
	env.Git(w, "remote", "add", "origin", origin)
	env.Git(w, "push", "-q", "origin", "main")

	r, err := gitscan.Open(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := snapshot.Build(context.Background(), snapshot.Input{Repo: r, Scan: s})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Branches == nil || len(snap.Branches) != 0 || snap.Warnings == nil {
		t.Errorf("branches=%v warnings=%v", snap.Branches, snap.Warnings)
	}
	if len(snap.Main.Anchors) != 1 || !snap.Main.Anchors[0].IsHead || snap.Main.Anchors[0].Commit.SHA != c0 || snap.Main.OlderHidden {
		t.Errorf("anchors=%+v olderHidden=%v", snap.Main.Anchors, snap.Main.OlderHidden)
	}
}

func TestStatusPriority(t *testing.T) {
	// Status derivation through Build is covered in TestBuild; this table
	// documents the priority order exhaustively via a tiny repo.
	env := gittest.New(t)
	origin := env.InitBare("origin.git")
	w := env.Init("work")
	env.Commit(w, "c0")
	env.Git(w, "remote", "add", "origin", origin)
	env.Git(w, "push", "-q", "origin", "main")
	env.Git(w, "switch", "-q", "-c", "b")
	env.Commit(w, "b1")

	r, err := gitscan.Open(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		pr   *model.PullRequest
		want model.Status
	}{
		{nil, model.StatusNoPR},
		{&model.PullRequest{IsDraft: true, Mergeable: "CONFLICTING", Checks: "failing", ReviewDecision: "APPROVED"}, model.StatusDraft},
		{&model.PullRequest{Mergeable: "CONFLICTING", Checks: "failing", ReviewDecision: "APPROVED"}, model.StatusConflicts},
		{&model.PullRequest{Mergeable: "MERGEABLE", Checks: "failing", ReviewDecision: "APPROVED"}, model.StatusCIFailing},
		{&model.PullRequest{Mergeable: "UNKNOWN", Checks: "pending", ReviewDecision: "APPROVED"}, model.StatusApproved},
		{&model.PullRequest{Mergeable: "MERGEABLE", Checks: "passing", ReviewDecision: "CHANGES_REQUESTED"}, model.StatusReview},
		{&model.PullRequest{}, model.StatusReview},
	}
	for _, tt := range tests {
		var prs []model.PullRequest
		if tt.pr != nil {
			p := *tt.pr
			p.HeadRef, p.BaseRef, p.Number = "b", "main", 1
			prs = []model.PullRequest{p}
		}
		snap, err := snapshot.Build(context.Background(), snapshot.Input{Repo: r, Scan: s, OpenPRs: prs})
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Branches) != 1 || snap.Branches[0].Status != tt.want {
			t.Errorf("pr=%+v: got %+v, want %s", tt.pr, snap.Branches, tt.want)
		}
	}
}
