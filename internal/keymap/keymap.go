package keymap

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

type Mode string

const (
	Panel  Mode = "panel"
	Viewer Mode = "viewer"
	Editor Mode = "editor"
	Find   Mode = "find"
)

type Binding struct {
	Key  tcell.Key
	Rune rune
	Mod  tcell.ModMask
}

type Action struct {
	Name     string
	Mode     Mode
	Describe string
	Defaults []string
}

var Defaults = []Action{
	{"help", Panel, "Help", []string{"F1"}},
	{"menu", Panel, "Settings", []string{"F2"}},
	{"view", Panel, "View", []string{"F3"}},
	{"edit", Panel, "Edit", []string{"F4"}},
	{"copy", Panel, "Copy", []string{"F5"}},
	{"move", Panel, "Move", []string{"F6"}},
	{"rename", Panel, "Rename", []string{"Alt-R", "Shift-F6"}},
	{"create-link", Panel, "Create link", []string{"Alt-L", "Shift-F5"}},
	{"edit-link", Panel, "Edit link", []string{"Alt-E", "Shift-F4"}},
	{"mkdir", Panel, "Create directory", []string{"F7"}},
	{"delete", Panel, "Delete", []string{"F8"}},
	{"quit", Panel, "Quit", []string{"F10", "Ctrl-Q"}},
	{"mark", Panel, "Mark", []string{"Ctrl-T"}},
	{"refresh", Panel, "Refresh", []string{"Ctrl-R"}},
	{"swap-panels", Panel, "Swap panels", []string{"Ctrl-U"}},
	{"quick-search", Panel, "Quick search", []string{"Ctrl-S", "Alt-S"}},
	{"dir-sizes", Panel, "Directory sizes", []string{"Ctrl-L"}},
	{"open-shell", Panel, "Open shell", []string{"Ctrl-O"}},
	{"output-log", Panel, "Output log", []string{"Alt-O"}},
	{"connect", Panel, "Connect", []string{"Ctrl-\\"}},
	{"find-files", Panel, "Find", []string{"Alt-F"}},
	{"open-with", Panel, "Open with", []string{"Alt-W", "Space"}},
	{"sync-other-panel", Panel, "Sync other panel", []string{"Alt-I"}},
	{"git-history", Panel, "Git history", []string{"Alt-G"}},
	{"history-browser", Panel, "History browser", []string{"Alt-H"}},
	{"toggle-hidden", Panel, "Toggle hidden files", []string{"Alt-."}},
	{"sort-order", Panel, "Sort order", []string{"Alt-T"}},
	{"viewer-quit", Viewer, "Back", []string{"F3", "F10", "q"}},
	{"viewer-wrap", Viewer, "Wrap", []string{"F2", "w"}},
	{"viewer-hex", Viewer, "Hex", []string{"F4", "h"}},
	{"viewer-raw", Viewer, "Raw", []string{"F8", "r"}},
	{"viewer-goto", Viewer, "Go to line", []string{"F5"}},
	{"viewer-search", Viewer, "Search", []string{"F7", "/"}},
	{"viewer-search-next", Viewer, "Next match", []string{"n", "Ctrl-S"}},
	{"viewer-search-prev", Viewer, "Previous match", []string{"N"}},
	{"viewer-line-numbers", Viewer, "Line numbers", []string{"Alt-N"}},
	{"viewer-top", Viewer, "Top", []string{"g"}},
	{"viewer-bottom", Viewer, "Bottom", []string{"G"}},
	{"editor-save", Editor, "Save", []string{"F2", "Ctrl-S"}},
	{"editor-quit", Editor, "Quit", []string{"F10", "F3", "Ctrl-Q"}},
	{"editor-search", Editor, "Search", []string{"F7"}},
	{"editor-goto", Editor, "Go to line", []string{"F5"}},
	{"editor-delete-line", Editor, "Delete line", []string{"F8"}},
	{"editor-undo", Editor, "Undo", []string{"Ctrl-Z"}},
	{"editor-redo", Editor, "Redo", []string{"Ctrl-Y"}},
	{"editor-line-numbers", Editor, "Line numbers", []string{"Alt-N"}},
	{"editor-select-start", Editor, "Start selection", []string{"Alt-A"}},
	{"editor-select-end", Editor, "End selection", []string{"Alt-S"}},
	{"editor-copy", Editor, "Copy selection", []string{"Alt-C"}},
	{"editor-cut", Editor, "Cut selection", []string{"Alt-X"}},
	{"editor-paste", Editor, "Paste", []string{"Alt-V"}},
	{"find-open", Find, "Open result", []string{"F3", "Enter"}},
	{"find-panelise", Find, "Panelise", []string{"F5"}},
	{"find-again", Find, "Search again", []string{"F7"}},
	{"find-stop", Find, "Stop", []string{"Ctrl-C"}},
	{"find-quit", Find, "Quit", []string{"F10", "q"}},
}

