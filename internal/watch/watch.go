// Package watch reports changes to a git repository's refs and worktree
// metadata, so the app can recompute its map shortly after they settle.
//
// fsnotify is not recursive, so GitPaths enumerates every directory that
// holds refs or worktree state, and Watcher adds directories created later
// under those trees on the fly.
package watch

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// defaultMaxWait bounds how long a continuous stream of events can keep
// postponing onChange.
const defaultMaxWait = 2 * time.Second

// ErrClosed is returned by SetPaths once the watcher has been closed.
var ErrClosed = errors.New("watch: watcher is closed")

// GitPaths returns the directories to watch for a repository: the git common
// dir itself (HEAD, packed-refs and FETCH_HEAD live there), every directory
// under <common>/refs, <common>/worktrees and each <common>/worktrees/<name>.
// Missing directories are skipped. The objects directory is never returned.
func GitPaths(gitCommonDir string) ([]string, error) {
	if gitCommonDir == "" {
		return nil, errors.New("watch: empty git common dir")
	}
	root := filepath.Clean(gitCommonDir)

	var paths []string
	if isDir(root) {
		paths = append(paths, root)
	}

	refs := filepath.Join(root, "refs")
	if isDir(refs) {
		err := filepath.WalkDir(refs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				paths = append(paths, p)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("watch: walking %s: %w", refs, err)
		}
	}

	worktrees := filepath.Join(root, "worktrees")
	if isDir(worktrees) {
		paths = append(paths, worktrees)
		entries, err := os.ReadDir(worktrees)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("watch: reading %s: %w", worktrees, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				paths = append(paths, filepath.Join(worktrees, e.Name()))
			}
		}
	}
	return paths, nil
}

// Watcher calls onChange after filesystem changes under its watched
// directories settle.
//
// onChange runs on the watcher's own goroutine. It may call SetPaths, but it
// must not call Close, which waits for the in-flight onChange to return.
type Watcher struct {
	fsw      *fsnotify.Watcher
	debounce time.Duration
	maxWait  time.Duration
	onChange func()

	mu     sync.Mutex
	paths  map[string]bool // directories currently watched, cleaned
	closed bool

	done      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

// New starts watching paths. onChange is called after a trailing debounce of
// `debounce`, but never delayed more than 2s during a continuous stream of
// events. Paths that do not exist are skipped.
func New(paths []string, debounce time.Duration, onChange func()) (*Watcher, error) {
	return newWatcher(paths, debounce, defaultMaxWait, onChange)
}

func newWatcher(paths []string, debounce, maxWait time.Duration, onChange func()) (*Watcher, error) {
	if onChange == nil {
		return nil, errors.New("watch: nil onChange")
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch: %w", err)
	}
	w := &Watcher{
		fsw:      fsw,
		debounce: debounce,
		maxWait:  maxWait,
		onChange: onChange,
		paths:    make(map[string]bool),
		done:     make(chan struct{}),
	}

	w.mu.Lock()
	err = w.setPathsLocked(paths)
	w.mu.Unlock()
	if err != nil {
		_ = fsw.Close()
		return nil, err
	}

	w.wg.Add(1)
	go w.run()
	return w, nil
}

// SetPaths replaces the watched set, for example when switching repos.
// Paths that do not exist are skipped. It does not fire onChange.
func (w *Watcher) SetPaths(paths []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}
	return w.setPathsLocked(paths)
}

// Close stops the watcher. It waits for any in-flight onChange to return, so
// onChange is never called after Close returns. Close is idempotent.
func (w *Watcher) Close() error {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.done)
		w.mu.Unlock()

		w.closeErr = w.fsw.Close()
		w.wg.Wait()
	})
	return w.closeErr
}

