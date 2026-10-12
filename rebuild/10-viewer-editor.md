# 10 — internal/viewer, internal/editor, internal/clipboard

## The viewer (F3)

Takes over the screen. Opens through `vfs.FS`, so it works on remote panels
and inside archives.

```go
func Open(ctx, fs vfs.FS, path string) (*Viewer, error)
func FromBytes(name string, data []byte) *Viewer
func FromBytesAs(name, lexerName string, data []byte) *Viewer
```

`FromBytes` is for text bc produced itself — an archive's table of
contents, a git diff. `FromBytesAs` keeps the name **shown** apart from the
name used to **pick a lexer**: chroma chooses by extension, and a diff
titled `9f04b87 add the zebra.diff` would be inventing a file that does not
exist.

### Modes

| Mode | Shows |
| --- | --- |
| text | syntax-highlighted, wrapped |
| raw | the same text, no colours |
| hex | offset, 16 bytes, ASCII gutter |

A binary opens as hex automatically — text is not what it is.

### Keys

`F2`/`w` wrap, `F4`/`h` hex, `F8`/`r` raw, `F5` goto, `F7`/`/` search,
`n`/`N` next and previous hit, `g`/`G` top and end, `Alt-N` line numbers,
`F3`/`F10`/`q` quit.

Search is case-insensitive. **In hex mode it searches the bytes**, so text
inside a binary can still be found.

Three limits, all different and all deliberate:

| Constant | Where | Value |
| --- | --- | --- |
| `viewer.SizeLimit` | how much of a file is read | 32 MiB |
| `syntax.HighlightLimit` | how much is coloured | 2 MiB |
| `editor.SizeLimit` | the most the editor will open | 16 MiB |
| `editor.HighlightLimit` | how much the editor colours | 128 KiB |

A truncated file says so in the status. Past the highlight limit the text
still appears, uncoloured — the editor's is much lower because it
re-highlights on every change.

### F8 on an archive listing

When the viewer is showing an archive's contents, F8 means *the archive
file itself*, as a hex dump, and F8 again returns. "Raw" on a listing bc
generated would be the same text without colours, which is no use; what
someone pressing F8 on an archive wants is the file. The state is held in
`App.archiveView` and cleared when the viewer closes, so F8 in the next
viewer is the ordinary toggle again.

## The editor (F4)

```go
func Open(ctx, fs, path) (*Editor, error)        // refuses binaries with ErrBinary
func OpenBinary(ctx, fs, path) (*Editor, error)  // byte mode
```

Lines are `[][]rune`. Tabs are **kept as tabs** in the file — makefiles need
them — and expanded only for display, at `TabWidth = 8`.

### Keys

`F2`/`Ctrl-S` save, `F7` search, `F5` goto, `F8` delete the line,
`Ctrl-Z` undo, `Ctrl-Y` redo, `Alt-N` line numbers, `F10`/`F3` quit,
`Ctrl-B`/`Ctrl-F` word jumps, Tab inserts a real tab.

Quitting with unsaved changes asks first.

### Undo

Every change goes through one primitive:

```go
func (e *Editor) replace(start, count int, with ...[]rune)
```

which records what it replaced. Expressing inserts, deletes, splits and
joins as one operation keeps undo honest — there is only one thing to get
right. A run of typing on one line **coalesces** into a single undo step so
undo does not crawl back one character at a time; a block delete or paste
is always its own step.

### Saving

Atomic: write beside the file, rename over it. The same path works for
local and remote because both go through `fs.Create` then `fs.Rename`.

### Byte mode

`F4` on a binary asks first, and says what changes rather than "are you
sure": **one byte per character, nothing converted, and saving it unchanged
writes exactly the same file.**

This matters because the text path cannot promise that. Text mode folds
`\r\n` to `\n` and `[]rune(line)` turns every invalid UTF-8 byte into
U+FFFD, which writes back as three bytes of replacement character — right
for text, destruction for anything else.

In byte mode `setBytes` splits only on `\n` and stores each byte as a rune;
`Bytes()` reverses it exactly. A rune above 0xFF has no single-byte
spelling, so typing one is refused rather than written as something else.

There is a test that writes **all 256 byte values** plus CRLF pairs and
invalid sequences, opens, saves without typing, and compares. And a
companion asserting the *text* path would have mangled the same input —
which is the reason byte mode exists.

### Selection

```
Alt-A  start selecting      Alt-C  copy
Alt-S  end selecting        Alt-X  cut
                            Alt-V  paste
```

Between the two marks the region follows the cursor, so a selection is
visible as it is made. **Not shift-arrows** — a terminal has to define a
sequence for every shifted key and many define none, which is exactly the
problem explicit marks avoid.

Marks are clamped on read, because an edit can shorten the line one points
into. `DeleteSelection` and `InsertBytes` are each one undo step however
many lines they cover. Pasting into a byte-mode file stays bytes; pasting
text folds CRLF, because the document uses `\n` throughout.

The selection is drawn by **reading each cell back and setting it again**
with a different style, rather than drawing the text twice — the layout,
tab expansion and syntax colouring have already happened, and working out
where a rune landed a second time is how two copies drift apart.

`Palette.Selection` is the text style reversed: legible against whatever
background the syntax theme chose, without inventing a colour that might
clash with it.

## internal/clipboard

A copied selection, held in **a file** under the system temp directory.

```go
func New() *Clipboard
func (c *Clipboard) Copy(data []byte) (path string, err error)
func (c *Clipboard) Paste() ([]byte, error)
func (c *Clipboard) Path() string
func (c *Clipboard) Describe() string   // "$TMPDIR/bc-clip-123"
func (c *Clipboard) Close() error
```

A file rather than a variable, because the point of a clipboard is to be
reachable from outside the program that filled it: the path is shown when
something is copied, so it can be fed to a shell command, read by another
tool, or pasted into a second bc. A terminal program cannot reach the
desktop clipboard without help the terminal may not offer.

Each copy replaces the last, so a long session does not leave a trail. An
empty clipboard pastes nothing **and no error** — pasting before copying is
a normal thing to do by accident. So does a file something else has
removed; it lives in a temp directory and that is allowed.

Byte-transparent: all 256 values survive.

## internal/syntax

A thin wrapper over chroma.

```go
func Highlight(filename, text, styleName string) (lines []Line, language string, pal Palette, ok bool)
func PaletteFor(styleName string) Palette
```

`ok=false` when no lexer matched or the text is too big, and the caller
falls back to plain text. A `Line` is a slice of `Span{Text, Style}`.

`Palette` carries `Text`, `Background` and `Selection`. Selection is part of
the palette rather than the app theme because it has to sit against
whatever background the syntax style chose.

Diffs highlight if the name ends `.diff` — which is why `FromBytesAs`
exists.
