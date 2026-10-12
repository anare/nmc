package vfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
)

const CachedContentLimit = 1 << 20

type Cached struct {
	fs      FS
	mu      sync.RWMutex
	listing map[string][]Entry
	stats   map[string]Entry
	content map[string][]byte
}

func NewCached(fs FS) *Cached {
	return &Cached{fs: fs, listing: make(map[string][]Entry), stats: make(map[string]Entry), content: make(map[string][]byte)}
}

func (c *Cached) Name() string { return c.fs.Name() }

func (c *Cached) List(ctx context.Context, dir string) ([]Entry, error) {
	c.mu.RLock()
	entries, ok := c.listing[dir]
	c.mu.RUnlock()
	if ok {
		return append([]Entry(nil), entries...), nil
	}
	entries, err := c.fs.List(ctx, dir)
	if err != nil {
		return nil, err
	}
	copyEntries := append([]Entry(nil), entries...)
	c.mu.Lock()
	c.listing[dir] = copyEntries
	c.mu.Unlock()
	return append([]Entry(nil), copyEntries...), nil
}

func (c *Cached) Stat(ctx context.Context, name string) (Entry, error) {
	c.mu.RLock()
	entry, ok := c.stats[name]
	c.mu.RUnlock()
	if ok {
		return entry, nil
	}
	entry, err := c.fs.Stat(ctx, name)
	if err != nil {
		return Entry{}, err
	}
	c.mu.Lock()
	c.stats[name] = entry
	c.mu.Unlock()
	return entry, nil
}

func (c *Cached) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	c.mu.RLock()
	data, ok := c.content[name]
	c.mu.RUnlock()
	if ok {
		return io.NopCloser(bytes.NewReader(append([]byte(nil), data...))), nil
	}
	entry, err := c.Stat(ctx, name)
	if err != nil {
		return nil, err
	}
	reader, err := c.fs.Open(ctx, name)
	if err != nil {
		return nil, err
	}
	if entry.SizeUnknown || entry.Size > CachedContentLimit {
		return reader, nil
	}
	data, err = io.ReadAll(io.LimitReader(reader, CachedContentLimit+1))
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	if len(data) <= CachedContentLimit {
		if err := reader.Close(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.content[name] = append([]byte(nil), data...)
		c.mu.Unlock()
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return &joinedReadCloser{Reader: io.MultiReader(bytes.NewReader(data), reader), closer: reader}, nil
}

type joinedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *joinedReadCloser) Close() error { return r.closer.Close() }

func (c *Cached) Create(ctx context.Context, name string, mode os.FileMode) (io.WriteCloser, error) {
	writer, err := c.fs.Create(ctx, name, mode)
	if err != nil {
		return nil, err
	}
	return &invalidatingWriter{WriteCloser: writer, invalidate: func() { c.invalidate(name) }}, nil
}

type invalidatingWriter struct {
	io.WriteCloser
	invalidate func()
}

func (w *invalidatingWriter) Close() error {
	err := w.WriteCloser.Close()
	w.invalidate()
	return err
}

func (c *Cached) Mkdir(ctx context.Context, name string) error {
	err := c.fs.Mkdir(ctx, name)
	if err == nil {
		c.invalidate(name)
	}
	return err
}

func (c *Cached) Remove(ctx context.Context, name string) error {
	err := c.fs.Remove(ctx, name)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		c.invalidate(name)
	}
	return err
}

func (c *Cached) Rename(ctx context.Context, from, to string) error {
	err := c.fs.Rename(ctx, from, to)
	if err == nil {
		c.invalidate(from)
		c.invalidate(to)
	}
	return err
}

func (c *Cached) Symlink(ctx context.Context, target, link string) error {
	err := c.fs.Symlink(ctx, target, link)
	if err == nil {
		c.invalidate(link)
	}
	return err
}

func (c *Cached) Readlink(ctx context.Context, name string) (string, error) {
	return c.fs.Readlink(ctx, name)
}

func (c *Cached) Link(ctx context.Context, target, link string) error {
	err := c.fs.Link(ctx, target, link)
	if err == nil {
		c.invalidate(link)
	}
	return err
}

func (c *Cached) Join(elem ...string) string { return c.fs.Join(elem...) }
func (c *Cached) Dir(name string) string     { return c.fs.Dir(name) }
func (c *Cached) IsAbs(name string) bool     { return c.fs.IsAbs(name) }

func (c *Cached) Invalidate() {
	c.mu.Lock()
	c.listing = make(map[string][]Entry)
	c.stats = make(map[string]Entry)
	c.content = make(map[string][]byte)
	c.mu.Unlock()
}

func (c *Cached) invalidate(name string) {
	c.Invalidate()
}
