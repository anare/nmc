package panel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anare/nmc/internal/ui"
	"github.com/anare/nmc/internal/vfs"
	"github.com/gdamore/tcell/v2"
)

func TestPanelRestoresTheCursorWhenReturningToAParent(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "alpha", IsDir: true}, {Name: "beta", IsDir: true}})
	fs.Put("/beta", []vfs.Entry{{Name: "inside.txt"}})
	p, err := New(fs, "/", vfs.Options{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	p.SetRect(0, 0, 80, 24)
	p.MoveTo(2)
	if got, ok := p.Selected(); !ok || got.Name != "beta" {
		t.Fatalf("selected entry = %#v, want beta", got)
	}
	if handled, err := p.Enter(context.Background()); !handled || err != nil {
		t.Fatalf("enter returned handled=%v, err=%v", handled, err)
	}
	if err := p.GoUp(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, ok := p.Selected(); !ok || got.Name != "beta" {
		t.Fatalf("cursor after returning = %#v, want beta", got)
	}
}

func TestPanelCannotMarkTheParentAndMarkingAdvances(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "work", IsDir: true}})
	fs.Put("/work", []vfs.Entry{{Name: "a"}, {Name: "b"}})
	p, err := New(fs, "/work", vfs.Options{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	p.ToggleMark()
	if len(p.Marked) != 0 || p.Cursor != 1 {
		t.Fatalf("parent entry was marked: %#v", p.Marked)
	}
	p.ToggleMark()
	if len(p.Marked) != 1 || p.Cursor != 2 {
		t.Fatalf("marking did not mark and advance: marks=%#v cursor=%d", p.Marked, p.Cursor)
	}
	if got := p.Targets(); len(got) != 1 || got[0] != "/work/a" {
		t.Fatalf("targets = %#v, want /work/a", got)
	}
}

func TestFailedDirectoryChangeKeepsTheCurrentListing(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "visible.txt"}})
	p, err := New(fs, "/", vfs.Options{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	before := append([]vfs.Entry(nil), p.Entries...)
	if err := p.SetDir(context.Background(), "/missing", ""); err == nil {
		t.Fatal("SetDir succeeded for a missing directory")
	}
	if len(p.Entries) != len(before) || p.Entries[0].Name != before[0].Name || p.Cwd != "/" || p.Err == nil {
		t.Fatalf("failed SetDir changed visible state: cwd=%q entries=%#v error=%v", p.Cwd, p.Entries, p.Err)
	}
}

func TestPanelDrawsFullLayoutAtEightyByTwentyFour(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "work", IsDir: true}})
	fs.Put("/work", []vfs.Entry{
		{Name: "sub", IsDir: true, ModTime: time.Date(2026, 10, 5, 18, 6, 0, 0, time.UTC)},
		{Name: "run.sh", Mode: 0100755, Size: 18, ModTime: time.Date(2026, 10, 5, 18, 6, 0, 0, time.UTC)},
		{Name: "notes.md", Size: 412, ModTime: time.Date(2026, 10, 5, 18, 6, 0, 0, time.UTC)},
	})
	p, err := New(fs, "/work", vfs.Options{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	p.SetRect(0, 0, 80, 24)
	screen := newPanelScreen(t, 80, 24)
	p.Draw(screen, ui.DefaultTheme())

	if got := panelScreenRow(screen, 0); !strings.Contains(got, "/work") {
		t.Fatalf("panel title row = %q, want path", got)
	}
	if got := panelScreenRow(screen, 1); !strings.Contains(got, "Name") || !strings.Contains(got, "Size") || !strings.Contains(got, "Modify time") {
		t.Fatalf("column heading row = %q, want all columns", got)
	}
	if got := panelScreenRow(screen, 2); !strings.Contains(got, "../") {
		t.Fatalf("parent row = %q, want ../", got)
	}
	if got := panelScreenRow(screen, 5); !strings.Contains(got, "run.sh*") {
		t.Fatalf("executable row = %q, want run.sh indicator", got)
	}
	if got := panelScreenRow(screen, 22); !strings.Contains(got, "UP--DIR") {
		t.Fatalf("footer row = %q, want selected parent description", got)
	}
}

func TestPanelDrawsAtFortyByTenAndKeepsCursorVisible(t *testing.T) {
	fs := vfs.NewMemFS("test")
	entries := make([]vfs.Entry, 8)
	for i := range entries {
		entries[i] = vfs.Entry{Name: fmt.Sprintf("file-%02d.txt", i), Size: int64(i)}
	}
	fs.Put("/", entries)
	p, err := New(fs, "/", vfs.Options{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	p.SetRect(0, 0, 40, 10)
	p.End()
	screen := newPanelScreen(t, 40, 10)
	p.Draw(screen, ui.DefaultTheme())

	if got := panelScreenRow(screen, 1); !strings.Contains(got, "Name") || !strings.Contains(got, "Size") {
		t.Fatalf("narrow column heading = %q, want name and size columns", got)
	}
	if got := panelScreenRow(screen, 6); !strings.Contains(got, "file-07.txt") {
		t.Fatalf("last visible row = %q, want cursor entry", got)
	}
	if got := panelScreenRune(screen, 38, 5); got != '█' {
		t.Fatalf("scrollbar thumb = %q, want visible thumb", got)
	}
	if p.Cursor < p.Top || p.Cursor >= p.Top+p.VisibleRows() {
		t.Fatalf("cursor %d is outside visible range %d..%d", p.Cursor, p.Top, p.Top+p.VisibleRows())
	}
}

func TestPanelDrawStylesCursorAndMarkedEntries(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "alpha.go"}, {Name: "beta.txt"}})
	p, err := New(fs, "/", vfs.Options{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	p.SetRect(0, 0, 40, 10)
	p.MoveTo(0)
	p.Marked["/beta.txt"] = true
	screen := newPanelScreen(t, 40, 10)
	theme := ui.DefaultTheme()
	p.Draw(screen, theme)

	cursorFG, cursorBG, _ := panelScreenStyle(screen, 1, 2)
	wantCursorFG, wantCursorBG, _ := theme.Selection.Decompose()
	if cursorFG != wantCursorFG || cursorBG != wantCursorBG {
		t.Fatalf("cursor cell style = (%v, %v), want (%v, %v)", cursorFG, cursorBG, wantCursorFG, wantCursorBG)
	}
	markedFG, _, _ := panelScreenStyle(screen, 1, 3)
	wantMarkedFG, _, _ := theme.Marked.Decompose()
	if markedFG != wantMarkedFG {
		t.Fatalf("marked foreground = %v, want %v", markedFG, wantMarkedFG)
	}
}

func TestDisplayNameKeepsTheIndicatorWhenTruncated(t *testing.T) {
	entry := vfs.Entry{Name: "a-directory-with-a-long-name", IsDir: true}
	if got := DisplayName(entry, 6); got != "a-di~/" {
		t.Fatalf("truncated display name = %q, want %q", got, "a-di~/")
	}
}

func TestPanelFooterShowsSymlinkAndHardLinkDetails(t *testing.T) {
	theme := ui.DefaultTheme()
	p := &Panel{Entries: []vfs.Entry{{Name: "config", IsLink: true, Target: "/etc/app.conf"}}}
	p.MoveTo(0)
	if got, _ := p.footer(theme); got != "config -> /etc/app.conf" {
		t.Fatalf("symlink footer = %q", got)
	}
	p.Entries[0] = vfs.Entry{Name: "data.db", Links: 3}
	if got, _ := p.footer(theme); got != "data.db   3 links" {
		t.Fatalf("hard-link footer = %q", got)
	}
	p.Err = fmt.Errorf("listing failed")
	if got, _ := p.footer(theme); got != "listing failed" {
		t.Fatalf("error footer = %q", got)
	}
}

func newPanelScreen(t *testing.T, width, height int) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(width, height)
	t.Cleanup(screen.Fini)
	return screen
}

func panelScreenRow(screen tcell.SimulationScreen, y int) string {
	width, _ := screen.Size()
	var b strings.Builder
	for x := 0; x < width; x++ {
		r, _, _, _ := screen.GetContent(x, y)
		if r == 0 {
			r = ' '
		}
		b.WriteRune(r)
	}
	return b.String()
}

func panelScreenRune(screen tcell.SimulationScreen, x, y int) rune {
	r, _, _, _ := screen.GetContent(x, y)
	return r
}

func panelScreenStyle(screen tcell.SimulationScreen, x, y int) (tcell.Color, tcell.Color, tcell.AttrMask) {
	_, _, style, _ := screen.GetContent(x, y)
	return style.Decompose()
}
