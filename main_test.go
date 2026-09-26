package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestSplitCommandLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "simple", input: "vim", want: []string{"vim"}},
		{name: "whitespace", input: "  vim   file  ", want: []string{"vim", "file"}},
		{name: "double quoted arg", input: "code --goto \"a b.txt:1\"", want: []string{"code", "--goto", "a b.txt:1"}},
		{name: "single quoted arg", input: "editor 'my file.txt'", want: []string{"editor", "my file.txt"}},
		{name: "escaped space", input: "my\\ editor --wait", want: []string{"my editor", "--wait"}},
		{name: "mixed quotes", input: "cmd \"two words\" 'three words'", want: []string{"cmd", "two words", "three words"}},
		{name: "unterminated double", input: "code \"oops", wantErr: true},
		{name: "unterminated single", input: "code 'oops", wantErr: true},
		{name: "trailing escape", input: "code \\", wantErr: true},
		{name: "empty", input: "   ", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitCommandLine(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil with result %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d (%#v), want %d (%#v)", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("arg %d mismatch: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func newTestState(t *testing.T) *appState {
	t.Helper()
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	left := newPanel("Left")
	right := newPanel("Right")
	if err := left.load(tmp, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := right.load(tmp, 0, true); err != nil {
		t.Fatal(err)
	}
	return &appState{
		app:      tview.NewApplication(),
		pages:    tview.NewPages().AddPage("main", tview.NewBox(), true, true),
		panels:   [2]*panel{left, right},
		status:   tview.NewTextView(),
		cmdInput: tview.NewInputField(),
	}
}

func TestEscSequenceTimeoutAndFunctionMapping(t *testing.T) {
	s := newTestState(t)
	if got := s.keyHandler(tcell.NewEventKey(tcell.KeyESC, 0, tcell.ModNone)); got != nil {
		t.Fatalf("expected ESC to be captured")
	}
	if !s.escPending {
		t.Fatalf("expected escPending after ESC")
	}
	if got := s.keyHandler(tcell.NewEventKey(tcell.KeyRune, '0', tcell.ModNone)); got != nil {
		t.Fatalf("expected ESC+0 sequence to be captured")
	}
	if s.escPending {
		t.Fatalf("expected escPending false after ESC+0")
	}
	if got := s.keyHandler(tcell.NewEventKey(tcell.KeyESC, 0, tcell.ModNone)); got != nil {
		t.Fatalf("expected ESC to be captured")
	}
	if got := s.keyHandler(tcell.NewEventKey(tcell.KeyRune, escTimeoutRune, tcell.ModNone)); got != nil {
		t.Fatalf("expected timeout rune consumed")
	}
	if !s.passEsc {
		t.Fatalf("expected passEsc true after timeout dispatch")
	}
	if got := s.keyHandler(tcell.NewEventKey(tcell.KeyESC, 0, tcell.ModNone)); got == nil {
		t.Fatalf("expected standalone ESC to pass through")
	}
}

func TestCopyMoveDirectoryDestinationGuards(t *testing.T) {
	t.Run("copy rejects descendant destination", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "src")
		_ = os.MkdirAll(src, 0o755)
		_ = os.WriteFile(filepath.Join(src, "f.txt"), []byte("x"), 0o644)
		if err := copyPath(src, filepath.Join(src, "child")); err == nil {
			t.Fatalf("expected error when copying into descendant path")
		}
	})

	t.Run("move rejects descendant destination", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "src")
		_ = os.MkdirAll(src, 0o755)
		_ = os.WriteFile(filepath.Join(src, "f.txt"), []byte("x"), 0o644)
		if err := movePath(src, filepath.Join(src, "child")); err == nil {
			t.Fatalf("expected error when moving into descendant path")
		}
	})

	t.Run("copy and move reject identical paths", func(t *testing.T) {
		root := t.TempDir()
		srcFile := filepath.Join(root, "same.txt")
		_ = os.WriteFile(srcFile, []byte("x"), 0o644)
		if err := copyPath(srcFile, srcFile); err == nil {
			t.Fatalf("expected identical copy error")
		}
		if err := movePath(srcFile, srcFile); err == nil {
			t.Fatalf("expected identical move error")
		}
		equivalent := filepath.Join(root, ".", "same.txt")
		if err := copyPath(srcFile, equivalent); err == nil {
			t.Fatalf("expected equivalent-path copy error")
		}
		if err := movePath(srcFile, equivalent); err == nil {
			t.Fatalf("expected equivalent-path move error")
		}
	})

	t.Run("copy and move non-descendant work", func(t *testing.T) {
		root := t.TempDir()
		srcDir := filepath.Join(root, "src")
		dstDir := filepath.Join(root, "dst")
		_ = os.MkdirAll(srcDir, 0o755)
		_ = os.WriteFile(filepath.Join(srcDir, "f.txt"), []byte("x"), 0o644)
		if err := copyPath(srcDir, dstDir); err != nil {
			t.Fatalf("copyPath failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dstDir, "f.txt")); err != nil {
			t.Fatalf("copied file missing: %v", err)
		}
		srcFile := filepath.Join(root, "m.txt")
		dstFile := filepath.Join(root, "m2.txt")
		_ = os.WriteFile(srcFile, []byte("m"), 0o644)
		if err := movePath(srcFile, dstFile); err != nil {
			t.Fatalf("movePath failed: %v", err)
		}
	})
}

