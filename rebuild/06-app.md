# 06 — internal/app

The application: one event loop, one screen, all the modes.

## Structure

`internal/app` is 17 files, one per feature area:

| File | Holds |
| --- | --- |
| `app.go` | the `App` struct, `New`, `Run`, `Draw`, status line |
| `keys.go` | **`HandleKey` — the dispatch order**, actions, prefixes |
| `ops.go` | copy, move, delete, mkdir, F1 help |
| `links.go` | rename, create link, edit link |
| `view.go` | F3 viewer |
| `edit.go` | F4 editor, binary confirmation, clipboard |
| `cmdline.go` | the command line |
| `shellview.go` | Ctrl-O subshell and the output log |
| `search.go` | quick search |
| `find.go` | the find dialog and results screen |
| `remote.go` | Ctrl-\ connect, reconnect |
| `settings.go` | the F2 menu and the shortcuts screen |
| `keytest.go` | the key tester |
| `handlers.go` | archives, open-with, the handler screen |
| `gitlog.go` | the git history screen |
| `gitwrite.go` | commit and branch switching |
| `dirsize.go` | Ctrl-L |

## App state

The screens are mutually exclusive pointers; non-nil means "in front":

```go
dialog   *dialog.Dialog    // modal box
form     *dialog.Form      // multi-field modal
viewer   *viewer.Viewer    // F3
editor   *editor.Editor    // F4
find     *findState        // search results
settings *settingsState    // F2 shortcuts
keytest  *keyTestState     // F2 key tester
gitlog   *gitLogState      // Alt-G history
archiveView *archiveView   // the viewer is showing an archive listing
```

Plus: `panels [2]*panel.Panel`, `active int`, `cmdline`, `keys *keymap.Map`,
`handlers *handlers.Set`, `histStore *history.Store`, `clip`, `shellVisible`,
`lineNumbers`, `escPending`, `ctrlXPending`, `status`, `statusIsError`,
`quit`.

### Seams for tests

Three function fields are replaced by tests so nothing touches the real
terminal or network:

```go
launchOp   func(context.Context, fileop.Operation, func(fileop.Progress)) fileop.Result
dialRemote func(ctx, user, host string, port int) (vfs.FS, string, error)
suspendRun func(a *App, dir, command string) error
shellSession func(*App, string) (string, error)
```

## The event loop

```go
for !a.quit {
    a.Draw()
    switch ev := a.screen.PollEvent().(type) {
    case *tcell.EventKey:    a.HandleKey(ev)
    case *tcell.EventResize: a.screen.Sync()
    case *opProgress:        a.dialog.SetProgress(ev.p)
    case *opFinished:        a.finish(ev.title, ev.r)
    }
}
```

Background work (file operations) runs on its own goroutine and reports
back by posting custom events with `screen.PostEvent`, so only the loop
goroutine ever draws.

## HandleKey — the dispatch order

**This order is the most order-dependent thing in the program.** Each step
is in its place for a reason that was found by a bug.

1. **`dialog`** — modal, takes everything.
2. **`form`** — modal in the same way.
3. **`keytest`** — before *everything*, including Esc. It must see each key
   exactly as it arrives, which is why leaving it takes Esc twice.
4. **`settings`** — before the Esc handling: a key capture in progress has
   to swallow every key, and Esc is how you abandon one. The screen has no
   function keys of its own, so no Esc prefix is lost.
5. **`gitlog`, but only when no viewer or editor is over it** — before the
   keymap, or a panel binding fires over the top and F4 opens the editor
   instead of widening the diff; before Esc, so Esc leaves. Skipped while a
   diff is open so `q` closes the diff, not the list behind it.
6. **`escPending`** — the key after a lone Esc.
7. **`ctrlXPending`** — the key after Ctrl-X. Before the keymap, so the
   second key is never mistaken for a binding of its own.
8. **Esc** starts the Esc prefix.
9. **Ctrl-X** starts the Ctrl-X prefix — panel mode only, and not during
   quick search.
10. **quick search**, while its bar is up.
11. **`typing := mode == Panel && cmdlineClaims(ev)`** — a plain character
    belongs to the command line once there is something on it.
12. **the keymap lookup** for the current mode, skipped when `typing`.
13. **editor / viewer / find** — their own structural keys.
14. **the command line**.
15. **`normalKey`** — panel navigation.

### Modes

```go
func (a *App) mode() keymap.Mode {
    switch {
    case a.editor != nil: return keymap.Editor
    case a.viewer != nil: return keymap.Viewer
    case a.find   != nil: return keymap.Find
    }
    return keymap.Panel
}
```

Screens with fixed keys (settings, key tester, git history) are **not**
modes — they are handled before the lookup instead.

### Prefixes

**Esc** — `Esc 1`…`Esc 9` are F1–F9, `Esc 0` is F10, `Esc x` is Alt-x.
This is how everything stays reachable on a terminal with no usable
function or Alt keys. `Esc Esc` cancels, and leaves the viewer or editor.

**Ctrl-X** — mc's prefix. `s` symbolic link, `l` hard link, `e` edit a
symlink. The table names *actions*, not keys, so `Ctrl-X s` keeps working
after create-link is rebound. The status line lists the choices while it
waits, because a key that eats the next one has to admit it.

### Why command keys are not in `normalKey`

`normalKey` holds only structural keys — Tab, arrows, Enter, Backspace.
Every command goes through the keymap. A second copy in the switch would
mean a rebound key still did its old job as well. A function key reaching
`normalKey` is unbound, and says so rather than going quiet.

## Drawing

`Draw()` picks one screen, then always draws the hint bar:

```
too small? → message and stop
editor  → editor.Draw,   bar from Editor bindings
viewer  → viewer.Draw,   bar from Viewer bindings
keytest → drawKeyTest,   ui.KeyTestKeys
gitlog  → drawGitLog,    ui.GitLogKeys
settings→ drawSettings,  ui.SettingsKeys
find    → drawFind,      bar from Find bindings
shell   → drawShellView
default → both panels (+ quick search bar)
```

The command line is drawn only in panel mode. The dialog or form, if any,
is drawn last, over the top.

## Status line

`setStatus(format, args...)` — transient, cleared on the next keystroke in
panel mode. `showError(title, err)` opens a modal instead, for anything
worth reading twice. `statusIsError` colours it.
