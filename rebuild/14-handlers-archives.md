# 14 — internal/handlers, internal/archivefs

External commands for archives and previewing. There are too many archive
formats to implement and the tools already exist; the useful thing is a way
to call them.

## ~/.bchandlers

```ini
[archive tar]
match   = *.tar, *.tar.gz, *.tgz, *.tar.bz2, *.tbz, *.tar.xz, *.txz, *.tar.zst
list    = tar -tf "$ARCHIVE"
extract = tar -xf "$ARCHIVE" -C "$DEST" -- "$ENTRY"

[archive zip]
match   = *.zip, *.jar, *.war, *.whl, *.apk, *.egg
list    = unzip -Z1 -- "$ARCHIVE"
extract = unzip -o -q -d "$DEST" -- "$ARCHIVE" "$ENTRY"

[open images]
match = *.jpg, *.png, *.gif, *.svg, *.heic
run   = qlmanage -p "$FILE" >/dev/null 2>&1
```

### Paths never go into the command text

They arrive in the **environment** of an `sh -c`: `$ARCHIVE`, `$ENTRY`,
`$DEST`, `$FILE`. Pasting a path into command text makes every file name
containing a quote a bug, and some of those bugs run as commands. There is
a test with a file called `a $(touch /tmp/pwned) 'b.txt` that checks
nothing was executed.

### Resolution order

1. a named pattern in the file
2. a named pattern bc ships
3. a catch-all in the file
4. a catch-all bc ships

**Specificity first**, because a catch-all is not a statement about jpegs —
it is "anything else". **The file ahead of the defaults**, because it is the
user's.

**The shipped handlers are consulted at all** because the file is written
only once. Without that, a handler added in a later version would never
reach anyone who had already run an earlier one — which happened: a
`~/.bchandlers` written before `[open images]` existed meant jpegs
hex-dumped forever.

`F2 > File handlers` lists what is in play, marks with `!` any whose
program is not installed, says whether each came from your file or from bc,
and opens the file for editing. Leaving the editor reloads it.

bc writes the file only when it is missing, so comments and hand-written
handlers survive. There is **no Save**: it is a file for people to edit, and
round-tripping comments is a promise not worth making.

## archivefs

An archive as a `vfs.FS`. Knows no formats: the listing comes from running
the handler's `list` command once, and a file is produced by running
`extract` into a temporary directory.

```go
func Open(ctx, h handlers.Handler, archive string) (*FS, error)
```

- **Read-only.** Writing back means rewriting the archive, a different
  operation with different failure modes; a panel write that only appeared
  to go in would be worse than a refusal. Every mutating method returns
  `ErrReadOnly`.
- **`Name()` is `tar:src.tar.gz`, never `""`** — `fileop` reads an empty
  name as "the host's own filesystem" and would copy out with plain file
  operations.
- **Parents are inferred.** `tar -tf` lists files, so a zip containing
  `a/b/c.txt` may never mention `a` or `a/b`; without inferring them,
  walking in stops at the first level.
- **Extraction is cached** for the life of the panel. Viewing a file and
  then copying it should not run the archive tool twice, and an archive does
  not change while it is open. `Close` removes the scratch directory.
- A `list` line may be `size<TAB>path`; without a size the entry is
  `SizeUnknown` and the panel shows `--`.

### In the app

`Enter` on an archive walks in (`Panel.EnterFS`); `..` or `Left` at the
root walks out. **The root offers `..`** — leaving it out made the root a
dead end whose only exit was a key you had to already know.

`F5` copies out, through the same cross-filesystem machinery a remote panel
uses. `F3` on an archive lists its contents instead of hex-dumping the
compressed bytes. Archives open **only on the local filesystem**, because
the handler commands run on this machine; bc says so rather than failing
obscurely.

## Open-with

`Alt-W` or `Space` (on an empty command line) hands the file to its
configured program. A file inside an archive or on a remote host is copied
here first, since another program cannot open a path that only exists in a
`.tar.gz`.

The child is **started and not waited for** — a previewer is a window the
user closes — with a goroutine to reap it and remove any temporary copy.

`F3` also defers to a handler, but **only a named one**: letting the
catch-all take F3 would send every text file to an external viewer. A
binary with no handler of its own still hex-dumps, and if the configured
program is missing F3 says so *and shows the file anyway*.
