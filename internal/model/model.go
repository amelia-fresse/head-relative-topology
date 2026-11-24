// Package model holds the data contract shared by the Go backend and the
// frontend (via generated Wails bindings). Every other package produces or
// consumes these types; change them deliberately.
package model

import "time"

// Status is the single derived state shown by a branch dot.
type Status string

const (
	StatusNoPR      Status = "no_pr"
	StatusDraft     Status = "draft"
	StatusConflicts Status = "conflicts"
	StatusCIFailing Status = "ci_failing"
	StatusApproved  Status = "approved"
	StatusReview    Status = "review"
)

// Snapshot is everything the frontend needs to draw the map for one repo.
//
// Main anchors are ordered newest → oldest (top → bottom in the vertical
// view). The frontend may drop anchors (e.g. convergence points when "show
// merged" is off, or anchors whose only branches are hidden); the number of
// collapsed commits between two consecutive visible anchors A (newer) and B
// (older) is B.Behind - A.Behind - 1, clamped to 0 (on histories with merge
// commits two anchors that are not ancestors of each other can tie).
type Snapshot struct {
	Repo        RepoInfo  `json:"repo"`
	Main        MainLine  `json:"main"`
	Branches    []Branch  `json:"branches"`
	GeneratedAt time.Time `json:"generatedAt"`
	// FetchedAt is when `git fetch` last succeeded (zero if never this session).
	FetchedAt time.Time `json:"fetchedAt"`
	// Warnings are non-fatal problems (gh missing, not logged in, fetch failed…).
	Warnings []string `json:"warnings"`
	// Agents is true when the T3 Code CLI is available to start threads.
	Agents bool `json:"agents"`
}

type RepoInfo struct {
	// Path is the absolute path of the repository's main worktree.
	Path string `json:"path"`
	// Slug is "owner/name" on GitHub, empty when the remote isn't GitHub.
	Slug string `json:"slug"`
	// Remote is the remote name used for main, usually "origin".
	Remote string `json:"remote"`
	// DefaultBranch is the short name of main, e.g. "main".
	DefaultBranch string `json:"defaultBranch"`
}

type MainLine struct {
	// Ref is the full display ref, e.g. "origin/main".
	Ref     string   `json:"ref"`
	Head    Commit   `json:"head"`
	Anchors []Anchor `json:"anchors"`
	// OlderHidden is true when main has history older than the oldest anchor.
	OlderHidden bool `json:"olderHidden"`
	// Local is the local copy of main (refs/heads/<default>), nil when it
	// doesn't exist or main itself is a local ref (no remote).
	Local *LocalMain `json:"local"`
}

// LocalMain places the local default branch relative to the remote one. It
// sits on the main line at BaseSHA (the anchor with IsLocalMain set).
type LocalMain struct {
	Name     string `json:"name"`
	SHA      string `json:"sha"`
	ShortSHA string `json:"shortSha"`
	// BaseSHA is the merge-base with the remote main.
	BaseSHA string `json:"baseSha"`
	// Ahead: local commits not on the remote main (unpushed).
	Ahead int `json:"ahead"`
	// Behind: remote main commits not in the local main (not pulled).
	Behind   int       `json:"behind"`
	Worktree *Worktree `json:"worktree"`
	// Thread is the most urgent unsettled T3 Code thread in the main
	// checkout; nil when there is none. ActiveThreads counts them all.
	Thread        *AgentThread `json:"thread"`
	ActiveThreads int          `json:"activeThreads"`
}

type Commit struct {
	SHA      string    `json:"sha"`
	ShortSHA string    `json:"shortSha"`
	Subject  string    `json:"subject"`
	Time     time.Time `json:"time"`
}

