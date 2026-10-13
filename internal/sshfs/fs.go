package sshfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anare/nmc/internal/vfs"
)

var ErrConnectionLost = errors.New("SSH connection lost")

const (
	defaultTimeout = 30 * time.Second
)

type FS struct {
	name    string
	host    string
	user    string
	port    int
	socket  string
	dir     string
	timeout time.Duration

	mu       sync.Mutex
	platform string
	closed   bool
	cleaned  bool
}

func (fs *FS) checkOpen() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.closed {
		return ErrConnectionLost
	}
	return nil
}

func classifyCommandError(output []byte, err error) error {
	detail := tidySSHError(string(output), err)
	lower := strings.ToLower(detail)
	if errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(lower, "control socket connect") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection timed out") ||
		strings.Contains(lower, "no route to host") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "could not resolve hostname") ||
		strings.Contains(lower, "connection closed") {
		return fmt.Errorf("%w: %s", ErrConnectionLost, detail)
	}
	return errors.New(detail)
}

func Dial(ctx context.Context, user, host string, port int) (*FS, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.HasPrefix(host, "-") {
		return nil, "", errors.New("SSH host is empty or invalid")
	}
	if port < 0 || port > 65535 {
		return nil, "", fmt.Errorf("invalid SSH port %d", port)
	}
	if port == 0 {
		port = 22
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dir, err := os.MkdirTemp(cacheDir, "nmc-ssh-")
	if err != nil {
		return nil, "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, "", err
	}
	fs := &FS{
		name: fmt.Sprintf("ssh:%s", host), host: host, user: user, port: port,
		socket: dir + "/s", dir: dir, timeout: timeoutFromEnvironment(),
	}
	output, err := fs.startMaster(ctx)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, tidySSHError(string(output), err), err
	}
	return fs, tidySSHError(string(output), nil), nil
}

func ParseLocation(location string) (user, host string, port int, remotePath string, err error) {
	parsed, err := url.Parse(location)
	if err != nil {
		return "", "", 0, "", err
	}
	if !strings.EqualFold(parsed.Scheme, "sftp") || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", 0, "", errors.New("expected sftp://[user@]host[:port]/path")
	}
	host = parsed.Hostname()
	if parsed.User != nil {
		user = parsed.User.Username()
		if _, present := parsed.User.Password(); present || user == "" {
			return "", "", 0, "", errors.New("SSH URL must not contain a password and must include a username")
		}
	}
	port = 22
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", "", 0, "", errors.New("invalid SSH port")
		}
	}
	remotePath = parsed.Path
	if remotePath == "" {
		remotePath = "/"
	}
	remotePath = path.Clean(remotePath)
	return user, host, port, remotePath, nil
}

func timeoutFromEnvironment() time.Duration {
	if seconds, err := strconv.Atoi(os.Getenv("BC_SSH_TIMEOUT")); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return defaultTimeout
}

func (fs *FS) Name() string { return fs.name }

func (fs *FS) startMaster(ctx context.Context) ([]byte, error) {
	args := []string{"-M", "-N", "-f", "-o", "ControlMaster=yes", "-o", "ControlPersist=60", "-S", fs.socket}
	if fs.port > 0 && fs.port != 22 {
		args = append(args, "-p", strconv.Itoa(fs.port))
	}
	if fs.user != "" {
		args = append(args, "-l", fs.user)
	}
	args = append(args, "--", fs.host)
	command := exec.CommandContext(ctx, "ssh", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s: %w", ErrConnectionLost, commandError(output, err))
	}
	return output, nil
}

func (fs *FS) commandArgs() []string {
	args := []string{"-S", fs.socket, "-o", "ControlMaster=no", "-o", "BatchMode=yes"}
	if fs.port > 0 && fs.port != 22 {
		args = append(args, "-p", strconv.Itoa(fs.port))
	}
	if fs.user != "" {
		args = append(args, "-l", fs.user)
	}
	return append(args, "--", fs.host)
}

