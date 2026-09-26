package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	escTimeoutRune = '\U0010ffff'
	maxViewSize    = 512 * 1024
)

var (
	mcBackground       = tcell.NewRGBColor(0, 0, 96)
	mcPanelBackground  = tcell.NewRGBColor(0, 0, 128)
	mcDialogBackground = tcell.NewRGBColor(0, 0, 84)
	mcAccent           = tcell.NewRGBColor(0, 170, 255)
	mcAccentMuted      = tcell.NewRGBColor(70, 120, 200)
)

type sortMode int

const (
	sortByName sortMode = iota
	sortByExt
	sortByTime
	sortBySize
)

func (s sortMode) String() string {
	switch s {
	case sortByExt:
		return "ext"
	case sortByTime:
		return "time"
	case sortBySize:
		return "size"
	default:
		return "name"
	}
}

type listItem struct {
	name    string
	path    string
	isDir   bool
	size    int64
	modTime time.Time
	ext     string
	mode    os.FileMode
}

type panel struct {
	name       string
	list       *tview.List
	path       string
	items      []listItem
	showHidden bool
	sortMode   sortMode
	marked     map[string]bool
	history    []string
	historyIx  int
}

func newPanel(title string) *panel {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetUseStyleTags(true, false)
	l.SetBorder(true)
	l.SetTitle(" " + title + " ")
	l.SetBackgroundColor(mcPanelBackground)
	l.SetMainTextColor(tcell.ColorWhite)
	l.SetSelectedBackgroundColor(mcAccent)
	l.SetSelectedTextColor(tcell.ColorBlack)
	l.SetBorderColor(mcAccentMuted)
	l.SetTitleColor(tcell.ColorWhite)
	return &panel{
		name:       title,
		list:       l,
		showHidden: true,
		sortMode:   sortByName,
		marked:     map[string]bool{},
		historyIx:  -1,
	}
}

func (p *panel) pushHistory(path string) {
	if p.historyIx >= 0 && p.historyIx < len(p.history) && p.history[p.historyIx] == path {
		return
	}
	if p.historyIx >= 0 && p.historyIx < len(p.history)-1 {
		p.history = p.history[:p.historyIx+1]
	}
	p.history = append(p.history, path)
	p.historyIx = len(p.history) - 1
}

func (p *panel) load(path string, selectIndex int, addHistory bool) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return err
	}

	if absPath != p.path {
		p.marked = map[string]bool{}
	}

	items := make([]listItem, 0, len(entries)+1)
	if parent := filepath.Dir(absPath); parent != absPath {
		items = append(items, listItem{name: "..", path: parent, isDir: true})
	}

	for _, e := range entries {
		name := e.Name()
		if !p.showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		fullPath := filepath.Join(absPath, name)
		info, infoErr := e.Info()
		if infoErr != nil {
			continue
		}
		items = append(items, listItem{
			name:    name,
			path:    fullPath,
			isDir:   e.IsDir(),
			size:    info.Size(),
			modTime: info.ModTime(),
			ext:     strings.ToLower(filepath.Ext(name)),
			mode:    info.Mode(),
		})
	}

	if len(items) > 1 {
		sort.Slice(items[1:], func(i, j int) bool {
			a := items[i+1]
			b := items[j+1]
			if a.isDir != b.isDir {
				return a.isDir
			}
			switch p.sortMode {
			case sortByExt:
				if a.ext != b.ext {
					return a.ext < b.ext
				}
			case sortByTime:
				if !a.modTime.Equal(b.modTime) {
					return a.modTime.After(b.modTime)
				}
			case sortBySize:
				if a.size != b.size {
					return a.size > b.size
				}
			}
			return strings.ToLower(a.name) < strings.ToLower(b.name)
		})
	}

	p.path = absPath
	p.items = items

	visibleMarks := map[string]bool{}
	for _, item := range p.items {
		if p.marked[item.path] {
			visibleMarks[item.path] = true
		}
	}
	p.marked = visibleMarks

	if addHistory {
		p.pushHistory(absPath)
	}

	p.list.Clear()
	for _, item := range p.items {
		p.list.AddItem(p.renderItem(item), "", 0, nil)
	}
	p.updateTitle()

	if len(p.items) == 0 {
		return nil
	}
	if selectIndex < 0 {
		selectIndex = 0
	}
	if selectIndex >= len(p.items) {
		selectIndex = len(p.items) - 1
	}
	p.list.SetCurrentItem(selectIndex)
	return nil
}

func (p *panel) renderItem(item listItem) string {
	mark := " "
	if p.marked[item.path] {
		mark = "[yellow::b]*[-:-:-]"
	}
	if item.name == ".." {
		return fmt.Sprintf("%s [#7fb3ff::b]drwxr-xr-x[-:-:-] %10s %s %s", mark, "-", "-", "[#a6c8ff::b]../[-:-:-]")
	}
	size := fmt.Sprintf("%d", item.size)
	if item.isDir {
		size = "-"
	}
	modified := item.modTime.Format("2006-01-02 15:04")
	return fmt.Sprintf("%s %s %10s %s %s", mark, item.mode.String(), size, modified, colorizeName(item, p.marked[item.path]))
}

