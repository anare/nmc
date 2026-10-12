package archivefs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anare/nmc/internal/handlers"
	"github.com/anare/nmc/internal/vfs"
)

func TestParseListingInfersDirectoriesAndReadsSizes(t *testing.T) {
	entries, files, err := parseListing([]byte("12\tone/two/file.txt\nempty/\n./top.bin\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !entries["/one"].IsDir || !entries["/one/two"].IsDir {
		t.Fatalf("inferred directories missing: %#v", entries)
	}
	if entry := entries["/one/two/file.txt"]; entry.Size != 12 || entry.SizeUnknown || entry.IsDir {
		t.Fatalf("sized file entry = %#v", entry)
	}
	if entry := entries["/top.bin"]; !entry.SizeUnknown {
		t.Fatalf("size-less file entry = %#v", entry)
	}
	if !entries["/empty"].IsDir || !files["/top.bin"] || files["/empty"] {
		t.Fatalf("directory/file classification incorrect: entries=%#v files=%#v", entries, files)
	}
}

func TestParseListingRejectsUnsafeAndConflictingPaths(t *testing.T) {
	for _, listing := range []string{
		"/absolute\n",
		"../escape\n",
		"nested/../../escape\n",
		"nested\\escape\n",
		"same\nsame/child\n",
	} {
		if _, _, err := parseListing([]byte(listing)); err == nil {
			t.Errorf("parseListing(%q) accepted unsafe or conflicting path", listing)
		}
	}
}

func TestFilesystemListsOpensCachesAndCleansExtractedFiles(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bundle.tar")
	if err := os.WriteFile(archive, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := archive + ".calls"
	handler := handlers.Handler{
		Name: "test", Kind: handlers.Archive,
		List: `printf '5	nested/file.txt
empty/
'`,
		Extract: `printf x >> "$ARCHIVE.calls"; mkdir -p "$DEST/$(dirname "$ENTRY")"; printf hello > "$DEST/$ENTRY"`,
	}
	fs, err := Open(context.Background(), handler, archive)
	if err != nil {
		t.Fatal(err)
	}
	if fs.Name() != "archive:bundle.tar" {
		t.Fatalf("filesystem name = %q", fs.Name())
	}
	root, err := fs.List(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(root) != 3 || !root[0].IsUp {
		t.Fatalf("root listing = %#v, want parent and two entries", root)
	}
	nested, err := fs.List(context.Background(), "/nested")
	if err != nil || len(nested) != 2 || !nested[0].IsUp || nested[1].Name != "file.txt" {
		t.Fatalf("nested listing=%#v err=%v", nested, err)
	}
	entry, err := fs.Stat(context.Background(), "/nested/file.txt")
	if err != nil || entry.Size != 5 || entry.SizeUnknown {
		t.Fatalf("stat entry=%#v err=%v", entry, err)
	}
	for range 2 {
		reader, err := fs.Open(context.Background(), "/nested/file.txt")
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		closeErr := reader.Close()
		if err != nil || closeErr != nil || string(data) != "hello" {
			t.Fatalf("read=%q err=%v closeErr=%v", data, err, closeErr)
		}
	}
	count, err := os.ReadFile(calls)
	if err != nil || string(count) != "x" {
		t.Fatalf("extraction calls=%q err=%v; want one cached extraction", count, err)
	}
	scratch := fs.scratch
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch directory remains after Close: %v", err)
	}
	if _, err := fs.List(context.Background(), "/"); !errors.Is(err, ErrClosed) {
		t.Fatalf("List after Close error=%v", err)
	}
}

func TestFilesystemRejectsSymlinkExtractionAndMutations(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bundle.zip")
	if err := os.WriteFile(archive, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := handlers.Handler{
		Name: "test", Kind: handlers.Archive,
		List:    `printf 'secret.txt\n'`,
		Extract: `ln -s /etc/passwd "$DEST/$ENTRY"`,
	}
	fs, err := Open(context.Background(), handler, archive)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if _, err := fs.Open(context.Background(), "/secret.txt"); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink extraction error=%v", err)
	}
	if !errors.Is(fs.Mkdir(context.Background(), "/new"), ErrReadOnly) ||
		!errors.Is(fs.Remove(context.Background(), "/secret.txt"), ErrReadOnly) {
		t.Fatal("archive mutation was not rejected")
	}
}

func TestFilesystemRejectsNonCanonicalAndTraversalPaths(t *testing.T) {
	fs := &FS{entries: map[string]vfs.Entry{"/": {Name: "/", IsDir: true}}}
	for _, name := range []string{"relative", "/../outside", "/nested/../file", "//file", "/file/"} {
		if _, err := fs.Stat(context.Background(), name); err == nil {
			t.Errorf("Stat(%q) accepted noncanonical path", name)
		}
	}
}
