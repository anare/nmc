# 09 — internal/fileop, and the file commands

`fileop` has no UI dependency at all. It is given paths and filesystems and
reports progress through a callback.

## Operation

```go
type Operation struct {
    Op     string   // "Copy", "Move", "Delete"
    Pairs  []Pair   // source → destination
    Paths  []string // delete only
    Policy Policy   // Fail, Overwrite, Skip

    SrcFS vfs.FS    // nil means the host's own filesystem
    DstFS vfs.FS
}
```

`Run(ctx, op, report)` executes it on the caller's goroutine — the app runs
it on its own and posts progress back as events.

**Both filesystems nil is the fast path**: a local copy goes through the
`os` package directly. `acrossFilesystems()` decides, using the
`Name() == ""` rule.

## Progress

```go
type Progress struct {
    Op, File string
    FileDone, FileTotal int64
    Done, Total int64
    Items, ItemTotal int
    Started time.Time
}
```

`Percent()` prefers the byte total and falls back to the item count.
**Both branches clamp.** Only one of them did, and the one that did not was
the branch used when no byte total is known — exactly where a miscount is
most likely, because there is nothing else to check it against.

Total bytes are counted before the first byte moves, so the bar means
something from the start. Across filesystems the bar moves a whole item at
a time, because `CopyFS` streams without reporting — a bar that jumps is
better than one that lies.

### The crash this caused

`runTransfer` counts each pair as it finishes. `transferFS` counted the
same pair **again**, so `Items` reached twice `ItemTotal`. On a local copy
that was invisible because `Percent` used the byte total and clamped. An
archive reports no sizes, so the item count was all there was — and that
branch had no clamp. 200% asked the progress bar to repeat a negative
number of characters:

```
panic: strings: negative Repeat count
```

The copy itself had already succeeded. Three fixes, only the first of which
was the bug: stop double-counting; clamp both branches of `Percent`; clamp
inside the drawing code, because a number produced elsewhere must not be
able to kill the program from inside a draw.

## Planning

`Plan(srcs, dest)` for local, `PlanFS(ctx, srcFS, srcs, dstFS, dest)` for
anything else. An existing **directory** receives the sources under their
own names; any other path renames a single source and is refused for
several. Copying something onto itself, or a directory into its own
subtree, is refused before anything is touched — but only when both ends
are the same filesystem, where the comparison means something.

`ConflictsFS` lists destinations that already exist so **one** dialog can
ask about the whole batch: Overwrite, Skip, Cancel.

## Copying

- Directories recurse.
- Symlinks are copied **as symlinks**, not followed.
- Permissions and modification times are preserved.
- A move is a rename where possible, falling back to copy-and-delete across
  filesystems.
- One failure does not abandon the batch: the rest run and the failures are
  reported together.
- **A zero mode falls back to `0644`** — a backend reporting no permissions
  must not produce a file nobody can read.
- A cancelled copy removes the partial destination; a cancelled move leaves
  its source alone.

## The commands

### F5 Copy / F6 Move

Prompt for a destination, prefilled with the other panel's directory.
The destination's **own** filesystem is asked whether it is a directory —
asking the local one about a remote path always says no, and every source
then lands on the same single file.

### F8 Delete

Confirms first, and says explicitly when a directory is going with
everything inside it. Goes through `SrcFS.Remove` when the panel is not
local.

### F7 Mkdir

Creates in the active panel and selects the result.

### Shift-F6 / Alt-R — Rename

Prompts for a *name*, prefilled with the current one, rather than a
destination directory. A name containing `/` is treated as a path, which
renames and moves in one go. Renaming onto something that exists asks
first. **Several marked files is refused** rather than silently renaming
only the one under the cursor.

### Shift-F5 / Alt-L / Ctrl-X s — Create link

A from-and-to form with a kind:

```
Create a link
  From (existing): /var/log/messages
  To (link name):  /home/u/log
  [x] Symbolic link (off = hard link)
```

Symbolic is the default: it works across filesystems and on directories.
A symlink target is **not** checked — a link to something not there yet is
legal and sometimes the point. A hard link **is** checked, because it has
to resolve now, and naming a directory is refused in words rather than left
to the OS.

Neither kind crosses filesystems, so the other panel's directory is only
offered as a default when both panels are on the same one.

### Shift-F4 / Alt-E / Ctrl-X e — Edit a symlink

Shows the link as the pair it is; either half can change. Changing the
target repoints it, changing the path renames it, changing both does both.

**Repointing is a create beside the old one followed by an atomic rename
over it** — there is no system call for changing a link's target, and
removing it first would lose the link if the create then failed. The
repoint happens before any rename, so a failure anywhere leaves the link
where it was, pointing where it was.

The target is re-read from disk when the form opens rather than taken from
the listing: it is about to be written back.

`F4` on a symlink opens this form rather than the editor — the file can be
edited from wherever it lives, the link only from here. The form carries an
**Edit file** button for the other intention, left off when the link is
broken.
