# 18 — Build order

Sixteen phases. Each ends with something runnable and a gate that decides
it. The order matters: later phases depend on interfaces earlier ones
establish, and three of them exist only because the original build got the
order wrong and had to retrofit.

Write the gate script **before** the phase. It is the specification of
done, and writing it first stops "done" drifting.

---

## 1 — Panels

`cmd/bc`, `internal/ui`, `internal/vfs` (Entry, Local, sorting,
classification), `internal/panel`.

Two file lists, Tab between them, arrows/PgUp/PgDn/Home/End, Enter and Left
to walk the tree. Sized from the live terminal, redrawn on resize. Colours
and `ls -F` indicators. The footer. `Ctrl-T` marking.

**Gate:** panels draw at 80×24 and at 40×10; the cursor cannot leave the
list; each directory remembers its cursor; `..` is never marked.

## 2 — Dialogs and the command line

`internal/dialog`, `internal/shell` (Run, Buffer).

Message, confirm and input dialogs. `TextInput` with its editing keys. The
command line, running commands, the output log, `Alt-Enter`.

**Gate:** Esc cancels everywhere; the editing keys belong to the command
line only when it holds something.

## 3 — File operations

`internal/fileop`. Copy, move, delete, mkdir, with the progress dialog and
cancel.

**Gate:** a batch with one failure completes the rest; a cancelled copy
leaves no partial file; the progress bar never panics (table of absurd
inputs).

## 4 — The viewer

`internal/syntax`, `internal/viewer`. Text, raw and hex modes; search;
wrapping; line numbers.

**Gate:** a binary opens as hex; search finds text inside a binary; a file
over the size limit says so.

## 5 — The editor

`internal/editor`. Buffer, undo through one `replace` primitive, atomic
save, tabs preserved.

**Gate:** one undo reverses a multi-line change; a run of typing is one
undo step; an interrupted save leaves the original.

## 6 — The subshell

`shell.Session` on a pty, `Ctrl-O` toggling.

**Gate:** the shell survives being hidden and shown; the screen is not
cleared on either transition.

## 7 — The filesystem interface

Extract `vfs.FS` from what `Local` already does; add `MemFS` and
`vfs.Cached`. Make panel, viewer, editor and fileop go through it.

**Do this before anything remote.** Retrofitting an interface under four
subsystems is the single most expensive thing in this list.

**Gate:** the panel test suite passes against `MemFS` as well as `Local`.

## 8 — Remote panels

`internal/sshfs`. ControlMaster, three listing parsers, transfers, the
connect dialog, `Ctrl-R` reconnect.

**Gate:** all three parsers against captured real output; a directory with
a broken symlink still lists; the failure dialog carries ssh's words.

**Also: actually wire F5 to it.** Phase 6 of the original build gated on
the machinery compiling, and a copy from a remote panel silently did
nothing for several phases.

## 9 — History

`internal/history`, persisted under `$XDG_STATE_HOME`. A cursor on every
input. `Alt-P`/`Alt-N`, the history browser, the combo box.

**Gate:** tests set `XDG_STATE_HOME`; history survives a restart.

## 10 — Find

`internal/find`, the form, the streaming results screen, panelise.

**Gate:** results stream while searching; `Ctrl-C` keeps what was found.

## 11 — The keymap

`internal/keymap`, `~/.bcrc`, the F2 settings screen, the key tester.

Remove every hard-coded command key from the switch statements — a second
copy means a rebound key still does its old job.

**Gate:** rebinding takes effect in all four modes; reserved keys are
refused; the hint bar and F1 list are built from live bindings.

## 12 — Links and the shifted row

Rename separate from move; symbolic and hard links; `Shift-F4/5/6` folded
from F13–F24; `Alt` spellings listed first.

**Gate:** `Shift-F5` and a bare `F17` reach the same action; plain and
shifted keys are distinct.

## 13 — What the panel shows

`Entry.Target`, `Entry.Links`, `SizeUnknown`; the thirteen classes; the
footer showing link targets and counts.

**Gate:** no two classes share a colour; no class is the background colour;
the indicator survives truncation.

## 14 — Binary editing, selection, handlers

Byte mode in the editor; selection and the file clipboard;
`internal/handlers` and `internal/archivefs`; `Alt-W`; archives in panels.

**Gate:** all 256 byte values survive an open-and-save; paths never reach a
command as text; an archive is read-only.

## 15 — Settings and the Ctrl-X prefix

`[settings]` in `~/.bcrc`, **Save all settings**; `Ctrl-X` as a prefix;
`keymap.Reserved` enforced.

**Gate:** a setting survives a restart; binding Tab is refused and Tab
still switches panels.

## 16 — Git

`internal/git`; the history screen; diffs; commit; branches; `F3` on `..`.

**Gate:** a repository with no commits shows an empty history, not an
error; a refused switch shows git's full message; both panels reload after
a switch.

---

## What to do differently

Three things the original build paid for:

1. **Do the `vfs.FS` interface early (phase 7).** It was retrofitted, and
   every subsystem written before it had to be revisited.
2. **Gate on behaviour, not on symbols existing.** `need_grep "func CopyFS"`
   passes happily while nothing calls it.
3. **Write `isolateState` before the first test that touches `$HOME` or
   `$XDG_STATE_HOME`.** Both were polluted for real before anyone noticed.
