package app

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/anare/nmc/internal/dialog"
	"github.com/anare/nmc/internal/editor"
	"github.com/anare/nmc/internal/git"
	"github.com/anare/nmc/internal/history"
	"github.com/anare/nmc/internal/keymap"
	"github.com/anare/nmc/internal/panel"
	"github.com/anare/nmc/internal/sshfs"
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

func TestPanelKeymapSwapsPanelsAndQuickSearchSelectsEntries(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "alpha"}, {Name: "beta"}, {Name: "bravo"}})
	a, err := New(newTestScreen(t, 80, 24), fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.panels[1].SetDir(context.Background(), "/", "bravo"); err != nil {
		t.Fatal(err)
	}
	left, right := a.panels[0], a.panels[1]
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlU, 0, tcell.ModCtrl))
	if a.panels[0] != right || a.panels[1] != left || a.active != 0 {
		t.Fatal("Ctrl-U did not exchange panel contents while preserving the active side")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlS, 0, tcell.ModCtrl))
	if !a.quickSearching {
		t.Fatal("Ctrl-S did not start panel quick search")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone))
	if selected, _ := a.panels[0].Selected(); selected.Name != "bravo" {
		t.Fatalf("quick-search selected %q, want bravo", selected.Name)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if a.quickSearching {
		t.Fatal("Escape did not leave quick search")
	}
}

func TestCommandHistoryNavigationBrowserAndClearFollowKeymap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, err := New(newTestScreen(t, 80, 24), vfs.NewMemFS("test"), "/")
	if err != nil {
		t.Fatal(err)
	}
	a.history.Add(history.Command, "echo older")
	a.history.Add(history.Command, "echo newer")
	if err := a.history.Save(); err != nil {
		t.Fatal(err)
	}
	a.cmdHistory = a.history.Cursor(history.Command)
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModAlt))
	if a.cmd.Text() != "echo newer" {
		t.Fatalf("Alt-P command line = %q", a.cmd.Text())
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModAlt))
	if a.cmd.Text() != "echo older" {
		t.Fatalf("second Alt-P command line = %q", a.cmd.Text())
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModAlt))
	if a.cmd.Text() != "echo newer" {
		t.Fatalf("Alt-N command line = %q", a.cmd.Text())
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModAlt))
	if a.dialog == nil || a.dialog.Title != "Command history" {
		t.Fatalf("Alt-H history browser = %#v", a.dialog)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlD, 0, tcell.ModCtrl))
	if got := a.history.Cursor(history.Command).Entries(); len(got) != 1 || got[0] != "echo older" {
		t.Fatalf("Ctrl-D did not forget the selected history entry: %q", got)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.dialog != nil || a.cmd.Text() != "echo older" {
		t.Fatalf("history selection text=%q dialog=%v", a.cmd.Text(), a.dialog)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlY, 0, tcell.ModCtrl))
	if !a.cmd.Empty() {
		t.Fatalf("Ctrl-Y did not clear command line: %q", a.cmd.Text())
	}
}

