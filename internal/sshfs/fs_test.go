package sshfs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLocation(t *testing.T) {
	user, host, port, remotePath, err := ParseLocation("sftp://alice@example.test:2202/work/notes%20here")
	if err != nil {
		t.Fatal(err)
	}
	if user != "alice" || host != "example.test" || port != 2202 || remotePath != "/work/notes here" {
		t.Fatalf("parsed location = %q %q %d %q", user, host, port, remotePath)
	}
	user, host, port, remotePath, err = ParseLocation("sftp://example.test")
	if err != nil || user != "" || host != "example.test" || port != 22 || remotePath != "/" {
		t.Fatalf("default location = %q %q %d %q err=%v", user, host, port, remotePath, err)
	}
	for _, invalid := range []string{
		"https://example.test/path",
		"sftp:///path",
		"******example.test/path",
		"sftp://example.test:0/path",
		"sftp://example.test/path?query=1",
	} {
		if _, _, _, _, err := ParseLocation(invalid); err == nil {
			t.Errorf("accepted invalid location %q", invalid)
		}
	}
}

func TestDialUsesPrivateControlDirectoryAndReconnects(t *testing.T) {
	sshDir := t.TempDir()
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sshDir, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", sshDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	fs, output, err := Dial(context.Background(), "alice", "example.test", 2222)
	if err != nil {
		t.Fatalf("Dial output=%q err=%v", output, err)
	}
	info, err := os.Stat(fs.dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("control directory mode=%v", info.Mode().Perm())
	}
	if err := fs.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	dir := fs.dir
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control directory remained after close: %v", err)
	}
}

func TestListUsesCachedPlatformAndQuotedRemoteDirectory(t *testing.T) {
	sshDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "ssh.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$SSH_LOG"
last=
for arg do last=$arg; done
case "$last" in
  *"stat --version"*) printf gnu ;;
  *"cd --"*)
    printf 'L\tdirectory\t0\t1700000000\t41ed\t2\tfolder\n'
    printf 'L\tsymbolic link\t8\t1700000001\ta1ff\t1\tshortcut\n'
    printf 'R\tshortcut\tpath with spaces\n'
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(sshDir, "ssh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", sshDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SSH_LOG", logPath)
	fs := &FS{name: "ssh:example.test", host: "example.test", socket: "/tmp/nmc-test-socket", timeout: defaultTimeout}
	entries, err := fs.List(context.Background(), "/remote dir/'quoted'")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || !entries[0].IsUp || entries[1].Name != "folder" ||
		!entries[2].IsLink || entries[2].Target != "path with spaces" {
		t.Fatalf("remote entries = %+v", entries)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	if strings.Count(log, "stat --version") != 1 {
		t.Fatalf("platform probe was not cached: %q", log)
	}
	if !strings.Contains(log, "cd -- "+shellQuote("/remote dir/'quoted'")+" ||") {
		t.Fatalf("remote path was not safely quoted: %q", log)
	}
}

func TestShellQuoteAndRemoteFSIdentity(t *testing.T) {
	const name = `a $(touch /tmp/nope) 'b`
	if got, want := shellQuote(name), `'a $(touch /tmp/nope) '\''b'`; got != want {
		t.Fatalf("shellQuote(%q) = %q, want %q", name, got, want)
	}
	fs := &FS{name: "ssh:host"}
	if fs.Name() == "" {
		t.Fatal("remote filesystem was identified as local")
	}
	if got := fs.Join("/root", "file"); got != "/root/file" {
		t.Fatalf("remote Join = %q", got)
	}
}

func TestRemoteOperationsQuotePathsAndPreserveRemoteErrors(t *testing.T) {
	sshDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "ssh.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$SSH_LOG"
last=
for arg do last=$arg; done
case "$last" in
  "cat -- '/remote/file'") printf payload ;;
  *"umask 022; cat >"*) cat >/dev/null ;;
  "mkdir -- "*) ;;
  "mv -- "*) ;;
  "ln -s -- "*) ;;
  "permission-denied") printf 'Permission denied by remote command\n' >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(sshDir, "ssh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", sshDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SSH_LOG", logPath)
	fs := &FS{name: "ssh:example.test", host: "example.test", socket: "/tmp/nmc-test-socket", timeout: defaultTimeout}
	reader, err := fs.Open(context.Background(), "/remote/file")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "payload" {
		t.Fatalf("Open returned %q err=%v", data, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	const hostile = `dir ' $(touch /tmp/sshfs-pwned)`
	if err := fs.Mkdir(context.Background(), hostile); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rename(context.Background(), hostile, "/to"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Symlink(context.Background(), hostile, "/link"); err != nil {
		t.Fatal(err)
	}
	writer, err := fs.Create(context.Background(), hostile, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = fs.run(context.Background(), "permission-denied")
	if err == nil || errors.Is(err, ErrConnectionLost) || !strings.Contains(err.Error(), "Permission denied by remote command") {
		t.Fatalf("remote command error = %v", err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "mkdir -- "+shellQuote(hostile)) ||
		!strings.Contains(string(log), "mv -- "+shellQuote(hostile)+" '/to'") ||
		!strings.Contains(string(log), "chmod 600 -- "+shellQuote(hostile)) {
		t.Fatalf("remote path arguments were not quoted: %q", log)
	}
	if _, err := os.Stat("/tmp/sshfs-pwned"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hostile remote name executed locally: %v", err)
	}
}
