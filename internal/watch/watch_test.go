package watch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testDebounce = 100 * time.Millisecond
	// settle is long enough for any pending debounce to have fired.
	settle  = 4 * testDebounce
	timeout = 3 * time.Second
)

// fakeRepo lays out the parts of a git common dir that GitPaths knows about.
func fakeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"refs/heads", "refs/remotes/origin", "worktrees/x"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(root, "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(root, "packed-refs"), "")
	writeFile(t, filepath.Join(root, "refs/heads/main"), "0000000000000000000000000000000000000000\n")
	return root
}

func repoPaths(t *testing.T, root string) []string {
	t.Helper()
	paths, err := GitPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type counter struct{ n atomic.Int32 }

func (c *counter) inc()       { c.n.Add(1) }
func (c *counter) get() int32 { return c.n.Load() }
func (c *counter) reset()     { c.n.Store(0) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (w *Watcher) isWatched(p string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.paths[filepath.Clean(p)]
}

func newTestWatcher(t *testing.T, paths []string, onChange func()) *Watcher {
	t.Helper()
	w, err := New(paths, testDebounce, onChange)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func TestGitPaths(t *testing.T) {
	t.Parallel()
	root := fakeRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "refs/heads/feat/deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "objects/ab"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths, err := GitPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(paths))
	for _, p := range paths {
		got[p] = true
	}
	want := []string{
		root,
		filepath.Join(root, "refs"),
		filepath.Join(root, "refs/heads"),
		filepath.Join(root, "refs/heads/feat"),
		filepath.Join(root, "refs/heads/feat/deep"),
		filepath.Join(root, "refs/remotes"),
		filepath.Join(root, "refs/remotes/origin"),
		filepath.Join(root, "worktrees"),
		filepath.Join(root, "worktrees/x"),
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("GitPaths missing %s", w)
		}
	}
	for _, p := range paths {
		if filepath.Base(p) == "objects" {
			t.Errorf("GitPaths returned objects dir: %s", p)
		}
	}

	missing, err := GitPaths(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("GitPaths on missing dir: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("GitPaths on missing dir = %v, want none", missing)
	}
}

func TestWriteTriggersOnce(t *testing.T) {
	t.Parallel()
	root := fakeRepo(t)
	var c counter
	newTestWatcher(t, repoPaths(t, root), c.inc)

	writeFile(t, filepath.Join(root, "refs/heads/main"), "1111111111111111111111111111111111111111\n")
	waitFor(t, "callback", func() bool { return c.get() >= 1 })
	time.Sleep(settle)
	if n := c.get(); n != 1 {
		t.Fatalf("callbacks = %d, want 1", n)
	}
}

func TestBurstCoalesces(t *testing.T) {
	t.Parallel()
	root := fakeRepo(t)
	var c counter
	newTestWatcher(t, repoPaths(t, root), c.inc)

	main := filepath.Join(root, "refs/heads/main")
	for i := 0; i < 20; i++ {
		writeFile(t, main, fmt.Sprintf("%040d\n", i))
		time.Sleep(3 * time.Millisecond)
	}
	waitFor(t, "callback", func() bool { return c.get() >= 1 })
	time.Sleep(settle)
	if n := c.get(); n < 1 || n > 2 {
		t.Fatalf("callbacks = %d for a burst of 20 writes, want 1 or 2", n)
	}
}

func TestNestedDirPicked(t *testing.T) {
	t.Parallel()
	root := fakeRepo(t)
	var c counter
	w := newTestWatcher(t, repoPaths(t, root), c.inc)

	feat := filepath.Join(root, "refs/heads/feat")
	nested := filepath.Join(feat, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "feat/sub to be watched", func() bool {
		return w.isWatched(feat) && w.isWatched(nested)
	})
	// Let the callbacks caused by creating the directories settle.
	time.Sleep(settle)
	c.reset()

	writeFile(t, filepath.Join(nested, "topic"), "0000000000000000000000000000000000000000\n")
	waitFor(t, "callback for nested write", func() bool { return c.get() >= 1 })
	time.Sleep(settle)
	if n := c.get(); n != 1 {
		t.Fatalf("callbacks = %d for nested write, want 1", n)
	}
}

func TestLockFilesAloneDontTrigger(t *testing.T) {
	t.Parallel()
	root := fakeRepo(t)
	var c counter
	newTestWatcher(t, repoPaths(t, root), c.inc)

	lock := filepath.Join(root, "refs/heads/main.lock")
	writeFile(t, lock, "2222222222222222222222222222222222222222\n")
	time.Sleep(settle / 2)
	for i := 0; i < 3; i++ {
		writeFile(t, lock, fmt.Sprintf("%040d\n", i))
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	time.Sleep(settle)
	if n := c.get(); n != 0 {
		t.Fatalf("callbacks = %d for lock-file-only activity, want 0", n)
	}

	// Git's lock protocol ends with a rename onto the real ref. That must count.
	writeFile(t, lock, "3333333333333333333333333333333333333333\n")
	if err := os.Rename(lock, filepath.Join(root, "refs/heads/main")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "callback after rename", func() bool { return c.get() >= 1 })
	time.Sleep(settle)
	if n := c.get(); n != 1 {
		t.Fatalf("callbacks = %d after lock rename, want 1", n)
	}
}

func TestMaxWaitDuringStream(t *testing.T) {
	t.Parallel()
	root := fakeRepo(t)
	var c counter
	// Writes every 25ms never leave a 150ms quiet gap, so only maxWait can fire.
	w, err := newWatcher(repoPaths(t, root), 150*time.Millisecond, 400*time.Millisecond, c.inc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	main := filepath.Join(root, "refs/heads/main")
	deadline := time.Now().Add(1500 * time.Millisecond)
	for i := 0; time.Now().Before(deadline); i++ {
		writeFile(t, main, fmt.Sprintf("%040d\n", i))
		time.Sleep(25 * time.Millisecond)
	}
	if n := c.get(); n < 2 {
		t.Fatalf("callbacks = %d during a 1.5s stream with maxWait 400ms, want >= 2", n)
	}
}

func TestSetPathsSwitchesRepo(t *testing.T) {
	t.Parallel()
	repo1 := fakeRepo(t)
	repo2 := fakeRepo(t)
	var c counter
	w := newTestWatcher(t, repoPaths(t, repo1), c.inc)

	if err := w.SetPaths(repoPaths(t, repo2)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo1, "refs/heads/main"), "4444444444444444444444444444444444444444\n")
	time.Sleep(settle)
	if n := c.get(); n != 0 {
		t.Fatalf("callbacks = %d for write to previous repo, want 0", n)
	}

	writeFile(t, filepath.Join(repo2, "refs/heads/main"), "5555555555555555555555555555555555555555\n")
	waitFor(t, "callback for new repo", func() bool { return c.get() >= 1 })
}

func TestNoCallbackAfterClose(t *testing.T) {
	t.Parallel()
	root := fakeRepo(t)
	var c counter
	w, err := New(repoPaths(t, root), testDebounce, c.inc)
	if err != nil {
		t.Fatal(err)
	}

	// Leave a debounce pending, then close before it fires.
	writeFile(t, filepath.Join(root, "refs/heads/main"), "6666666666666666666666666666666666666666\n")
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	atClose := c.get()

	writeFile(t, filepath.Join(root, "refs/heads/main"), "7777777777777777777777777777777777777777\n")
	time.Sleep(settle)
	if n := c.get(); n != atClose {
		t.Fatalf("callbacks went from %d to %d after Close", atClose, n)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := w.SetPaths(nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetPaths after Close = %v, want ErrClosed", err)
	}
}
