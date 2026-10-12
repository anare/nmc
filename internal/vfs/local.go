package vfs

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

type Local struct{}

func NewLocal() *Local { return &Local{} }

func (Local) Name() string { return "" }

func (Local) List(ctx context.Context, dir string) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(items)+1)
	parent := filepath.Dir(filepath.Clean(dir))
	if parent != filepath.Clean(dir) {
		up, statErr := localEntry("..", parent)
		if statErr != nil {
			return nil, statErr
		}
		up.IsUp = true
		result = append(result, up)
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry, err := localEntry(item.Name(), filepath.Join(dir, item.Name()))
		if err != nil {
			continue
		}
		result = append(result, entry)
	}
	return result, nil
}

func (Local) Stat(ctx context.Context, path string) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	return localEntry(filepath.Base(path), path)
}

func localEntry(name, path string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	entry := Entry{
		Name:    name,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Mode:    info.Mode(),
		Links:   linkCount(info.Sys()),
	}
	if info.Mode()&os.ModeSymlink != 0 {
		entry.IsLink = true
		entry.Target, err = os.Readlink(path)
		if err != nil {
			return Entry{}, err
		}
		target, statErr := os.Stat(path)
		if statErr != nil {
			entry.Broken = true
		} else {
			entry.IsDir = target.IsDir()
		}
	}
	return entry, nil
}

func linkCount(sys any) int {
	v := reflect.ValueOf(sys)
	if !v.IsValid() || v.Kind() == reflect.Pointer && v.IsNil() {
		return 0
	}
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0
	}
	field := v.FieldByName("Nlink")
	if !field.IsValid() {
		return 0
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if field.Uint() > uint64(^uint(0)>>1) {
			return int(^uint(0) >> 1)
		}
		return int(field.Uint())
	default:
		return 0
	}
}

func (Local) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (Local) Create(ctx context.Context, path string, mode os.FileMode) (io.WriteCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if mode == 0 {
		mode = 0o644
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
}

func (Local) Mkdir(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Mkdir(path, 0o755)
}

func (Local) Remove(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func (Local) Rename(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(from, to)
}

func (Local) Symlink(ctx context.Context, target, link string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Symlink(target, link)
}

func (Local) Readlink(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return os.Readlink(path)
}

func (Local) Link(ctx context.Context, target, link string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Link(target, link)
}

func (Local) Join(elem ...string) string { return filepath.Join(elem...) }
func (Local) Dir(path string) string     { return filepath.Dir(path) }
func (Local) IsAbs(path string) bool     { return filepath.IsAbs(path) }
