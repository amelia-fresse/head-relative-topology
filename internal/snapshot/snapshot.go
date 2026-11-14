// Package snapshot assembles a model.Snapshot from a git scan and GitHub PR
// data: it merges local and remote branches, drops merged ones, detects
// stacking, computes ahead/behind counts and collapses main into anchors.
package snapshot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amelia-fresse/head-relative-topology/internal/gitscan"
	"github.com/amelia-fresse/head-relative-topology/internal/model"
)

// Input is everything Build needs.
type Input struct {
	Repo *gitscan.Repo
	Scan *gitscan.Scan
	// OpenPRs come from GitHub; nil when gh is unavailable.
	OpenPRs []model.PullRequest
	// MergedPRs are recently merged PRs within the user's window; those that
	// landed on main become convergence anchors.
	MergedPRs []model.MergedPR
	// MergedHeadRefs are head branch names of recently merged PRs (a bigger
	// set than MergedPRs), used to drop squash-merged leftover branches.
	MergedHeadRefs map[string]bool
	// Hide returns a non-empty reason when a fully computed branch should be
	// hidden. May be nil.
	Hide      func(b model.Branch) (reason string)
	FetchedAt time.Time
	Warnings  []string
}

// cand is a branch being computed.
type cand struct {
	b         model.Branch
	localSHA  string
	remoteSHA string
	mainBase  string // merge-base(tip, main head)
	parentTip string
}

