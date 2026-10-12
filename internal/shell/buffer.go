package shell

import (
	"strings"
	"sync"
)

const defaultBufferLimit = 1 << 20

type Buffer struct {
	mu    sync.Mutex
	text  []byte
	limit int
}

func NewBuffer(limit int) *Buffer {
	if limit <= 0 {
		limit = defaultBufferLimit
	}
	return &Buffer{limit: limit}
}

func (b *Buffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.text = append(b.text, data...)
	if excess := len(b.text) - b.limit; excess > 0 {
		b.text = append([]byte(nil), b.text[excess:]...)
	}
	return len(data), nil
}

func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.text)
}

func (b *Buffer) Lines() []string {
	return strings.Split(b.String(), "\n")
}

func (b *Buffer) Clear() {
	b.mu.Lock()
	b.text = nil
	b.mu.Unlock()
}
