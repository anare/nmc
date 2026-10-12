package fileop

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anare/nmc/internal/vfs"
)

func TestPlanRejectsCopyingDirectoryIntoItself(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Plan([]string{source}, filepath.Join(source, "child")); err == nil {
		t.Fatal("Plan allowed copying a directory into its own subtree")
	}
}

func TestPlanUsesTheDestinationFilesystemToDetectDirectories(t *testing.T) {
	ctx := context.Background()
	sourceFS, destinationFS := vfs.NewMemFS("source"), vfs.NewMemFS("destination")
	sourceFS.Put("/", []vfs.Entry{{Name: "data.txt"}})
	destinationFS.Put("/", []vfs.Entry{{Name: "target", IsDir: true}})
	pairs, err := PlanFS(ctx, sourceFS, []string{"/data.txt"}, destinationFS, "/target")
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0] != (Pair{Src: "/data.txt", Dst: "/target/data.txt"}) {
		t.Fatalf("unexpected plan: %#v", pairs)
	}
}

func TestRunCopiesAcrossFilesystemsAndClampsProgress(t *testing.T) {
	ctx := context.Background()
	sourceFS, destinationFS := vfs.NewMemFS("source"), vfs.NewMemFS("destination")
	sourceFS.Put("/", []vfs.Entry{{Name: "docs", IsDir: true}})
	sourceFS.PutFile("/docs/a.txt", []byte("hello"), 0o640)
	destinationFS.Put("/", []vfs.Entry{{Name: "out", IsDir: true}})
	pairs, err := PlanFS(ctx, sourceFS, []string{"/docs/a.txt"}, destinationFS, "/out")
	if err != nil {
		t.Fatal(err)
	}

	var observed []int
	result := Run(ctx, Operation{Op: "Copy", Pairs: pairs, SrcFS: sourceFS, DstFS: destinationFS}, func(p Progress) {
		observed = append(observed, p.Percent())
	})
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	reader, err := destinationFS.Open(ctx, "/out/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "hello" {
		t.Fatalf("copied file contents = %q, error = %v", content, err)
	}
	info, err := destinationFS.Stat(ctx, "/out/a.txt")
	if err != nil || info.Size != 5 {
		t.Fatalf("copied file metadata = %#v, error = %v", info, err)
	}
	if len(observed) == 0 || observed[len(observed)-1] != 100 {
		t.Fatalf("progress did not finish at 100%%: %v", observed)
	}
	if (Progress{Done: 200, Total: 100}).Percent() != 100 || (Progress{Items: 200, ItemTotal: 100}).Percent() != 100 {
		t.Fatal("progress percent was not clamped")
	}
}

func TestRunContinuesAfterOnePairFails(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), Operation{
		Op: "Copy",
		Pairs: []Pair{
			{Src: filepath.Join(root, "missing.txt"), Dst: filepath.Join(destination, "missing.txt")},
			{Src: source, Dst: filepath.Join(destination, "source.txt")},
		},
	}, nil)
	if result.Items != 1 || result.Err() == nil {
		t.Fatalf("unexpected batch result: %#v", result)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "source.txt")); err != nil || string(data) != "kept" {
		t.Fatalf("successful pair was not completed: %q, %v", data, err)
	}
}

func TestLocalCopyPreservesModeAndModificationTime(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	destination := filepath.Join(root, "copy.txt")
	modified := time.Unix(1_700_000_000, 0)
	if err := os.WriteFile(source, []byte("data"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, modified, modified); err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), Operation{Op: "Copy", Pairs: []Pair{{Src: source, Dst: destination}}}, nil)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 || !info.ModTime().Equal(modified) {
		t.Fatalf("copied mode/time = %v/%v, want 0640/%v", info.Mode().Perm(), info.ModTime(), modified)
	}
}