func (w *Watcher) setPathsLocked(paths []string) error {
	want := make(map[string]bool, len(paths))
	for _, p := range paths {
		want[filepath.Clean(p)] = true
	}

	for p := range w.paths {
		if !want[p] {
			_ = w.fsw.Remove(p)
			delete(w.paths, p)
		}
	}

	var errs []error
	for p := range want {
		if w.paths[p] {
			continue
		}
		if err := w.addLocked(p); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// addLocked watches one directory non-recursively. A missing path is not an
// error.
func (w *Watcher) addLocked(p string) error {
	if err := w.fsw.Add(p); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("watch %s: %w", p, err)
	}
	w.paths[p] = true
	return nil
}

func (w *Watcher) run() {
	defer w.wg.Done()

	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	pending := false
	var first time.Time

	for {
		select {
		case <-w.done:
			return

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if !w.handle(ev) {
				continue
			}
			now := time.Now()
			if !pending {
				pending = true
				first = now
			}
			due := now.Add(w.debounce)
			if limit := first.Add(w.maxWait); due.After(limit) {
				due = limit
			}
			timer.Stop()
			timer.Reset(time.Until(due))

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			if err != nil {
				log.Printf("watch: %v", err)
			}

		case <-timer.C:
			pending = false
			w.onChange()
		}
	}
}

// handle updates the watch set for one event and reports whether the event
// should count as a change.
func (w *Watcher) handle(ev fsnotify.Event) bool {
	name := filepath.Clean(ev.Name)
	base := filepath.Base(name)

	// Git takes <name>.lock, writes it, then renames it onto <name>. The
	// rename shows up as a Create (or Rename) for <name>, which is the
	// change we want, so the lock file itself is ignored.
	if strings.HasSuffix(base, ".lock") {
		return false
	}
	if ev.Op == fsnotify.Chmod {
		return false
	}

	parent := filepath.Dir(name)

	w.mu.Lock()
	defer w.mu.Unlock()

	// objects/ is never watched. Only refs trees may contain a directory
	// named objects, for example refs/heads/objects.
	if base == "objects" && !w.inTreeLocked(parent) {
		return false
	}
	if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
		w.forgetLocked(name)
	}
	if ev.Has(fsnotify.Create) {
		w.watchNewDirLocked(name, parent)
	}
	return true
}

// forgetLocked drops name and anything beneath it from the watch set, if it no
// longer exists. A path that has already been recreated keeps its watch entry
// and gets re-added by the Create that follows.
func (w *Watcher) forgetLocked(name string) {
	if _, err := os.Lstat(name); err == nil {
		return
	}
	prefix := name + string(filepath.Separator)
	for p := range w.paths {
		if p == name || strings.HasPrefix(p, prefix) {
			_ = w.fsw.Remove(p)
			delete(w.paths, p)
		}
	}
}

// watchNewDirLocked adds a directory created under a watched tree, along with
// its subdirectories, so nested refs such as refs/heads/feat/x are seen.
func (w *Watcher) watchNewDirLocked(name, parent string) {
	if !w.paths[parent] || !isDir(name) {
		return
	}
	base := filepath.Base(name)
	if base != "refs" && base != "worktrees" && !w.inTreeLocked(parent) {
		return
	}
	_ = filepath.WalkDir(name, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		w.addOneLocked(p)
		return nil
	})
}

// addOneLocked is like addLocked but always calls fsnotify. A directory that
// was removed and recreated has a stale map entry, so the map is not checked.
func (w *Watcher) addOneLocked(p string) {
	if err := w.fsw.Add(p); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("watch %s: %v", p, err)
		}
		delete(w.paths, p)
		return
	}
	w.paths[p] = true
}

// inTreeLocked reports whether dir lies inside a refs or worktrees tree. A
// strict ancestor only counts if it is itself watched, so the git common dir
// does not match just because it happens to sit under a directory named
// "worktrees".
func (w *Watcher) inTreeLocked(dir string) bool {
	if isTreeName(filepath.Base(dir)) {
		return true
	}
	for d := filepath.Dir(dir); filepath.Dir(d) != d; d = filepath.Dir(d) {
		if w.paths[d] && isTreeName(filepath.Base(d)) {
			return true
		}
	}
	return false
}

func isTreeName(base string) bool {
	return base == "refs" || base == "worktrees"
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
