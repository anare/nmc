# 07 — internal/keymap

Key bindings as data, so they can be changed from F2 and saved in
`~/.bcrc`.

## Binding

```go
type Binding struct {
    Key  tcell.Key
    Rune rune           // only meaningful when Key is tcell.KeyRune
    Mod  tcell.ModMask
}
```

### FromEvent — the normalisation

**This function is the hardest thing in the package and the most likely to
be reimplemented wrongly.** tcell describes one key press in three
overlapping ways, and comparing whole events never matches.

A real Ctrl-O arrives as `Key=KeyCtrlO` **and** `Mod=ModCtrl`, and
`ev.Rune()` helpfully reports `'o'`. A binding built from the name
`"Ctrl-O"` has only the key. Compare them raw and no Ctrl binding ever
fires.

```go
func FromEvent(ev *tcell.EventKey) Binding {
    b := Binding{Key: ev.Key(), Mod: ev.Modifiers()}

    if isCtrlKey(b.Key) {
        b.Mod &^= tcell.ModCtrl      // the key already says Ctrl
    }
    b.Key, b.Mod = foldShiftedFKey(b.Key, b.Mod)

    if b.Key != tcell.KeyRune {
        return b                      // Rune is meaningless here; stays zero
    }
    b.Rune = ev.Rune()
    b.Mod &^= tcell.ModShift          // Shift is baked into which rune arrived

    if b.Mod != 0 {
        b.Rune = foldRune(b.Rune)     // Alt-F and Alt-f are one key…
    }
    return b                          // …but n and N are two
}
```

Each line is a bug that happened:
- not dropping `ModCtrl` → no Ctrl binding ever matches
- not zeroing `Rune` → `F5` and some rune collide
- not dropping `ModShift` on runes → one key press is two bindings
- folding case *unconditionally* → the viewer's `n` and `N` collide

`foldShiftedFKey` turns F13–F24 into Shift+F1–F12 — see
[16-terminal-and-tool-facts.md](16-terminal-and-tool-facts.md).

### Spelling

`String()` and `Parse()` are inverses: `F5`, `Shift-F6`, `Ctrl-O`, `Alt-f`,
`Alt-Enter`, `Ctrl-\`. Parse is forgiving (`C-o`, `M-f`, any case) because
the file is edited by hand.

**Ctrl is looked up as a whole name first**, so `Ctrl-B` finds the control
character rather than being read as Ctrl plus the letter B.

**`Space` and `Comma` are spelled by name.** A space is invisible in a
config file and a comma is the separator between several keys for one
action, so neither can be written as itself.

**`Ctrl-I` and `Ctrl-H` are not offered.** A terminal sends them as the
same bytes as Tab and Backspace, so a separate binding would be a lie.

## Actions and modes

`Mode` is `panel`, `viewer`, `editor`, `find`. An `Action` is a string like
`"copy"`. `Defaults` is one table of `{Action, Mode, Describe, Defaults}` —
the single source of truth for the settings screen, the help, the hint bar
and the saved file. See [19-key-reference.md](19-key-reference.md).

Two invariant tests: every default is described and bound, and no two
actions share a key within one mode.

## Reserved keys

```go
func Reserved(b Binding) (why string, yes bool)
```

Tab, Backtab, bare Enter, Esc, Backspace, the unmodified arrows and
navigation keys, and Ctrl-X. These are how you move around and get out of
things — including out of the settings screen.

**`Rebind` refuses them**, which covers every path at once: the settings
screen names the key and the reason, and `Load` drops just that key from a
hand-edited line and reports it, keeping the rest.

This was a real hole. The README claimed these were not rebindable; nothing
enforced it, they merely had no default. Binding Tab to delete really did
stop Tab switching panels — three keystrokes to an unrecoverable state.

**Only the plain key is reserved.** `Alt-Enter` and `Ctrl-Left` are
ordinary bindings; it is unmodified movement that is structural.

## The file

`~/.bcrc`. INI-ish, sections by mode plus one for settings:

```ini
# bc settings and key bindings. Written by the F2 menu;
# safe to edit by hand. Delete a line to get the default back.

[settings]
hidden-files           = on
line-numbers           = off
sort-order             = Extension

[panel]
copy                   = Ctrl-G

[viewer]
viewer-hex             = F4, h
```

**Only what differs from the defaults is written.** A file pinning all
fifty bindings freezes today's defaults forever; a user who never touched
F5 should get whatever F5 becomes later. When nothing differs the file is
removed rather than left as comments.

`Load` never fails. It returns `(*Map, []string)` — the map it could build
and a list of problems. A bad line is skipped and reported; an unknown
`[section]` skips **its own lines** until the next valid section rather than
leaking them into the previous mode.

`[settings]` holds application preferences the keymap stores without
interpreting. One file, one writer, so neither half can overwrite the
other; an unrecognised setting is kept rather than dropped, so a file
written by a newer bc survives an older one.

Saving is atomic (temp file + rename).

## The F2 menu

```
Settings
  Shortcuts...
  Key tester...
  File handlers...
  Show hidden files: off
  Sort order: Name
  Line numbers when viewing and editing: off
  Save all settings to ~/.bcrc
```

Toggles **carry their current value in the label** — a settings list that
does not say what a setting *is* makes you open it to find out — and
choosing one applies it and reopens the menu.

Saving is deliberate, not automatic: toggling the sort to look at something
once should not change how bc starts tomorrow. Restored settings go to
**both** panels, since a preference saved from whichever panel was active
would otherwise leave the two different for no visible reason.

## The shortcuts screen

Every action grouped by mode, with its keys. `Enter` captures the next key
press and binds it; `Del` unbinds; `Ctrl-R` restores one default; `d`
restores all after a confirmation. Changes are written on the way out.

**The capture swallows every key**, including Esc (which abandons it) and
F2 — otherwise F2 would be the one key nobody could rebind.

Taking a key from another action is allowed, because refusing would force
the user to unbind first — but the other action is left unbound and **the
screen says which one lost it**. Nothing is silently shadowed.

Headings are skipped by the cursor; `*` marks a changed binding.

## The key tester

`F2 > Key tester` exists because "that shortcut does nothing" is otherwise
undiagnosable. Press a key and it shows how bc would spell it, what tcell
reported, and what the key currently does:

```
 Key tester   press any key to see what bc receives   Esc Esc to leave

  Key                   What tcell reports                Currently does
  Shift-F6              Key=296 Mod=0 Rune='\x00'         rename
  Alt-l                 Key=256 Mod=4 Rune='l'            create-link
```

It swallows every key — pressing F10 to see what it is must not quit — so
`Esc Esc` leaves. If nothing appears, the terminal is not sending it and no
setting in bc can help.