// Build computes the snapshot. Per-branch git failures become warnings and
// drop the branch; only repository-level failures (or ctx cancellation)
// return an error.
func Build(ctx context.Context, in Input) (model.Snapshot, error) {
	if in.Repo == nil || in.Scan == nil {
		return model.Snapshot{}, errors.New("snapshot: Repo and Scan are required")
	}
	repo, scan := in.Repo, in.Scan
	mainHead := scan.MainHead.SHA
	warnings := append([]string{}, in.Warnings...)
	var warnMu sync.Mutex
	warn := func(format string, args ...any) {
		warnMu.Lock()
		warnings = append(warnings, fmt.Sprintf(format, args...))
		warnMu.Unlock()
	}

	// 1. Union of local and remote branches keyed by short name.
	byName := map[string]*cand{}
	get := func(name string) *cand {
		c := byName[name]
		if c == nil {
			c = &cand{b: model.Branch{Name: name}}
			byName[name] = c
		}
		return c
	}
	for _, r := range scan.Local {
		if r.Name == repo.DefaultBranch {
			continue
		}
		c := get(r.Name)
		c.b.Local, c.localSHA = true, r.SHA
		c.b.TipSHA, c.b.LastCommitAt = r.SHA, r.CommitTime
	}
	for _, r := range scan.Remote {
		if r.Name == repo.DefaultBranch || r.Name == "HEAD" {
			continue
		}
		c := get(r.Name)
		c.b.Remote, c.remoteSHA = true, r.SHA
		if !c.b.Local {
			c.b.TipSHA, c.b.LastCommitAt = r.SHA, r.CommitTime
		}
	}

	// 2. Attach open PRs.
	for _, c := range byName {
		c.b.PR = pickPR(in.OpenPRs, c)
	}

	// 3. Drop fully merged branches (one git call for all ancestry checks).
	mergedRefs, err := repo.RefsMergedInto(ctx, mainHead)
	if err != nil {
		return model.Snapshot{}, err
	}
	mergedSHAs := make(map[string]bool, len(mergedRefs))
	for _, sha := range mergedRefs {
		mergedSHAs[sha] = true
	}
	for name, c := range byName {
		if mergedSHAs[c.b.TipSHA] || (c.b.PR == nil && in.MergedHeadRefs[name]) {
			delete(byName, name)
		}
	}

	names := sortedNames(byName)

	// Per-branch: unpushed/unpulled, merge-base with main, behind.
	failed := make([]error, len(names))
	err = gitscan.ForEach(ctx, len(names), func(i int) error {
		c := byName[names[i]]
		failed[i] = func() error {
			if c.b.Local && c.b.Remote && c.localSHA != c.remoteSHA {
				l, r, err := repo.LeftRight(ctx, c.localSHA, c.remoteSHA)
				if err != nil {
					return err
				}
				c.b.Unpushed, c.b.Unpulled = l, r
			}
			mb, err := repo.MergeBase(ctx, c.b.TipSHA, mainHead)
			if err != nil {
				return err
			}
			if mb == "" {
				return errUnrelated
			}
			c.mainBase = mb
			c.b.Behind, err = repo.Count(ctx, mb, mainHead)
			return err
		}()
		return nil
	})
	if err != nil {
		return model.Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	names = dropFailed(byName, names, failed, warn)

	// 4. Parent (stacking).
	if err := assignParents(ctx, repo, in, byName, names, warn); err != nil {
		return model.Snapshot{}, err
	}
	names = sortedNames(byName)

	// 5. BaseSHA and Ahead.
	failed = make([]error, len(names))
	err = gitscan.ForEach(ctx, len(names), func(i int) error {
		c := byName[names[i]]
		failed[i] = func() error {
			c.b.BaseSHA = c.mainBase
			if c.b.Parent != "" {
				mb, err := repo.MergeBase(ctx, c.b.TipSHA, c.parentTip)
				if err != nil {
					return err
				}
				if mb != "" {
					c.b.BaseSHA = mb
				}
			}
			n, err := repo.Count(ctx, c.b.BaseSHA, c.b.TipSHA)
			c.b.Ahead = n
			return err
		}()
		return nil
	})
	if err != nil {
		return model.Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	names = dropFailed(byName, names, failed, warn)
	// A branch whose parent was dropped above becomes main-based.
	for _, name := range names {
		c := byName[name]
		if c.b.Parent != "" && byName[c.b.Parent] == nil {
			c.b.Parent, c.b.BaseSHA = "", c.mainBase
			if ahead, err := repo.Count(ctx, c.mainBase, c.b.TipSHA); err == nil {
				c.b.Ahead = ahead
			}
		}
	}

	// 6–8. Status, worktree, hide.
	wtByBranch := map[string]gitscan.Worktree{}
	for _, wt := range scan.Worktrees {
		if wt.Branch != "" && !wt.Bare {
			wtByBranch[wt.Branch] = wt
		}
	}
	branches := make([]model.Branch, 0, len(names))
	for _, n := range names {
		c := byName[n]
		c.b.Status = deriveStatus(c.b.PR)
		if wt, ok := wtByBranch[n]; ok && c.b.Local {
			c.b.Worktree = &model.Worktree{Path: wt.Path, Dirty: wt.Dirty, IsMain: wt.IsMain}
		}
		if in.Hide != nil {
			if reason := in.Hide(c.b); reason != "" {
				c.b.Hidden, c.b.HiddenReason = true, reason
			}
		}
		branches = append(branches, c.b)
	}
	sort.SliceStable(branches, func(i, j int) bool {
		if branches[i].Behind != branches[j].Behind {
			return branches[i].Behind < branches[j].Behind
		}
		return branches[i].Name < branches[j].Name
	})

	// 9. Local main and anchors.
	localMain := buildLocalMain(ctx, repo, scan, wtByBranch, warn)
	localBase := ""
	if localMain != nil {
		localBase = localMain.BaseSHA
	}
	anchors, olderHidden, err := buildAnchors(ctx, repo, scan, byName, names, in.MergedPRs, localBase)
	if err != nil {
		return model.Snapshot{}, err
	}

	return model.Snapshot{
		Repo: model.RepoInfo{
			Path:          repo.Path,
			Slug:          repo.Slug,
			Remote:        repo.Remote,
			DefaultBranch: repo.DefaultBranch,
		},
		Main: model.MainLine{
			Ref:         scan.MainRef,
			Head:        scan.MainHead.Model(),
			Anchors:     anchors,
			OlderHidden: olderHidden,
			Local:       localMain,
		},
		Branches:    branches,
		GeneratedAt: time.Now().UTC(),
		FetchedAt:   in.FetchedAt,
		Warnings:    warnings,
	}, nil
}

var errUnrelated = errors.New("unrelated history")

// dropFailed removes branches whose computation failed, turning failures
// into warnings. Branches with history unrelated to main (gh-pages and other
// orphan branches) are dropped silently: they have no place on the map.
func dropFailed(byName map[string]*cand, names []string, failed []error, warn func(string, ...any)) []string {
	kept := names[:0:0]
	for i, n := range names {
		switch {
		case failed[i] == nil:
			kept = append(kept, n)
		case errors.Is(failed[i], errUnrelated):
			delete(byName, n)
		default:
			warn("branch %s skipped: %v", n, failed[i])
			delete(byName, n)
		}
	}
	return kept
}

// pickPR returns a copy of the open PR whose head is this branch. When
// several PRs share the head name (e.g. from forks) the one whose HeadSHA
// matches a local/remote tip wins, else the most recently updated.
func pickPR(prs []model.PullRequest, c *cand) *model.PullRequest {
	var best *model.PullRequest
	bestMatch := false
	for i := range prs {
		p := &prs[i]
		if p.HeadRef != c.b.Name {
			continue
		}
		match := p.HeadSHA != "" && (p.HeadSHA == c.localSHA || p.HeadSHA == c.remoteSHA)
		if best == nil || (match && !bestMatch) ||
			(match == bestMatch && p.UpdatedAt.After(best.UpdatedAt)) {
			best, bestMatch = p, match
		}
	}
	if best == nil {
		return nil
	}
	cp := *best
	if cp.FailingChecks == nil {
		cp.FailingChecks = []string{}
	} else {
		cp.FailingChecks = append([]string{}, cp.FailingChecks...)
	}
	return &cp
}

// deriveStatus applies the status priority order.
func deriveStatus(pr *model.PullRequest) model.Status {
	switch {
	case pr == nil:
		return model.StatusNoPR
	case pr.IsDraft:
		return model.StatusDraft
	case strings.EqualFold(pr.Mergeable, "CONFLICTING"):
		return model.StatusConflicts
	case strings.EqualFold(pr.Checks, "failing"):
		return model.StatusCIFailing
	case strings.EqualFold(pr.ReviewDecision, "APPROVED"):
		return model.StatusApproved
	default:
		return model.StatusReview
	}
}

// assignParents sets Parent and parentTip. A PR whose base is another branch
// in the set wins; branches without a PR get an ancestry-based parent: the
// nearest other kept branch whose tip is a strict ancestor of this tip (kept
// branches are, by construction, not reachable from main). Cycles are broken
// deterministically.
func assignParents(ctx context.Context, repo *gitscan.Repo, in Input, byName map[string]*cand, names []string, warn func(string, ...any)) error {
	tipOwners := map[string][]string{} // tip SHA → kept branch names
	for _, n := range names {
		sha := byName[n].b.TipSHA
		tipOwners[sha] = append(tipOwners[sha], n)
	}

	var needAncestry []string
	for _, n := range names {
		c := byName[n]
		if c.b.PR != nil {
			base := c.b.PR.BaseRef
			if base != "" && base != repo.DefaultBranch && base != n && byName[base] != nil {
				c.b.Parent = base
			}
			continue
		}
		needAncestry = append(needAncestry, n)
	}

	failed := make([]error, len(needAncestry))
	err := gitscan.ForEach(ctx, len(needAncestry), func(i int) error {
		c := byName[needAncestry[i]]
		failed[i] = func() error {
			reach, err := repo.RefsMergedInto(ctx, c.b.TipSHA)
			if err != nil {
				return err
			}
			seen := map[string]bool{}
			var cands []string
			for _, sha := range reach {
				if sha == c.b.TipSHA || seen[sha] {
					continue // not a strict ancestor / already counted
				}
				seen[sha] = true
				for _, owner := range tipOwners[sha] {
					if owner != c.b.Name {
						cands = append(cands, owner)
					}
				}
			}
			if len(cands) == 0 {
				return nil
			}
			sort.Strings(cands)
			best, bestN := "", -1
			for _, cn := range cands {
				n, err := repo.Count(ctx, byName[cn].b.TipSHA, c.b.TipSHA)
				if err != nil {
					return err
				}
				if bestN < 0 || n < bestN {
					best, bestN = cn, n
				}
			}
			c.b.Parent = best
			return nil
		}()
		return nil
	})
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for i, n := range needAncestry {
		if failed[i] != nil {
			// Not fatal: keep the branch main-based.
			warn("branch %s: stacking detection failed: %v", n, failed[i])
		}
	}

	breakCycles(byName, names)
	for _, n := range names {
		c := byName[n]
		if c.b.Parent != "" {
			c.parentTip = byName[c.b.Parent].b.TipSHA
		}
	}
	return nil
}

// breakCycles walks each parent chain; when a chain loops back, the branch
// that closes the loop (the first one revisited, in name order) is made
// main-based.
func breakCycles(byName map[string]*cand, names []string) {
	for _, start := range names {
		onPath := map[string]bool{}
		cur := start
		for cur != "" {
			if onPath[cur] {
				byName[cur].b.Parent = ""
				break
			}
			onPath[cur] = true
			c := byName[cur]
			if c == nil {
				break
			}
			cur = c.b.Parent
		}
	}
}

// buildAnchors collapses main into HEAD, divergence and convergence anchors,
// newest first.
func buildAnchors(ctx context.Context, repo *gitscan.Repo, scan *gitscan.Scan, byName map[string]*cand, names []string, merged []model.MergedPR, localBase string) ([]model.Anchor, bool, error) {
	mainHead := scan.MainHead.SHA
	type acc struct {
		a      model.Anchor
		behind int
		known  bool // behind already known
	}
	bySHA := map[string]*acc{}
	at := func(sha string) *acc {
		x := bySHA[sha]
		if x == nil {
			x = &acc{a: model.Anchor{BranchNames: []string{}, Merged: []model.MergedPR{}}}
			bySHA[sha] = x
		}
		return x
	}

	h := at(mainHead)
	h.a.IsHead, h.known, h.behind = true, true, 0

	for _, n := range names {
		c := byName[n]
		if c.b.Parent != "" {
			continue
		}
		x := at(c.mainBase)
		x.a.BranchNames = append(x.a.BranchNames, n)
		x.behind, x.known = c.b.Behind, true
	}

	if localBase != "" {
		at(localBase).a.IsLocalMain = true
	}

	// Convergence: only merge commits that actually landed on main.
	onMain := make([]bool, len(merged))
	err := gitscan.ForEach(ctx, len(merged), func(i int) error {
		sha := merged[i].MergeCommitSHA
		if sha == "" {
			return nil
		}
		ok, err := repo.IsAncestor(ctx, sha, mainHead)
		onMain[i] = err == nil && ok // unknown commit (not fetched) → skip
		return ctx.Err()
	})
	if err != nil {
		return nil, false, err
	}
	for i, pr := range merged {
		if onMain[i] {
			x := at(pr.MergeCommitSHA)
			x.a.Merged = append(x.a.Merged, pr)
		}
	}

	shas := make([]string, 0, len(bySHA))
	for sha := range bySHA {
		shas = append(shas, sha)
	}
	sort.Strings(shas)

	failed := make([]error, len(shas))
	err = gitscan.ForEach(ctx, len(shas), func(i int) error {
		x := bySHA[shas[i]]
		if x.known {
			return nil
		}
		x.behind, failed[i] = repo.Count(ctx, shas[i], mainHead)
		return ctx.Err()
	})
	if err != nil {
		return nil, false, err
	}
	for _, e := range failed {
		if e != nil {
			return nil, false, e
		}
	}

	infos, err := repo.Commits(ctx, shas...)
	if err != nil {
		return nil, false, err
	}
	infoBySHA := make(map[string]gitscan.CommitInfo, len(infos))
	for _, ci := range infos {
		infoBySHA[ci.SHA] = ci
	}

	anchors := make([]model.Anchor, 0, len(shas))
	for _, sha := range shas {
		x := bySHA[sha]
		ci, ok := infoBySHA[sha]
		if !ok {
			return nil, false, fmt.Errorf("commit %s not found", sha)
		}
		x.a.Commit = ci.Model()
		x.a.Behind = x.behind
		sort.Strings(x.a.BranchNames)
		sort.SliceStable(x.a.Merged, func(i, j int) bool {
			mi, mj := x.a.Merged[i], x.a.Merged[j]
			if !mi.MergedAt.Equal(mj.MergedAt) {
				return mi.MergedAt.After(mj.MergedAt)
			}
			return mi.Number < mj.Number
		})
		anchors = append(anchors, x.a)
	}
	sort.SliceStable(anchors, func(i, j int) bool {
		if anchors[i].Behind != anchors[j].Behind {
			return anchors[i].Behind < anchors[j].Behind
		}
		return anchors[i].Commit.Time.After(anchors[j].Commit.Time)
	})
	oldest := anchors[len(anchors)-1]
	olderHidden := len(infoBySHA[oldest.Commit.SHA].Parents) > 0
	return anchors, olderHidden, nil
}

// buildLocalMain compares the local default branch with the remote one.
// Failures become warnings: the marker is a nicety, not part of the map.
func buildLocalMain(ctx context.Context, repo *gitscan.Repo, scan *gitscan.Scan, wtByBranch map[string]gitscan.Worktree, warn func(string, ...any)) *model.LocalMain {
	lm := scan.LocalMain
	if lm == nil {
		return nil
	}
	mainHead := scan.MainHead.SHA
	base, err := repo.MergeBase(ctx, lm.SHA, mainHead)
	if err != nil {
		warn("local %s: %v", lm.Name, err)
		return nil
	}
	if base == "" {
		return nil // unrelated history
	}
	ahead, behind, err := repo.LeftRight(ctx, lm.SHA, mainHead)
	if err != nil {
		warn("local %s: %v", lm.Name, err)
		return nil
	}
	short := lm.SHA
	if len(short) > 7 {
		short = short[:7]
	}
	out := &model.LocalMain{Name: lm.Name, SHA: lm.SHA, ShortSHA: short, BaseSHA: base, Ahead: ahead, Behind: behind}
	if wt, ok := wtByBranch[lm.Name]; ok {
		out.Worktree = &model.Worktree{Path: wt.Path, Dirty: wt.Dirty, IsMain: wt.IsMain}
	}
	return out
}

func sortedNames(m map[string]*cand) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
