# 16 — Empirical findings

**Read this before writing any key handling.**

Every item here was found by a bug, and verified against the real thing —
terminfo, tcell's source, a captured byte stream on a pty, a container
running GNU coreutils. None of them is true by reasoning. A
reimplementation that reasons from first principles will get them wrong in
the same way.

---

## Terminals and tcell

### 1. tcell describes one key press three ways

A real Ctrl-O arrives as `Key=KeyCtrlO` (79) **and** `Mod=ModCtrl`, and
`ev.Rune()` synthesises `'o'`. Comparing a whole event against a binding
built from the name `"Ctrl-O"` therefore never matches. The redundant parts
must be dropped. See [07-keymap.md](07-keymap.md).

*Found by: no Ctrl binding firing at all. Diagnosed by printing the real
values tcell produced.*

### 2. Shift+F5 arrives as F5+Shift — but may arrive as F17

terminfo for `xterm-256color` defines no `kf17` the way you would guess: it
calls `\E[15;2~` — the sequence for Shift+F5 — **`kf17`**. Shifted function
keys are F13–F24 in terminfo's model.

tcell 2.13's `prepareKeys` no longer registers terminfo keycodes at all;
`input.go` parses CSI generically, matching `{M:'~', P:15}` → `KeyF5` and
decoding the `;2` through `calcModifier` into `ModShift`. **So on xterm
and clones, Shift+F5 arrives as F5+Shift.**

But a terminal that sends the plain `\E[31~` instead arrives as a bare
`F17`. Fold F13–F24 into Shift+F1–F12 and one binding covers both. The cost
— a real F13 cannot be bound separately — is the right trade.

*Verified in: `/usr/share/terminfo` via `infocmp`, and tcell's `input.go`.*

### 3. Many terminals send nothing at all for shifted function keys

This is the normal case, not a fault. A terminal only passes on
combinations it has an escape sequence for. When it has none, the key
either arrives unmodified or never arrives.

**Therefore: never make a shifted function key the only way to reach a
command.** Every such binding in bc has an Alt spelling listed *first*,
because anything showing one key per action shows the first.

### 4. Alt+arrows are usually unreachable; Ctrl+arrows are taken

Apple Terminal's `xterm-256color` defines **no** `kLFT5` or `kLFT3`, only
`kLFT=\E[1;2D`. And macOS binds Ctrl+arrows to Mission Control before any
program sees them.

bc's word jumps are therefore **`Ctrl-B` / `Ctrl-F`** — a pair nothing
intercepts — while still accepting both modifier forms where they arrive.

### 5. Every `Alt-x` is also `Esc` then `x`

Two ordinary keystrokes that nothing can swallow. This is the escape hatch
that makes bc usable on a terminal with no working Alt or function keys,
and it is why the Esc prefix exists. `Esc 1`…`Esc 0` reach F1–F10.

### 6. tcell's Suspend/Resume works — but Resume wipes the screen instantly

Verified on a real pty: suspend, run a command that prompts on `/dev/tty`,
resume. The prompt appears, typed input arrives, stderr is visible. The
handover is sound.

The trap is in the byte stream:

```
GOT[hunter2]\r\nthis went to stderr\r\n   ← the command's output
\x1b[?1049h                              ← Resume, immediately
```

`Resume` re-enters the alternate screen the moment the child exits, wiping
everything it printed. A prompt survives because the child *blocks* on it;
an error message lasts microseconds.

**Therefore: capture what a suspended child writes and show it again after
redrawing.** bc tees ssh's stderr — `io.MultiWriter(os.Stderr, &buf)` — so
warnings appear live *and* survive.

### 7. tcell clears inside the alternate buffer, so a shell's screen survives

Verified by capturing the byte stream. This is why Ctrl-O can toggle
between the panels and a live subshell without the shell's scrollback being
destroyed — and why bc must **not** issue its own clear on resume.

---

## git

### 8. `git log` exits non-zero in a repository with no commits

> fatal: your current branch 'main' does not have any commits yet

