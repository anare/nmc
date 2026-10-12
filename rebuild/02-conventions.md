# 02 — Conventions

These are not arbitrary. Following them is most of what makes the result
feel like one program rather than nineteen.

## Comments say why, never what

```go
// Backspace deliberately does nothing here. It edits the command line
// when there is one, and going up a directory is Left's job: a stray
// Backspace should never move the panel.
case tcell.KeyLeft:
```

The rule: if a comment could be deleted and the code would still be
understood, delete it. If a decision could be reversed by someone who did
not know what it cost, write down the cost.

Comments that earn their place:
- a non-obvious constraint (`%s is the subject line and git guarantees it
  has no newline, so the two separators cannot be confused with content`)
- a rejected alternative and why (`Shift-arrows would be the other way to
  do this, and are not used: a terminal has to define a sequence for every
  shifted key and many define none`)
- a trap (`Ctrl-I is Tab at the byte level, so offering them as separate
  bindings would be a lie`)

## Test names are sentences

```go
func TestTabCannotBeBoundAway(t *testing.T)
func TestASuccessfulConnectRepeatsWhatSSHMentioned(t *testing.T)
func TestTheDiffIsNarrowedToWhatWasAskedAbout(t *testing.T)
```

A failing test should state what broke without anyone opening the file.
`TestCopy2` does not.

Each test carries a comment saying what would go wrong if it failed —
usually the bug that caused it to be written:

```go
// TestTabCannotBeBoundAway is the regression for a hole found while adding
// the Ctrl-X prefix: the README said the structural keys were not
// rebindable, but nothing enforced it — they merely had no default.
```

## Tests assert behaviour, not implementation

Draw into a simulation screen and read the cells back. Run the real `git`,
the real `tar`, a real pty. A test that checks a function was called proves
nothing about what the user sees.

Table-driven where the cases are genuinely parallel; separate functions
where they are not.

## Error messages are sentences a person can act on

- `"F%d is not bound to anything (F2 to change that)"` — says what to do
- `"%s: copy it here first — archives open on the local filesystem"`
- `"nothing is selected — Alt-A starts a selection"`

Never `"error: invalid state"`. When an external tool fails, pass **its**
words through: git and ssh write better diagnostics than bc can, and they
name files and give commands.

## British spelling in prose

`colour`, `behaviour`, `normalise`, `recognise`. Code identifiers use the
Go-standard American where a Go API does (`Color` in tcell calls).

## Makefile

Self-documenting: `make help` greps `## ` comments out of the targets.

| Target | Does |
| --- | --- |
| `make build` | binary to `./bin/bc` |
| `make run` | build and run |
| `make test` | `go test ./...` |
| `make race` | with `-race` |
| `make fmt` | `gofmt -w` |
| `make vet` | `go vet ./...` |
| `make check` | fmt + vet + test — **run before every commit** |
| `make tidy` | `go mod tidy` |
| `make clean` | remove `./bin` |

## Saving files atomically

Every file bc writes — the editor's output, `~/.bcrc`, the history — is
written to a temporary file in the same directory and renamed over the
target. An interrupted save then leaves either the old file or the new one,
never half of either.

```go
tmp := fs.Join(fs.Dir(path), "."+name+".bc-tmp")
// write, close, check both errors
if err := fs.Rename(ctx, tmp, path); err != nil {
    _ = fs.Remove(ctx, tmp)
    return err
}
```
