# 12 — internal/find, internal/history

## Find (Alt-F)

A form:

```
Find
  Start in:       /home/user/project
  File name:      *.go
  Containing:
  [ ] Case sensitive   [ ] Whole words
  [ ] Include hidden   [ ] Search binaries
```

Every text field has its own history (`Alt-P`/`Alt-N`). Arrows and
Tab/Shift-Tab move between fields.

### Searching

Walks through `vfs.FS`, so it works on a remote panel and inside an
archive. Name matching is a glob; content matching is literal, with
optional case sensitivity and word boundaries. Binary files are skipped
unless asked for.

**It runs on a goroutine and streams hits into the screen as they arrive**,
so a search over a large tree is useful immediately. `Ctrl-C` stops it and
keeps what it found.

Remote searches run **one command** (`find` or `grep -rn`) rather than
walking a tree over the network.

### Results

```
 Find: *.go                                      42 hits  searching…
 internal/app/keys.go:214
 internal/app/view.go:31
```

`F3`/`Enter` opens the hit — in the viewer, at the matching line.
`F5` **panelises**: the hits become the panel's listing, so F5 and F8 can
act on them. `F7` searches again, `F10`/`q` leaves.

Paths are shown relative to the search root, not absolutely — a column of
`/home/user/projects/thing/internal/...` is unreadable.

## internal/history

Persistent input history, one list per kind.

```go
type Kind string   // Command, Path, Dir, Search, Goto, Host
type Store struct{ ... }
func (s *Store) Add(k Kind, value string)
func (s *Store) Cursor(k Kind) *Cursor
func (s *Store) Save() error
```

A `Cursor` walks one list: `Prev`, `Next`, `Reset`. Every input in the
program takes one, so Alt-P/Alt-N work everywhere — the find fields, the
connect dialog, the copy destination, the commit message.

Stored under `$XDG_STATE_HOME/bc/history` (`~/.local/state/bc/history` by
default). **Not** in `~/.bcrc`: that is configuration, meant to be edited;
this is state, written constantly.

Duplicates move to the front rather than being added again. Each list is
capped. Saved atomically on exit.

### A warning for tests

The store is loaded from the environment, so **tests must set
`XDG_STATE_HOME`** or they read and write the history of whoever runs
`go test`. This happened: `sftp://user@testhost/path` and `ls -la` turned
up in the author's real history file.

The same applies to `HOME` once `~/.bcrc` exists. bc's test helper sets
both:

```go
func isolateState(t *testing.T) {
    t.Setenv("XDG_STATE_HOME", t.TempDir())
    t.Setenv("HOME", t.TempDir())
}
```

## The history browser (Alt-H)

A picker over the focused field's history, with type-to-filter and Ctrl-D
to forget an entry.