func colorizeName(item listItem, marked bool) string {
	name := item.name
	if marked {
		if item.isDir && item.name != ".." {
			name += "/"
		}
		return fmt.Sprintf("[yellow::b]%s[-:-:-]", tview.Escape(name))
	}
	if item.isDir {
		if item.name != ".." {
			name += "/"
		}
		return fmt.Sprintf("[#89d1ff::b]%s[-:-:-]", tview.Escape(name))
	}
	switch item.ext {
	case ".go":
		return fmt.Sprintf("[#7cffb2::b]%s[-:-:-]", tview.Escape(name))
	case ".md", ".txt":
		return fmt.Sprintf("[#e8e8e8]%s[-:-:-]", tview.Escape(name))
	case ".json", ".yaml", ".yml", ".toml":
		return fmt.Sprintf("[#ffd27f]%s[-:-:-]", tview.Escape(name))
	case ".sh", ".bash", ".zsh":
		return fmt.Sprintf("[#ff9fd4::b]%s[-:-:-]", tview.Escape(name))
	case ".jpg", ".jpeg", ".png", ".gif", ".svg":
		return fmt.Sprintf("[#c8a8ff]%s[-:-:-]", tview.Escape(name))
	default:
		return fmt.Sprintf("[#d7e7ff]%s[-:-:-]", tview.Escape(name))
	}
}

func (p *panel) refresh() error {
	idx := p.list.GetCurrentItem()
	return p.load(p.path, idx, false)
}

func (p *panel) selectedItem() (listItem, bool) {
	if len(p.items) == 0 {
		return listItem{}, false
	}
	idx := p.list.GetCurrentItem()
	if idx < 0 || idx >= len(p.items) {
		return listItem{}, false
	}
	return p.items[idx], true
}

func (p *panel) move(delta int) {
	if len(p.items) == 0 {
		return
	}
	idx := p.list.GetCurrentItem() + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(p.items) {
		idx = len(p.items) - 1
	}
	p.list.SetCurrentItem(idx)
}

func (p *panel) pageMove(deltaPages int) {
	if len(p.items) == 0 {
		return
	}
	_, _, _, height := p.list.GetInnerRect()
	step := height - 1
	if step < 1 {
		step = 1
	}
	p.move(step * deltaPages)
}

func (p *panel) goIntoSelected() error {
	item, ok := p.selectedItem()
	if !ok || !item.isDir {
		return nil
	}
	if item.name == ".." {
		childName := filepath.Base(p.path)
		if err := p.load(item.path, 0, true); err != nil {
			return err
		}
		for i, candidate := range p.items {
			if candidate.name == childName {
				p.list.SetCurrentItem(i)
				break
			}
		}
		return nil
	}
	return p.load(item.path, 0, true)
}

func (p *panel) goParentViaList() error {
	for i, item := range p.items {
		if item.name == ".." {
			p.list.SetCurrentItem(i)
			return p.goIntoSelected()
		}
	}
	return nil
}

func (p *panel) historyBack() error {
	if p.historyIx <= 0 {
		return nil
	}
	p.historyIx--
	return p.load(p.history[p.historyIx], 0, false)
}

func (p *panel) historyForward() error {
	if p.historyIx < 0 || p.historyIx >= len(p.history)-1 {
		return nil
	}
	p.historyIx++
	return p.load(p.history[p.historyIx], 0, false)
}

func (p *panel) toggleMarkCurrent() {
	item, ok := p.selectedItem()
	if !ok || item.name == ".." {
		return
	}
	if p.marked[item.path] {
		delete(p.marked, item.path)
	} else {
		p.marked[item.path] = true
	}
	_ = p.refresh()
}

func (p *panel) selectAll() {
	for _, item := range p.items {
		if item.name != ".." {
			p.marked[item.path] = true
		}
	}
	_ = p.refresh()
}

func (p *panel) clearSelection() {
	p.marked = map[string]bool{}
	_ = p.refresh()
}

func (p *panel) invertSelection() {
	for _, item := range p.items {
		if item.name == ".." {
			continue
		}
		if p.marked[item.path] {
			delete(p.marked, item.path)
		} else {
			p.marked[item.path] = true
		}
	}
	_ = p.refresh()
}

