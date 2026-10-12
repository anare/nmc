# 13 — internal/sshfs

A remote host as a `vfs.FS`, by running the `ssh` command. No Go ssh
library: the system ssh already honours `~/.ssh/config`, agents, jump
hosts, keys and everything else an operator has set up.

## Connecting

`Ctrl-\` opens a combo input (with history) for `sftp://user@host:port/path`.
`cd sftp://…` on the command line does the same.

```go
func Dial(ctx, user, host string, port int) (*FS, string, error)
```

Dials once with:

```
ssh -N -f -o ControlMaster=yes -o ControlPath=<socket> -o ControlPersist=60
```

which parks a master process and leaves a socket. Every later command
reuses it with `ControlMaster=no` and `BatchMode=yes` — no second
handshake, no second prompt, and those commands can never block on one.

The socket lives in a `0700` directory under `os.UserCacheDir()` —
`$XDG_CACHE_HOME/bc/ssh/` on Linux, `~/Library/Caches/bc/ssh/` on macOS —
falling back to a per-uid directory under `$TMPDIR`. `App.Close` sends
`ssh -O exit`, which also removes the socket.

### The interactive part

**Only the master connection can prompt.** The screen is suspended, bc
prints which host it is connecting to — a blank screen needs explaining, or
it looks like a crash while ssh waits — and ssh gets the real terminal.

`Dial` returns **what ssh said** as well as the filesystem, because the
caller is about to draw over the terminal ssh said it on. stderr is
**teed**, not captured: `io.MultiWriter(os.Stderr, &buf)` keeps warnings
visible live *and* keeps a copy for after the redraw.

Without that, `Permission denied (publickey)` was replaced by
`exit status 255` the instant bc came back. See
[16-terminal-and-tool-facts.md](16-terminal-and-tool-facts.md) item 6.

`tidySSHError` picks the last line that is not a routine
`Warning: Permanently added …` or a `debug1:` line — the real reason comes
*after* the banner. The failure dialog shows that plus anything else ssh
printed; a warning on a *successful* connection goes to the status line.

## Listing

Platform is probed once per connection and cached:

```
stat --version >/dev/null 2>&1 && echo gnu || (stat -f %z . >/dev/null 2>&1 && echo bsd || echo ls)
```

Three parsers, all tested against captured real output:

| Platform | Command |
| --- | --- |
| gnu | `stat --printf 'L\t%F\t%s\t%Y\t%f\t%h\t%n\n' -- *` |
| bsd | `stat -f 'L\t%HT\t%z\t%m\t%Xp\t%l\t%N\n' -- *` |
| ls | `LC_ALL=C ls -lAn --time-style=+%s` |

Plus, in the same round trip, a loop for symlink targets:

```sh
for f in *; do if [ -L "$f" ]; then printf 'R\t%s\t%s\n' "$f" "$(readlink -- "$f")"; fi; done 2>/dev/null
```

`readlink` rather than a stat format, because the formats that print a
target **quote** it — GNU `%N` gives `'link' -> 'target'` — and unpicking
those quotes is guesswork the moment a name contains one.

The `if` form rather than `[ -L "$f" ] && printf`: the `&&` form leaves a
non-zero status when the last entry is not a link, and that status is read
as the listing's. Confirmed on a real shell.

### The exit status must come from the `cd` alone

```
cd -- <dir> || exit 1; stat …; <readlink loop>; stat -L …; exit 0
```

The trailing `stat -L` pass — the one deciding which links resolve — exits
non-zero when any of them does not. Without `exit 0`, **a directory
containing one broken symlink failed to list at all.** A missing directory
is still an error and an empty one is still empty; all four cases were
verified in a container.

## Transfers

`tar` over ssh for directories; `ssh host cat` for single files, which
gives an exact byte count for the progress bar.

## Failure

Every call has a timeout (`BC_SSH_TIMEOUT`, default 30s). `ErrConnectionLost`
on a dead socket; `Ctrl-R` on a remote panel reconnects and relists, so a
dropped link is one keystroke to recover rather than a restart.

Shell quoting goes through **one** helper, `shellQuote`, used by every
command. A hostile-name test file exercises it. No command text is built by
string concatenation anywhere else.

## Caching

`vfs.Cached` wraps the remote FS: listings, platform probe, stats and small
file contents. `Ctrl-R` always invalidates and re-reads — it is the "I know
better" key and must never be served from cache.

## Testing

A fake `ssh` script on `PATH` answers canned responses keyed by the exact
command line. **It exits 0 for anything it does not recognise**, so a test
that only checks the canned response proves nothing — assertions must also
check the recorded command line. That was verified by mutation: unquoting
the paths in `Symlink` made the tests fail as they should.

Integration tests against a real `sshd` sit behind `BC_SSH_IT=1`, skipped
by default.
