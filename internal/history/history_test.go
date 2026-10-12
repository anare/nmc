package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryIsSeparatedByKindAndSavedAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "bc", "history")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Add(Command, "ls -la")
	store.Add(Command, "pwd")
	store.Add(Command, "ls -la")
	store.Add(Search, "*.go")
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("history file permissions = %o, want 0600", info.Mode().Perm())
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	command := loaded.Cursor(Command)
	if got := command.Prev(""); got != "ls -la" {
		t.Fatalf("newest command = %q, want ls -la", got)
	}
	if got := command.Prev("ignored"); got != "pwd" {
		t.Fatalf("previous command = %q, want pwd", got)
	}
	if got := command.Next(""); got != "ls -la" {
		t.Fatalf("next command = %q, want ls -la", got)
	}
	if got := command.Next(""); got != "" {
		t.Fatalf("next after newest = %q, want empty draft", got)
	}
	if got := loaded.Cursor(Search).Prev(""); got != "*.go" {
		t.Fatalf("search history = %q, want *.go", got)
	}
}

func TestHistoryCursorRestoresTheCurrentDraft(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "history"))
	if err != nil {
		t.Fatal(err)
	}
	store.Add(Command, "ls")
	cursor := store.Cursor(Command)
	if got := cursor.Prev("draft"); got != "ls" {
		t.Fatalf("previous history = %q, want ls", got)
	}
	if got := cursor.Next("ls"); got != "draft" {
		t.Fatalf("restored input = %q, want draft", got)
	}
	cursor.Reset()
	if got := cursor.Next("still here"); got != "still here" {
		t.Fatalf("next on reset cursor = %q, want original input", got)
	}
}
