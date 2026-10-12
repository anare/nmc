package vfs

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalFilesystemListsParentAndSymlinkMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, "child")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "notes-link")
	if err := os.Symlink(file, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	fs := NewLocal()
	entries, err := fs.List(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || !entries[0].IsUp {
		t.Fatalf("expected parent entry followed by two files; got %#v", entries)
	}
	var gotLink Entry
	for _, entry := range entries {
		if entry.Name == "notes-link" {
			gotLink = entry
		}
	}
	if !gotLink.IsLink || gotLink.Broken || gotLink.IsDir || gotLink.Target != file {
		t.Fatalf("unexpected symlink metadata: %#v", gotLink)
	}
}

func TestMemFilesystemSupportsDirectoriesAndFiles(t *testing.T) {
	ctx := context.Background()
	fs := NewMemFS("test")
	fs.Put("/", []Entry{{Name: "docs", IsDir: true}})
	if err := fs.Mkdir(ctx, "/docs/new"); err != nil {
		t.Fatal(err)
	}

	writer, err := fs.Create(ctx, "/docs/new/readme.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "memory"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.List(ctx, "/docs/new")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Name != "readme.txt" || entries[1].Size != 6 {
		t.Fatalf("unexpected in-memory listing: %#v", entries)
	}
	reader, err := fs.Open(ctx, "/docs/new/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "memory" {
		t.Fatalf("unexpected file contents %q, error %v", data, err)
	}
}

func TestMemFilesystemFollowsSymlinksWhenOpeningFiles(t *testing.T) {
	ctx := context.Background()
	fs := NewMemFS("test")
	fs.PutFile("/target.txt", []byte("target"), 0o644)
	if err := fs.Symlink(ctx, "target.txt", "/link.txt"); err != nil {
		t.Fatal(err)
	}
	entry, err := fs.Stat(ctx, "/link.txt")
	if err != nil || !entry.IsLink || entry.Broken || entry.IsDir {
		t.Fatalf("unexpected symlink metadata: %#v, error: %v", entry, err)
	}
	reader, err := fs.Open(ctx, "/link.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "target" {
		t.Fatalf("symlink contents = %q, error = %v", data, err)
	}
}

func TestEntryClassificationPrefersFileKindAndBackupName(t *testing.T) {
	if got := (Entry{Name: "run.go", IsDir: true}).Class(); got != ClassDir {
		t.Fatalf("directory classified as %q", got)
	}
	if got := (Entry{Name: "run.go", Mode: 0o755}).Class(); got != ClassExec {
		t.Fatalf("executable classified as %q", got)
	}
	if got := (Entry{Name: "picture.jpg~"}).Class(); got != ClassTemp {
		t.Fatalf("backup classified as %q", got)
	}
}
