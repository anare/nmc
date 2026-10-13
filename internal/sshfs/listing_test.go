package sshfs

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/anare/nmc/internal/vfs"
)

func TestParseGNUListing(t *testing.T) {
	entries, err := ParseGNUListing(
		"L\tdirectory\t0\t1700000000\t41ed\t2\tfolder\n" +
			"L\tregular file\t12\t1700000001\t81a4\t1\tnote file.txt\n" +
			"L\tsymbolic link\t9\t1700000002\ta1ff\t1\tshortcut\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []vfs.Entry{
		{Name: "folder", IsDir: true, Size: 0, ModTime: time.Unix(1700000000, 0), Mode: os.ModeDir | 0o755, Links: 2},
		{Name: "note file.txt", Size: 12, ModTime: time.Unix(1700000001, 0), Mode: 0o644, Links: 1},
		{Name: "shortcut", IsLink: true, Size: 9, ModTime: time.Unix(1700000002, 0), Mode: os.ModeSymlink | 0o777, Links: 1},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries=%+v want=%+v", entries, want)
	}
}

func TestParseBSDListingAndSymlinkRecords(t *testing.T) {
	entries, err := ParseBSDListing(
		"L\tDirectory\t0\t1700000000\t040755\t2\tfolder\n" +
			"L\tRegular File\t5\t1700000001\t100644\t1\tfile.txt\n" +
			"L\tSymbolic Link\t4\t1700000002\t120777\t1\tlink\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !entries[0].IsDir || entries[0].Mode.Perm() != 0o755 || entries[1].Mode.Perm() != 0o644 || !entries[2].IsLink {
		t.Fatalf("BSD listing parsed incorrectly: %+v", entries)
	}
	if err := ParseSymlinkRecords("R\tlink\tpath with spaces\nD\tlink\n", entries); err != nil {
		t.Fatal(err)
	}
	if entries[2].Target != "path with spaces" || !entries[2].IsDir {
		t.Fatalf("link metadata = %+v", entries[2])
	}
}

func TestParseBrokenSymlinkRecord(t *testing.T) {
	entries := []vfs.Entry{{Name: "dangling", IsLink: true}}
	if err := ParseSymlinkRecords("R\tdangling\tmissing\nB\tdangling\n", entries); err != nil {
		t.Fatal(err)
	}
	if !entries[0].Broken || entries[0].Target != "missing" {
		t.Fatalf("broken link metadata = %+v", entries[0])
	}
}

func TestParseLSListingPreservesNamesAndSymlinkTargets(t *testing.T) {
	entries, err := ParseLSListing(
		"drwxr-xr-x 2 1000 1000 4096 1700000000 directory name\n" +
			"lrwxrwxrwx 1 1000 1000 4 1700000001 shortcut name -> target with spaces\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].IsDir || entries[0].Name != "directory name" {
		t.Fatalf("directory entry = %+v", entries)
	}
	if !entries[1].IsLink || entries[1].Name != "shortcut name" || entries[1].Target != "target with spaces" {
		t.Fatalf("link entry = %+v", entries[1])
	}
}

func TestListingParsersRejectMalformedRecords(t *testing.T) {
	if _, err := ParseGNUListing("not a stat record"); err == nil {
		t.Fatal("malformed GNU listing was accepted")
	}
	if _, err := ParseBSDListing("L\tDirectory\tbad\t0\t040755\t1\tfolder"); err == nil {
		t.Fatal("malformed BSD listing was accepted")
	}
	if _, err := ParseLSListing("-rw-r--r-- 1 1000 1000 nope 1700000000 file"); err == nil {
		t.Fatal("malformed ls listing was accepted")
	}
}
