package find

import (
	"context"
	"reflect"
	"testing"

	"github.com/anare/nmc/internal/vfs"
)

func TestSearchStreamsLiteralWholeWordHitsAndSkipsHiddenFiles(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.PutFile("/src/a.go", []byte("cat scatter\ncatégorie cat\n"), 0o644)
	fs.PutFile("/src/b.go", []byte("CAT\n"), 0o644)
	fs.PutFile("/src/.secret.go", []byte("cat\n"), 0o644)
	fs.PutFile("/src/other.txt", []byte("cat\n"), 0o644)
	var streamed []Hit
	hits, err := Search(context.Background(), fs, Options{
		Root: "/src", NamePattern: "*.go", Contains: "cat", WholeWords: true,
	}, func(hit Hit) { streamed = append(streamed, hit) })
	if err != nil {
		t.Fatal(err)
	}
	want := []Hit{
		{Path: "a.go", Line: 1, Text: "cat scatter"},
		{Path: "a.go", Line: 2, Text: "catégorie cat"},
		{Path: "b.go", Line: 1, Text: "CAT"},
	}
	if !reflect.DeepEqual(hits, want) || !reflect.DeepEqual(streamed, want) {
		t.Fatalf("hits=%+v streamed=%+v want=%+v", hits, streamed, want)
	}
}

func TestSearchHonoursCaseAndHiddenOptions(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.PutFile("/root/.hidden/note.txt", []byte("Needle\n"), 0o644)
	fs.PutFile("/root/note.txt", []byte("needle\n"), 0o644)
	hits, err := Search(context.Background(), fs, Options{
		Root: "/root", NamePattern: "*.txt", Contains: "Needle", CaseSensitive: true,
	}, nil)
	if err != nil || len(hits) != 0 {
		t.Fatalf("case-sensitive default-hidden result=%+v err=%v", hits, err)
	}
	hits, err = Search(context.Background(), fs, Options{
		Root: "/root", NamePattern: "*.txt", Contains: "Needle", CaseSensitive: true, IncludeHidden: true,
	}, nil)
	if err != nil || len(hits) != 1 || hits[0].Path != ".hidden/note.txt" {
		t.Fatalf("include-hidden result=%+v err=%v", hits, err)
	}
}

func TestSearchRejectsInvalidGlobAndHonoursCancellation(t *testing.T) {
	fs := vfs.NewMemFS("test")
	if _, err := Search(context.Background(), fs, Options{Root: "/", NamePattern: "["}, nil); err == nil {
		t.Fatal("invalid glob was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hits, err := Search(ctx, fs, Options{Root: "/"}, nil)
	if err == nil || len(hits) != 0 {
		t.Fatalf("cancelled search hits=%v err=%v", hits, err)
	}
}