That is an **empty history, not a failure**. The message is translated, so
it cannot be matched on. The reliable question is whether `HEAD` exists:

```go
_, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD")
```

Ask it only on the failure path, so it costs nothing normally.

### 9. `%s` in a pretty-format is always one line

Verified with a multi-line commit message: `%s` gives the subject only.
That is what makes `--pretty=format:%H%x00%h%x00%an%x00%at%x00%s` safe to
parse with NUL between fields and newline between records. Subjects
containing pipes and tabs survive.

### 10. git resolves absolute pathspecs itself, symlinks included

`git -C /private/var/.../repo log -- /var/.../repo/b.txt` works, even
though `/var` is a symlink to `/private/var`. **Checked directly through a
symlinked repository** after a mutation test refused to fail.

Relative pathspecs are still used — it is what git's documentation uses, it
keeps the command repeatable by hand, and an outside path fails with a
sentence — but this fixes nothing, and claiming otherwise was wrong.

### 11. git's refusals are multi-line and the useful part is not first

```
error: Your local changes to the following files would be overwritten by checkout:
	a.txt
Please commit your changes or stash them before you switch branches.
Aborting
```

The first line alone is useless. Commands that can refuse — switch, commit —
keep the **whole** message; read-only ones keep one line.

Similarly `git commit` writes "nothing to commit" to **stdout**, not stderr,
and the missing-identity error names the two commands that fix it. Pass all
of it through; bc cannot write a better message than git already has.

### 12. `git status --porcelain` quotes awkward paths

Use `-z`. It gives NUL-terminated records with no escaping, so a filename
containing a quote or a space arrives intact. Renames are followed by an
extra path field, which must be skipped.

---

## ssh

### 13. Only the first connection can be interactive

bc dials once with `ControlMaster=yes -o ControlPersist=60`, which parks a
master process and leaves a socket. Every later command reuses it with
`ControlMaster=no` and `BatchMode=yes` — correct, because those must never
block on a prompt. The master connection is the only one that may ask for
a password, and the screen must be suspended for it.

### 14. `BatchMode yes` in `~/.ssh/config` makes password hosts unreachable

ssh will not prompt for anything, so the failure is `Permission denied`.
Worth mentioning in the docs; bc cannot work around it.

---

## Archive tools

### 15. `tar -tf` lists files, not directories

A `.zip` containing `a/b/c.txt` may never mention `a` or `a/b`. **Parents
must be inferred** or walking into an archive stops at the first level.

### 16. Most listings give no sizes

`unzip -Z1` and `tar -tf` print names only. Show `--`, not `0`: zero is a
measurement nobody took and is indistinguishable from an empty file.

Sizes *are* available from `zipinfo`'s columns — verified, including names
with spaces — but a future `unzip` changing that layout would make the
strict filter match nothing and the archive look **empty**, which is worse
than a missing number. It is offered as a commented opt-in instead.

### 17. `[ -L "$f" ] && printf ...` leaves a non-zero exit status

In a loop over a directory whose last entry is not a symlink, the `&&` form
exits 1. If that status is read as the listing's success, a whole directory
fails to list. Use `if ... then ... fi`. Confirmed on a real shell.

---

## Go and the standard library

### 18. `syscall.Stat_t.Nlink` is `uint16` on darwin, `uint64` on linux

Hence `nlink_unix.go` / `nlink_other.go` behind build tags. The conversion
differs, not the idea.

### 19. `filepath.Rel` fails across a symlink boundary

`/private/var/x` versus `/var/x` are the same place and `Rel` does not know
it. Resolve both with `EvalSymlinks` before comparing — but note
`EvalSymlinks` fails on paths that do not exist, so it cannot be the only
attempt.

### 20. A zero `os.FileMode` passed to `Create` makes an unreadable file

A backend that reports no permissions — an archive listing with no mode —
would otherwise produce a copy nobody can open. Fall back to `0644`. A copy
that succeeds and cannot be read is not a success.