func (p *panel) selectedPaths() []string {
	if len(p.marked) > 0 {
		paths := make([]string, 0, len(p.marked))
		for path := range p.marked {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		return paths
	}
	item, ok := p.selectedItem()
	if !ok || item.name == ".." {
		return nil
	}
	return []string{item.path}
}

func (p *panel) cycleSortMode() {
	p.sortMode = (p.sortMode + 1) % 4
	_ = p.refresh()
}

func (p *panel) toggleHidden() {
	p.showHidden = !p.showHidden
	_ = p.refresh()
}

func (p *panel) search(query string) bool {
	if query == "" || len(p.items) == 0 {
		return false
	}
	start := p.list.GetCurrentItem() + 1
	for i := 0; i < len(p.items); i++ {
		idx := (start + i) % len(p.items)
		item := p.items[idx]
		if item.name == ".." {
			continue
		}
		if strings.Contains(strings.ToLower(item.name), query) {
			p.list.SetCurrentItem(idx)
			return true
		}
	}
	return false
}

func (p *panel) updateTitle() {
	flags := []string{fmt.Sprintf("sort:%s", p.sortMode.String())}
	if !p.showHidden {
		flags = append(flags, "hidden:off")
	}
	if len(p.marked) > 0 {
		flags = append(flags, fmt.Sprintf("sel:%d", len(p.marked)))
	}
	p.list.SetTitle(fmt.Sprintf(" %s: %s [%s] ", p.name, p.path, strings.Join(flags, " ")))
}

type appState struct {
	app        *tview.Application
	pages      *tview.Pages
	panels     [2]*panel
	active     int
	status     *tview.TextView
	escPending bool
	escTimer   *time.Timer
	passEsc    bool
	shellCmd   *exec.Cmd
	shellIn    io.WriteCloser
	shellView  *tview.TextView
	shellInput *tview.InputField
	shellMu    sync.Mutex
	cmdInput   *tview.InputField
}

func (s *appState) setStatus(msg string) {
	s.status.SetText(" " + tview.Escape(msg))
}

func (s *appState) executeCommandLine() {
	if s.cmdInput == nil {
		return
	}
	cmd := strings.TrimSpace(s.cmdInput.GetText())
	if cmd == "" {
		return
	}
	s.cmdInput.SetText("")
	if s.shellIn == nil {
		s.setStatus("Shell is not available")
		return
	}
	s.appendShellOutput("$ " + cmd + "\n")
	_, err := io.WriteString(s.shellIn, cmd+"\n")
	if err != nil {
		s.setError("Command", cmd, err)
		return
	}
	s.setStatus("Command sent: " + cmd)
}

func (s *appState) appendCommandRune(r rune) {
	if s.cmdInput == nil {
		return
	}
	s.cmdInput.SetText(s.cmdInput.GetText() + string(r))
}

func (s *appState) backspaceCommandRune() {
	if s.cmdInput == nil {
		return
	}
	text := []rune(s.cmdInput.GetText())
	if len(text) == 0 {
		return
	}
	s.cmdInput.SetText(string(text[:len(text)-1]))
}

func (s *appState) setError(action, target string, err error) {
	if err == nil {
		return
	}
	friendly := err.Error()
	if os.IsPermission(err) {
		friendly = "permission denied"
	} else if os.IsNotExist(err) {
		friendly = "not found"
	}
	s.setStatus(fmt.Sprintf("%s %s: %s", action, target, friendly))
}

func (s *appState) activePanel() *panel {
	return s.panels[s.active]
}

func (s *appState) passivePanel() *panel {
	return s.panels[1-s.active]
}

func (s *appState) switchPanel() {
	s.active = 1 - s.active
	s.stylePanels()
	s.app.SetFocus(s.activePanel().list)
	s.updateInfoStatus()
}

func (s *appState) stylePanels() {
	for i, p := range s.panels {
		if i == s.active {
			p.list.SetBorderColor(mcAccent)
		} else {
			p.list.SetBorderColor(mcAccentMuted)
		}
		p.updateTitle()
	}
}

func (s *appState) updateInfoStatus() {
	a := s.activePanel()
	item, ok := a.selectedItem()
	if !ok {
		s.setStatus("No items")
		return
	}
	kind := "FILE"
	if item.isDir {
		kind = "DIR"
	}
	size := "-"
	if !item.isDir {
		size = fmt.Sprintf("%d", item.size)
	}
	s.setStatus(fmt.Sprintf("%s | %s | size=%s | modified=%s | selected=%d", kind, item.path, size, item.modTime.Format("2006-01-02 15:04:05"), len(a.marked)))
}

func (s *appState) handleFunctionKey(num int) {
	switch num {
	case 1:
		s.showHelp()
	case 2:
		s.showUserMenu()
	case 3:
		s.viewSelected()
	case 4:
		s.editSelected()
	case 5:
		s.copySelected()
	case 6:
		s.moveSelected()
	case 7:
		s.makeDirectory()
	case 8:
		s.deleteSelected()
	case 9:
		s.showTopMenu()
	case 10:
		s.app.Stop()
	}
}

func (s *appState) closeOverlay(name string) {
	if name == "shell" {
		s.pages.SwitchToPage("main")
	} else {
		s.pages.RemovePage(name)
	}
	s.resetEscState()
	s.app.SetFocus(s.activePanel().list)
}

func (s *appState) showOverlay(name string, primitive tview.Primitive, focus tview.Primitive) {
	s.resetEscState()
	s.pages.AddAndSwitchToPage(name, primitive, true)
	s.app.SetFocus(focus)
}

func (s *appState) overlayVisible() bool {
	name, _ := s.pages.GetFrontPage()
	return name != "main"
}

func (s *appState) appendShellOutput(text string) {
	s.shellMu.Lock()
	defer s.shellMu.Unlock()
	if s.shellView == nil {
		return
	}
	s.shellView.Write([]byte(text))
	s.shellView.ScrollToEnd()
}

func (s *appState) shellPageVisible() bool {
	name, _ := s.pages.GetFrontPage()
	return name == "shell"
}

func (s *appState) startShell() error {
	shell := os.Getenv("SHELL")
	if strings.TrimSpace(shell) == "" {
		if runtime.GOOS == "windows" {
			shell = os.Getenv("COMSPEC")
		}
		if strings.TrimSpace(shell) == "" {
			shell = "sh"
		}
	}
	cmd := exec.Command(shell)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	s.shellCmd = cmd
	s.shellIn = stdin
	go s.streamShellOutput(stdout)
	go s.streamShellOutput(stderr)
	return nil
}

func (s *appState) streamShellOutput(r io.Reader) {
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			content := string(buf[:n])
			s.app.QueueUpdateDraw(func() {
				s.appendShellOutput(content)
			})
		}
		if err != nil {
			return
		}
	}
}