func TestDirectorySizesRunAsynchronouslyForLocalPanels(t *testing.T) {
	if _, err := exec.LookPath("du"); err != nil {
		t.Skip("du is not installed")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "data.txt"), []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	screen := newTestScreen(t, 80, 24)
	a, err := New(screen, vfs.NewLocal(), directory)
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlL, 0, tcell.ModCtrl))
	if a.dialog != nil || !strings.Contains(a.status, "Calculating") {
		t.Fatalf("directory sizes did not start asynchronously: dialog=%v status=%q", a.dialog, a.status)
	}
	result := make(chan directorySizesFinished, 1)
	go func() {
		for {
			if interrupt, ok := screen.PollEvent().(*tcell.EventInterrupt); ok {
				if finished, ok := interrupt.Data().(directorySizesFinished); ok {
					result <- finished
					return
				}
			}
		}
	}()
	var finished directorySizesFinished
	select {
	case finished = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for directory size output")
	}
	if finished.err != nil {
		t.Fatalf("directory size command failed: %v output=%q", finished.err, finished.output)
	}
	if !strings.Contains(string(finished.output), "data.txt") && !strings.Contains(string(finished.output), ".") {
		t.Fatalf("directory size output = %q", finished.output)
	}
	a.dialog = dialog.NewList("Directory sizes", strings.Split(strings.TrimSuffix(string(finished.output), "\n"), "\n"))
	if a.dialog == nil || !strings.Contains(strings.Join(a.dialog.Lines, "\n"), ".") {
		t.Fatalf("directory sizes list = %#v", a.dialog)
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

func TestCtrlBackslashConnectsAndDisconnectsRemotePanel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	sshDir := t.TempDir()
	script := `#!/bin/sh
last=
for arg do last=$arg; done
case "$last" in
  *"stat --version"*) printf gnu ;;
  *"cd --"*) printf 'L\tregular file\t4\t1700000000\t81a4\t1\tnote.txt\n' ;;
esac
`
	if err := os.WriteFile(filepath.Join(sshDir, "ssh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", sshDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	screen := newTestScreen(t, 80, 24)
	a, err := New(screen, vfs.NewMemFS(""), "/")
	if err != nil {
		t.Fatal(err)
	}
	a.dialRemote = sshfs.Dial
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, tcell.ModCtrl))
	if a.dialog == nil || a.dialog.Input == nil {
		t.Fatalf("connect binding did not open combo input: %#v", a.dialog)
	}
	a.dialog.Input.SetText("sftp://alice@example.test:2222/work")
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.dialog != nil || a.panels[0].FS.Name() != "ssh:example.test" || a.panels[0].Cwd != "/work" {
		t.Fatalf("remote panel connection: dialog=%#v fs=%q cwd=%q status=%q", a.dialog, a.panels[0].FS.Name(), a.panels[0].Cwd, a.status)
	}
	if entries := a.panels[0].Entries; len(entries) != 2 || entries[1].Name != "note.txt" {
		t.Fatalf("remote listing = %+v", entries)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, tcell.ModCtrl))
	if a.dialog == nil || a.dialog.Title != "Disconnect remote" {
		t.Fatalf("remote disconnect prompt = %#v", a.dialog)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.panels[0].FS.Name() != "" || a.panels[0].Cwd != "/" || len(a.remoteFS) != 0 {
		t.Fatalf("remote panel did not disconnect: fs=%q cwd=%q remotes=%d", a.panels[0].FS.Name(), a.panels[0].Cwd, len(a.remoteFS))
	}
}

func TestPanelCommandsFollowReboundKeymap(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := keymap.Parse("q")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Rebind(keymap.Panel, "quit", binding); err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF10, 0, tcell.ModNone))
	if a.quit {
		t.Fatal("F10 still quit after the quit action was rebound")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone))
	if !a.quit {
		t.Fatal("rebound q did not invoke quit")
	}
}

func TestFunctionKeyHintsFollowCurrentBindings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := New(newTestScreen(t, 80, 24), vfs.NewMemFS("test"), "/")
	if err != nil {
		t.Fatal(err)
	}
	if got := a.actionHints(keymap.Panel)[4]; got != "Copy" {
		t.Fatalf("default F5 hint = %q, want Copy", got)
	}
	binding, err := keymap.Parse("Alt-C")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Rebind(keymap.Panel, "copy", binding); err != nil {
		t.Fatal(err)
	}
	if got := a.actionHints(keymap.Panel)[4]; got != "" {
		t.Fatalf("F5 hint remains after rebinding copy: %q", got)
	}
}

