package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptyRepositoryHasEmptyHistory(t *testing.T) {
	dir := initRepo(t)
	commits, err := Log(context.Background(), dir, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 0 {
		t.Fatalf("empty repository history = %+v", commits)
	}
}

func TestGitReadWriteWorkflow(t *testing.T) {
	ctx := context.Background()
	dir := initRepo(t)
	path := filepath.Join(dir, "note with spaces.txt")
	if err := os.WriteFile(path, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := CurrentStatus(ctx, dir)
	if err != nil || len(status.Untracked) != 1 || status.Untracked[0] != "note with spaces.txt" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	hash, err := CommitChanges(ctx, dir, "initial import", true)
	if err != nil || len(hash) < 7 {
		t.Fatalf("commit hash=%q err=%v", hash, err)
	}
	if branch := Describe(ctx, dir); branch != "master" && branch != "main" {
		t.Fatalf("branch description = %q", branch)
	}
	commits, err := Log(ctx, dir, path, 20)
	if err != nil || len(commits) != 1 || commits[0].Subject != "initial import" {
		t.Fatalf("log=%+v err=%v", commits, err)
	}
	diff, err := Show(ctx, dir, hash, path)
	if err != nil || !strings.Contains(string(diff), "first") {
		t.Fatalf("show diff=%q err=%v", diff, err)
	}
	if err := os.WriteFile(path, []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err = CurrentStatus(ctx, dir)
	if err != nil || len(status.Modified) != 1 {
		t.Fatalf("modified status=%+v err=%v", status, err)
	}
	branches, err := Branches(ctx, dir)
	if err != nil || len(branches) != 1 {
		t.Fatalf("branches=%+v err=%v", branches, err)
	}
	runGit(t, dir, "branch", "feature")
	if err := Switch(ctx, dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if got := Describe(ctx, dir); got != "feature" {
		t.Fatalf("after switch branch=%q", got)
	}
}

func TestRootOutsideRepositoryReportsSentinel(t *testing.T) {
	if _, err := Root(context.Background(), t.TempDir()); err != ErrNotARepo {
		t.Fatalf("Root error=%v, want ErrNotARepo", err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.invalid")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
