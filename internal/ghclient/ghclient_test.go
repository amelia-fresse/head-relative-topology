package ghclient

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// call records one invocation of a fake Runner.
type call struct {
	dir  string
	name string
	args []string
}

// fakeRunner returns canned output and records every call. out maps the
// joined argument string to its response; unmatched calls fail the test.
type fakeRunner struct {
	t     *testing.T
	calls []call
	out   map[string]string
	errs  map[string]error
}

func (f *fakeRunner) run(_ context.Context, dir, name string, args ...string) (string, error) {
	f.calls = append(f.calls, call{dir: dir, name: name, args: args})
	key := strings.Join(args, " ")
	if err, ok := f.errs[key]; ok {
		return "", err
	}
	if out, ok := f.out[key]; ok {
		return out, nil
	}
	f.t.Fatalf("unexpected gh call: %s %s", name, key)
	return "", nil
}

func newFake(t *testing.T, repoDir string) (*Client, *fakeRunner) {
	t.Helper()
	f := &fakeRunner{t: t, out: map[string]string{}, errs: map[string]error{}}
	c := NewWithRunner(repoDir, f.run)
	c.lookPath = func(string) (string, error) { return "/usr/bin/gh", nil }
	return c, f
}

const repoDir = "/work/acme/app"

func TestAvailable(t *testing.T) {
	const authKey = "auth status"

	t.Run("installed and logged in", func(t *testing.T) {
		c, f := newFake(t, repoDir)
		f.out[authKey] = ""
		if err := c.Available(context.Background()); err != nil {
			t.Fatalf("Available: %v", err)
		}
		want := []call{{dir: repoDir, name: "gh", args: []string{"auth", "status"}}}
		if !reflect.DeepEqual(f.calls, want) {
			t.Errorf("calls = %+v, want %+v", f.calls, want)
		}
	})

	t.Run("not on PATH", func(t *testing.T) {
		c, f := newFake(t, repoDir)
		c.lookPath = func(string) (string, error) { return "", errors.New("not found") }
		err := c.Available(context.Background())
		if err == nil || err.Error() != "gh not found on PATH" {
			t.Fatalf("err = %v, want gh not found on PATH", err)
		}
		if len(f.calls) != 0 {
			t.Errorf("gh was run although it is not on PATH: %+v", f.calls)
		}
	})

	t.Run("not logged in", func(t *testing.T) {
		c, f := newFake(t, repoDir)
		cause := errors.New("gh auth status: You are not logged into any GitHub hosts.")
		f.errs[authKey] = cause
		err := c.Available(context.Background())
		if err == nil {
			t.Fatal("Available: want error")
		}
		if !strings.HasPrefix(err.Error(), "gh is not logged in: run `gh auth login`") {
			t.Errorf("err = %q", err)
		}
		if !errors.Is(err, cause) {
			t.Errorf("error does not wrap the gh failure: %v", err)
		}
	})

	t.Run("cancelled context is reported as cancellation", func(t *testing.T) {
		c, f := newFake(t, repoDir)
		f.errs[authKey] = errors.New("signal: killed")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.Available(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestOpenPRsArgsAndParsing(t *testing.T) {
	c, f := newFake(t, repoDir)
	f.out["pr list --state open --limit 200 --json "+openPRFields] = openPRsFixture

	prs, err := c.OpenPRs(context.Background())
	if err != nil {
		t.Fatalf("OpenPRs: %v", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("gh calls = %d, want 1", len(f.calls))
	}
	got := f.calls[0]
	if got.dir != repoDir || got.name != "gh" {
		t.Errorf("ran %s in %q, want gh in %q", got.name, got.dir, repoDir)
	}
	if len(prs) != 5 {
		t.Errorf("len(prs) = %d, want 5 (fork PR skipped)", len(prs))
	}
	for _, p := range prs {
		if p.Number == 430 {
			t.Error("cross-repository PR 430 was not skipped")
		}
	}
}

func TestOpenPRsExactFieldList(t *testing.T) {
	c, f := newFake(t, repoDir)
	f.out["pr list --state open --limit 200 --json "+openPRFields] = `[]`
	if _, err := c.OpenPRs(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"pr", "list", "--state", "open", "--limit", "200", "--json",
		"number,title,url,author,isDraft,baseRefName,headRefName,headRefOid,reviewDecision,statusCheckRollup,mergeable,updatedAt,isCrossRepository",
	}
	if !reflect.DeepEqual(f.calls[0].args, want) {
		t.Errorf("args = %v\nwant   %v", f.calls[0].args, want)
	}
}

func TestOpenPRsRunnerError(t *testing.T) {
	c, f := newFake(t, repoDir)
	cause := errors.New("gh pr list: none of the git remotes point to a GitHub host")
	f.errs["pr list --state open --limit 200 --json "+openPRFields] = cause
	_, err := c.OpenPRs(context.Background())
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want wrapped %v", err, cause)
	}
}

func TestMergedPRsArgsAndParsing(t *testing.T) {
	c, f := newFake(t, repoDir)
	key := "pr list --state merged --limit 50 --json " + mergedPRFields
	f.out[key] = mergedPRsFixture

	merged, err := c.MergedPRs(context.Background(), 50)
	if err != nil {
		t.Fatalf("MergedPRs: %v", err)
	}
	want := []call{{dir: repoDir, name: "gh", args: []string{
		"pr", "list", "--state", "merged", "--limit", "50", "--json",
		"number,title,url,headRefName,author,mergedAt,mergeCommit",
	}}}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %+v, want %+v", f.calls, want)
	}
	if len(merged) != 4 || merged[0].Number != 410 {
		t.Errorf("merged = %+v, want 4 entries newest first", merged)
	}
}

func TestMergedPRsRejectsNonPositiveLimit(t *testing.T) {
	for _, limit := range []int{0, -1} {
		c, f := newFake(t, repoDir)
		if _, err := c.MergedPRs(context.Background(), limit); err == nil {
			t.Errorf("limit %d: want error", limit)
		}
		if len(f.calls) != 0 {
			t.Errorf("limit %d: gh was run", limit)
		}
	}
}

func TestMergedPRsRunnerError(t *testing.T) {
	c, f := newFake(t, repoDir)
	cause := errors.New("gh: HTTP 502")
	f.errs["pr list --state merged --limit 10 --json "+mergedPRFields] = cause
	_, err := c.MergedPRs(context.Background(), 10)
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want wrapped %v", err, cause)
	}
}

func TestEmptyGhOutputIsAnError(t *testing.T) {
	c, f := newFake(t, repoDir)
	f.out["pr list --state open --limit 200 --json "+openPRFields] = ""
	if _, err := c.OpenPRs(context.Background()); err == nil {
		t.Error("OpenPRs with empty output: want error")
	}
}