func (fs *FS) run(ctx context.Context, remote string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, fs.timeout)
	defer cancel()
	fs.mu.Lock()
	closed := fs.closed
	fs.mu.Unlock()
	if closed {
		return nil, ErrConnectionLost
	}
	args := append(fs.commandArgs(), remote)
	command := exec.CommandContext(ctx, "ssh", args...)
	var output, stderr bytes.Buffer
	command.Stdout, command.Stderr = &output, &stderr
	err := command.Run()
	if err != nil {
		if ctx.Err() != nil {
			return output.Bytes(), fmt.Errorf("%w: %v", ErrConnectionLost, ctx.Err())
		}
		return output.Bytes(), classifyCommandError(stderr.Bytes(), err)
	}
	return output.Bytes(), nil
}

func (fs *FS) List(ctx context.Context, dir string) ([]vfs.Entry, error) {
	platform, err := fs.remotePlatform(ctx)
	if err != nil {
		return nil, err
	}
	quoted := shellQuote(dir)
	var command, separator string
	switch platform {
	case "gnu":
		command = "stat --printf 'L\\t%F\\t%s\\t%Y\\t%f\\t%h\\t%n\\n' -- "
		separator = "* .*"
	case "bsd":
		command = "stat -f 'L\\t%HT\\t%z\\t%m\\t%Xp\\t%l\\t%N\\n' -- "
		separator = "* .*"
	default:
		command = "LC_ALL=C ls -lAn --time-style=+%s -- "
		separator = "."
	}
	var listing string
	if platform == "ls" {
		listing = "LC_ALL=C ls -lAn --time-style=+%s"
	} else {
		listing = "for f in " + separator + "; do case \"$f\" in .|..) continue;; esac; [ -e \"$f\" ] || [ -L \"$f\" ] || continue; " +
			command + "\"$f\" 2>/dev/null; done"
	}
	remote := "cd -- " + quoted + " || exit 1; " + listing + "; " +
		"listing_status=$?; [ \"$listing_status\" -eq 0 ] || exit \"$listing_status\"; " +
		"for f in * .*; do case \"$f\" in .|..) continue;; esac; if [ -L \"$f\" ]; then printf 'R\\t%s\\t%s\\n' \"$f\" \"$(readlink -- \"$f\")\"; [ -e \"$f\" ] || printf 'B\\t%s\\n' \"$f\"; [ -d \"$f\" ] && printf 'D\\t%s\\n' \"$f\"; fi; done 2>/dev/null; exit 0"
	output, err := fs.run(ctx, remote)
	if err != nil {
		return nil, err
	}
	var entries []vfs.Entry
	var listingLines, linkLines []string
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		if strings.HasPrefix(line, "R\t") || strings.HasPrefix(line, "B\t") || strings.HasPrefix(line, "D\t") {
			linkLines = append(linkLines, line)
		} else if line != "" && (platform != "ls" || strings.HasPrefix(line, "total ") || strings.ContainsRune("-dlbcps", rune(line[0]))) {
			listingLines = append(listingLines, line)
		}
	}
	listingOutput := strings.Join(listingLines, "\n")
	switch platform {
	case "gnu":
		entries, err = ParseGNUListing(listingOutput)
	case "bsd":
		entries, err = ParseBSDListing(listingOutput)
	default:
		entries, err = ParseLSListing(listingOutput)
	}
	if err != nil {
		return nil, err
	}
	if err := ParseSymlinkRecords(strings.Join(linkLines, "\n"), entries); err != nil {
		return nil, err
	}
	if parent := path.Dir(path.Clean(dir)); parent != path.Clean(dir) {
		entries = append([]vfs.Entry{{Name: "..", IsDir: true, IsUp: true, Mode: os.ModeDir}}, entries...)
	}
	return entries, nil
}

