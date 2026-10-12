package app

import (
	"strings"
	"testing"

	"github.com/anare/nmc/internal/vfs"
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
	if leftX != 0 || rightX != 40 || leftY != 0 || rightY != 0 || leftW != 40 || rightW != 40 || leftH != 22 || rightH != 22 {
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