func TestF2SettingsPersistHiddenFilePreference(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: ".hidden"}, {Name: "visible"}})
	screen := newTestScreen(t, 80, 24)
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF2, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.panels[0].Opt.ShowHidden {
		t.Fatal("settings menu did not turn hidden files off")
	}
	for range 6 {
		a.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.status != "Settings saved" {
		t.Fatalf("save status = %q", a.status)
	}
	if _, err := os.Stat(filepath.Join(home, ".bcrc")); err != nil {
		t.Fatalf("settings file was not written: %v", err)
	}
	restarted, err := New(newTestScreen(t, 80, 24), fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.panels[0].Opt.ShowHidden {
		t.Fatal("hidden-file preference did not survive restart")
	}
	for _, entry := range restarted.panels[0].Entries {
		if entry.Name == ".hidden" {
			t.Fatal("hidden file remained visible after restart")
		}
	}
}

func TestF2KeyTesterCapturesKeysAndRequiresTwoEscapes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := New(newTestScreen(t, 80, 24), vfs.NewMemFS("test"), "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF2, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !a.keyTest {
		t.Fatal("settings menu did not open the key tester")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlZ, 0, tcell.ModCtrl))
	if !strings.Contains(a.keyTestText, "Key=") {
		t.Fatalf("key tester did not report the key: %q", a.keyTestText)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if !a.keyTest {
		t.Fatal("one Escape left the key tester")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if a.keyTest {
		t.Fatal("two Escapes did not leave the key tester")
	}
}

func TestShortcutEditorBindsPersistsAndRejectsReservedKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a, err := New(newTestScreen(t, 80, 24), vfs.NewMemFS("test"), "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF2, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.keyCapture == nil {
		t.Fatal("shortcut menu did not begin key capture")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone))
	if a.keyCapture != nil {
		t.Fatal("key capture did not finish")
	}
	if action, ok := a.keys.Lookup(keymap.Panel, tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone)); !ok || action != "help" {
		t.Fatalf("new binding resolves to %q, %v", action, ok)
	}
	data, err := os.ReadFile(filepath.Join(home, ".bcrc"))
	if err != nil || !strings.Contains(string(data), "help = z") {
		t.Fatalf("binding was not persisted: %q err=%v", data, err)
	}
	restarted, err := New(newTestScreen(t, 80, 24), vfs.NewMemFS("test"), "/")
	if err != nil {
		t.Fatal(err)
	}
	if action, ok := restarted.keys.Lookup(keymap.Panel, tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone)); !ok || action != "help" {
		t.Fatalf("saved binding did not load: %q, %v", action, ok)
	}
	restarted.keyCapture = &bindingCapture{mode: keymap.Panel, action: "view"}
	restarted.HandleKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if restarted.dialog == nil || !strings.Contains(strings.Join(restarted.dialog.Lines, " "), "reserved") {
		t.Fatal("shortcut capture did not reject reserved Tab")
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

func TestArchiveEnterF3ListingAndRootExit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	archivePath := filepath.Join(t.TempDir(), "sample.nmcpack")
	if err := os.WriteFile(archivePath, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := `[archive test]
match = *.nmcpack
list = printf nested/file.txt
extract = mkdir -p "$DEST/nested"; printf contents > "$DEST/$ENTRY"
`
	if err := os.WriteFile(filepath.Join(home, ".bchandlers"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	screen := newTestScreen(t, 80, 24)
	a, err := New(screen, vfs.NewLocal(), filepath.Dir(archivePath))
	if err != nil {
		t.Fatal(err)
	}
	panel := a.panels[0]
	for index, entry := range panel.Entries {
		if entry.Name == filepath.Base(archivePath) {
			panel.MoveTo(index)
			break
		}
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF3, 0, tcell.ModNone))
	if a.viewer == nil || !strings.Contains(string(a.viewer.Data), "nested/file.txt") {
		t.Fatalf("F3 did not show archive listing: viewer=%#v status=%q", a.viewer, a.status)
	}
	a.viewer = nil
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if panel.FS.Name() != "archive:sample.nmcpack" || panel.Cwd != "/" || !panel.Nested() {
		t.Fatalf("archive enter state: fs=%q cwd=%q nested=%v", panel.FS.Name(), panel.Cwd, panel.Nested())
	}
	panel.MoveTo(0)
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if panel.FS.Name() != "" || panel.Cwd != filepath.Dir(archivePath) || panel.Nested() || len(a.archiveFS) != 0 {
		t.Fatalf("archive root did not return to host: fs=%q cwd=%q nested=%v tracked=%d", panel.FS.Name(), panel.Cwd, panel.Nested(), len(a.archiveFS))
	}
	if entry, ok := panel.Selected(); !ok || entry.Name != filepath.Base(archivePath) {
		t.Fatalf("return selection = %#v", entry)
	}
}

func TestF5CopiesARealZipMemberOutOfArchive(t *testing.T) {
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Skip("unzip is not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	archivePath := filepath.Join(root, "sample.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	member, err := writer.Create("nested/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write([]byte("copied from archive")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	screen := newTestScreen(t, 80, 24)
	a, err := New(screen, vfs.NewLocal(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, archive := range a.archiveFS {
			_ = archive.Close()
		}
	})
	if err := a.panels[1].SetDir(context.Background(), destination, ""); err != nil {
		t.Fatal(err)
	}
	selectLocalFile(t, a.panels[0], filepath.Base(archivePath))
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	selectLocalFile(t, a.panels[0], "nested")
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	selectLocalFile(t, a.panels[0], "readme.txt")
	a.HandleKey(tcell.NewEventKey(tcell.KeyF5, 0, tcell.ModNone))
	if a.dialog == nil {
		t.Fatal("F5 did not request the copy destination")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	waitForOperation(t, a, screen)
	data, err := os.ReadFile(filepath.Join(destination, "readme.txt"))
	if err != nil || string(data) != "copied from archive" {
		t.Fatalf("copied archive member=%q err=%v status=%q", data, err, a.status)
	}
}

func TestF3UsesNamedOpenHandlerAndFallsBackWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	file := filepath.Join(t.TempDir(), "preview.nmcview")
	if err := os.WriteFile(file, []byte("preview contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := `[open preview]
match = *.nmcview
run = sh -c 'printf opened > "$FILE.called"'
`
	if err := os.WriteFile(filepath.Join(home, ".bchandlers"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(newTestScreen(t, 80, 24), vfs.NewLocal(), filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	selectLocalFile(t, a.panels[0], filepath.Base(file))
	a.HandleKey(tcell.NewEventKey(tcell.KeyF3, 0, tcell.ModNone))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(file + ".called"); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := os.Stat(file + ".called"); err != nil || a.viewer != nil {
		t.Fatalf("F3 did not launch named handler: viewer=%v err=%v status=%q", a.viewer != nil, err, a.status)
	}

	missingHome := t.TempDir()
	t.Setenv("HOME", missingHome)
	missingConfig := `[open preview]
match = *.nmcview
run = /definitely/not/installed
`
	if err := os.WriteFile(filepath.Join(missingHome, ".bchandlers"), []byte(missingConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	fallback, err := New(newTestScreen(t, 80, 24), vfs.NewLocal(), filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	selectLocalFile(t, fallback.panels[0], filepath.Base(file))
	fallback.HandleKey(tcell.NewEventKey(tcell.KeyF3, 0, tcell.ModNone))
	if fallback.viewer == nil || fallback.status != "Open-with program unavailable; showing file" {
		t.Fatalf("F3 fallback viewer=%v status=%q", fallback.viewer != nil, fallback.status)
	}
}

func selectLocalFile(t *testing.T, p *panel.Panel, name string) {
	t.Helper()
	for index, entry := range p.Entries {
		if entry.Name == name {
			p.MoveTo(index)
			return
		}
	}
	t.Fatalf("panel does not contain %q", name)
}

func TestViewerActionsUseViewerKeymap(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.PutFile("/readme.txt", []byte("alpha"), 0o644)
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF3, 0, tcell.ModNone))
	initialWrap := a.viewer.Wrap
	binding, err := keymap.Parse("Ctrl-W")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Rebind(keymap.Viewer, "viewer-wrap", binding); err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone))
	if a.viewer.Wrap != initialWrap {
		t.Fatal("old w binding still toggled wrapping")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlW, 0, tcell.ModCtrl))
	if a.viewer.Wrap == initialWrap {
		t.Fatal("rebound Ctrl-W did not toggle wrapping")
	}
}

func TestFindActionsUseFindKeymap(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	a, err := New(screen, vfs.NewMemFS("test"), "/")
	if err != nil {
		t.Fatal(err)
	}
	a.findState = &findState{}
	binding, err := keymap.Parse("z")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Rebind(keymap.Find, "find-quit", binding); err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone))
	if a.findState == nil {
		t.Fatal("old q binding still closed Find")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone))
	if a.findState != nil {
		t.Fatal("rebound z did not close Find")
	}
}

func TestEditorActionsUseEditorKeymap(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.PutFile("/notes.txt", []byte("alpha"), 0o644)
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF4, 0, tcell.ModNone))
	if a.editor == nil {
		t.Fatal("F4 did not open editor")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	binding, err := keymap.Parse("Alt-U")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Rebind(keymap.Editor, "editor-undo", binding); err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlZ, 0, tcell.ModCtrl))
	if got := string(a.editor.Bytes()); got != "xalpha" {
		t.Fatalf("old Ctrl-Z binding still undid edit: %q", got)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModAlt))
	if got := string(a.editor.Bytes()); got != "alpha" {
		t.Fatalf("rebound Alt-U did not undo edit: %q", got)
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

func TestGitHistoryCanCommitWithoutExistingCommits(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
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
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := New(newTestScreen(t, 80, 24), vfs.NewLocal(), dir)
	if err != nil {
		t.Fatal(err)
	}
	a.panels[0].MoveTo(0)
	a.HandleKey(tcell.NewEventKey(tcell.KeyF3, 0, tcell.ModNone))
	if a.dialog == nil || !strings.Contains(a.dialog.Title, "history is empty") {
		t.Fatalf("empty history menu = %#v", a.dialog)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.dialog == nil || a.dialog.Title != "Commit changes" {
		t.Fatalf("commit selection dialog = %#v", a.dialog)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.dialog == nil || a.dialog.Input == nil {
		t.Fatalf("commit message input = %#v", a.dialog)
	}
	a.dialog.Input.SetText("first commit")
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.dialog == nil || !strings.HasPrefix(a.dialog.Title, "Git:") {
		t.Fatalf("history did not reopen after commit: %#v", a.dialog)
	}
	commits, err := git.Log(context.Background(), dir, "", 10)
	if err != nil || len(commits) != 1 || commits[0].Subject != "first commit" {
		t.Fatalf("commits=%+v err=%v", commits, err)
	}
}

func TestGitHistoryCanSwitchBranchesAndReloadBothPanels(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "note.txt")
	run("commit", "-q", "-m", "initial")
	run("branch", "feature")
	a, err := New(newTestScreen(t, 80, 24), vfs.NewLocal(), dir)
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF3, 0, tcell.ModNone))
	a.dialog.Selected = 1
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.dialog == nil || a.dialog.Title != "Switch branch" {
		t.Fatalf("branch menu = %#v", a.dialog)
	}
	for index, item := range a.dialog.Items {
		if strings.TrimPrefix(item, "* ") == "feature" {
			a.dialog.Selected = index
			break
		}
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.dialog == nil || a.dialog.Title != "Switch branch" || a.dialog.Buttons[0] != "Switch" {
		t.Fatalf("branch confirmation = %#v", a.dialog)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if branch := git.Describe(context.Background(), dir); branch != "feature" {
		t.Fatalf("current branch = %q, want feature", branch)
	}
	if a.dialog != nil {
		t.Fatalf("successful switch left dialog open: %#v", a.dialog)
	}
}

func TestCtrlOTogglesPersistentInteractiveShellSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY sessions are Unix-only")
	}
	screen := newTestScreen(t, 80, 24)
	a, err := New(screen, vfs.NewMemFS("test"), "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlO, 0, tcell.ModCtrl))
	if !a.shellVisible || a.session == nil {
		t.Fatal("Ctrl-O did not create and show a shell session")
	}
	if _, err := a.session.Write([]byte("printf 'shell-marker\\n'\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(strings.Join(a.session.Output(), "\n"), "shell-marker") {
		time.Sleep(time.Millisecond * 5)
	}
	if output := strings.Join(a.session.Output(), "\n"); !strings.Contains(output, "shell-marker") {
		t.Fatalf("interactive shell output = %q", output)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlO, 0, tcell.ModCtrl))
	if a.shellVisible || a.session == nil {
		t.Fatal("Ctrl-O hid or terminated the session incorrectly")
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyCtrlO, 0, tcell.ModCtrl))
	if !a.shellVisible || a.session == nil {
		t.Fatal("Ctrl-O did not restore the existing shell session")
	}
	_ = a.session.Close()
}

func TestEditorSelectionUsesFileClipboardForCopyCutAndPaste(t *testing.T) {
	screen := newTestScreen(t, 80, 24)
	fs := vfs.NewMemFS("test")
	fs.PutFile("/note.txt", []byte("hello"), 0o644)
	a, err := New(screen, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyF4, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModAlt))
	a.HandleKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 's', tcell.ModAlt))
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModAlt))
	data, err := a.clipboard.Paste()
	if err != nil || string(data) != "he" {
		t.Fatalf("copied selection=%q err=%v", data, err)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModAlt))
	if got := string(a.editor.Bytes()); got != "llo" {
		t.Fatalf("after cut=%q", got)
	}
	a.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModAlt))
	if got := string(a.editor.Bytes()); got != "hello" {
		t.Fatalf("after paste=%q", got)
	}
	if !a.editor.Undo() || string(a.editor.Bytes()) != "llo" {
		t.Fatalf("undo paste=%q", a.editor.Bytes())
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