func (s *appState) showShellPage() {
	if s.shellView == nil {
		s.shellView = tview.NewTextView().
			SetDynamicColors(true).
			SetScrollable(true).
			SetWrap(true)
		s.shellView.SetBorder(true)
		s.shellView.SetTitle(" Shell (Ctrl+O to return) ")
		s.shellView.SetBackgroundColor(mcDialogBackground)
		s.shellView.SetBorderColor(mcAccent)
		s.shellInput = tview.NewInputField().SetLabel("$ ")
		s.shellInput.SetFieldBackgroundColor(mcPanelBackground)
		s.shellInput.SetFieldTextColor(tcell.ColorWhite)
		s.shellInput.SetLabelColor(tcell.ColorWhite)
		s.shellInput.SetDoneFunc(func(key tcell.Key) {
			switch key {
			case tcell.KeyEnter:
				cmd := strings.TrimSpace(s.shellInput.GetText())
				s.shellInput.SetText("")
				if cmd != "" {
					s.appendShellOutput("$ " + cmd + "\n")
					if s.shellIn != nil {
						_, _ = io.WriteString(s.shellIn, cmd+"\n")
					}
				}
			case tcell.KeyCtrlO, tcell.KeyEsc:
				s.closeOverlay("shell")
			}
		})
		layout := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(s.shellView, 0, 1, false).
			AddItem(s.shellInput, 1, 0, true)
		layout.SetBorder(true)
		layout.SetTitle(" Parallel Shell ")
		layout.SetBorderColor(mcAccent)
		layout.SetBackgroundColor(mcDialogBackground)
		layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyCtrlO {
				s.closeOverlay("shell")
				return nil
			}
			return event
		})
		s.pages.AddPage("shell", layout, true, false)
		s.appendShellOutput("Shell preloaded. Type commands and press Enter.\n")
	}
	s.resetEscState()
	s.pages.ShowPage("shell")
	s.pages.SwitchToPage("shell")
	s.app.SetFocus(s.shellInput)
}

func (s *appState) toggleShellPage() {
	if s.shellPageVisible() {
		s.closeOverlay("shell")
		s.setStatus("Returned to panels")
		return
	}
	s.showShellPage()
	s.setStatus("Shell active (Ctrl+O to return)")
}

func (s *appState) resetEscState() {
	s.escPending = false
	s.passEsc = false
	if s.escTimer != nil {
		s.escTimer.Stop()
		s.escTimer = nil
	}
}

func centered(width, height int, p tview.Primitive) tview.Primitive {
	return tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(p, width, 1, true).
			AddItem(nil, 0, 1, false), height, 1, true).
		AddItem(nil, 0, 1, false)
}

func (s *appState) showConfirm(title, msg string, onYes func()) {
	name := fmt.Sprintf("confirm-%d", time.Now().UnixNano())
	modal := tview.NewModal()
	modal.SetText(msg)
	modal.AddButtons([]string{"Cancel", "OK"})
	modal.SetTitle(" " + title + " ")
	modal.SetBackgroundColor(mcDialogBackground)
	modal.SetBorderColor(mcAccent)
	modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyESC {
			s.closeOverlay(name)
			return nil
		}
		return event
	})
	modal.SetDoneFunc(func(_ int, label string) {
		s.closeOverlay(name)
		if label == "OK" && onYes != nil {
			onYes()
		}
	})
	s.showOverlay(name, modal, modal)
}

func (s *appState) showInput(title, label, initial string, onOK func(string)) {
	name := fmt.Sprintf("input-%d", time.Now().UnixNano())
	input := tview.NewInputField().SetLabel(label).SetText(initial)
	submit := func() {
		value := strings.TrimSpace(input.GetText())
		s.closeOverlay(name)
		onOK(value)
	}
	form := tview.NewForm().
		AddFormItem(input).
		AddButton("OK", submit).
		AddButton("Cancel", func() {
			s.closeOverlay(name)
		})
	form.SetBorder(true)
	form.SetTitle(" " + title + " ")
	form.SetButtonsAlign(tview.AlignCenter)
	form.SetBackgroundColor(mcDialogBackground)
	form.SetBorderColor(mcAccent)
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyESC {
			s.closeOverlay(name)
			return nil
		}
		return event
	})
	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			submit()
		case tcell.KeyESC:
			s.closeOverlay(name)
		}
	})
	input.SetFieldBackgroundColor(mcPanelBackground)
	input.SetFieldTextColor(tcell.ColorWhite)
	input.SetLabelColor(tcell.ColorWhite)
	s.showOverlay(name, centered(70, 9, form), input)
}

