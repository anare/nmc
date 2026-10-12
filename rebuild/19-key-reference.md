# 19 — Key reference

Every default binding. All of them are changeable in `F2 > Shortcuts`
except the reserved ones at the end.

## Panel

| Key | Action | Name in `~/.bcrc` |
| --- | --- | --- |
| `F1` | this key list | `help` |
| `F2` | settings and shortcuts | `menu` |
| `F3` | view the file (or git history on `..`) | `view` |
| `F4` | edit the file (or the symlink) | `edit` |
| `F5` | copy | `copy` |
| `F6` | move | `move` |
| `Alt-R` `Shift-F6` | rename where it is | `rename` |
| `Alt-L` `Shift-F5` | create a link, symbolic or hard | `create-link` |
| `Alt-E` `Shift-F4` | change where a symlink points | `edit-link` |
| `F7` | create a directory | `mkdir` |
| `F8` | delete | `delete` |
| `F10` `Ctrl-Q` | quit | `quit` |
| `Ctrl-T` | mark the entry, step down | `mark` |
| `Ctrl-R` | re-read the directory | `refresh` |
| `Ctrl-U` | swap the panels | `swap-panels` |
| `Ctrl-S` `Alt-S` | quick search in the panel | `quick-search` |
| `Ctrl-L` | directory sizes | `dir-sizes` |
| `Ctrl-O` | show the shell | `open-shell` |
| `Alt-O` | show the output log | `output-log` |
| `Ctrl-\` | connect to a host | `connect` |
| `Alt-F` | find files and text | `find-files` |
| `Alt-W` `Space` | open with another program | `open-with` |
| `Alt-I` | show this directory in the other panel | `sync-other-panel` |
| `Alt-G` | git history of the entry | `git-history` |
| `Alt-H` | history browser | `history-browser` |
| `Alt-P` | previous in history | `history-prev` |
| `Alt-N` | next in history | `history-next` |
| `Alt-.` | show or hide dotfiles | `toggle-hidden` |
| `Alt-T` | cycle the sort field | `sort-order` |
| `Ctrl-Y` | clear the command line | `clear-line` |
| `Ctrl-C` | stop the running command | `stop-command` |
| `Alt-Enter` | run on the real terminal | `run-on-terminal` |

`Space` reaches `open-with` **only while the command line is empty**; once
anything is typed it is a space again.

## Viewer

| Key | Action | Name |
| --- | --- | --- |
| `F3` `F10` `q` | back to the panels | `viewer-quit` |
| `F2` `w` | wrap long lines | `viewer-wrap` |
| `F4` `h` | hex dump | `viewer-hex` |
| `F8` `r` | raw, no colours (or the archive file) | `viewer-raw` |
| `F5` | go to a line | `viewer-goto` |
| `F7` `/` | search | `viewer-search` |
| `n` `Ctrl-S` | next hit | `viewer-search-next` |
| `N` | previous hit | `viewer-search-prev` |
| `Alt-N` | line numbers | `viewer-line-numbers` |
| `g` | to the top | `viewer-top` |
| `G` | to the end | `viewer-bottom` |

## Editor

| Key | Action | Name |
| --- | --- | --- |
| `F2` `Ctrl-S` | save | `editor-save` |
| `F10` `F3` `Ctrl-Q` | quit | `editor-quit` |
| `F7` | search | `editor-search` |
| `F5` | go to a line | `editor-goto` |
| `F8` | delete the line | `editor-delete-line` |
| `Ctrl-Z` | undo | `editor-undo` |
| `Ctrl-Y` | redo | `editor-redo` |
| `Alt-N` | line numbers | `editor-line-numbers` |
| `Alt-A` | start selecting | `editor-select-start` |
| `Alt-S` | end selecting | `editor-select-end` |
| `Alt-C` | copy the selection | `editor-copy` |
| `Alt-X` | cut the selection | `editor-cut` |
| `Alt-V` | paste | `editor-paste` |

## Find results

| Key | Action | Name |
| --- | --- | --- |
| `F3` | open the hit | `find-open` |
| `F5` | put hits in the panel | `find-panelise` |
| `F7` | search again | `find-again` |
| `Ctrl-C` | stop searching | `find-stop` |
| `F10` `q` | back to the panels | `find-quit` |

## Fixed keys

These screens have keys of their own, handled before the keymap:

**Git history** — `Enter`/`F3` diff, `F4`/`a` whole commit, `F5`/`c`
commit, `F6`/`b` branch, `q`/`Esc`/`F10` back.

**Shortcuts** — `Enter` bind, `Del` unbind, `Ctrl-R`/`r` one default, `d`
all defaults, `F2`/`Esc`/`q` save and leave.

**Key tester** — every key is recorded; `Esc Esc` leaves.

## Prefixes

| | |
| --- | --- |
| `Esc 1`…`Esc 9` | F1–F9 |
| `Esc 0` | F10 |
| `Esc x` | Alt-x, for any letter |
| `Esc Esc` | cancel; leave the viewer or editor |
| `Ctrl-X s` | symbolic link |
| `Ctrl-X l` | hard link |
| `Ctrl-X e` | edit a symlink |

## Reserved — cannot be rebound

`Tab`, `Backtab`, bare `Enter`, `Esc`, `Backspace`, the unmodified arrows
and `PgUp`/`PgDn`/`Home`/`End`, and `Ctrl-X`. `F1` always opens the help for
the screen you are on.

Modified versions are **not** reserved: `Alt-Enter` and `Ctrl-Left` are
ordinary bindings.

`Ctrl-I` and `Ctrl-H` cannot be bound at all — a terminal sends them as the
same bytes as `Tab` and `Backspace`.
