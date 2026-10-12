package handlers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Kind string

const (
	Archive Kind = "archive"
	Open    Kind = "open"
)

type Handler struct {
	Name     string
	Kind     Kind
	Patterns []string
	List     string
	Extract  string
	Run      string
	Source   string
}

type Set struct {
	handlers []Handler
}

func Defaults() []Handler {
	return []Handler{
		{Name: "tar", Kind: Archive, Patterns: []string{"*.tar", "*.tar.gz", "*.tgz", "*.tar.bz2", "*.tbz", "*.tar.xz", "*.txz", "*.tar.zst"}, List: `tar -tf "$ARCHIVE"`, Extract: `tar -xf "$ARCHIVE" -C "$DEST" -- "$ENTRY"`, Source: "bc"},
		{Name: "zip", Kind: Archive, Patterns: []string{"*.zip", "*.jar", "*.war", "*.whl", "*.apk", "*.egg"}, List: `unzip -Z1 -- "$ARCHIVE"`, Extract: `unzip -o -q -d "$DEST" -- "$ARCHIVE" "$ENTRY"`, Source: "bc"},
		{Name: "images", Kind: Open, Patterns: []string{"*.jpg", "*.jpeg", "*.png", "*.gif", "*.svg", "*.heic", "*.webp"}, Run: `xdg-open "$FILE"`, Source: "bc"},
		{Name: "open-any", Kind: Open, Patterns: []string{"*"}, Run: `xdg-open "$FILE"`, Source: "bc"},
	}
}

func Load(path string) (*Set, []string) {
	set := &Set{handlers: Defaults()}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return set, nil
	}
	if err != nil {
		return set, []string{err.Error()}
	}
	defer file.Close()

	var problems []string
	current := -1
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			fields := strings.Fields(strings.TrimSpace(line[1 : len(line)-1]))
			if len(fields) != 2 || Kind(strings.ToLower(fields[0])) != Archive && Kind(strings.ToLower(fields[0])) != Open {
				current = -1
				problems = append(problems, fmt.Sprintf("line %d: expected [archive name] or [open name]", lineNumber))
				continue
			}
			set.handlers = append(set.handlers, Handler{Name: fields[1], Kind: Kind(strings.ToLower(fields[0])), Source: "user"})
			current = len(set.handlers) - 1
			continue
		}
		if current < 0 {
			problems = append(problems, fmt.Sprintf("line %d: setting outside a handler section", lineNumber))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			problems = append(problems, fmt.Sprintf("line %d: expected key = value", lineNumber))
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		handler := &set.handlers[current]
		switch key {
		case "match":
			for _, pattern := range strings.Split(value, ",") {
				pattern = strings.TrimSpace(pattern)
				if pattern == "" {
					continue
				}
				if _, err := filepath.Match(pattern, ""); err != nil {
					problems = append(problems, fmt.Sprintf("line %d: invalid pattern %q", lineNumber, pattern))
					continue
				}
				handler.Patterns = append(handler.Patterns, pattern)
			}
		case "list":
			handler.List = value
		case "extract":
			handler.Extract = value
		case "run":
			handler.Run = value
		default:
			problems = append(problems, fmt.Sprintf("line %d: unknown handler setting %q", lineNumber, key))
		}
	}
	if err := scanner.Err(); err != nil {
		problems = append(problems, err.Error())
	}
	for _, handler := range set.handlers {
		if handler.Source == "user" && (len(handler.Patterns) == 0 || handler.Kind == Archive && (handler.List == "" || handler.Extract == "") || handler.Kind == Open && handler.Run == "") {
			problems = append(problems, fmt.Sprintf("handler %q is incomplete", handler.Name))
		}
	}
	return set, problems
}

func EnsureConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	const contents = `# External archive and open-with handlers. Paths are environment variables.
[archive tar]
match = *.tar, *.tar.gz, *.tgz, *.tar.bz2, *.tbz, *.tar.xz, *.txz, *.tar.zst
list = tar -tf "$ARCHIVE"
extract = tar -xf "$ARCHIVE" -C "$DEST" -- "$ENTRY"

[archive zip]
match = *.zip, *.jar, *.war, *.whl, *.apk, *.egg
list = unzip -Z1 -- "$ARCHIVE"
extract = unzip -o -q -d "$DEST" -- "$ARCHIVE" "$ENTRY"

[open images]
match = *.jpg, *.jpeg, *.png, *.gif, *.svg, *.heic, *.webp
run = xdg-open "$FILE"
`
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(contents)
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}

func (set *Set) All(kind Kind) []Handler {
	var result []Handler
	for _, handler := range set.handlers {
		if handler.Kind == kind {
			result = append(result, clone(handler))
		}
	}
	return result
}

func (set *Set) Archive(name string) (Handler, bool) {
	return set.resolve(Archive, name, false)
}

func (set *Set) NamedOpen(name string) (Handler, bool) {
	return set.resolve(Open, name, true)
}

func (set *Set) OpenWith(name string) (Handler, bool) {
	return set.resolve(Open, name, false)
}

func (set *Set) resolve(kind Kind, name string, namedOnly bool) (Handler, bool) {
	base := strings.ToLower(filepath.Base(name))
	var matches []Handler
	best := -1
	for _, handler := range set.handlers {
		if handler.Kind != kind {
			continue
		}
		for _, pattern := range handler.Patterns {
			if namedOnly && pattern == "*" {
				continue
			}
			matched, _ := filepath.Match(strings.ToLower(pattern), base)
			if !matched {
				continue
			}
			score := specificity(pattern)
			if score > best {
				matches = matches[:0]
				best = score
			}
			if score == best {
				matches = append(matches, handler)
			}
		}
	}
	if len(matches) == 0 {
		return Handler{}, false
	}
	for _, candidate := range matches {
		if candidate.Source == "user" {
			return clone(candidate), true
		}
	}
	return clone(matches[0]), true
}

func specificity(pattern string) int {
	score := 0
	for _, character := range pattern {
		if character != '*' && character != '?' && character != '[' && character != ']' {
			score++
		}
	}
	return score
}

func clone(handler Handler) Handler {
	handler.Patterns = append([]string(nil), handler.Patterns...)
	return handler
}

func Run(ctx context.Context, command string, values map[string]string) ([]byte, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("handler command is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = make([]string, 0, len(os.Environ())+len(values))
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if _, replace := values[key]; !replace {
			cmd.Env = append(cmd.Env, item)
		}
	}
	for key, value := range values {
		switch key {
		case "ARCHIVE", "DEST", "ENTRY", "FILE":
			cmd.Env = append(cmd.Env, key+"="+value)
		default:
			return nil, fmt.Errorf("unsupported handler variable %q", key)
		}
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return output, errors.New(message)
	}
	return output, nil
}

func ProgramAvailable(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	_, err := exec.LookPath(fields[0])
	return err == nil
}

func (set *Set) Sort() {
	sort.SliceStable(set.handlers, func(i, j int) bool {
		if set.handlers[i].Kind != set.handlers[j].Kind {
			return set.handlers[i].Kind < set.handlers[j].Kind
		}
		return set.handlers[i].Name < set.handlers[j].Name
	})
}
