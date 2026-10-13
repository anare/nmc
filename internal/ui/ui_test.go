package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/anare/nmc/internal/vfs"
	"github.com/gdamore/tcell/v2"
)

func TestRectEdgesUseExclusiveRightAndBottom(t *testing.T) {
	rect := Rect{X: 3, Y: 4, W: 8, H: 5}
	if rect.Right() != 11 || rect.Bottom() != 9 {
		t.Fatalf("rect edges = (%d, %d), want (11, 9)", rect.Right(), rect.Bottom())
	}
}

func TestTextClipsWideCharactersWithoutDrawingHalf(t *testing.T) {
	screen := newScreen(t, 8, 2)
	style := tcell.StyleDefault.Foreground(tcell.ColorWhite)
	if got := Text(screen, 0, 0, 2, "A界B", style); got != 1 {
		t.Fatalf("cells used = %d, want 1", got)
	}
	if got := screenRow(screen, 0); got != "A       " {
		t.Fatalf("drawn row = %q, want %q", got, "A       ")
	}
	if got := Text(screen, 0, 1, 3, "A界B", style); got != 3 {
		t.Fatalf("cells used = %d, want 3", got)
	}
	family := "👨‍👩‍👧‍👦"
	if got := Text(screen, 0, 1, 2, family+"x", style); got != 2 {
		t.Fatalf("emoji cluster used %d cells, want 2", got)
	}
}

func TestTextPaddedFillsUnusedCells(t *testing.T) {
	screen := newScreen(t, 8, 1)
	style := tcell.StyleDefault.Background(tcell.ColorBlue)
	if got := TextPadded(screen, 2, 0, 5, "xy", style); got != 2 {
		t.Fatalf("cells used = %d, want 2", got)
	}
	if got := screenRow(screen, 0); got != "  xy    " {
		t.Fatalf("drawn row = %q, want %q", got, "  xy    ")
	}
}

func TestTruncationMarksTheCutWithoutSplittingWideRunes(t *testing.T) {
	if got := Truncate("a界bc", 3); got != "a~" {
		t.Fatalf("right truncation = %q, want %q", got, "a~")
	}
	if got := TruncateLeft("a界bc", 3); got != "~bc" {
		t.Fatalf("left truncation = %q, want %q", got, "~bc")
	}
	if got := Truncate("界", 1); got != "~" {
		t.Fatalf("one-cell truncation = %q, want %q", got, "~")
	}
}

func TestDrawBoxUsesRequestedFrameCharacters(t *testing.T) {
	screen := newScreen(t, 8, 5)
	DrawBox(screen, Rect{X: 1, Y: 1, W: 6, H: 3}, Single, tcell.StyleDefault)
	if got, want := screenRow(screen, 1), " ┌────┐ "; got != want {
		t.Fatalf("top border = %q, want %q", got, want)
	}
	if got, want := screenRow(screen, 2), " │    │ "; got != want {
		t.Fatalf("side border = %q, want %q", got, want)
	}
	if got, want := screenRow(screen, 3), " └────┘ "; got != want {
		t.Fatalf("bottom border = %q, want %q", got, want)
	}
}

func TestDefaultThemeGivesEveryFileClassAUniquePalette(t *testing.T) {
	theme := DefaultTheme()
	entries := []vfs.Entry{
		{IsDir: true},
		{Name: "run", Mode: 0100755},
		{Name: "link", IsLink: true},
		{Name: "broken", IsLink: true, Broken: true},
		{Name: "device", Mode: os.ModeDevice | 0600},
		{Name: "archive.zip"},
		{Name: "image.png"},
		{Name: "sound.mp3"},
		{Name: "code.go"},
		{Name: "readme.md"},
		{Name: "config.json"},
		{Name: "scratch.tmp"},
		{Name: "plain"},
	}
	type palette struct {
		fg   tcell.Color
		bold bool
	}
	seen := make(map[palette]vfs.Class, len(entries))
	for _, entry := range entries {
		fg, _, attr := theme.FileTypeStyle(entry).Decompose()
		key := palette{fg: fg, bold: attr&tcell.AttrBold != 0}
		if previous, ok := seen[key]; ok {
			t.Fatalf("classes %q and %q share a foreground/bold palette", previous, entry.Class())
		}
		seen[key] = entry.Class()
	}
	panelBG := tcell.NewRGBColor(0, 0, 128)
	for _, entry := range entries {
		fg, _, _ := theme.FileTypeStyle(entry).Decompose()
		if fg == panelBG {
			t.Fatalf("class %q uses panel background as foreground", entry.Class())
		}
	}
}

func TestEntryStyleLayersMarkAndCursor(t *testing.T) {
	theme := DefaultTheme()
	entry := vfs.Entry{Name: "main.go"}
	fg, bg, _ := theme.EntryStyle(entry, false, true).Decompose()
	if fg != tcell.ColorBlack || bg != tcell.NewRGBColor(0, 170, 170) {
		t.Fatalf("cursor style = (%v, %v), want black on cyan", fg, bg)
	}
	fg, bg, _ = theme.EntryStyle(entry, true, true).Decompose()
	if fg != tcell.ColorYellow || bg != tcell.NewRGBColor(0, 170, 170) {
		t.Fatalf("marked cursor style = (%v, %v), want yellow on cyan", fg, bg)
	}
}

func TestHintBarFillsTheRequestedWidth(t *testing.T) {
	screen := newScreen(t, 13, 1)
	var labels [10]string
	for i := range labels {
		labels[i] = string(rune('A' + i))
	}
	DrawHintBar(screen, 0, 13, DefaultTheme(), labels)
	got := screenRow(screen, 0)
	if len([]rune(got)) != 13 || !strings.Contains(got, "1A") || !strings.HasSuffix(got, "0") {
		t.Fatalf("hint row = %q, want 13 cells, first label and final key", got)
	}
}

func TestHintBarHighlightsFunctionKeyButtons(t *testing.T) {
	screen := newScreen(t, 40, 1)
	var labels [10]string
	labels[0] = "Help"
	theme := DefaultTheme()
	DrawHintBar(screen, 0, 40, theme, labels)
	_, _, keyStyle, _ := screen.GetContent(0, 0)
	_, _, labelStyle, _ := screen.GetContent(1, 0)
	_, keyBG, _ := keyStyle.Decompose()
	_, labelBG, _ := labelStyle.Decompose()
	_, barBG, _ := theme.HintBar.Decompose()
	_, buttonBG, _ := theme.HintButton.Decompose()
	if keyBG != buttonBG || labelBG != buttonBG || barBG == buttonBG {
		t.Fatalf("keybar backgrounds key=%v label=%v bar=%v button=%v", keyBG, labelBG, barBG, buttonBG)
	}
}

func newScreen(t *testing.T, width, height int) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(width, height)
	t.Cleanup(screen.Fini)
	return screen
}

func screenRow(screen tcell.SimulationScreen, y int) string {
	width, _ := screen.Size()
	var b strings.Builder
	for x := 0; x < width; x++ {
		ch, _, _, _ := screen.GetContent(x, y)
		if ch == 0 {
			ch = ' '
		}
		b.WriteRune(ch)
	}
	return b.String()
}
