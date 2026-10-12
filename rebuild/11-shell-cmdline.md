# 11 — internal/shell, the command line, Ctrl-O, Ctrl-L

## The command line

A `dialog.TextInput` drawn under the panels with the current directory as a
prompt. Typing a printable character starts a command; Enter runs it.

### Who owns a key

```go
func (a *App) cmdlineOwns(ev *tcell.EventKey) bool {
    switch ev.Key() {
    case tcell.KeyRune:
        return ev.Modifiers()&tcell.ModAlt == 0
    case Backspace, Delete, Left, Right, Ctrl-A, Ctrl-E, Ctrl-K, Ctrl-W, Ctrl-B, Ctrl-F:
        return !a.cmdline.Empty()     // only once there is something to edit
    }
    return false
}
```

The editing keys belong to the command line **only when it holds
something**, so Left and Right navigate the panel the rest of the time.

### Why Backspace does not go up a directory

Deliberate. Backspace edits the command line when there is one, and going
up is Left's job. A stray Backspace should never move the panel.

### Plain characters versus bindings

```go
func (a *App) cmdlineClaims(ev *tcell.EventKey) bool
```

A plain character belongs to the command line **as soon as there is
something on it**, and the keymap gives way. This is what lets `Space`
preview the file under the cursor when nothing is typed and still be a
space in the middle of a command.

The rule is general, not a Space special case. Without it, binding anything
to a plain character would make that character useless for typing commands.

### Running

`Enter` with text runs it; `Enter` with an empty line enters the directory.
`cd` is intercepted and changes the panel (including `cd sftp://…`). Output
goes to the scrollback and the status line reports the exit code. `Ctrl-C`
stops a running command — by killing the **process group**
(`Setpgid` + `syscall.Kill(-pid)`), so a pipeline dies with it.

`Alt-Enter` runs on the real terminal instead: suspend the screen, run,
"Press Enter to return to bc". For editors and pagers that need the
terminal to themselves.

`Ctrl-Y` clears the line. `Alt-P`/`Alt-N` walk the command history;
`Alt-H` opens the history browser.

## The subshell (Ctrl-O)

A **persistent** shell on a pty, started on first use, kept alive for the
session. Ctrl-O shows it over the panels; Ctrl-O again hides it. The shell
is not killed and not restarted: your directory, your variables and your
half-typed command are all still there when you come back.

`creack/pty` for the pty, `golang.org/x/term` for raw mode.

**The screen is not cleared on either transition.** tcell's clear happens
inside the alternate buffer, so the shell's own screen survives underneath
— see [16-terminal-and-tool-facts.md](16-terminal-and-tool-facts.md) item 7.
Issuing a clear on resume destroys exactly what the feature is for.

## The output log (Alt-O)

Commands typed on bc's command line write to a scrollback buffer rather
than the subshell. Alt-O shows it, with arrows, PgUp/PgDn, Home/End. The
same screen state as the subshell view (`shellVisible`), which is why a
plain character there is typing rather than a binding: there is no file
under a visible cursor to act on.

## Directory sizes (Ctrl-L)

Runs `du -h -d 1` in the panel's directory and shows the output in a
scrollable dialog. A popup rather than a column, because computing it for
every row on every listing is slow and almost never wanted.

## internal/shell

| | |
| --- | --- |
| `Run(shell, dir, command) (output, error)` | capture output |
| `RunInteractive(shell, dir, command) error` | the real terminal |
| `Session` | the persistent pty subshell |
| `Buffer` | scrollback, capped |
| `Job` | a running command, cancellable |

`Job.Kill()` had a race: it could be called before `cmd.Start()` returned,
and then killed nothing. It records the request and applies it once the
process exists.

`shell.Default()` resolves which shell: `$SHELL` when set, then `/bin/bash`,
then `/bin/sh`, then the bare name `sh` for `exec` to find on `PATH`.
