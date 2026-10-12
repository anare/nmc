package app

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/anare/nmc/internal/editor"
	"github.com/anare/nmc/internal/vfs"
	"github.com/anare/nmc/internal/viewer"
	"github.com/gdamore/tcell/v2"
)

// TestDrawSplitsTheTerminalBetweenTwoPanels catches layout regressions that
// can hide one panel or consume the rows reserved for status and hints.
func TestDrawSplitsTheTerminalBetweenTwoPanels(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "alpha", IsDir: true}, {Name: "file.txt"}})
	fs.Put("/alpha", []vfs.Entry{{Name: "inside.txt"}})
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.Draw()

	leftX, leftY, leftW, leftH := a.panels[0].Rect()
	rightX, rightY, rightW, rightH := a.panels[1].Rect()
	if leftX != 0 || rightX != 40 || leftY != 0 || rightY != 0 || leftW != 40 || rightW != 40 || leftH != 21 || rightH != 21 {
		t.Fatalf("panel rectangles = (%d,%d %dx%d), (%d,%d %dx%d)", leftX, leftY, leftW, leftH, rightX, rightY, rightW, rightH)
	}
	if got := screenText(screen); !strings.Contains(got, "alpha/") || !strings.Contains(got, "file.txt") || !strings.Contains(got, "Quit") {
		t.Fatalf("screen is missing panel entries or hints: %q", got)
	}
}

// TestHandleKeySwitchesPanelsAndNavigatesDirectories protects independent
// panel navigation and keeps Backspace from unexpectedly leaving a directory.
func TestHandleKeySwitchesPanelsAndNavigatesDirectories(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "alpha", IsDir: true}, {Name: "beta"}})
	fs.Put("/alpha", []vfs.Entry{{Name: "inside.txt"}})
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if a.active != 1 {
		t.Fatalf("active panel = %d, want 1", a.active)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if a.panels[1].Cursor != 1 {
		t.Fatalf("right panel cursor = %d, want 1", a.panels[1].Cursor)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.panels[1].Cwd != "/alpha" {
		t.Fatalf("right panel directory = %q, want /alpha", a.panels[1].Cwd)
	}
	if a.panels[0].Cwd != "/" {
		t.Fatalf("left panel directory changed to %q", a.panels[0].Cwd)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone))
	if a.panels[1].Cwd != "/alpha" {
		t.Fatalf("Backspace changed directory to %q; only Left should go up", a.panels[1].Cwd)
	}
}

// TestHandleKeyMarkRefreshAndQuit covers the structural commands required to
// use the first runnable panel build without a command-line subsystem.
func TestHandleKeyMarkRefreshAndQuit(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "alpha"}, {Name: "beta"}})
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlT, 0, tcell.ModCtrl))
	if len(a.panels[0].Marked) != 1 || a.panels[0].Cursor != 1 {
		t.Fatalf("Ctrl-T did not mark and advance: marks=%v cursor=%d", a.panels[0].Marked, a.panels[0].Cursor)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlR, 0, tcell.ModCtrl))
	if a.status != "Directory refreshed" {
		t.Fatalf("refresh status = %q", a.status)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF10, 0, tcell.ModNone))
	if !a.quit {
		t.Fatal("F10 did not request quit")
	}
}

// TestDrawReportsWhenTerminalIsTooSmall prevents unusable frames at sizes
// below the minimum required by the panel layout.
func TestDrawReportsWhenTerminalIsTooSmall(t *testing.T) {
	screen := newTestScreen(t, 39, 9)
	fs := vfs.NewMemFS("test")
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.Draw()
	if got := screenText(screen); !strings.Contains(got, "Terminal too small") {
		t.Fatalf("small-terminal screen = %q", got)
	}
}

func TestCommandLineOwnsEditingKeysOnlyWhenItHasText(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "alpha", IsDir: true}})
	fs.Put("/alpha", []vfs.Entry{{Name: "inside.txt"}})
	a, err := New(screen, fs, "/alpha")
	if err != nil {
		t.Fatal(err)
	}
	a.cmd.SetText("command")
	a.HandleKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if a.panels[0].Cwd != "/alpha" || a.cmd.Cursor() != len("command")-1 {
		t.Fatalf("left key moved panel or failed to edit command: cwd=%q cursor=%d", a.panels[0].Cwd, a.cmd.Cursor())
	}
	a.cmd.SetText("")
	a.HandleKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if a.panels[0].Cwd != "/" {
		t.Fatalf("Left did not navigate when command line empty: %q", a.panels[0].Cwd)
	}
}