type Map struct {
	bindings map[Mode]map[Binding]string
	settings map[string]string
}

func New() *Map {
	m := &Map{bindings: make(map[Mode]map[Binding]string), settings: make(map[string]string)}
	for _, action := range Defaults {
		for _, name := range action.Defaults {
			binding, err := Parse(name)
			if err == nil {
				m.bind(action.Mode, binding, action.Name)
			}
		}
	}
	return m
}

func FromEvent(event *tcell.EventKey) Binding {
	binding := Binding{Key: event.Key(), Mod: event.Modifiers()}
	if isCtrlKey(binding.Key) {
		binding.Mod &^= tcell.ModCtrl
	}
	binding.Key, binding.Mod = foldShiftedFKey(binding.Key, binding.Mod)
	if binding.Key != tcell.KeyRune {
		return binding
	}
	binding.Rune = event.Rune()
	binding.Mod &^= tcell.ModShift
	if binding.Mod != 0 && binding.Rune >= 'A' && binding.Rune <= 'Z' {
		binding.Rune += 'a' - 'A'
	}
	return binding
}

func foldShiftedFKey(key tcell.Key, mod tcell.ModMask) (tcell.Key, tcell.ModMask) {
	if key >= tcell.KeyF13 && key <= tcell.KeyF24 {
		return tcell.KeyF1 + (key - tcell.KeyF13), mod | tcell.ModShift
	}
	return key, mod
}

func isCtrlKey(key tcell.Key) bool { return key >= tcell.KeyCtrlA && key <= tcell.KeyCtrlZ }

func Parse(name string) (Binding, error) {
	parts := strings.Split(name, "-")
	mod := tcell.ModMask(0)
	for len(parts) > 1 {
		switch strings.ToLower(parts[0]) {
		case "ctrl", "c":
			mod |= tcell.ModCtrl
		case "alt", "meta", "m":
			mod |= tcell.ModAlt
		case "shift":
			mod |= tcell.ModShift
		default:
			break
		}
		if strings.EqualFold(parts[0], "ctrl") || strings.EqualFold(parts[0], "c") ||
			strings.EqualFold(parts[0], "alt") || strings.EqualFold(parts[0], "meta") ||
			strings.EqualFold(parts[0], "m") || strings.EqualFold(parts[0], "shift") {
			parts = parts[1:]
		} else {
			break
		}
	}
	keyName := strings.Join(parts, "-")
	if key, ok := ctrlNames[strings.ToLower(keyName)]; ok && mod&tcell.ModCtrl != 0 {
		return Binding{Key: key}, nil
	}
	keyNameLower := strings.ToLower(keyName)
	if keyNameLower == "space" {
		return Binding{Key: tcell.KeyRune, Rune: ' ', Mod: mod &^ tcell.ModShift}, nil
	}
	if keyNameLower == "comma" {
		return Binding{Key: tcell.KeyRune, Rune: ',', Mod: mod &^ tcell.ModShift}, nil
	}
	if key, ok := specialKeys[keyNameLower]; ok {
		return Binding{Key: key, Mod: mod}, nil
	}
	if len(keyName) > 1 && (keyName[0] == 'F' || keyName[0] == 'f') {
		number, err := strconv.Atoi(keyName[1:])
		if err == nil && number >= 1 && number <= 24 {
			key := tcell.KeyF1 + tcell.Key(number-1)
			if key >= tcell.KeyF13 {
				key, mod = foldShiftedFKey(key, mod)
			}
			return Binding{Key: key, Mod: mod}, nil
		}
	}
	runes := []rune(keyName)
	if len(runes) == 1 {
		r := runes[0]
		if mod&tcell.ModShift != 0 {
			r = []rune(strings.ToUpper(string(r)))[0]
		}
		if mod != 0 && r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		mod &^= tcell.ModShift
		return Binding{Key: tcell.KeyRune, Rune: r, Mod: mod}, nil
	}
	return Binding{}, fmt.Errorf("unknown key %q", name)
}

