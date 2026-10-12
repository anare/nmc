package vfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

type memContent struct{ data []byte }

type memNode struct {
	entry   Entry
	content *memContent
	target  string
}

type MemFS struct {
	mu    sync.RWMutex
	label string
	nodes map[string]*memNode
}

func NewMemFS(label string) *MemFS {
	return &MemFS{
		label: label,
		nodes: map[string]*memNode{
			"/": {entry: Entry{Name: "/", IsDir: true, Mode: os.ModeDir | 0o755, ModTime: time.Now()}},
		},
	}
}

func (m *MemFS) Name() string { return m.label }

func (m *MemFS) Put(dir string, entries []Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir = m.clean(dir)
	m.ensureDir(dir)
	for _, entry := range entries {
		if entry.Name == "" || entry.Name == "." || entry.Name == ".." || strings.Contains(entry.Name, "/") {
			continue
		}
		entry.IsUp = false
		if entry.ModTime.IsZero() {
			entry.ModTime = time.Now()
		}
		if entry.IsDir && entry.Mode.Perm() == 0 {
			entry.Mode = os.ModeDir | 0o755
		} else if !entry.IsDir && entry.Mode.Perm() == 0 {
			entry.Mode = 0o644
		}
		node := &memNode{entry: entry, target: entry.Target}
		if !entry.IsDir && !entry.IsLink {
			node.content = &memContent{}
		}
		m.nodes[path.Join(dir, entry.Name)] = node
		if entry.IsDir {
			m.ensureDir(path.Join(dir, entry.Name))
		}
	}
}

func (m *MemFS) PutFile(name string, data []byte, mode os.FileMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = m.clean(name)
	if mode == 0 {
		mode = 0o644
	}
	parent := path.Dir(name)
	m.ensureDir(parent)
	content := &memContent{data: append([]byte(nil), data...)}
	m.nodes[name] = &memNode{
		entry:   Entry{Name: path.Base(name), Size: int64(len(data)), Mode: mode, ModTime: time.Now()},
		content: content,
	}
}

func (m *MemFS) ensureDir(dir string) {
	dir = m.clean(dir)
	if _, ok := m.nodes[dir]; ok {
		return
	}
	parent := path.Dir(dir)
	if parent != dir {
		m.ensureDir(parent)
	}
	m.nodes[dir] = &memNode{entry: Entry{Name: path.Base(dir), IsDir: true, Mode: os.ModeDir | 0o755, ModTime: time.Now()}}
}

func (m *MemFS) clean(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

func (m *MemFS) List(ctx context.Context, dir string) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir = m.clean(dir)
	m.mu.RLock()
	defer m.mu.RUnlock()
	parent, ok := m.nodes[dir]
	if !ok {
		return nil, os.ErrNotExist
	}
	if !parent.entry.IsDir {
		return nil, errors.New("not a directory")
	}
	var result []Entry
	if dir != "/" {
		entry := m.nodes[path.Dir(dir)].entry
		entry.Name, entry.IsUp = "..", true
		result = append(result, entry)
	}
	prefix := strings.TrimSuffix(dir, "/") + "/"
	for name, node := range m.nodes {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		if rest == "" || strings.Contains(rest, "/") {
			continue
		}
		entry := node.entry
		entry.Name = rest
		if node.content != nil {
			entry.Size = int64(len(node.content.data))
		}
		result = append(result, entry)
	}
	return result, nil
}

func (m *MemFS) Stat(ctx context.Context, name string) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	name = m.clean(name)
	m.mu.RLock()
	defer m.mu.RUnlock()
	node, ok := m.nodes[name]
	if !ok {
		return Entry{}, os.ErrNotExist
	}
	entry := node.entry
	entry.Name = path.Base(name)
	if node.content != nil {
		entry.Size = int64(len(node.content.data))
	}
	if node.entry.IsLink {
		target := node.target
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(name), target)
		}
		if targetNode, exists := m.nodes[m.clean(target)]; exists {
			entry.IsDir = targetNode.entry.IsDir
		} else {
			entry.Broken = true
		}
	}
	return entry, nil
}

func (m *MemFS) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	node, ok := m.nodes[m.clean(name)]
	if !ok {
		return nil, os.ErrNotExist
	}
	if node.content == nil {
		return nil, errors.New("not a regular file")
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), node.content.data...))), nil
}

type memWriter struct {
	*bytes.Buffer
	closeFn func([]byte)
}

func (w *memWriter) Close() error {
	w.closeFn(append([]byte(nil), w.Bytes()...))
	return nil
}

