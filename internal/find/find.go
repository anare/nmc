package find

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/anare/nmc/internal/vfs"
)

type Options struct {
	Root          string
	NamePattern   string
	Contains      string
	CaseSensitive bool
	WholeWords    bool
	IncludeHidden bool
	SearchBinary  bool
}

type Hit struct {
	Path string
	Line int
	Text string
}

func Search(ctx context.Context, fs vfs.FS, options Options, emit func(Hit)) ([]Hit, error) {
	if fs == nil {
		return nil, errors.New("no filesystem")
	}
	if options.Root == "" {
		options.Root = "/"
	}
	if options.NamePattern == "" {
		options.NamePattern = "*"
	}
	if _, err := path.Match(options.NamePattern, ""); err != nil {
		return nil, err
	}
	var hits []Hit
	var walk func(string) error
	walk = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := fs.List(ctx, dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsUp || (!options.IncludeHidden && strings.HasPrefix(entry.Name, ".")) {
				continue
			}
			name := fs.Join(dir, entry.Name)
			if entry.IsDir && !entry.IsLink {
				if err := walk(name); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return err
					}
				}
				continue
			}
			matched, _ := path.Match(options.NamePattern, entry.Name)
			if !matched {
				continue
			}
			if options.Contains == "" {
				hit := Hit{Path: relative(fs, options.Root, name)}
				hits = append(hits, hit)
				if emit != nil {
					emit(hit)
				}
			}
			if options.Contains != "" {
				fileHits, searchErr := searchFile(ctx, fs, name, options)
				if searchErr != nil {
					if errors.Is(searchErr, context.Canceled) || errors.Is(searchErr, context.DeadlineExceeded) {
						return searchErr
					}
					continue
				}
				for _, hit := range fileHits {
					hit.Path = relative(fs, options.Root, name)
					hits = append(hits, hit)
					if emit != nil {
						emit(hit)
					}
				}
			}
		}
		return nil
	}
	err := walk(options.Root)
	return hits, err
}

func searchFile(ctx context.Context, fs vfs.FS, name string, options Options) ([]Hit, error) {
	reader, err := fs.Open(ctx, name)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	buffered := bufio.NewReader(reader)
	prefix, _ := buffered.Peek(8192)
	if !options.SearchBinary && (bytes.IndexByte(prefix, 0) >= 0 || !utf8.Valid(prefix)) {
		return nil, nil
	}
	needle := options.Contains
	if !options.CaseSensitive {
		needle = strings.ToLower(needle)
	}
	var hits []Hit
	lineNumber := 0
	for {
		if err := ctx.Err(); err != nil {
			return hits, err
		}
		line, readErr := buffered.ReadString('\n')
		if len(line) > 0 {
			lineNumber++
			text := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			candidate := text
			if !options.CaseSensitive {
				candidate = strings.ToLower(candidate)
			}
			if contains(candidate, needle, options.WholeWords) {
				hits = append(hits, Hit{Line: lineNumber, Text: text})
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return hits, readErr
		}
	}
	return hits, nil
}

func contains(line, needle string, wholeWords bool) bool {
	for start := 0; start <= len(line)-len(needle); {
		offset := strings.Index(line[start:], needle)
		if offset < 0 {
			return false
		}
		index := start + offset
		end := index + len(needle)
		if !wholeWords || !wordBefore(line, index) && !wordAfter(line, end) {
			return true
		}
		start = index + 1
	}
	return false
}

func wordBefore(value string, index int) bool {
	if index <= 0 || index > len(value) {
		return false
	}
	previous, _ := utf8.DecodeLastRuneInString(value[:index])
	return isWord(previous)
}

func wordAfter(value string, index int) bool {
	if index < 0 || index >= len(value) {
		return false
	}
	next, _ := utf8.DecodeRuneInString(value[index:])
	return isWord(next)
}

func isWord(value rune) bool {
	return value == '_' || unicode.IsLetter(value) || unicode.IsDigit(value)
}

func relative(fs vfs.FS, root, name string) string {
	if name == root {
		return "."
	}
	prefix := strings.TrimSuffix(root, "/") + "/"
	if strings.HasPrefix(name, prefix) {
		return strings.TrimPrefix(name, prefix)
	}
	return name
}