func TestShellFromPasswd(t *testing.T) {
	data := strings.Join([]string{
		"root:x:0:0:root:/root:/bin/bash",
		"user:x:1000:1000:User:/home/user:/bin/zsh",
	}, "\n")
	if got := shellFromPasswd("1000", strings.NewReader(data)); got != "/bin/zsh" {
		t.Fatalf("expected /bin/zsh, got %q", got)
	}
	if got := shellFromPasswd("9999", strings.NewReader(data)); got != "" {
		t.Fatalf("expected empty shell, got %q", got)
	}
}

func TestPanelSortModesKeepParentFirst(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.go")
	c := filepath.Join(root, "c.go")
	_ = os.WriteFile(a, []byte("12345"), 0o644)
	_ = os.WriteFile(b, []byte("1"), 0o644)
	_ = os.WriteFile(c, []byte("2"), 0o644)
	_ = os.Chtimes(a, time.Unix(100, 0), time.Unix(100, 0))
	_ = os.Chtimes(b, time.Unix(200, 0), time.Unix(200, 0))
	_ = os.Chtimes(c, time.Unix(200, 0), time.Unix(200, 0))

	p := newPanel("Test")
	p.sortMode = sortByName
	if err := p.load(root, 0, false); err != nil {
		t.Fatal(err)
	}
	if len(p.items) < 3 || p.items[0].name != ".." {
		t.Fatalf("expected parent first for name sort")
	}

	p.sortMode = sortByExt
	if err := p.load(root, 0, false); err != nil {
		t.Fatal(err)
	}
	if p.items[0].name != ".." {
		t.Fatalf("expected parent first for ext sort")
	}
	if p.items[1].name != "b.go" || p.items[2].name != "c.go" {
		t.Fatalf("expected ext ties sorted by name")
	}

	p.sortMode = sortByTime
	if err := p.load(root, 0, false); err != nil {
		t.Fatal(err)
	}
	if p.items[0].name != ".." || p.items[1].name != "b.go" {
		t.Fatalf("expected newest first for time sort")
	}
	if p.items[1].name != "b.go" || p.items[2].name != "c.go" {
		t.Fatalf("expected time ties sorted by name")
	}

	p.sortMode = sortBySize
	if err := p.load(root, 0, false); err != nil {
		t.Fatal(err)
	}
	if p.items[0].name != ".." || p.items[1].name != "a.txt" {
		t.Fatalf("expected largest first for size sort")
	}
	if p.items[2].name != "b.go" || p.items[3].name != "c.go" {
		t.Fatalf("expected size ties sorted by name")
	}
}