func (m *MemFS) Create(ctx context.Context, name string, mode os.FileMode) (io.WriteCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if mode == 0 {
		mode = 0o644
	}
	name = m.clean(name)
	m.mu.Lock()
	if _, exists := m.nodes[name]; !exists {
		parent, ok := m.nodes[path.Dir(name)]
		if !ok || !parent.entry.IsDir {
			m.mu.Unlock()
			return nil, os.ErrNotExist
		}
	}
	entry := Entry{Name: path.Base(name), Mode: mode, ModTime: time.Now()}
	content := &memContent{}
	m.nodes[name] = &memNode{entry: entry, content: content}
	m.mu.Unlock()
	return &memWriter{Buffer: &bytes.Buffer{}, closeFn: func(data []byte) {
		m.mu.Lock()
		defer m.mu.Unlock()
		content.data = data
	}}, nil
}

func (m *MemFS) Mkdir(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name = m.clean(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.nodes[name]; exists {
		return os.ErrExist
	}
	parent, ok := m.nodes[path.Dir(name)]
	if !ok || !parent.entry.IsDir {
		return os.ErrNotExist
	}
	m.nodes[name] = &memNode{entry: Entry{Name: path.Base(name), IsDir: true, Mode: os.ModeDir | 0o755, ModTime: time.Now()}}
	return nil
}

func (m *MemFS) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name = m.clean(name)
	if name == "/" {
		return errors.New("cannot remove root")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nodes[name]; !ok {
		return os.ErrNotExist
	}
	prefix := strings.TrimSuffix(name, "/") + "/"
	for key := range m.nodes {
		if key == name || strings.HasPrefix(key, prefix) {
			delete(m.nodes, key)
		}
	}
	return nil
}

func (m *MemFS) Rename(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	from, to = m.clean(from), m.clean(to)
	m.mu.Lock()
	defer m.mu.Unlock()
	node, ok := m.nodes[from]
	if !ok {
		return os.ErrNotExist
	}
	if _, ok := m.nodes[to]; ok {
		return os.ErrExist
	}
	if parent, ok := m.nodes[path.Dir(to)]; !ok || !parent.entry.IsDir {
		return os.ErrNotExist
	}
	prefix := strings.TrimSuffix(from, "/") + "/"
	updates := map[string]*memNode{}
	for key, value := range m.nodes {
		if key == from {
			updates[to] = value
		} else if strings.HasPrefix(key, prefix) {
			updates[to+strings.TrimPrefix(key, from)] = value
		}
	}
	for key := range updates {
		if _, exists := m.nodes[key]; exists && !strings.HasPrefix(key, prefix) {
			return os.ErrExist
		}
	}
	for key := range m.nodes {
		if key == from || strings.HasPrefix(key, prefix) {
			delete(m.nodes, key)
		}
	}
	for key, value := range updates {
		m.nodes[key] = value
	}
	node.entry.Name = path.Base(to)
	return nil
}

func (m *MemFS) Symlink(ctx context.Context, target, link string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	link = m.clean(link)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.nodes[link]; exists {
		return os.ErrExist
	}
	if parent, ok := m.nodes[path.Dir(link)]; !ok || !parent.entry.IsDir {
		return os.ErrNotExist
	}
	m.nodes[link] = &memNode{
		entry:  Entry{Name: path.Base(link), IsLink: true, Target: target, Mode: os.ModeSymlink | 0o777, ModTime: time.Now()},
		target: target,
	}
	return nil
}

func (m *MemFS) Readlink(ctx context.Context, name string) (string, error) {
	entry, err := m.Stat(ctx, name)
	if err != nil {
		return "", err
	}
	if !entry.IsLink {
		return "", errors.New("not a symlink")
	}
	return entry.Target, nil
}

func (m *MemFS) Link(ctx context.Context, target, link string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target, link = m.clean(target), m.clean(link)
	m.mu.Lock()
	defer m.mu.Unlock()
	node, ok := m.nodes[target]
	if !ok || node.entry.IsDir || node.entry.IsLink {
		return os.ErrInvalid
	}
	if _, exists := m.nodes[link]; exists {
		return os.ErrExist
	}
	if parent, ok := m.nodes[path.Dir(link)]; !ok || !parent.entry.IsDir {
		return os.ErrNotExist
	}
	node.entry.Links++
	clone := *node
	clone.entry.Name = path.Base(link)
	clone.entry.Links = node.entry.Links
	m.nodes[target] = node
	m.nodes[link] = &clone
	return nil
}

func (m *MemFS) Join(elem ...string) string { return path.Join(elem...) }
func (m *MemFS) Dir(name string) string     { return path.Dir(name) }
func (m *MemFS) IsAbs(name string) bool     { return path.IsAbs(name) }
