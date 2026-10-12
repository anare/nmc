# 03 — internal/vfs

The filesystem abstraction. Everything a panel can do goes through it, so
a feature written once works locally, remotely and inside archives.

## The interface

```go
type FS interface {
    // Name identifies the backend in the panel title: "" for local,
    // "user@host" for remote, "tar:x.tar.gz" for an archive.
    Name() string

    List(ctx context.Context, dir string) ([]Entry, error)
    Stat(ctx context.Context, path string) (Entry, error)

    Open(ctx context.Context, path string) (io.ReadCloser, error)
    Create(ctx context.Context, path string, mode os.FileMode) (io.WriteCloser, error)

    Mkdir(ctx context.Context, path string) error
    Remove(ctx context.Context, path string) error   // recursive
    Rename(ctx context.Context, from, to string) error

    Symlink(ctx context.Context, target, link string) error
    Readlink(ctx context.Context, path string) (string, error)
    Link(ctx context.Context, target, link string) error   // hard link

    // Path arithmetic belongs to the backend: a remote host is always
    // slash-separated whatever the client runs.
    Join(elem ...string) string
    Dir(path string) string
    IsAbs(path string) bool
}
```

`Name() == ""` means "the host's own filesystem" and `fileop` branches on
it. Getting this wrong is the single most damaging mistake available in
this package — see [01-overview.md](01-overview.md).

Backends that hold resources also implement `io.Closer`; the app collects
those and closes them on exit.

## Entry

```go
type Entry struct {
    Name    string
    IsDir   bool
    IsLink  bool
    Broken  bool      // symlink whose target cannot be stat'ed
    IsUp    bool      // the ".." pseudo-entry
    Size    int64
    ModTime time.Time
    Mode    os.FileMode

    Target      string // where a symlink points, "" otherwise
    Links       int    // names this file has on disk; 0 = backend did not say
    SizeUnknown bool   // the backend reported no size
}
```

**For a symlink, the metadata is the link's own (`lstat`) but `IsDir`
reflects the target**, so a link to a directory behaves like one when you
press Enter.

`SizeUnknown` exists because an archive listing usually gives names and
nothing else. Showing `0` there is a claim nobody measured and is
indistinguishable from an empty file; the panel shows `--`.

### Derived properties

| Method | Returns |
| --- | --- |
| `IsExec()` | regular file with any execute bit |
| `IsSpecial()` | device, socket or named pipe |
| `IsHardLink()` | `Links > 1`, and not a directory or symlink |
| `Ext()` | lower-cased extension, no dot; `.bashrc` has none |
| `Indicator()` | the `ls -F` suffix: `/ * @ = |` or `""` |
| `SizeString(w)` | `UP--DIR`, `DIR`, `!LINK`, `SPECIAL`, `--`, a count, or a human size |
| `Class()` | which colour group — see below |

`IsHardLink` excludes directories because every directory has at least two
links by construction, so the count says nothing there.

## File classification

One function, `Class()`, returns exactly one of thirteen classes:

```
ClassPlain ClassDir ClassExec ClassLink ClassBroken ClassSpecial
ClassArchive ClassImage ClassMedia ClassSource ClassDoc ClassData ClassTemp
```

Driven by two maps — `extClass` (extension → class) and `nameClass` (whole
name → class, for `Makefile`, `.gitignore`, `README`, `go.mod`). **The
grouping function panics at init on a duplicate extension**, so "an
extension belongs to exactly one class" is a property of the data rather
than of the order of a long if-chain.

**What a file *is* beats what it is called.** Mode bits are checked first,
so a directory named `notes.txt` is a directory and an executable
`build.sh` is runnable rather than source. Only an ordinary file is
classified by name. A trailing `~` means a backup whatever the extension
says.

## Implementations

### Local

`os` calls, with `ctx.Err()` checked at the top of each. Fills `Target` for
symlinks (one extra `Readlink`, only for links) and `Links` from
`syscall.Stat_t.Nlink` — which is `uint16` on darwin and `uint64` on linux,
so it lives behind a build tag (`nlink_unix.go` / `nlink_other.go`).

`List` prepends `..` unless the directory is the filesystem root.

### Cached (decorator)

Wraps any FS. Caches directory listings (TTL), `Stat` results derived from
listings, and the content of small files. Every mutating call invalidates
the path **and its parent's listing**. Concurrent `List` of the same
directory is collapsed with singleflight.

`Readlink` is deliberately **not** cached: a target is only read just
before it is shown or written back, and a stale one would be written into
the filesystem.

### MemFS

For tests. `Put(dir, entries)` defines a listing. Tracks blobs, symlink
targets and hard links. Use a non-empty label when a test needs the backend
to look remote.

### Location

`ParseLocation` reads what the user or the command line gives:
```
/var/log                   → local, absolute
./relative                 → local, made absolute
sftp://user@host:2222/var  → remote
```
An unconnected remote location is a `remoteRef` whose every method returns
`errNotConnected` — so a panel can point at a host before Ctrl-\ dials it.

## Sorting

`SortMode`: `SortByName`, `SortByExt`, `SortBySize`, `SortByTime`.
Directories first, then the chosen field, then case-insensitive name as the
tie-break. `..` always first.

`ParseSortMode` reads a mode back from `~/.bcrc`, forgiving about case and
accepting `time` for `Modify time`. **It returns `ok=false` for anything it
does not recognise** rather than falling back to `SortByName`, so the
caller can tell a typo from a choice and leave the setting alone.
