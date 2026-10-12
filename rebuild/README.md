# Rebuilding bc — specification

bc ("Blue Commander") is a Midnight Commander clone for the terminal,
written in Go. This directory is a complete specification: enough to build
it again from an empty directory, without the original source.

It was written from the finished program, not from memory. Numbers,
formats and command lines in it were checked against the code and, where
they describe something outside the program — a terminal, git, ssh — against
the real thing.

## What exists today

| | |
| --- | --- |
| Source | ~18,100 lines of Go across 19 packages |
| Tests | ~20,200 lines, 830 test functions |
| Dependencies | 5 direct (see [01-overview.md](01-overview.md)) |

## How to use this

Read [01-overview.md](01-overview.md) first — it explains the shape of the
program and the one architectural rule that everything else depends on.
Then work through [18-build-order.md](18-build-order.md), which puts the
subsystems in an order where each one ends with something runnable.

**Read [16-terminal-and-tool-facts.md](16-terminal-and-tool-facts.md)
before writing any key handling.** It is a list of things that are not
true-by-reasoning about terminals, tcell, git and ssh, each one discovered
by a bug. Every item in it cost an hour or more to find. Several are
actively counter-intuitive, and a reimplementation that reasons from first
principles will get them wrong in the same way.

## The documents

| File | Covers |
| --- | --- |
| [01-overview.md](01-overview.md) | What bc is, package map, the dependency rule |
| [02-conventions.md](02-conventions.md) | Code and comment style, Makefile |
| [03-vfs.md](03-vfs.md) | The filesystem interface and its implementations |
| [04-ui.md](04-ui.md) | Drawing primitives, theme, colours |
| [05-panel.md](05-panel.md) | The file list: model and rendering |
| [06-app.md](06-app.md) | The application, event loop, key dispatch order |
| [07-keymap.md](07-keymap.md) | Bindings, `~/.bcrc`, the F2 settings menu |
| [08-dialogs.md](08-dialogs.md) | Dialog, Form, TextInput, Combo, Progress |
| [09-fileops.md](09-fileops.md) | Copy, move, delete, links, progress |
| [10-viewer-editor.md](10-viewer-editor.md) | F3 viewer, F4 editor, selection, clipboard |
| [11-shell-cmdline.md](11-shell-cmdline.md) | The subshell, command line, output log |
| [12-find-history.md](12-find-history.md) | Find files/text, persistent history |
| [13-sshfs.md](13-sshfs.md) | Remote panels over ssh |
| [14-handlers-archives.md](14-handlers-archives.md) | `~/.bchandlers`, archives, open-with |
| [15-git.md](15-git.md) | History, diffs, commit, branches |
| [16-terminal-and-tool-facts.md](16-terminal-and-tool-facts.md) | **Empirical findings — read first** |
| [17-testing.md](17-testing.md) | How this is tested, and the gate scripts |
| [18-build-order.md](18-build-order.md) | Phased plan |
| [19-key-reference.md](19-key-reference.md) | Every default binding |

## What "done" looks like

Each phase in the build order has a gate script that decides it. A phase is
finished when its gate passes and `make check` is clean. The gates are not
decoration: three of them caught regressions in the original build that the
author had not noticed.
