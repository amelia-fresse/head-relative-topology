// Package execx runs external tools (git, gh) without flashing console
// windows on Windows and with consistent error messages.
package execx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Run executes name with args in dir and returns trimmed stdout.
// On failure the error includes the command and its stderr.
func Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	out, err := RunRaw(ctx, dir, name, args...)
	return strings.TrimRight(string(out), "\r\n"), err
}

// RunRaw is Run without trimming, for callers that parse exact output.
func RunRaw(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	hideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), &Error{Cmd: name + " " + strings.Join(args, " "), Stderr: msg, Err: err}
	}
	return stdout.Bytes(), nil
}

// LookPath reports whether a tool is installed.
func LookPath(name string) (string, error) { return exec.LookPath(name) }

// Error is returned when a command exits unsuccessfully.
type Error struct {
	Cmd    string
	Stderr string
	Err    error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Cmd, e.Stderr) }
func (e *Error) Unwrap() error { return e.Err }
