package history

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Kind string

const (
	Command Kind = "command"
	Path    Kind = "path"
	Dir     Kind = "directory"
	Search  Kind = "search"
	Goto    Kind = "goto"
	Host    Kind = "host"
)

const maxEntries = 1000

type Store struct {
	mu      sync.RWMutex
	path    string
	entries map[Kind][]string
}

func Open(path string) (*Store, error) {
	store := &Store{path: path, entries: make(map[Kind][]string)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &store.entries); err != nil {
		return nil, err
	}
	for kind, entries := range store.entries {
		if len(entries) > maxEntries {
			store.entries[kind] = entries[:maxEntries]
		}
	}
	return store, nil
}

func OpenFromEnvironment() (*Store, error) {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return Open(filepath.Join(stateHome, "bc", "history"))
}

func (s *Store) Add(kind Kind, value string) {
	if value == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.entries[kind]
	next := make([]string, 0, min(len(old)+1, maxEntries))
	next = append(next, value)
	for _, item := range old {
		if item != value && len(next) < maxEntries {
			next = append(next, item)
		}
	}
	s.entries[kind] = next
}

func (s *Store) Cursor(kind Kind) *Cursor {
	s.mu.RLock()
	entries := append([]string(nil), s.entries[kind]...)
	s.mu.RUnlock()
	return &Cursor{entries: entries, index: -1}
}

func (s *Store) Save() error {
	s.mu.RLock()
	data, err := json.Marshal(s.entries)
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".history-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

type Cursor struct {
	entries []string
	index   int
	draft   string
}

func (c *Cursor) Prev(current string) string {
	if len(c.entries) == 0 {
		return current
	}
	if c.index == -1 {
		c.draft = current
		c.index = 0
	} else if c.index < len(c.entries)-1 {
		c.index++
	}
	return c.entries[c.index]
}

func (c *Cursor) Next(current string) string {
	if c.index < 0 {
		return current
	}
	if c.index == 0 {
		c.index = -1
		return c.draft
	}
	c.index--
	return c.entries[c.index]
}

func (c *Cursor) Reset() {
	c.index = -1
	c.draft = ""
}
