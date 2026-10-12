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

func TestSelectionCopyDeleteAndPasteAreSingleUndoSteps(t *testing.T) {
	editor := FromBytes("notes.txt", []byte("one\ntwo\nthree"))
	editor.X, editor.Y = 1, 0
	editor.StartSelection()
	editor.X, editor.Y = 2, 1
	if got, ok := editor.Selection(); !ok || string(got) != "ne\ntw" {
		t.Fatalf("selection=%q ok=%v", got, ok)
	}
	if !editor.DeleteSelection() || string(editor.Bytes()) != "oo\nthree" {
		t.Fatalf("delete selection contents=%q", editor.Bytes())
	}
	if !editor.Undo() || string(editor.Bytes()) != "one\ntwo\nthree" {
		t.Fatalf("undo selection delete contents=%q", editor.Bytes())
	}
	editor.X, editor.Y = 3, 0
	editor.InsertBytes([]byte("A\r\nB"))
	if got := string(editor.Bytes()); got != "oneA\nB\ntwo\nthree" {
		t.Fatalf("pasted text=%q", got)
	}
	if !editor.Undo() || string(editor.Bytes()) != "one\ntwo\nthree" {
		t.Fatalf("undo paste contents=%q", editor.Bytes())
	}
}

func TestBinarySelectionAndPastePreserveAllByteValues(t *testing.T) {
	data := []byte{0, 1, 0xff, '\n', 2, 3}
	fs := vfs.NewMemFS("test")
	fs.PutFile("/raw.bin", data, 0o644)
	editor, err := OpenBinary(context.Background(), fs, "/raw.bin")
	if err != nil {
		t.Fatal(err)
	}
	editor.StartSelection()
	editor.X, editor.Y = 2, 0
	selected, ok := editor.Selection()
	if !ok || !bytes.Equal(selected, []byte{0, 1}) {
		t.Fatalf("binary selection=%v ok=%v", selected, ok)
	}
	editor.InsertBytes([]byte{0xfe, '\n', 0xfd})
	if got, want := editor.Bytes(), []byte{0xfe, '\n', 0xfd, 0xff, '\n', 2, 3}; !bytes.Equal(got, want) {
		t.Fatalf("binary paste=%v want=%v", got, want)
	}
}
