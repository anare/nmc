package archivefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/anare/nmc/internal/handlers"
	"github.com/anare/nmc/internal/vfs"
)

var ErrReadOnly = errors.New("archive filesystem is read-only")
var ErrClosed = errors.New("archive filesystem is closed")

type FS struct {
	mu        sync.Mutex
	name      string
	archive   string
	handler   handlers.Handler
	scratch   string
	entries   map[string]vfs.Entry
	files     map[string]bool
	extracted map[string]string
	closed    bool
}

func Open(ctx context.Context, handler handlers.Handler, archive string) (*FS, error) {
	if handler.Kind != handlers.Archive || handler.List == "" || handler.Extract == "" {
		return nil, errors.New("incomplete archive handler")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	archive, err := filepath.Abs(archive)
	if err != nil {
		return nil, err
	}
	output, err := handlers.Run(ctx, handler.List, map[string]string{"ARCHIVE": archive})
	if err != nil {
		return nil, fmt.Errorf("list archive: %w", err)
	}
	entries, files, err := parseListing(output)
	if err != nil {
		return nil, err
	}
	scratch, err := os.MkdirTemp("", "nmc-archive-")
	if err != nil {
		return nil, err
	}
	return &FS{
		name:      "archive:" + filepath.Base(archive),
		archive:   archive,
		handler:   handler,
		scratch:   scratch,
		entries:   entries,
		files:     files,
		extracted: make(map[string]string),
	}, nil
}

func parseListing(output []byte) (map[string]vfs.Entry, map[string]bool, error) {
	entries := map[string]vfs.Entry{"/": {Name: "/", IsDir: true, Mode: os.ModeDir | 0o555}}
	files := make(map[string]bool)
	for lineNumber, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		var size int64
		knownSize := false
		if before, after, ok := strings.Cut(line, "\t"); ok && before != "" {
			if parsed, err := strconv.ParseInt(before, 10, 64); err == nil && parsed >= 0 {
				size, knownSize, line = parsed, true, after
			}
		}
		name := line
		if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, `\`) || path.IsAbs(name) || filepath.VolumeName(name) != "" {
			return nil, nil, fmt.Errorf("unsafe archive path on listing line %d", lineNumber+1)
		}
		isDir := strings.HasSuffix(name, "/")
		name = strings.TrimSuffix(name, "/")
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return nil, nil, fmt.Errorf("unsafe archive path on listing line %d: traversal is not allowed", lineNumber+1)
			}
		}
		name = path.Clean(name)
		if name == "." {
			continue
		}
		if name == ".." || strings.HasPrefix(name, "../") {
			return nil, nil, fmt.Errorf("unsafe archive path on listing line %d", lineNumber+1)
		}
		full := "/" + name
		if previous, exists := entries[full]; exists && previous.IsDir != isDir && !previous.IsDir {
			return nil, nil, fmt.Errorf("conflicting archive entries for %q", name)
		}
		if isDir {
			entries[full] = vfs.Entry{Name: path.Base(full), IsDir: true, Mode: os.ModeDir | 0o555}
		} else {
			entry := vfs.Entry{Name: path.Base(full), Mode: 0o444, Size: size, SizeUnknown: !knownSize}
			if previous, exists := entries[full]; exists && previous.IsDir {
				return nil, nil, fmt.Errorf("conflicting archive entries for %q", name)
			}
			entries[full] = entry
			files[full] = true
		}
		for parent := path.Dir(full); parent != "/"; parent = path.Dir(parent) {
			if existing, exists := entries[parent]; exists && !existing.IsDir {
				return nil, nil, fmt.Errorf("archive file %q is also a parent directory", parent)
			}
			entries[parent] = vfs.Entry{Name: path.Base(parent), IsDir: true, Mode: os.ModeDir | 0o555}
		}
	}
	return entries, files, nil
}

func (f *FS) Name() string { return f.name }

func (f *FS) List(ctx context.Context, dir string) ([]vfs.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, err := f.key(dir)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, ErrClosed
	}
	entry, ok := f.entries[key]
	if !ok || !entry.IsDir {
		return nil, os.ErrNotExist
	}
	result := make([]vfs.Entry, 0)
	if key != "/" {
		result = append(result, vfs.Entry{Name: "..", IsDir: true, IsUp: true, Mode: os.ModeDir | 0o555})
	}
	if key == "/" {
		result = append(result, vfs.Entry{Name: "..", IsDir: true, IsUp: true, Mode: os.ModeDir | 0o555})
	}
	for childPath, child := range f.entries {
		if childPath == "/" || path.Dir(childPath) != key {
			continue
		}
		result = append(result, child)
	}
	return result, nil
}

func (f *FS) Stat(ctx context.Context, name string) (vfs.Entry, error) {
	if err := ctx.Err(); err != nil {
		return vfs.Entry{}, err
	}
	key, err := f.key(name)
	if err != nil {
		return vfs.Entry{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return vfs.Entry{}, ErrClosed
	}
	entry, ok := f.entries[key]
	if !ok {
		return vfs.Entry{}, os.ErrNotExist
	}
	return entry, nil
}

func (f *FS) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, err := f.key(name)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, ErrClosed
	}
	if !f.files[key] {
		return nil, os.ErrNotExist
	}
	extracted, ok := f.extracted[key]
	if !ok {
		extracted, err = f.extract(ctx, key)
		if err != nil {
			return nil, err
		}
		f.extracted[key] = extracted
	}
	return os.Open(extracted)
}

func (f *FS) extract(ctx context.Context, key string) (string, error) {
	destination, err := os.MkdirTemp(f.scratch, "member-")
	if err != nil {
		return "", err
	}
	output, err := handlers.Run(ctx, f.handler.Extract, map[string]string{
		"ARCHIVE": f.archive,
		"DEST":    destination,
		"ENTRY":   strings.TrimPrefix(key, "/"),
	})
	if err != nil {
		_ = os.RemoveAll(destination)
		return "", fmt.Errorf("extract %q: %w", key, err)
	}
	_ = output
	target := filepath.Join(destination, filepath.FromSlash(strings.TrimPrefix(key, "/")))
	relative, err := filepath.Rel(destination, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		_ = os.RemoveAll(destination)
		return "", errors.New("unsafe extracted archive path")
	}
	current := destination
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			_ = os.RemoveAll(destination)
			return "", fmt.Errorf("archive handler did not extract %q: %w", key, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			_ = os.RemoveAll(destination)
			return "", errors.New("archive entry is a symbolic link")
		}
		if current != target && !info.IsDir() {
			_ = os.RemoveAll(destination)
			return "", errors.New("archive path contains a non-directory")
		}
		if current == target && !info.Mode().IsRegular() {
			_ = os.RemoveAll(destination)
			return "", errors.New("archive entry is not a regular file")
		}
	}
	return target, nil
}

func (f *FS) key(name string) (string, error) {
	if !path.IsAbs(name) || strings.ContainsRune(name, 0) {
		return "", errors.New("archive path must be absolute")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", errors.New("archive path traversal is not allowed")
		}
	}
	clean := path.Clean(name)
	if clean != name && !(clean == "/" && name == "") {
		return "", errors.New("archive path must be clean")
	}
	return clean, nil
}

func (f *FS) Create(context.Context, string, os.FileMode) (io.WriteCloser, error) {
	return nil, ErrReadOnly
}

func (f *FS) Mkdir(context.Context, string) error              { return ErrReadOnly }
func (f *FS) Remove(context.Context, string) error             { return ErrReadOnly }
func (f *FS) Rename(context.Context, string, string) error     { return ErrReadOnly }
func (f *FS) Symlink(context.Context, string, string) error    { return ErrReadOnly }
func (f *FS) Readlink(context.Context, string) (string, error) { return "", ErrReadOnly }
func (f *FS) Link(context.Context, string, string) error       { return ErrReadOnly }
func (f *FS) Join(elem ...string) string                       { return path.Join(elem...) }
func (f *FS) Dir(name string) string                           { return path.Dir(name) }
func (f *FS) IsAbs(name string) bool                           { return path.IsAbs(name) }

func (f *FS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return os.RemoveAll(f.scratch)
}
