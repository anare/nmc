package clipboard

import (
	"bytes"
	"os"
	"testing"
)

func TestClipboardPreservesBytesReplacesPreviousCopyAndCleansUp(t *testing.T) {
	clip := New()
	defer clip.Close()
	data := make([]byte, 256)
	for index := range data {
		data[index] = byte(index)
	}
	path, err := clip.Copy(data)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("clipboard mode=%v", info.Mode().Perm())
	}
	got, err := clip.Paste()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("clipboard data changed: err=%v", err)
	}
	if _, err := clip.Copy([]byte("replacement")); err != nil {
		t.Fatal(err)
	}
	if got, err := clip.Paste(); err != nil || string(got) != "replacement" {
		t.Fatalf("replacement contents=%q err=%v", got, err)
	}
	if err := clip.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("clipboard file remains after Close: %v", err)
	}
}

func TestEmptyClipboardPastesNothing(t *testing.T) {
	clip := New()
	defer clip.Close()
	data, err := clip.Paste()
	if err != nil || len(data) != 0 {
		t.Fatalf("empty paste=%v err=%v", data, err)
	}
}
