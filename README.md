# hrt — Head, Refs & Tips

A tiny side-window that shows where every branch and pull request sits relative to `origin/main`.

Built for AI-agent-driven development, where many branches move in parallel and what matters is
*where each one forked, how far behind it is, and whether it's mergeable* — not every commit in between.

- One point per branch; `origin/main` collapses to its divergence and convergence points.
- Reads your local repo with `git` and pull requests with `gh` (works with private repos via your `gh auth`).
- Hide branches you don't care about (by name, glob, or automatic rules). Rules stay private to you.
- Themes: default, babydoll, dolly. Compact mode for narrow windows.

## Status

Early development.

## Requirements

- `git` ≥ 2.38 and the GitHub CLI `gh` (logged in) on your `PATH`
- To build: Go ≥ 1.26, Node ≥ 20, [Wails v3](https://v3.wails.io) (`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`)
- Linux: GTK4 + WebKitGTK 6.0 (`webkitgtk-6.0`)

## Develop

```sh
wails3 dev        # run with hot reload
wails3 build      # produce bin/hrt
```

Run `hrt [path]` inside a repository (defaults to the current directory).