// Anchor is one commit on main worth showing: HEAD, a divergence point
// (merge-base of at least one main-based branch) or a convergence point (a
// merged PR landed as this commit). A single commit can be several of these.
type Anchor struct {
	Commit Commit `json:"commit"`
	// Behind is the number of commits on main after this one up to HEAD (HEAD = 0).
	Behind int  `json:"behind"`
	IsHead bool `json:"isHead"`
	// IsLocalMain marks where the local main sits (its merge-base with main).
	IsLocalMain bool `json:"isLocalMain"`
	// BranchNames lists main-based branches (Parent == "") whose merge-base is this commit.
	BranchNames []string `json:"branchNames"`
	// Merged lists PRs that landed on main as this commit.
	Merged []MergedPR `json:"merged"`
}

type MergedPR struct {
	Number   int       `json:"number"`
	Title    string    `json:"title"`
	URL      string    `json:"url"`
	HeadRef  string    `json:"headRef"`
	Author   string    `json:"author"`
	MergedAt time.Time `json:"mergedAt"`
	// MergeCommitSHA is the commit on main the PR became (merge or squash commit).
	MergeCommitSHA string `json:"mergeCommitSha"`
}

// Branch is one point on the map.
type Branch struct {
	// Name is the short branch name without remote prefix, e.g. "feat/auth-sso".
	Name string `json:"name"`
	// Local / Remote tell where the branch exists (refs/heads, refs/remotes/<remote>).
	Local  bool   `json:"local"`
	Remote bool   `json:"remote"`
	TipSHA string `json:"tipSha"`
	// Unpushed / Unpulled compare the local branch with its remote counterpart.
	Unpushed int `json:"unpushed"`
	Unpulled int `json:"unpulled"`

	// Parent is "" for branches based on main, otherwise the name of the
	// branch this one is stacked on (from the PR base or ancestry).
	Parent string `json:"parent"`
	// BaseSHA is the merge-base with the parent (or with main).
	BaseSHA string `json:"baseSha"`
	// Ahead is the number of commits on the branch not on its parent (or main).
	Ahead int `json:"ahead"`
	// Behind is the number of commits on main since the branch's merge-base
	// with main (always relative to main, even for stacked branches).
	Behind int `json:"behind"`

	LastCommitAt time.Time    `json:"lastCommitAt"`
	Status       Status       `json:"status"`
	PR           *PullRequest `json:"pr"`
	Worktree     *Worktree    `json:"worktree"`

	// Thread is the T3 Code agent thread working on this branch, if any.
	Thread *AgentThread `json:"thread"`
	// CopyOf is set on a working copy of another branch: the branch checked
	// out in the worktree of an agent hrt started for CopyOf (a rebase). The
	// map folds it into CopyOf's card.
	CopyOf string `json:"copyOf"`
	// Copy is this branch's working copy, while one exists.
	Copy *BranchCopy `json:"copy"`

	// Hidden is set by the user's hide rules; HiddenReason says which rule.
	Hidden       bool   `json:"hidden"`
	HiddenReason string `json:"hiddenReason"`
}

// Agent thread states shown on the map.
const (
	AgentRunning = "running"
	AgentWaiting = "waiting"
	AgentFailed  = "failed"
	AgentIdle    = "idle"
)

// BranchCopy summarises a branch's working copy (see Branch.CopyOf).
type BranchCopy struct {
	Name string `json:"name"`
	// Ahead / Behind are the copy's, relative to main.
	Ahead  int  `json:"ahead"`
	Behind int  `json:"behind"`
	Dirty  bool `json:"dirty"`
}

