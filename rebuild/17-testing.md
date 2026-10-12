# 17 — Testing

830 test functions, ~20,200 lines — slightly more test than source. That
ratio is not an accident: almost every one of them exists because something
broke.

## How things are tested

### Draw into a simulation screen and read the cells back

```go
sim := tcell.NewSimulationScreen("UTF-8")
sim.Init()
sim.SetSize(80, 24)
a.Draw()
if !strings.Contains(screenText(sim), "History: zebra.txt") { ... }
```

A real cell grid with no terminal behind it. This is why every drawing
package can be tested in CI with no TTY.

### Run the real tool

`git init` and real commits. Real `tar`, `zip`, `gzip` archives. A real pty
for the suspend/resume probe. A canned response proves the plumbing matches
itself; the real tool proves the assumption about its output.

### Assert behaviour, never implementation

`TestCopyOutOfAZipDoesNotCrash` opens a real zip, copies a file out,
redraws throughout, and checks the bytes arrived. Not "was `transferFS`
called".

### Test names are sentences

A failing test should state what broke without anyone opening the file.

### Isolate the environment

```go
func isolateState(t *testing.T) {
    t.Setenv("XDG_STATE_HOME", t.TempDir())
    t.Setenv("HOME", t.TempDir())
}
```

Without this, tests read and write the real `~/.bcrc` and the real history
file. Both happened. One test's rebinding leaked into the next, and the
author's history filled with `sftp://user@testhost/path`.

### Mutation-check anything important

Break the code deliberately and confirm the test fails:

```
$ # with the clamp removed
--- FAIL: TestDrawingAProgressBarNeverPanics
panic: strings: negative Repeat count
```

This is how the progress-bar fix was confirmed, and how the shell-quoting
tests were shown to be real. **It is also how a false claim was caught**:
a mutation that should have failed did not, and the "bug" being fixed
turned out never to have existed. See
[16-terminal-and-tool-facts.md](16-terminal-and-tool-facts.md) item 10.

## Where a fake needs care

A test double that is too agreeable proves nothing:

- The fake ssh **exits 0 for anything it does not recognise**, so a test
  must assert the recorded command line, not just the canned response.
- `fakeRemoteFS.Stat` returned success for *every* path, so every copy
  destination looked like an existing file and every conflict dialog fired.
  It must report `os.ErrNotExist` for paths it does not know.
- `fakeRemoteFS` needed a mutex: file operations write to it from their own
  goroutine, and `-race` found it.
- `fakeRemoteFS.Rename` was a no-op, which made the symlink repoint — a
  create-then-rename — look broken when it was not.

## Waiting for background work

File operations run on a goroutine and report through events the test loop
does not run. Wait on **the effect**, not a fixed sleep:

```go
func settle(t *testing.T, a *App, done func() bool) {
    deadline := time.Now().Add(3 * time.Second)
    for time.Now().Before(deadline) {
        if done() { return }
        time.Sleep(5 * time.Millisecond)
    }
    t.Logf("gave up; status = %q", a.status)
}
```

And wait for **content**, not existence: a shell redirect creates the file
the moment the command starts, before the program has written anything.

## The gate scripts

`plan/gates/phase-NN.sh`, driven by `plan/run.sh`. Each selects behavioural
tests for its phase, checks its key source files, and requests a build and
full-suite run. The runner caches the build and full suite once per invocation
even though each phase asks for them. For example:

```bash
need_file   internal/git/git.go
need_grep   "func CurrentStatus" internal/git/git.go
need_tests  ./internal/git 'TestCurrentStatus|TestCommitChanges' 10
need_build
need_suite
finish
```

`run.sh` runs every gate **once per invocation** and caches the results —
running them twice doubles the time and gives a flake two chances to turn a
green phase red. It once printed "every phase is complete" directly above
"11 of 14", because the two passes disagreed.

`need_suite` keeps the output when it fails. A gate that fails without
saying why turns a flake into a mystery.

`plan/STATUS.md` is regenerated from the selected gates, so it cannot drift
from the most recent run.

### Gates earn their keep

They caught real regressions:
- a grep for `miniStatusName` broke when it became a method — the gate was
  doing its job
- two gates grepped `keys.go` for literals the keymap had replaced,
  correctly flagging that the wiring had moved
- phase 6's gate passed while the feature it covered was **never wired up**:
  it checked the machinery compiled, not that F5 reached it. A copy from a
  remote panel silently did nothing for several phases. *Assert the
  behaviour, not the existence of a function.*

## Flakes

`TestTarSend` in `internal/sshfs` flaked once in roughly thirty full runs
and has not reproduced. It is noted rather than hidden.