func (s *appState) showText(title, body string) {
	name := fmt.Sprintf("text-%d", time.Now().UnixNano())
	text := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(false)
	text.SetBorder(true)
	text.SetTitle(" " + title + " ")
	text.SetBackgroundColor(mcDialogBackground)
	text.SetBorderColor(mcAccent)
	text.SetTitleColor(tcell.ColorWhite)
	text.SetText(body)
	text.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape || key == tcell.KeyEnter || key == tcell.KeyF10 {
			s.closeOverlay(name)
		}
	})
	frame := tview.NewFrame(text).
		AddText("Esc/Enter/F10: close", true, tview.AlignCenter, tcell.ColorGray)
	s.showOverlay(name, frame, text)
}

func (s *appState) refreshPanels() {
	for _, p := range s.panels {
		if err := p.refresh(); err != nil {
			s.setError("Refresh", p.path, err)
		}
	}
	s.stylePanels()
	s.updateInfoStatus()
}

func (s *appState) viewSelected() {
	a := s.activePanel()
	item, ok := a.selectedItem()
	if !ok || item.isDir || item.name == ".." {
		s.setStatus("F3 view requires a file")
		return
	}
	f, err := os.Open(item.path)
	if err != nil {
		s.setError("View", item.path, err)
		return
	}
	defer f.Close()
	content, readErr := io.ReadAll(io.LimitReader(f, maxViewSize+1))
	if readErr != nil {
		s.setError("View", item.path, readErr)
		return
	}
	truncated := len(content) > maxViewSize
	if truncated {
		content = content[:maxViewSize]
	}
	body := string(content)
	if truncated {
		body += "\n\n[truncated]"
	}
	s.showText("Viewer: "+item.path, body)
}

func splitCommandLine(command string) ([]string, error) {
	var parts []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
	}
	for _, r := range command {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && !inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case (r == ' ' || r == '\t') && !inSingle && !inDouble:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	if escaped || inSingle || inDouble {
		return nil, fmt.Errorf("invalid quoting in command")
	}
	flush()
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return parts, nil
}