func (fs *FS) remotePlatform(ctx context.Context) (string, error) {
	fs.mu.Lock()
	if fs.platform != "" {
		platform := fs.platform
		fs.mu.Unlock()
		return platform, nil
	}
	fs.mu.Unlock()
	output, err := fs.run(ctx, "stat --version >/dev/null 2>&1 && printf gnu || (stat -f %z . >/dev/null 2>&1 && printf bsd || printf ls)")
	if err != nil {
		return "", err
	}
	platform := strings.TrimSpace(string(output))
	switch platform {
	case "gnu", "bsd", "ls":
	default:
		return "", fmt.Errorf("unknown remote stat platform %q", platform)
	}
	fs.mu.Lock()
	if fs.platform == "" {
		fs.platform = platform
	}
	platform = fs.platform
	fs.mu.Unlock()
	return platform, nil
}

func (fs *FS) Stat(ctx context.Context, name string) (vfs.Entry, error) {
	platform, err := fs.remotePlatform(ctx)
	if err != nil {
		return vfs.Entry{}, err
	}
	quoted := shellQuote(name)
	var command string
	switch platform {
	case "gnu":
		command = "stat --printf 'L\\t%F\\t%s\\t%Y\\t%f\\t%h\\t%n\\n' -- " + quoted
	case "bsd":
		command = "stat -f 'L\\t%HT\\t%z\\t%m\\t%Xp\\t%l\\t%N\\n' -- " + quoted
	default:
		command = "LC_ALL=C ls -ldn --time-style=+%s -- " + quoted
	}
	output, err := fs.run(ctx, command)
	if err != nil {
		return vfs.Entry{}, err
	}
	var entries []vfs.Entry
	if platform == "gnu" {
		entries, err = ParseGNUListing(string(output))
	} else if platform == "bsd" {
		entries, err = ParseBSDListing(string(output))
	} else {
		entries, err = ParseLSListing(string(output))
	}
	if err != nil {
		return vfs.Entry{}, err
	}
	if len(entries) != 1 {
		return vfs.Entry{}, os.ErrNotExist
	}
	entry := entries[0]
	entry.Name = path.Base(name)
	return entry, nil
}

func (fs *FS) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := fs.checkOpen(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, fs.timeout)
	args := append(fs.commandArgs(), "cat -- "+shellQuote(name))
	command := exec.CommandContext(ctx, "ssh", args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		cancel()
		return nil, err
	}
	return &commandReader{ReadCloser: stdout, command: command, cancel: cancel, ctx: ctx, stderr: &stderr}, nil
}

type commandReader struct {
	io.ReadCloser
	command    *exec.Cmd
	cancel     context.CancelFunc
	ctx        context.Context
	stderr     *strings.Builder
	waitErr    error
	contextErr error
	waited     bool
}

func (reader *commandReader) Read(data []byte) (int, error) {
	count, err := reader.ReadCloser.Read(data)
	if errors.Is(err, io.EOF) {
		reader.waitErr = reader.wait()
		if reader.waitErr != nil {
			if reader.contextErr != nil {
				return count, fmt.Errorf("%w: %v", ErrConnectionLost, reader.contextErr)
			}
			return count, classifyCommandError([]byte(reader.stderr.String()), reader.waitErr)
		}
	}
	return count, err
}

func (reader *commandReader) wait() error {
	if !reader.waited {
		reader.waited = true
		reader.waitErr = reader.command.Wait()
		reader.contextErr = reader.ctx.Err()
		reader.cancel()
	}
	return reader.waitErr
}

func (reader *commandReader) Close() error {
	_ = reader.ReadCloser.Close()
	if reader.waited {
		return reader.waitErr
	}
	reader.cancel()
	return reader.command.Wait()
}

func (fs *FS) Create(ctx context.Context, name string, mode os.FileMode) (io.WriteCloser, error) {
	if err := fs.checkOpen(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, fs.timeout)
	if mode == 0 {
		mode = 0o644
	}
	modeText := strconv.FormatUint(uint64(mode.Perm()), 8)
	args := append(fs.commandArgs(), "umask 022; cat > "+shellQuote(name)+" && chmod "+modeText+" -- "+shellQuote(name))
	command := exec.CommandContext(ctx, "ssh", args...)
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		cancel()
		return nil, err
	}
	return &commandWriter{WriteCloser: stdin, command: command, cancel: cancel, ctx: ctx, stderr: &stderr}, nil
}

