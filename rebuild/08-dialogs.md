# 08 — internal/dialog

Modal boxes and the text input. No global state; the app owns one dialog
and one form pointer at a time.

## Dialog

One type with several modes, because they share a frame, focus ring and
button row:

| Constructor | For |
| --- | --- |
| `NewMessage(title, lines, isError)` | a notice with OK |
| `NewList(title, lines)` | a notice whose body **scrolls** |
| `NewConfirm(title, lines, buttons, onDone)` | a question; first button is the default |
| `NewInput(title, prompt, initial, onDone)` | one editable line |
| `NewComboInput(title, prompt, initial, cursor, onDone)` | one line with history drop-down |
| `NewPicker(title, items, onDone)` | a list to choose from, type-to-filter |
| `NewHistBrowser(...)` | `NewPicker` under its older name |
| `NewMenu(title, items, onDone)` | a picker **without** filtering |
| `NewProgress(title, onCancel)` | a progress bar |

```go
type Result struct {
    Button    int    // index into Buttons
    Text      string // the input's content
    Cancelled bool   // dismissed with Esc
}
func (r Result) OK() bool { return !r.Cancelled && r.Button == 0 }
```

`HandleKey` returns `(done bool, res Result)`. The app clears its pointer
**before** running `OnDone`, because the callback often opens another
dialog.

`NewMenu` exists separately from `NewPicker` because a four-entry menu does
not need searching, and a stray letter emptying the list would be baffling.

### Keys

Esc cancels. Enter accepts (from the input, or the focused button). Tab and
Shift-Tab move focus. **While the input has focus the arrows edit it**; they
only move focus when it does not. A list body scrolls with PgUp/PgDn.

### Layout

Width is `max(minInputWidth=44, longest line + padding)` capped at
`maxWidth=72`, centred. A body taller than the screen scrolls, and the
footer shows `1-16 of 23` — **whose width must be counted when sizing the
box**, or it truncates to `1-16 of 1~`.

## TextInput

One editable line, used by dialogs, forms and the command line.

| Key | Does |
| --- | --- |
| printable | insert |
| `Backspace` `Del` | delete |
| `Left` `Right` `Home` `End` | move |
| `Ctrl-A` `Ctrl-E` | start, end |
| `Ctrl-K` | kill to end |
| `Ctrl-W` | delete the previous word |
| `Ctrl-B` `Ctrl-F` | jump a word |
| `Ctrl-Left` `Ctrl-Right` `Alt-Left` `Alt-Right` | jump a word (where the terminal sends them) |
| `Alt-P` `Alt-N` | previous / next in history |

`WordJump(ev)` accepts **either** ModCtrl or ModAlt — see
[16-terminal-and-tool-facts.md](16-terminal-and-tool-facts.md) item 4. A
word is letters, digits and underscore; the same predicate drives
`Ctrl-W`, so deleting and jumping always agree.

**History is on Alt-P/Alt-N, not the arrows.** The arrows move focus
between fields, and a form where Up sometimes edits and sometimes navigates
is unpredictable.

The view scrolls horizontally to keep the cursor visible.

## Form

Several fields plus buttons, for the find dialog and the link dialogs.

```go
func TextField(label, initial string, hist *history.Cursor) Field
func CheckField(label string, on bool) Field
```

`FormResult` has `Values []string` and `Checks []bool`, one per field, with
`Value(i)` and `Checked(i)` bounds-checked.

**The focused field gets first refusal on every key**, so arrows fall
through to focus movement when the field does not want them. Space toggles
a checkbox but types into a text field. Tab always moves focus even when
the field has history.

## Combo

A text input with a history drop-down — used for the Ctrl-\ connect dialog.
Type to filter, Down to open and walk, Enter to accept a row, Esc to close
the drop-down and then to cancel, Ctrl-D to forget an entry.

**`NewCombo` must give its input the history cursor** (`input.WithHistory(c)`)
or Alt-P/N do nothing inside it. That was a real bug.

At most 8 rows (`comboMaxRows`).

## Progress

```
╔════════════ Copy ════════════╗
║ sub/notes.txt                ║
║ ███████████░░░░░░░░░░░   45% ║
║ 1.2M of 2.7M  3.4M/s  0:01   ║
║ file 2 of 7                  ║
╚══════════════════════════════╝
```

Esc calls `OnCancel` and **keeps the dialog open** — the operation
acknowledges the cancel by finishing, and the dialog goes then.

**The bar clamps what it is given.** A percentage is computed elsewhere and
a bad one must not kill the program from inside a draw:

```go
filled := min(max(barW*pct/100, 0), barW)
```

Without that clamp, `strings.Repeat` panicked with a negative count while
copying out of a zip — *after* the copy had succeeded. See
[09-fileops.md](09-fileops.md).

## Wrap

`Wrap(text, width) []string` — word wrapping for error bodies, so a long
message from git or ssh fits the box.