// AgentThread is a T3 Code thread working on a branch.
type AgentThread struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// State: "running", "waiting" (needs you), "failed" or "idle".
	State     string    `json:"state"`
	Link      string    `json:"link"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type PullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Author  string `json:"author"`
	IsDraft bool   `json:"isDraft"`
	BaseRef string `json:"baseRef"`
	HeadRef string `json:"headRef"`
	HeadSHA string `json:"headSha"`
	// ReviewDecision: "APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED" or "".
	ReviewDecision string `json:"reviewDecision"`
	// Checks: "passing", "failing", "pending" or "none".
	Checks string `json:"checks"`
	// FailingChecks holds the names of failing checks, for tooltips.
	FailingChecks []string `json:"failingChecks"`
	// Mergeable: "MERGEABLE", "CONFLICTING" or "UNKNOWN".
	Mergeable string    `json:"mergeable"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Worktree struct {
	Path  string `json:"path"`
	Dirty bool   `json:"dirty"`
	// IsMain is true for the repository's primary worktree.
	IsMain bool `json:"isMain"`
}

// Settings are global (not per repo) user preferences.
type Settings struct {
	// Theme: "default", "babydoll" or "dolly".
	Theme      string `json:"theme" toml:"theme"`
	Compact    bool   `json:"compact" toml:"compact"`
	ShowMerged bool   `json:"showMerged" toml:"show_merged"`
	ShowHidden bool   `json:"showHidden" toml:"show_hidden"`
	// OnlyRemote hides branches that exist only on this machine.
	OnlyRemote bool `json:"onlyRemote" toml:"only_remote"`
	// OnlyPRs hides branches without an open pull request.
	OnlyPRs     bool `json:"onlyPRs" toml:"only_prs"`
	AlwaysOnTop bool `json:"alwaysOnTop" toml:"always_on_top"`
	// FetchIntervalSec: how often to run `git fetch` (0 = never automatically).
	FetchIntervalSec int `json:"fetchIntervalSec" toml:"fetch_interval_sec"`
	// PRPollIntervalSec: how often to query GitHub.
	PRPollIntervalSec int `json:"prPollIntervalSec" toml:"pr_poll_interval_sec"`
	// MergedWindowHours: merged PRs newer than this are shown as convergence points.
	MergedWindowHours int `json:"mergedWindowHours" toml:"merged_window_hours"`
	// LastRepo is the active tab, reopened at startup when no path is given.
	LastRepo string `json:"lastRepo" toml:"last_repo"`
	// OpenRepos are the repositories open as tabs, in tab order.
	OpenRepos []string `json:"openRepos" toml:"open_repos"`
}

// Tab is one open repository. Every tab is kept up to date in the
// background; only the active one is drawn.
type Tab struct {
	// Path is the repository's main worktree, the tab's identity.
	Path string `json:"path"`
	// Name is a short label (the repo name from the slug, else the folder).
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Active bool   `json:"active"`
	// Attention is the most urgent status among visible branches:
	// "conflicts", "ci_failing" or "".
	Attention string `json:"attention"`
}

// DefaultSettings returns the settings used when no config file exists.
func DefaultSettings() Settings {
	return Settings{
		Theme:             "default",
		Compact:           false,
		ShowMerged:        true,
		ShowHidden:        false,
		AlwaysOnTop:       false,
		FetchIntervalSec:  120,
		PRPollIntervalSec: 60,
		MergedWindowHours: 48,
		OpenRepos:         []string{},
	}
}

// RepoConfig holds per-repository hide rules. It is private to the user and
// never written inside the repository.
type RepoConfig struct {
	// HiddenBranches are exact branch names hidden by hand.
	HiddenBranches []string `json:"hiddenBranches" toml:"hidden_branches"`
	// HidePatterns are globs matched against branch names, e.g. "dependabot/*".
	HidePatterns []string `json:"hidePatterns" toml:"hide_patterns"`
	// HideNoCommits hides branches with nothing of their own (Ahead == 0).
	HideNoCommits bool `json:"hideNoCommits" toml:"hide_no_commits"`
	// HideRemoteOnly hides branches that exist only on the remote and have no open PR.
	HideRemoteOnly bool `json:"hideRemoteOnly" toml:"hide_remote_only"`
	// StaleDays hides branches whose last commit is older than this (0 = off).
	StaleDays int `json:"staleDays" toml:"stale_days"`
}