type commandWriter struct {
	io.WriteCloser
	command *exec.Cmd
	cancel  context.CancelFunc
	ctx     context.Context
	stderr  *strings.Builder
	closed  bool
}

func (writer *commandWriter) Close() error {
	if writer.closed {
		return nil
	}
	writer.closed = true
	if err := writer.WriteCloser.Close(); err != nil {
		writer.cancel()
		_ = writer.command.Wait()
		return err
	}
	err := writer.command.Wait()
	contextErr := writer.ctx.Err()
	writer.cancel()
	if err != nil {
		if contextErr != nil {
			return fmt.Errorf("%w: %v", ErrConnectionLost, contextErr)
		}
		return classifyCommandError([]byte(writer.stderr.String()), err)
	}
	return nil
}

func (fs *FS) Mkdir(ctx context.Context, name string) error {
	_, err := fs.run(ctx, "mkdir -- "+shellQuote(name))
	return err
}

func (fs *FS) Remove(ctx context.Context, name string) error {
	_, err := fs.run(ctx, "rm -rf -- "+shellQuote(name))
	return err
}

func (fs *FS) Rename(ctx context.Context, from, to string) error {
	_, err := fs.run(ctx, "mv -- "+shellQuote(from)+" "+shellQuote(to))
	return err
}

func (fs *FS) Symlink(ctx context.Context, target, link string) error {
	_, err := fs.run(ctx, "ln -s -- "+shellQuote(target)+" "+shellQuote(link))
	return err
}

func (fs *FS) Readlink(ctx context.Context, name string) (string, error) {
	output, err := fs.run(ctx, "readlink -- "+shellQuote(name))
	return strings.TrimSuffix(string(output), "\n"), err
}

func (fs *FS) Link(ctx context.Context, target, link string) error {
	_, err := fs.run(ctx, "ln -- "+shellQuote(target)+" "+shellQuote(link))
	return err
}

func (fs *FS) Join(parts ...string) string { return path.Join(parts...) }
func (fs *FS) Dir(name string) string      { return path.Dir(name) }
func (fs *FS) IsAbs(name string) bool      { return path.IsAbs(name) }

func (fs *FS) Reconnect(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	fs.mu.Lock()
	if fs.cleaned {
		fs.mu.Unlock()
		return errors.New("SSH filesystem is closed")
	}
	fs.closed = true
	fs.mu.Unlock()
	if err := fs.stopMaster(ctx); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(fs.socket)
	}
	fs.mu.Lock()
	fs.closed = false
	fs.platform = ""
	fs.mu.Unlock()
	output, err := fs.startMaster(ctx)
	if err != nil {
		fs.mu.Lock()
		fs.closed = true
		fs.mu.Unlock()
		return fmt.Errorf("%s: %w", tidySSHError(string(output), err), err)
	}
	return nil
}

func (fs *FS) Close() error {
	fs.mu.Lock()
	if fs.cleaned {
		fs.mu.Unlock()
		return nil
	}
	fs.closed = true
	fs.cleaned = true
	fs.mu.Unlock()
	err := fs.stopMaster(context.Background())
	removeErr := os.RemoveAll(fs.dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(err, removeErr)
	}
	return removeErr
}

func (fs *FS) stopMaster(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	args := []string{"-S", fs.socket, "-O", "exit"}
	if fs.port > 0 && fs.port != 22 {
		args = append(args, "-p", strconv.Itoa(fs.port))
	}
	if fs.user != "" {
		args = append(args, "-l", fs.user)
	}
	args = append(args, "--", fs.host)
	command := exec.CommandContext(ctx, "ssh", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return commandError(output, err)
	}
	return os.Remove(fs.socket)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func commandError(output []byte, err error) error {
	message := tidySSHError(string(output), err)
	return errors.New(message)
}

func tidySSHError(output string, err error) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var useful string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Warning: Permanently added ") || strings.HasPrefix(line, "debug1:") {
			continue
		}
		useful = line
	}
	if useful != "" {
		return useful
	}
	if err != nil {
		return err.Error()
	}
	return ""
}