func (b Binding) String() string {
	var parts []string
	if b.Mod&tcell.ModCtrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if b.Mod&tcell.ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if b.Mod&tcell.ModShift != 0 {
		parts = append(parts, "Shift")
	}
	key := ""
	if b.Key == tcell.KeyRune {
		switch b.Rune {
		case ' ':
			key = "Space"
		case ',':
			key = "Comma"
		default:
			key = string(b.Rune)
		}
	} else if b.Key >= tcell.KeyF1 && b.Key <= tcell.KeyF12 {
		key = fmt.Sprintf("F%d", b.Key-tcell.KeyF1+1)
	} else {
		key = keyNames[b.Key]
	}
	if key == "" {
		key = fmt.Sprintf("Key%d", b.Key)
	}
	parts = append(parts, key)
	return strings.Join(parts, "-")
}

func Reserved(b Binding) (string, bool) {
	if b.Mod != 0 {
		return "", false
	}
	switch b.Key {
	case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyEnter, tcell.KeyEscape,
		tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyUp, tcell.KeyDown,
		tcell.KeyLeft, tcell.KeyRight, tcell.KeyPgUp, tcell.KeyPgDn,
		tcell.KeyHome, tcell.KeyEnd, tcell.KeyCtrlX, tcell.KeyF1:
		return "reserved for navigation", true
	default:
		return "", false
	}
}

func (m *Map) Lookup(mode Mode, event *tcell.EventKey) (string, bool) {
	action, ok := m.bindings[mode][FromEvent(event)]
	return action, ok
}

func (m *Map) Rebind(mode Mode, action string, bindings ...Binding) error {
	if defaultAction(mode, action) == "" {
		return fmt.Errorf("unknown action %q in mode %q", action, mode)
	}
	for _, binding := range bindings {
		if why, reserved := Reserved(binding); reserved {
			return fmt.Errorf("%s: %s", binding.String(), why)
		}
	}
	for binding, existing := range m.bindings[mode] {
		if existing == action {
			delete(m.bindings[mode], binding)
		}
	}
	for _, binding := range bindings {
		m.bind(mode, binding, action)
	}
	return nil
}

func (m *Map) Unbind(mode Mode, action string) {
	for binding, existing := range m.bindings[mode] {
		if existing == action {
			delete(m.bindings[mode], binding)
		}
	}
}

func (m *Map) bind(mode Mode, binding Binding, action string) {
	if m.bindings[mode] == nil {
		m.bindings[mode] = make(map[Binding]string)
	}
	m.bindings[mode][binding] = action
}

func (m *Map) Settings() map[string]string {
	result := make(map[string]string, len(m.settings))
	for key, value := range m.settings {
		result[key] = value
	}
	return result
}

func Load(path string) (*Map, []string) {
	m := New()
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return m, []string{err.Error()}
	}
	defer file.Close()
	var problems []string
	mode := Mode("")
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			candidate := Mode(strings.ToLower(strings.TrimSpace(line[1 : len(line)-1])))
			if candidate == Panel || candidate == Viewer || candidate == Editor || candidate == Find {
				mode = candidate
			} else if candidate == "settings" {
				mode = "settings"
			} else {
				mode = ""
				problems = append(problems, fmt.Sprintf("line %d: unknown section %q", lineNo, candidate))
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			problems = append(problems, fmt.Sprintf("line %d: expected key = value", lineNo))
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if mode == "settings" {
			m.settings[key] = value
			continue
		}
		if mode == "" {
			continue
		}
		action := defaultAction(mode, key)
		if action == "" {
			problems = append(problems, fmt.Sprintf("line %d: unknown action %q", lineNo, key))
			continue
		}
		var bindings []Binding
		for _, spelling := range strings.Split(value, ",") {
			binding, err := Parse(strings.TrimSpace(spelling))
			if err != nil {
				problems = append(problems, fmt.Sprintf("line %d: %v", lineNo, err))
				continue
			}
			if why, reserved := Reserved(binding); reserved {
				problems = append(problems, fmt.Sprintf("line %d: %s: %s", lineNo, binding.String(), why))
				continue
			}
			bindings = append(bindings, binding)
		}
		_ = m.Rebind(mode, action, bindings...)
	}
	if err := scanner.Err(); err != nil {
		problems = append(problems, err.Error())
	}
	return m, problems
}