func runExternalAttached(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runExternalDetached(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func isPathInside(base, candidate string) bool {
	baseAbs, baseErr := filepath.Abs(base)
	candidateAbs, candidateErr := filepath.Abs(candidate)
	if baseErr != nil || candidateErr != nil {
		return false
	}
	rel, err := filepath.Rel(baseAbs, candidateAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func (s *appState) editSelected() {
	a := s.activePanel()
	item, ok := a.selectedItem()
	if !ok || item.isDir || item.name == ".." {
		s.setStatus("F4 edit requires a file")
		return
	}
	editor := os.Getenv("EDITOR")
	if strings.TrimSpace(editor) == "" {
		editor = "vi"
	}
	parts, splitErr := splitCommandLine(editor)
	if splitErr != nil {
		s.setError("Edit", item.path, splitErr)
		return
	}
	var cmdErr error
	s.app.Suspend(func() {
		cmdErr = runExternalAttached(parts[0], append(parts[1:], item.path)...)
	})
	if cmdErr != nil {
		s.setError("Edit", item.path, cmdErr)
		return
	}
	s.refreshPanels()
	s.setStatus("Edited: " + item.path)
}

func openFileDefault(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return runExternalDetached("open", path)
	case "windows":
		return runExternalDetached("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		return runExternalDetached("xdg-open", path)
	}
}

func (s *appState) openSelected() {
	a := s.activePanel()
	item, ok := a.selectedItem()
	if !ok {
		return
	}
	if item.isDir {
		if err := a.goIntoSelected(); err != nil {
			s.setError("Open", item.path, err)
			return
		}
		s.updateInfoStatus()
		return
	}
	if cmdErr := openFileDefault(item.path); cmdErr != nil {
		s.setError("Launch", item.path, cmdErr)
		return
	}
	s.setStatus("Launched: " + item.path)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func copyPath(src, dst string) error {
	if src == dst {
		return fmt.Errorf("source and destination are identical")
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() && isPathInside(src, dst) {
		return fmt.Errorf("destination is inside source")
	}
	if dstInfo, statErr := os.Lstat(dst); statErr == nil {
		if dstInfo.IsDir() && dstInfo.Mode()&os.ModeSymlink == 0 {
			if err := os.RemoveAll(dst); err != nil {
				return err
			}
		} else if err := os.Remove(dst); err != nil {
			return err
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, linkErr := os.Readlink(src)
		if linkErr != nil {
			return linkErr
		}
		tmpDst := fmt.Sprintf("%s.nmc-tmp-%d", dst, time.Now().UnixNano())
		if err := os.Symlink(target, tmpDst); err != nil {
			return err
		}
		if _, err := os.Lstat(dst); err == nil {
			_ = os.Remove(tmpDst)
			return fmt.Errorf("destination changed during copy")
		} else if !os.IsNotExist(err) {
			_ = os.Remove(tmpDst)
			return err
		}
		if err := os.Rename(tmpDst, dst); err != nil {
			_ = os.Remove(tmpDst)
			return err
		}
		return nil
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			childSrc := filepath.Join(src, entry.Name())
			childDst := filepath.Join(dst, entry.Name())
			if err := copyPath(childSrc, childDst); err != nil {
				return err
			}
		}
		return nil
	}
	return copyFile(src, dst, info.Mode().Perm())
}

func movePath(src, dst string) error {
	if src == dst {
		return fmt.Errorf("source and destination are identical")
	}
	if srcInfo, err := os.Lstat(src); err == nil && srcInfo.IsDir() && isPathInside(src, dst) {
		return fmt.Errorf("destination is inside source")
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else {
		if !isCrossDeviceRenameError(err) {
			return err
		}
	}
	if dstInfo, err := os.Lstat(dst); err == nil {
		if dstInfo.IsDir() && dstInfo.Mode()&os.ModeSymlink == 0 {
			if err := os.RemoveAll(dst); err != nil {
				return err
			}
		} else if err := os.Remove(dst); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := copyPath(src, dst); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(src)
	}
	return os.Remove(src)
}

func isCrossDeviceRenameError(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) && errors.Is(linkErr.Err, syscall.EXDEV) {
		return true
	}
	if runtime.GOOS == "windows" {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "not same device") || strings.Contains(msg, "cross-device") {
			return true
		}
	}
	return false
}

func (s *appState) copySelected() {
	a := s.activePanel()
	b := s.passivePanel()
	targets := a.selectedPaths()
	if len(targets) == 0 {
		s.setStatus("F5 copy: nothing selected")
		return
	}
	copied := 0
	for _, src := range targets {
		dst := filepath.Join(b.path, filepath.Base(src))
		if src == dst {
			continue
		}
		if err := copyPath(src, dst); err != nil {
			s.setError("Copy", src, err)
			return
		}
		copied++
	}
	a.clearSelection()
	s.refreshPanels()
	s.setStatus(fmt.Sprintf("Copied %d item(s) to %s", copied, b.path))
}

func (s *appState) moveSelected() {
	a := s.activePanel()
	b := s.passivePanel()
	targets := a.selectedPaths()
	if len(targets) == 0 {
		s.setStatus("F6 move: nothing selected")
		return
	}
	moved := 0
	for _, src := range targets {
		dst := filepath.Join(b.path, filepath.Base(src))
		if src == dst {
			continue
		}
		if err := movePath(src, dst); err != nil {
			s.setError("Move", src, err)
			return
		}
		moved++
	}
	a.clearSelection()
	s.refreshPanels()
	s.setStatus(fmt.Sprintf("Moved %d item(s) to %s", moved, b.path))
}

func (s *appState) makeDirectory() {
	a := s.activePanel()
	s.showInput("Create directory", "Name: ", "", func(name string) {
		if name == "" {
			s.setStatus("F7 canceled")
			return
		}
		if name == "." || name == ".." || filepath.Base(name) != name || strings.Contains(name, "/") || strings.Contains(name, `\`) {
			s.setStatus("F7 requires a single directory name")
			return
		}
		target := filepath.Join(a.path, name)
		if err := os.Mkdir(target, 0o755); err != nil {
			s.setError("Mkdir", target, err)
			return
		}
		s.refreshPanels()
		s.setStatus("Created: " + target)
	})
}

func (s *appState) deleteSelected() {
	a := s.activePanel()
	targets := a.selectedPaths()
	if len(targets) == 0 {
		s.setStatus("F8 delete: nothing selected")
		return
	}
	msg := fmt.Sprintf("Delete %d item(s)?", len(targets))
	s.showConfirm("Delete", msg, func() {
		for _, path := range targets {
			info, err := os.Lstat(path)
			if err != nil {
				s.setError("Delete", path, err)
				return
			}
			if info.IsDir() {
				err = os.RemoveAll(path)
			} else {
				err = os.Remove(path)
			}
			if err != nil {
				s.setError("Delete", path, err)
				return
			}
		}
		a.clearSelection()
		s.refreshPanels()
		s.setStatus(fmt.Sprintf("Deleted %d item(s)", len(targets)))
	})
}

func (s *appState) goToPath() {
	a := s.activePanel()
	s.showInput("Go to path", "Path: ", a.path, func(value string) {
		if value == "" {
			return
		}
		if err := a.load(value, 0, true); err != nil {
			s.setError("Go to", value, err)
			return
		}
		s.updateInfoStatus()
	})
}

func (s *appState) promptSearch() {
	s.showInput("Search", "Pattern: ", "", func(value string) {
		query := strings.ToLower(strings.TrimSpace(value))
		if query == "" {
			return
		}
		if ok := s.activePanel().search(query); !ok {
			s.setStatus("Search: no match")
			return
		}
		s.updateInfoStatus()
	})
}

func (s *appState) showSortMenu() {
	options := []struct {
		label string
		mode  sortMode
	}{
		{"Sort by name", sortByName},
		{"Sort by extension", sortByExt},
		{"Sort by modified time", sortByTime},
		{"Sort by size", sortBySize},
	}
	name := fmt.Sprintf("sort-%d", time.Now().UnixNano())
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true)
	list.SetTitle(" Sort menu ")
	list.SetBackgroundColor(mcDialogBackground)
	list.SetBorderColor(mcAccent)
	list.SetMainTextColor(tcell.ColorWhite)
	list.SetSelectedBackgroundColor(mcAccent)
	list.SetSelectedTextColor(tcell.ColorBlack)
	for _, opt := range options {
		opt := opt
		list.AddItem(opt.label, "", 0, func() {
			a := s.activePanel()
			a.sortMode = opt.mode
			_ = a.refresh()
			s.stylePanels()
			s.updateInfoStatus()
			s.closeOverlay(name)
		})
	}
	current := int(s.activePanel().sortMode)
	if current >= 0 && current < len(options) {
		list.SetCurrentItem(current)
	}
	list.SetDoneFunc(func() { s.closeOverlay(name) })
	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyESC {
			s.closeOverlay(name)
			return nil
		}
		return event
	})
	s.showOverlay(name, centered(44, 11, list), list)
}

func (s *appState) showHelp() {
	help := strings.Join([]string{
		"nmc key reference",
		"",
		"Tab/Shift+Tab: switch panel",
		"Arrows/PgUp/PgDn/Home/End: navigate",
		"Enter/Right: open directory or launch file",
		"Left: go to parent directory",
		"Space/Insert: mark item",
		"+: select all    -: clear selection    *: invert selection",
		"Ctrl+R: refresh   Ctrl+S: search prompt   Ctrl+H: toggle hidden",
		"Ctrl+G: go to path    [: history back    ]: history forward",
		"Ctrl+O: toggle parallel shell",
		"Type to command line (bottom), Enter runs command if not empty",
		"F9: top menu / sort selector",
		"F3 view  F4 edit  F5 copy  F6 move  F7 mkdir  F8 delete  F10 quit",
		"ESC+1..0 maps to F1..F10",
		"",
		"Advanced features not implemented: VFS/plugins/network integration/background jobs.",
	}, "\n")
	s.showText("Help", help)
}

func (s *appState) showUserMenu() {
	menu := strings.Join([]string{
		"User menu",
		"",
		"Use function keys and shortcuts:",
		"- F3/F4/F5/F6/F7/F8/F10",
		"- Ctrl+R, Ctrl+S(search), Ctrl+H, Ctrl+G, Ctrl+O",
		"- Selection with Space/Insert/+/-/*",
		"- Type to bottom command line, press Enter to send to shell",
	}, "\n")
	s.showText("F2 User Menu", menu)
}

func (s *appState) showTopMenu() {
	s.showSortMenu()
}

func (s *appState) dispatchStandaloneEsc() {
	s.escPending = false
	s.passEsc = true
	s.setStatus("ESC")
	s.app.QueueEvent(tcell.NewEventKey(tcell.KeyESC, 0, tcell.ModNone))
}

func (s *appState) handleEscSequence(event *tcell.EventKey) bool {
	if !s.escPending {
		return false
	}
	if s.escTimer != nil {
		s.escTimer.Stop()
	}
	if event.Key() == tcell.KeyRune {
		r := event.Rune()
		if r >= '1' && r <= '9' {
			s.escPending = false
			s.handleFunctionKey(int(r - '0'))
			return true
		}
		if r == '0' {
			s.escPending = false
			s.handleFunctionKey(10)
			return true
		}
	}
	s.dispatchStandaloneEsc()
	return false
}

func (s *appState) keyHandler(event *tcell.EventKey) *tcell.EventKey {
	if s.overlayVisible() {
		if event.Key() == tcell.KeyRune && event.Rune() == escTimeoutRune {
			return nil
		}
		s.resetEscState()
		return event
	}

	if event.Key() == tcell.KeyRune && event.Rune() == escTimeoutRune {
		if s.escPending {
			s.dispatchStandaloneEsc()
		}
		return nil
	}
	if s.handleEscSequence(event) {
		return nil
	}

	a := s.activePanel()

	switch event.Key() {
	case tcell.KeyESC:
		if s.passEsc {
			s.passEsc = false
			return event
		}
		s.escPending = true
		s.setStatus("ESC detected: press 1-0 for F1-F10")
		if s.escTimer != nil {
			s.escTimer.Stop()
		}
		s.escTimer = time.AfterFunc(700*time.Millisecond, func() {
			s.app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, escTimeoutRune, tcell.ModNone))
		})
		return nil
	case tcell.KeyTAB, tcell.KeyBacktab:
		s.switchPanel()
		return nil
	case tcell.KeyUp:
		a.move(-1)
		s.updateInfoStatus()
		return nil
	case tcell.KeyDown:
		a.move(1)
		s.updateInfoStatus()
		return nil
	case tcell.KeyPgUp:
		a.pageMove(-1)
		s.updateInfoStatus()
		return nil
	case tcell.KeyPgDn:
		a.pageMove(1)
		s.updateInfoStatus()
		return nil
	case tcell.KeyHome:
		a.list.SetCurrentItem(0)
		s.updateInfoStatus()
		return nil
	case tcell.KeyEnd:
		if len(a.items) > 0 {
			a.list.SetCurrentItem(len(a.items) - 1)
			s.updateInfoStatus()
		}
		return nil
	case tcell.KeyRight, tcell.KeyEnter:
		if event.Key() == tcell.KeyEnter && s.cmdInput != nil && strings.TrimSpace(s.cmdInput.GetText()) != "" {
			s.executeCommandLine()
			return nil
		}
		s.openSelected()
		return nil
	case tcell.KeyLeft:
		if err := a.goParentViaList(); err != nil {
			s.setError("Open", filepath.Dir(a.path), err)
		} else {
			s.updateInfoStatus()
		}
		return nil
	case tcell.KeyCtrlR:
		s.refreshPanels()
		s.setStatus("Refreshed")
		return nil
	case tcell.KeyCtrlS:
		s.promptSearch()
		return nil
	case tcell.KeyCtrlH:
		a.toggleHidden()
		s.stylePanels()
		s.updateInfoStatus()
		return nil
	case tcell.KeyCtrlG:
		s.goToPath()
		return nil
	case tcell.KeyCtrlO:
		s.toggleShellPage()
		return nil
	case tcell.KeyInsert:
		a.toggleMarkCurrent()
		s.stylePanels()
		s.updateInfoStatus()
		return nil
	case tcell.KeyBackspace2:
		s.backspaceCommandRune()
		return nil
	case tcell.KeyF1, tcell.KeyF2, tcell.KeyF3, tcell.KeyF4, tcell.KeyF5,
		tcell.KeyF6, tcell.KeyF7, tcell.KeyF8, tcell.KeyF9, tcell.KeyF10:
		n := int(event.Key()-tcell.KeyF1) + 1
		s.handleFunctionKey(n)
		return nil
	}

	if event.Key() == tcell.KeyRune {
		s.escPending = false
		r := event.Rune()
		switch r {
		case ' ':
			a.toggleMarkCurrent()
			s.stylePanels()
			s.updateInfoStatus()
			return nil
		case '+':
			a.selectAll()
			s.stylePanels()
			s.updateInfoStatus()
			return nil
		case '-':
			a.clearSelection()
			s.stylePanels()
			s.updateInfoStatus()
			return nil
		case '*':
			a.invertSelection()
			s.stylePanels()
			s.updateInfoStatus()
			return nil
		case '[':
			if err := a.historyBack(); err != nil {
				s.setError("History", a.path, err)
			} else {
				s.updateInfoStatus()
			}
			return nil
		case ']':
			if err := a.historyForward(); err != nil {
				s.setError("History", a.path, err)
			} else {
				s.updateInfoStatus()
			}
			return nil
		}
		if r >= 32 && r != '+' && r != '-' && r != '*' && r != '[' && r != ']' {
			s.appendCommandRune(r)
			return nil
		}
	}

	return event
}

func run() error {
	app := tview.NewApplication()
	tview.Styles.PrimitiveBackgroundColor = mcBackground
	tview.Styles.ContrastBackgroundColor = mcPanelBackground
	tview.Styles.MoreContrastBackgroundColor = mcDialogBackground
	tview.Styles.BorderColor = mcAccent
	tview.Styles.TitleColor = tcell.ColorWhite
	tview.Styles.GraphicsColor = mcAccent
	tview.Styles.PrimaryTextColor = tcell.ColorWhite
	tview.Styles.SecondaryTextColor = tcell.ColorLightCyan

	left := newPanel("Left")
	right := newPanel("Right")

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if err := left.load(cwd, 0, true); err != nil {
		return err
	}
	if err := right.load(cwd, 0, true); err != nil {
		return err
	}

	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" Tab switch | Type goes to cmd line; Enter runs cmd when non-empty | F3/F4/F5/F6/F7/F8/F10 | Ctrl+R refresh Ctrl+S search Ctrl+O shell F9 sort ")
	status.SetBorder(true)
	status.SetTitle(" Keys/Status ")
	status.SetBackgroundColor(mcPanelBackground)
	status.SetBorderColor(mcAccent)
	status.SetTextColor(tcell.ColorWhite)
	status.SetTitleColor(tcell.ColorWhite)

	cmdInput := tview.NewInputField().SetLabel(" cmd> ")
	cmdInput.SetFieldBackgroundColor(mcPanelBackground)
	cmdInput.SetFieldTextColor(tcell.ColorWhite)
	cmdInput.SetLabelColor(tcell.ColorWhite)
	cmdInput.SetBorder(true)
	cmdInput.SetTitle(" Command line ")
	cmdInput.SetBorderColor(mcAccentMuted)

	panels := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(left.list, 0, 1, true).
		AddItem(right.list, 0, 1, false)

	root := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(panels, 0, 1, true).
		AddItem(status, 3, 0, false).
		AddItem(cmdInput, 3, 0, false)

	pages := tview.NewPages().
		AddPage("main", root, true, true)

	state := &appState{
		app:      app,
		pages:    pages,
		panels:   [2]*panel{left, right},
		status:   status,
		cmdInput: cmdInput,
	}

	cmdInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			state.executeCommandLine()
		case tcell.KeyEsc:
			app.SetFocus(state.activePanel().list)
		}
	})

	left.list.SetChangedFunc(func(int, string, string, rune) { state.updateInfoStatus() })
	right.list.SetChangedFunc(func(int, string, string, rune) { state.updateInfoStatus() })

	state.stylePanels()
	state.updateInfoStatus()
	if err := state.startShell(); err != nil {
		state.setStatus("Shell preload failed: " + err.Error())
	}

	app.SetInputCapture(state.keyHandler)
	return app.SetRoot(pages, true).SetFocus(left.list).Run()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nmc failed:", err)
		os.Exit(1)
	}
}
