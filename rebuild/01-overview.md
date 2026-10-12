# 01 — Overview

## What bc is

A two-panel file manager for the terminal, modelled on Midnight Commander.
Two file lists side by side, a command line underneath, function keys along
the bottom. It exists because its author's security team would not allow
`mc` on their machines.

It does: browsing, copying, moving, deleting, renaming, linking; viewing
and editing files; running shell commands; browsing remote hosts over ssh;
browsing inside archives; finding files by name and content; showing git
history and committing. All of it from the keyboard.

## Tech stack

| Dependency | Version | Why |
| --- | --- | --- |
| `github.com/gdamore/tcell/v2` | 2.13.10 | Terminal control |
| `github.com/mattn/go-runewidth` | 0.0.19 | Display width of wide characters |
| `github.com/alecthomas/chroma/v2` | 2.27.0 | Syntax highlighting, 200+ languages |
| `github.com/creack/pty` | 1.1.24 | The persistent subshell |
| `golang.org/x/term` | 0.46.0 | Raw mode for the subshell |

**tcell rather than tview or Bubble Tea.** bc draws panels, a text viewer,
an editor and a hex dump — all of them custom. A widget toolkit would be
something to work around rather than with. tcell is the layer that gives a
cell grid and key events and nothing else, which is exactly the amount of
help this needs.

Go 1.24+ (the code uses `for range N` over integers and `min`/`max`).

## Package map

```
cmd/bc              flags, screen lifecycle, panic-safe terminal restore
internal/
  app       5300    the application: event loop, screens, key dispatch
  vfs       1600    filesystem interface, local backend, cache, entry model
  dialog    1500    modal boxes, forms, text input, combo box
  sshfs     1400    remote filesystem over the ssh command
  editor    1200    the F4 editor
  fileop    1000    copy, move, delete — no UI dependency
  keymap     980    bindings, ~/.bcrc, key normalisation
  viewer     900    the F3 viewer, hex dump, search
  panel      780    one file list: state, cursor arithmetic, rendering
  shell      780    running commands, the Ctrl-O session, scrollback
  handlers   500    ~/.bchandlers: archive and open commands
  git        440    history, diffs, commit, branches
  ui         390    drawing primitives, theme, hint bar
  archivefs  370    an archive presented as a vfs.FS
  find       300    find by name and content
  history    300    persistent input history
  syntax     180    chroma wrapper
  clipboard  120    copied text, in a file
```

## The dependency rule

**Nothing below `app` knows about the terminal unless its job is drawing.**

- `vfs`, `fileop`, `find`, `history`, `git`, `handlers`, `clipboard`,
  `archivefs`, `shell` import no tcell at all. They are testable without a
  TTY and have no opinion about how anything looks.
- `ui`, `panel`, `dialog`, `viewer`, `editor` draw, so they take a
  `tcell.Screen` — but they are given one, they never create one.
- `app` is the only package that owns a screen, an event loop or global
  state.
- `cmd/bc` is 89 lines: parse flags, make a screen, run the app, restore
  the terminal if anything panics.

This is what makes 830 tests possible. Everything that draws is tested
against `tcell.NewSimulationScreen`, which gives a real cell grid with no
terminal behind it; everything else is tested as plain Go.

## The central abstraction

`vfs.FS` is an interface with 14 methods. Four things implement it:

| | |
| --- | --- |
| `vfs.Local` | the host's own filesystem |
| `sshfs.FS` | a remote host, by running `ssh` |
| `archivefs.FS` | inside a `.tar.gz`/`.zip`/`.7z`, by running the archive tool |
| `vfs.MemFS` | in memory, for tests |

Plus `vfs.Cached`, a decorator that wraps any of them.

A `Panel` holds an `FS` and a path. Everything a panel does — listing,
copying, viewing, editing, searching — goes through the interface, so
features arrive on remote hosts and inside archives for free. When a panel
walks into a `.tar.gz` it swaps its `FS`; nothing else changes.

**The one rule that makes this work:** `FS.Name()` returns `""` for the
host's own filesystem and something non-empty for everything else.
`fileop` uses it to decide between the fast local path and the streaming
one. An implementation that returns `""` when it is not local will have its
files copied with `os.Rename` and fail strangely.
