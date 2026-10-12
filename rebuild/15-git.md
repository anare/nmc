# 15 — internal/git

History, diffs, commit and branches, by running `git`.

Shelling out rather than linking a library: git is installed wherever
anyone would want this, it understands every repository layout bc might
meet, and it stays correct as repositories gain features bc has never heard
of. A process per question is nothing against a human deciding what to look
at next.

Nothing here rewrites history: no amend, no rebase, no force, and bc never
pushes.

## Reading

```go
func Root(ctx, dir) (string, error)                     // ErrNotARepo
func Log(ctx, dir, path string, limit int) ([]Commit, error)
func Show(ctx, dir, hash, path string) ([]byte, error)
func Diff(ctx, dir, path string) ([]byte, error)
func Describe(ctx, dir) string                          // branch, or "detached at abc1234"
```

```go
type Commit struct {
    Hash, Short, Author, Subject string
    When time.Time
}
```

### The log format

```
--pretty=format:%H%x00%h%x00%an%x00%at%x00%s
```

NUL between fields, newline between records. Safe because **`%s` is the
subject line and git guarantees it holds no newline** — verified with a
multi-line commit message, and with subjects containing pipes and tabs.

`--follow` is added for a regular file, since tracking it through renames
is most of the point of asking for its history; git only accepts it for a
single file, so directories go without.

### Two things that are not obvious

**`git log` exits non-zero in a repository with no commits.** That is an
empty history, not a failure, and the message is translated so it cannot be
matched on. Ask whether `HEAD` exists instead — on the failure path only,
so it costs nothing normally.

**Pathspecs are made relative to the root.** Not because git mishandles an
absolute one — it resolves them itself, symlinks included, and this was
checked directly — but because relative is what git's documentation uses, it
keeps the command repeatable by hand from an error message, and a path
outside the repository then fails with a sentence.

## Writing

```go
func CurrentStatus(ctx, dir) (Status, error)
func CommitChanges(ctx, dir, message string, stageAll bool) (hash string, err error)
func Branches(ctx, dir) ([]Branch, error)
func Switch(ctx, dir, name string) error
```

`Status` separates `Staged`, `Modified` and `Untracked`. Read with
`git status --porcelain=v1 -z` — **`-z`** because git otherwise escapes
paths containing quotes or spaces, and unescaping is guesswork. A rename
record is followed by its source path, which must be skipped.

`CommitChanges` is named for the verb because `Commit` is the type a log
entry comes back as.

### Refusals are passed through whole

```go
func runDetail(ctx, dir, args...) ([]byte, error)   // error carries all of stderr
```

git's refusal to switch branches names the files in the way and says what
to do about them — and all of that is **after** the first line:

```
error: Your local changes to the following files would be overwritten by checkout:
	a.txt
Please commit your changes or stash them before you switch branches.
```

Read-only commands keep one line (`run`); the ones that can refuse keep the
whole message (`runDetail`). `git commit` also writes "nothing to commit"
to **stdout**, not stderr, so `runDetail` looks at both.

## The history screen (Alt-G, or F3 on `..`)

```
 History: zebra.txt                                   main  2 commits
 *        now        working tree    uncommitted changes
 f509b2e  2026-10-05 Ada Lovelace    a second line, and some notes
 9f04b87  2026-10-05 Ada Lovelace    initial import
```

Alt-G on a file, a directory, or the panel's own directory when the cursor
is on `..`. **F3 on `..` opens the same thing** — there is no file there to
view, so the key was dead; what `..` stands for is the directory you are
in, and its history is the one thing worth showing for it.

| Key | Does |
| --- | --- |
| `Enter` `F3` | the selected change as a diff |
| `F4` `a` | the whole commit, unnarrowed |
| `F5` `c` | commit |
| `F6` `b` | switch branch |
| `q` `Esc` `F10` | back |

**The diff is narrowed to what the history was asked about.** A commit
touching two hundred files is unreadable when you came for one; F4 is there
for when the rest is the point.

**Uncommitted changes are the first row**, not a command of their own. They
are the newest thing that happened to the file, so they sit where the
newest commit would, and one key reaches both. A clean tree has no such
row.

Diffs open in the viewer through `FromBytesAs(name, "commit.diff", data)`,
so chroma colours them without the title claiming to be a file called
`.diff`.

The last 300 commits are read; the header says so when the list is cut
short.

**Local only.** The commands work against a working tree on this machine,
and bc says so rather than failing obscurely on a remote panel.

### Committing

```
Commit
  Message:
  [x] Stage 2 other changes first (git add -A)
```

The box **states its own effect**, with a count from `git status`, so the
choice can be made without going to look. It starts ticked only when
nothing is staged — otherwise the commit would be empty; with something
staged, that staging is a decision to respect. A clean tree is refused
before the form opens.

The flow the screen is built around: `Alt-G`, `Enter` on the uncommitted
row to read the diff, `q`, `c` to commit what you just read.

### Switching

Local branches, most recently committed to first, current one marked `*`,
in a picker with type-to-filter.

**Both panels are reloaded afterwards**, because the working tree has
changed underneath them and a stale listing would show files that are no
longer there.

### A note for tests

The app tests point `HOME` at a temp directory, so git has no global
config and a commit is refused with *"Please tell me who you are"*. Give
the test repository a local identity:

```go
runGit(t, dir, "config", "user.name", "Ada Lovelace")
runGit(t, dir, "config", "user.email", "a@example.com")
```

That failure is worth one test of its own, since it is exactly what a
first-time user sees.
