# 05 — internal/panel

One file list. Two of them side by side are the program.

## State

```go
type Panel struct {
    FS   vfs.FS
    Cwd  string
    Entries []vfs.Entry
    Marked  map[string]bool
    Cursor, Top int          // selected index, first visible row
    Opt  vfs.Options         // sort mode, show hidden
    Err  error               // last listing failure, shown in the footer

    rect ui.Rect
    memo map[string]string   // directory → name the cursor was on
    prevFS vfs.FS            // saved by ConnectRemote
    prevCwd string
    back []returnTo          // stack for EnterFS / LeaveFS
}
```

## Cursor and scrolling

`MoveBy`, `MoveTo`, `PageBy`, `Home`, `End`. All clamp; an empty panel is
safe. `PageBy` keeps **one row of overlap** so you can read across a page
boundary. `SetRect` re-clamps the scroll offset, so resizing never leaves
the cursor off screen.

`VisibleRows()` is the rect height minus `chromeRows` (5: top border,
column headings, separator, footer, bottom border).

## Navigation

- `Enter()` — into a directory, or up if the cursor is on `..`. Returns
  `handled=false` for a file so the caller can try something else (an
  archive, or a message).
- `GoUp()` — to the parent, selecting the directory just left.
- `SetDir(dir, selectName)` — relist and position the cursor.

**Each directory remembers where the cursor was.** `memo` is keyed by path
and written on the way out, so walking down and back up returns you to the
entry you came from. This is one of those details nobody notices until it
is missing.

**A failed listing keeps the old one.** `SetDir` records the error in
`Err` and leaves `Entries` alone, so a panel pointing at a directory that
has gone still shows something and says why in the footer.

## Marking

`ToggleMark()` flips the current entry and steps down, so holding Ctrl-T
marks a run. `..` can never be marked. `Targets()` returns the marked
entries, or the one under the cursor when nothing is marked — the rule
every file operation uses.

## Switching filesystems

Two mechanisms, deliberately separate:

- `ConnectRemote(fs, path)` / `DisconnectRemote()` — one saved place, used
  by Ctrl-\.
- `EnterFS(fs, path, backTo)` / `LeaveFS()` / `Nested()` — a **stack**, used
  for archives, so an archive inside an archive can be left one level at a
  time.

They do not disturb each other: connecting to a host is a property of the
panel that survives walking into and out of an archive on it.

`EnterFS` restores the previous FS if the new one cannot be listed — a panel
pointing at a filesystem it cannot read is worse than one that never moved.

## Panelise

`Panelize(root, entries)` replaces the listing with an arbitrary set, which
is how search results become something F5 and F8 can act on. Names are kept
relative to `root` so `CurrentPath()` still resolves. Ctrl-R is the way
back: `Reload()` re-reads the directory and the panelised view is gone.

## Rendering

```
┌─ /home/user/project ─────────────────┐
│Name              │  Size  │Modify time│
│..                │ UP--DIR│Oct  5 18:06│
│sub/              │     DIR│Oct  5 18:06│
│run.sh*           │      18│Oct  5 18:06│
│notes.md          │     412│Oct  5 18:06│
├──────────────────────────────────────┤
│notes.md                           412│
└──────────────────────────────────────┘
```

**Draw order matters.** Column dividers are drawn *before* the title, or
they overwrite it. On the cursor row the dividers take the selection bar's
style, or the bar has holes in it.

### Columns

Name, size, modify time. **Narrow panels drop the time column first and the
size column second** rather than overflowing. Below 40×10 the app says the
terminal is too small instead of drawing a broken frame.

### Names

`displayName(e, width)` appends `Indicator()`. **The indicator survives
truncation** — a long name is cut one character shorter to keep it. The last
character of a long name is almost never what distinguishes it, the
indicator always says something, and dropping it would mean the names most
likely to need it never show it. `a-directory-with-a-long-name` becomes
`a-di~/`.

### The footer

Describes the entry under the cursor, and is the one place with room to say
more than a name:

| State | Shows |
| --- | --- |
| marked files | `3 files marked, 1.2M bytes` |
| symlink | `config -> /etc/app.conf`, or `-> (broken) ` |
| hard link | `data.db   3 links` |
| listing error | the error, in the broken-link colour |
| otherwise | the name and size |

### Scrollbar

A track and thumb on the right edge, only when the list overflows. The
position marker's width must be included in the dialog width calculation —
forgetting that truncates it to `1-16 of 1~`.
