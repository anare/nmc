package editor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anare/nmc/internal/vfs"
)

func TestTypingRunIsOneUndoStepAndRedoRestoresIt(t *testing.T) {
	editor := FromBytes("notes.txt", []byte("ab"))
	editor.X = 1
	editor.InsertRune('X')
	editor.InsertRune('Y')
	if got := string(editor.Bytes()); got != "aXYb" {
		t.Fatalf("typed text = %q", got)
	}
	if !editor.Undo() || string(editor.Bytes()) != "ab" {
		t.Fatalf("one undo did not reverse the typing run: %q", editor.Bytes())
	}
	if !editor.Redo() || string(editor.Bytes()) != "aXYb" {
		t.Fatalf("redo did not restore typing: %q", editor.Bytes())
	}
}

func TestMultiLineReplacementUndoRestoresAllLines(t *testing.T) {
	editor := FromBytes("notes.txt", []byte("first\nsecond"))
	editor.X, editor.Y = 2, 0
	editor.Newline()
	if got := string(editor.Bytes()); got != "fi\nrst\nsecond" {
		t.Fatalf("split contents = %q", got)
	}
	if !editor.Undo() || string(editor.Bytes()) != "first\nsecond" {
		t.Fatalf("undo contents = %q", editor.Bytes())
	}
}

func TestBinaryModeRoundTripsEveryByteExactly(t *testing.T) {
	data := make([]byte, 256)
	for index := range data {
		data[index] = byte(index)
	}
	data = append(data, '\r', '\n', 0xff, 0xfe)
	fs := vfs.NewMemFS("test")
	fs.PutFile("/all.bin", data, 0o644)
	if _, err := Open(context.Background(), fs, "/all.bin"); err != ErrBinary {
		t.Fatalf("text open error = %v, want ErrBinary", err)
	}
	editor, err := OpenBinary(context.Background(), fs, "/all.bin")
	if err != nil {
		t.Fatal(err)
	}
	if got := editor.Bytes(); !bytes.Equal(got, data) {
		t.Fatalf("binary bytes changed: got %d bytes want %d", len(got), len(data))
	}
}

func TestSaveWritesAtomicallyAndKeepsTabs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Makefile")
	original := []byte("target:\n\techo ok\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	editor, err := Open(context.Background(), vfs.NewLocal(), path)
	if err != nil {
		t.Fatal(err)
	}
	editor.Y, editor.X = 1, len(editor.Lines[1])
	editor.InsertRune('!')
	if err := editor.Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "target:\n\techo ok!\n" || editor.Changed {
		t.Fatalf("saved contents = %q changed=%v", got, editor.Changed)
	}
}