func TestF7InputDialogCreatesAndSelectsDirectory(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "existing"}})
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF7, 0, tcell.ModNone))
	if a.dialog == nil {
		t.Fatal("F7 did not open the directory input dialog")
	}
	a.dialog.Input.SetText("created")
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	selected, ok := a.panels[0].Selected()
	if a.dialog != nil || !ok || selected.Name != "created" {
		t.Fatalf("mkdir result: dialog=%v selected=%#v", a.dialog != nil, a.panels[0].Entries[a.panels[0].Cursor])
	}
}

func TestF5CopiesAndF8DeletesThroughFileOperationLayer(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.PutFile("/source.txt", []byte("copy me"), 0o644)
	if err := fs.Mkdir(context.Background(), "/destination"); err != nil {
		t.Fatal(err)
	}
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.panels[1].SetDir(context.Background(), "/destination", ""); err != nil {
		t.Fatal(err)
	}
	a.panels[0].MoveTo(1)
	a.HandleKey(tcell.NewEventKey(tcell.KeyF5, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	waitForOperation(t, a, screen)
	reader, err := fs.Open(context.Background(), "/destination/source.txt")
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 7)
	_, err = reader.Read(data)
	_ = reader.Close()
	if err != nil || string(data) != "copy me" {
		t.Fatalf("copied file contents = %q, err=%v", data, err)
	}
	if _, err := fs.Stat(context.Background(), "/source.txt"); err != nil {
		t.Fatalf("F5 copy unexpectedly removed source: %v", err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF6, 0, tcell.ModNone))
	if a.dialog == nil {
		t.Fatal("F6 did not open a destination prompt")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlK, 0, tcell.ModCtrl))
	for _, r := range "/moved.txt" {
		a.HandleKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.dialog == nil || a.dialog.Title != "Move" {
		t.Fatalf("F6 did not start a move operation: dialog=%#v status=%q", a.dialog, a.status)
	}
	waitForOperation(t, a, screen)
	if _, err := fs.Stat(context.Background(), "/source.txt"); err == nil {
		t.Fatal("F6 did not remove the moved source")
	}
	if _, err := fs.Stat(context.Background(), "/moved.txt"); err != nil {
		t.Fatalf("F6 did not create the destination: %v", err)
	}

	for index, entry := range a.panels[0].Entries {
		if entry.Name == "moved.txt" {
			a.panels[0].MoveTo(index)
			break
		}
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF8, 0, tcell.ModNone))
	if a.dialog == nil {
		t.Fatal("F8 did not ask for delete confirmation")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	waitForOperation(t, a, screen)
	if _, err := fs.Stat(context.Background(), "/moved.txt"); err == nil {
		t.Fatal("F8 did not delete the selected item")
	}
}

func TestF3OpensViewerAndViewerSearchesThenCloses(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.PutFile("/readme.txt", []byte("alpha beta\nbeta"), 0o644)
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF3, 0, tcell.ModNone))
	if a.viewer == nil {
		t.Fatalf("F3 failed to open viewer: status=%q dialog=%v", a.status, a.dialog)
	}
	a.viewer.Search("beta")
	if len(a.viewer.Hits) != 2 {
		t.Fatalf("viewer hits = %v, want two", a.viewer.Hits)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone))
	if a.viewer != nil {
		t.Fatal("q did not close viewer")
	}
}

func TestCommandRunsInBackgroundAndOutputCanBeViewed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.runCommand("printf hello", false)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(a.output.String(), "hello") {
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(a.output.String(), "hello") {
		t.Fatalf("command output = %q", a.output.String())
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'o', tcell.ModAlt))
	if a.dialog == nil || !strings.Contains(strings.Join(a.dialog.Lines, "\n"), "hello") {
		t.Fatal("Alt-O did not show command output")
	}
}

func TestViewerAltNOnlyTogglesLineNumbers(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	a, err := New(screen, vfs.NewMemFS("test"), "/")
	if err != nil {
		t.Fatal(err)
	}
	a.viewer = viewer.FromBytes("notes.txt", []byte("first\nsecond"))
	a.viewer.Search("second")
	a.viewer.Hit = 0
	a.viewer.Top = 0
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModAlt))
	if !a.viewer.LineNumber || a.viewer.Top != 0 {
		t.Fatalf("Alt-N changed line-number=%v top=%d", a.viewer.LineNumber, a.viewer.Top)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone))
	if a.viewer.Top != 1 {
		t.Fatalf("plain n top = %d, want next search hit on line 2", a.viewer.Top)
	}
}

