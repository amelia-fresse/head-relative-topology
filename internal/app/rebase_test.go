package app

import (
	"strings"
	"testing"
)

func TestRebasePrompt(t *testing.T) {
	both := rebasePrompt("feat/x", "origin/main", "origin", true, true)
	for _, want := range []string{
		"`git rebase origin/main`",
		"4. Point `feat/x` at the result with `git branch -f feat/x HEAD`",
		"5. Update the remote branch: `git push --force-with-lease origin HEAD:feat/x`",
		"6. Only if every step above worked, clean up",
		"git worktree remove <path>",
		"git branch -D <temporary branch>",
	} {
		if !strings.Contains(both, want) {
			t.Errorf("prompt lacks %q:\n%s", want, both)
		}
	}
	if p := rebasePrompt("feat/x", "origin/main", "origin", true, false); strings.Contains(p, "push") {
		t.Errorf("a local-only branch's prompt asks to push:\n%s", p)
	}
	remoteOnly := rebasePrompt("feat/x", "origin/main", "origin", false, true)
	if strings.Contains(remoteOnly, "git branch -f") || !strings.Contains(remoteOnly, "Don't create a local `feat/x` branch") {
		t.Errorf("a remote-only branch's prompt makes a local branch:\n%s", remoteOnly)
	}
}
