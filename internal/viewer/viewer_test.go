package viewer

import (
	"context"
	"strings"
	"testing"

	"github.com/anare/nmc/internal/ui"
	"github.com/anare/nmc/internal/vfs"
	"github.com/gdamore/tcell/v2"
)

func TestBinaryContentOpensInHexAndSearchesBytes(t *testing.T) {
	view := FromBytes("sample.bin", []byte{0, 'a', 'B', 'c', 0xff})
	if view.Mode != HexMode {
		t.Fatalf("mode = %v, want hex", view.Mode)
	}
	if !view.Search("Bc") || len(view.Hits) != 1 || view.Hits[0] != 2 {
		t.Fatalf("byte search results = %#v", view.Hits)
	}
}

func TestTextSearchIsCaseInsensitiveAndCyclesResults(t *testing.T) {
	view := FromBytes("notes.txt", []byte("Alpha\nbeta alpha\n"))
	if !view.Search("ALPHA") || len(view.Hits) != 2 || view.Top != 0 {
		t.Fatalf("text search results = %#v top=%d", view.Hits, view.Top)
	}
	if !view.NextHit(false) || view.Top != 1 {
		t.Fatalf("next hit top = %d, want 1", view.Top)
	}
	if !view.NextHit(true) || view.Top != 0 {
		t.Fatalf("previous hit top = %d, want 0", view.Top)
	}
}

func TestOpenReportsWhenTheFileExceedsTheLimit(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.PutFile("/large.txt", make([]byte, SizeLimit+1), 0o644)
	view, err := Open(context.Background(), fs, "/large.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Truncated || len(view.Data) != SizeLimit || !strings.Contains(view.Status(), "truncated") {
		t.Fatalf("truncated=%v size=%d status=%q", view.Truncated, len(view.Data), view.Status())
	}
}

func TestDrawShowsHexOffsetsAndHonoursWrapAndLineNumberSettings(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(60, 10)
	hexView := FromBytes("bytes.bin", []byte{0, 1, 2, 3})
	hexView.Draw(screen, ui.Rect{W: 60, H: 10}, ui.DefaultTheme())
	if got := viewerScreenText(screen); !strings.Contains(got, "00000000") || !strings.Contains(got, "00010203") {
		t.Fatalf("hex screen = %q", got)
	}
	screen.Clear()
	textView := FromBytes("text.txt", []byte("line one\nline two"))
	textView.LineNumber = true
	textView.Draw(screen, ui.Rect{W: 60, H: 10}, ui.DefaultTheme())
	if got := viewerScreenText(screen); !strings.Contains(got, "1 line one") || !strings.Contains(got, "2 line two") {
		t.Fatalf("text screen = %q", got)
	}
}

func viewerScreenText(screen tcell.SimulationScreen) string {
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