func TestF4EditsAndPromptsBeforeDiscardingChanges(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.PutFile("/notes.txt", []byte("hello"), 0o644)
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF4, 0, tcell.ModNone))
	if a.editor == nil || a.editor.Path != "/notes.txt" {
		t.Fatalf("F4 did not open editor: editor=%v status=%q", a.editor, a.status)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, '!', tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyF10, 0, tcell.ModNone))
	if a.dialog == nil || a.dialog.Title != "Unsaved changes" {
		t.Fatal("quit did not prompt for unsaved changes")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.editor != nil {
		t.Fatal("discard did not close editor")
	}
	reader, err := fs.Open(context.Background(), "/notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 5)
	_, _ = reader.Read(data)
	_ = reader.Close()
	if string(data) != "hello" {
		t.Fatalf("discard changed file contents to %q", data)
	}

	opened, err := editor.Open(context.Background(), fs, "/notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if opened.Changed {
		t.Fatal("new editor should start clean")
	}
}

func TestRenameCreateAndEditSymbolicLinkFlows(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.PutFile("/original.txt", []byte("value"), 0o644)
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModAlt))
	replaceInput(a, "/renamed.txt")
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if _, err := fs.Stat(context.Background(), "/original.txt"); err == nil {
		t.Fatal("rename left the original path")
	}
	if _, err := fs.Stat(context.Background(), "/renamed.txt"); err != nil {
		t.Fatalf("rename destination is missing: %v", err)
	}

	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'l', tcell.ModAlt))
	replaceInput(a, "/shortcut.txt")
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	entry, err := fs.Stat(context.Background(), "/shortcut.txt")
	if err != nil || !entry.IsLink || entry.Target != "/renamed.txt" {
		t.Fatalf("created link = %+v err=%v", entry, err)
	}

	for index, entry := range a.panels[0].Entries {
		if entry.Name == "shortcut.txt" {
			a.panels[0].MoveTo(index)
			break
		}
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF16, 0, tcell.ModNone))
	replaceInput(a, "/new-target.txt")
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	replaceInput(a, "/shortcut-new.txt")
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	entry, err = fs.Stat(context.Background(), "/shortcut-new.txt")
	if err != nil || !entry.IsLink || entry.Target != "/new-target.txt" {
		t.Fatalf("edited link = %+v err=%v", entry, err)
	}
	if _, err := fs.Stat(context.Background(), "/shortcut.txt"); err == nil {
		t.Fatal("edit link left the previous link path")
	}
}

func TestF3OnParentShowsLocalGitHistory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	run("init", "-q")
	run("config", "user.name", "Test User")
	run("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(dir+"/note.txt", []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "note.txt")
	run("commit", "-m", "initial import", "-q")
	screen := newTestScreen(t, 80, 24)
	a, err := New(screen, vfs.NewLocal(), dir)
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF3, 0, tcell.ModNone))
	if a.dialog == nil || a.dialog.Title == "" || len(a.dialog.Items) == 0 {
		t.Fatalf("F3 on parent did not open Git history: dialog=%#v status=%q", a.dialog, a.status)
	}
}

func replaceInput(a *App, text string) {
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlK, 0, tcell.ModCtrl))
	for _, value := range text {
		a.HandleKey(tcell.NewEventKey(tcell.KeyRune, value, tcell.ModNone))
	}
}

func newTestScreen(t *testing.T, width, height int) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(width, height)
	t.Cleanup(screen.Fini)
	return screen
}

func screenText(screen tcell.SimulationScreen) string {
	width, height := screen.Size()
	var b strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, _, _, _ := screen.GetContent(x, y)
			if r != 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func waitForOperation(t *testing.T, a *App, screen tcell.SimulationScreen) {
	t.Helper()
	finishedEvents := make(chan operationFinished, 1)
	go func() {
		for {
			event := screen.PollEvent()
			if interrupt, ok := event.(*tcell.EventInterrupt); ok {
				if finished, ok := interrupt.Data().(operationFinished); ok {
					finishedEvents <- finished
					return
				}
			}
		}
	}()
	select {
	case finished := <-finishedEvents:
		a.finishOperation(finished.result)
		if err := finished.result.Err(); err != nil {
			t.Fatalf("file operation failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for file operation")
	}
}