func (m *Map) Save(path string) error {
	defaultMap := New()
	var lines []string
	settings := make([]string, 0, len(m.settings))
	for key := range m.settings {
		settings = append(settings, key)
	}
	sort.Strings(settings)
	if len(settings) > 0 {
		lines = append(lines, "[settings]")
		for _, key := range settings {
			lines = append(lines, key+" = "+m.settings[key])
		}
	}
	modes := []Mode{Panel, Viewer, Editor, Find}
	for _, mode := range modes {
		var actions []Action
		for _, action := range Defaults {
			if action.Mode == mode {
				actions = append(actions, action)
			}
		}
		var entries []string
		for _, action := range actions {
			var current []string
			for binding, boundAction := range m.bindings[mode] {
				if boundAction == action.Name {
					current = append(current, binding.String())
				}
			}
			sort.Strings(current)
			var defaults []string
			for binding, boundAction := range defaultMap.bindings[mode] {
				if boundAction == action.Name {
					defaults = append(defaults, binding.String())
				}
			}
			sort.Strings(defaults)
			if strings.Join(current, ",") != strings.Join(defaults, ",") {
				entries = append(entries, action.Name+" = "+strings.Join(current, ", "))
			}
		}
		if len(entries) > 0 {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, "["+string(mode)+"]")
			lines = append(lines, entries...)
		}
	}
	if len(lines) == 0 {
		_ = os.Remove(path)
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".bcrc-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.WriteString(strings.Join(lines, "\n") + "\n")
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func defaultAction(mode Mode, name string) string {
	for _, action := range Defaults {
		if action.Mode == mode && action.Name == name {
			return action.Name
		}
	}
	return ""
}

var ctrlNames = buildCtrlNames()

func buildCtrlNames() map[string]tcell.Key {
	names := map[string]tcell.Key{"\\": tcell.KeyCtrlBackslash}
	for letter := 0; letter < 26; letter++ {
		names[string(rune('a'+letter))] = tcell.KeyCtrlA + tcell.Key(letter)
	}
	return names
}

var specialKeys = map[string]tcell.Key{
	"tab": tcell.KeyTab, "backtab": tcell.KeyBacktab, "enter": tcell.KeyEnter,
	"esc": tcell.KeyEscape, "escape": tcell.KeyEscape, "backspace": tcell.KeyBackspace2,
	"delete": tcell.KeyDelete, "insert": tcell.KeyInsert, "up": tcell.KeyUp, "down": tcell.KeyDown,
	"left": tcell.KeyLeft, "right": tcell.KeyRight, "home": tcell.KeyHome, "end": tcell.KeyEnd,
	"pgup": tcell.KeyPgUp, "pgdn": tcell.KeyPgDn,
}

var keyNames = buildKeyNames()

func buildKeyNames() map[tcell.Key]string {
	names := map[tcell.Key]string{
		tcell.KeyTab: "Tab", tcell.KeyBacktab: "Backtab", tcell.KeyEnter: "Enter",
		tcell.KeyEscape: "Esc", tcell.KeyBackspace: "Backspace", tcell.KeyBackspace2: "Backspace",
		tcell.KeyDelete: "Del", tcell.KeyInsert: "Insert", tcell.KeyUp: "Up", tcell.KeyDown: "Down",
		tcell.KeyLeft: "Left", tcell.KeyRight: "Right", tcell.KeyHome: "Home", tcell.KeyEnd: "End",
		tcell.KeyPgUp: "PgUp", tcell.KeyPgDn: "PgDn", tcell.KeyCtrlQ: "Ctrl-Q",
		tcell.KeyCtrlX: "Ctrl-X", tcell.KeyCtrlBackslash: "Ctrl-\\",
	}
	for letter := 0; letter < 26; letter++ {
		key := tcell.KeyCtrlA + tcell.Key(letter)
		if _, exists := names[key]; !exists {
			names[key] = "Ctrl-" + strings.ToUpper(string(rune('a'+letter)))
		}
	}
	return names
}
