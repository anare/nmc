package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/anare/nmc/internal/clipboard"
	"github.com/anare/nmc/internal/dialog"
	"github.com/anare/nmc/internal/editor"
	"github.com/anare/nmc/internal/fileop"
	"github.com/anare/nmc/internal/find"
	"github.com/anare/nmc/internal/git"
	"github.com/anare/nmc/internal/history"
	"github.com/anare/nmc/internal/keymap"
	"github.com/anare/nmc/internal/panel"
	"github.com/anare/nmc/internal/shell"
	"github.com/anare/nmc/internal/ui"
	"github.com/anare/nmc/internal/vfs"
	"github.com/anare/nmc/internal/viewer"
	"github.com/gdamore/tcell/v2"
)

const (
	minWidth  = 40
	minHeight = 10
)

type App struct {
	screen       tcell.Screen
	panels       [2]*panel.Panel
	active       int
	theme        ui.Theme
	status       string
	quit         bool
	ctrlXPending bool
	dialog       *dialog.Dialog
	cmd          *dialog.TextInput
	output       *shell.Buffer
	opCancel     context.CancelFunc
	viewer       *viewer.Viewer
	editor       *editor.Editor
	history      *history.Store
	cmdHistory   *history.Cursor
	keys         *keymap.Map
	configPath   string
	lineNumbers  bool
	keyTest      bool
	keyTestEsc   bool
	keyTestText  string
	keyCapture   *bindingCapture
	job          *shell.Job
	commandCtx   context.CancelFunc
	session      *shell.Session
	sessionCtx   context.CancelFunc
	shellVisible bool
	findState    *findState
	clipboard    *clipboard.Clipboard
}

func New(screen tcell.Screen, fs vfs.FS, cwd string) (*App, error) {
	if screen == nil || fs == nil {
		return nil, os.ErrInvalid
	}
	keys := keymap.New()
	configPath := ""
	if home, err := os.UserHomeDir(); err == nil {
		configPath = filepath.Join(home, ".bcrc")
		keys, _ = keymap.Load(configPath)
	}
	settings := keys.Settings()
	options := vfs.Options{ShowHidden: !strings.EqualFold(settings["hidden-files"], "off")}
	if sortMode, ok := vfs.ParseSortMode(settings["sort-order"]); ok {
		options.SortMode = sortMode
	}
	left, err := panel.New(fs, cwd, options)
	if err != nil {
		return nil, err
	}
	right, err := panel.New(fs, cwd, options)
	if err != nil {
		return nil, err
	}
	a := &App{
		screen: screen, panels: [2]*panel.Panel{left, right}, theme: ui.DefaultTheme(),
		cmd: dialog.NewTextInput(""), output: shell.NewBuffer(1 << 20), clipboard: clipboard.New(),
		keys: keys, configPath: configPath,
		lineNumbers: strings.EqualFold(settings["line-numbers"], "on"),
	}
	if store, err := history.OpenFromEnvironment(); err == nil {
		a.history = store
		a.cmdHistory = store.Cursor(history.Command)
	}
	return a, nil
}

func (a *App) Run() error {
	if err := a.screen.Init(); err != nil {
		return err
	}
	defer a.screen.Fini()
	defer func() {
		if a.commandCtx != nil {
			a.commandCtx()
		}
		if a.opCancel != nil {
			a.opCancel()
		}
		if a.sessionCtx != nil {
			a.sessionCtx()
		}
		if a.session != nil {
			_ = a.session.Close()
		}
		if a.clipboard != nil {
			_ = a.clipboard.Close()
		}
	}()
	for !a.quit {
		a.Draw()
		switch event := a.screen.PollEvent().(type) {
		case *tcell.EventKey:
			a.HandleKey(event)
		case *tcell.EventResize:
			a.screen.Sync()
		case *tcell.EventInterrupt:
			switch update := event.Data().(type) {
			case operationProgress:
				if a.dialog != nil {
					a.dialog.SetProgress(update.progress.Percent(), fmt.Sprintf("%s — %s", update.progress.Op, update.progress.File))
				}
			case operationFinished:
				a.finishOperation(update.result)
			case commandFinished:
				a.finishCommand(update.err)
			case shellSessionFinished:
				a.session = nil
				a.shellVisible = false
				if a.sessionCtx != nil {
					a.sessionCtx()
					a.sessionCtx = nil
				}
				a.status = "Shell session ended"
			case shellOutputUpdate:
			case findHitEvent:
				if a.findState != nil {
					a.findState.hits = append(a.findState.hits, update.hit)
				}
			case findDoneEvent:
				if a.findState != nil {
					a.findState.searching = false
					a.findState.err = update.err
				}
			}
		}
	}
	return nil
}

func (a *App) Draw() {
	width, height := a.screen.Size()
	a.screen.Clear()
	if width < minWidth || height < minHeight {
		message := fmt.Sprintf("Terminal too small (%dx%d); need at least %dx%d", width, height, minWidth, minHeight)
		ui.TextCenter(a.screen, 0, height/2, width, message, a.theme.Error)
		a.screen.Show()
		return
	}
	if a.shellVisible && a.session != nil {
		a.drawShell(width, height)
		a.screen.Show()
		return
	}
	if a.keyTest {
		a.drawKeyTester(width, height)
		a.screen.Show()
		return
	}
	if a.keyCapture != nil {
		ui.TextCenter(a.screen, 0, height/2, width, "Press a key to bind; Esc cancels", a.theme.Selection)
		a.screen.Show()
		return
	}
	if a.findState != nil {
		a.drawFind(width, height)
		a.screen.Show()
		return
	}
	if a.viewer != nil {
		a.viewer.Draw(a.screen, ui.Rect{X: 0, Y: 0, W: width, H: height - 2}, a.theme)
		ui.TextPadded(a.screen, 0, height-2, width, a.viewer.Status(), a.theme.Panel)
		a.drawHints(height)
		if a.dialog != nil {
			a.dialog.Draw(a.screen, a.theme)
		}
		a.screen.Show()
		return
	}
	if a.editor != nil {
		a.editor.Draw(a.screen, ui.Rect{X: 0, Y: 0, W: width, H: height - 2}, a.theme)
		status := ""
		if a.editor.Changed {
			status = "Modified"
		}
		ui.TextPadded(a.screen, 0, height-2, width, status, a.theme.Panel)
		a.drawHints(height)
		if a.dialog != nil {
			a.dialog.Draw(a.screen, a.theme)
		}
		a.screen.Show()
		return
	}

	panelHeight := height - 3
	leftWidth := width / 2
	rightWidth := width - leftWidth
	a.panels[0].SetRect(0, 0, leftWidth, panelHeight)
	a.panels[1].SetRect(leftWidth, 0, rightWidth, panelHeight)
	a.panels[0].Draw(a.screen, a.theme)
	a.panels[1].Draw(a.screen, a.theme)
	ui.Text(a.screen, 0, height-3, min(width, 16), a.panels[a.active].Cwd+">", a.theme.HintKey)
	promptWidth := min(width, 16)
	a.cmd.Draw(a.screen, promptWidth, height-3, width-promptWidth, a.theme.Panel)

	statusStyle := a.theme.Panel
	if a.panels[a.active].Err != nil {
		statusStyle = a.theme.BrokenLink
		if a.status == "" {
			a.status = a.panels[a.active].Err.Error()
		}
	}
	ui.TextPadded(a.screen, 0, height-2, width, strings.TrimSpace(a.status), statusStyle)
	a.drawHints(height)
	if a.dialog != nil {
		a.dialog.Draw(a.screen, a.theme)
	}
	a.screen.Show()
}

func (a *App) drawHints(height int) {
	width, _ := a.screen.Size()
	var hints [10]string
	if a.shellVisible {
		hints[9] = "Panels"
	} else if a.findState != nil {
		hints[2], hints[4], hints[6], hints[9] = "Open", "Panelise", "Find again", "Quit"
	} else if a.editor != nil {
		hints[1], hints[3], hints[4], hints[7] = "Save", "Delete line", "Search", "Quit"
	} else if a.viewer != nil {
		hints[1], hints[2], hints[3], hints[4], hints[6], hints[7] = "Wrap", "Quit", "Hex", "Copy", "Search", "Raw"
	} else {
		hints[2], hints[3], hints[4], hints[5], hints[6], hints[7], hints[9] = "View", "Copy", "Move", "Mkdir", "Delete", "Refresh", "Quit"
	}
	ui.DrawHintBar(a.screen, height-1, width, a.theme, hints)
}

func (a *App) drawShell(width, height int) {
	_ = a.session.Resize(width, height-2)
	lines := a.session.Output()
	start := max(0, len(lines)-(height-2))
	for row, line := range lines[start:] {
		ui.TextPadded(a.screen, 0, row, width, stripANSI(line), a.theme.Panel)
	}
	ui.TextPadded(a.screen, 0, height-2, width, "Interactive shell — Ctrl-O returns to panels", a.theme.HintLabel)
	a.drawHints(height)
}

func stripANSI(value string) string {
	var result strings.Builder
	escape := false
	csi := false
	for _, char := range value {
		if escape {
			if csi {
				if char >= '@' && char <= '~' {
					escape, csi = false, false
				}
				continue
			}
			if char == '[' {
				csi = true
				continue
			}
			escape = false
			continue
		}
		if char == 0x1b {
			escape = true
			continue
		}
		if char == '\r' {
			continue
		}
		result.WriteRune(char)
	}
	return result.String()
}

func (a *App) toggleShell() {
	if a.session != nil {
		a.shellVisible = !a.shellVisible
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.sessionCtx = cancel
	session, err := shell.StartSession(ctx, shell.Default(), a.panels[a.active].Cwd, func() {
		_ = a.screen.PostEvent(tcell.NewEventInterrupt(shellOutputUpdate{}))
	})
	if err != nil {
		cancel()
		a.sessionCtx = nil
		a.dialog = dialog.NewMessage("Shell", []string{err.Error()}, true)
		return
	}
	a.session = session
	a.shellVisible = true
	go func() {
		_ = session.Wait()
		_ = a.screen.PostEvent(tcell.NewEventInterrupt(shellSessionFinished{}))
	}()
}

func (a *App) handleShellKey(event *tcell.EventKey) {
	if event.Key() == tcell.KeyCtrlO {
		a.shellVisible = false
		return
	}
	var data []byte
	switch event.Key() {
	case tcell.KeyEnter:
		data = []byte{'\r'}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		data = []byte{0x7f}
	case tcell.KeyTab:
		data = []byte{'\t'}
	case tcell.KeyUp:
		data = []byte("\x1b[A")
	case tcell.KeyDown:
		data = []byte("\x1b[B")
	case tcell.KeyRight:
		data = []byte("\x1b[C")
	case tcell.KeyLeft:
		data = []byte("\x1b[D")
	case tcell.KeyRune:
		if event.Modifiers()&tcell.ModCtrl != 0 {
			if event.Rune() >= 'a' && event.Rune() <= 'z' {
				data = []byte{byte(event.Rune() - 'a' + 1)}
			}
		} else if event.Modifiers()&tcell.ModAlt != 0 {
			data = append([]byte{0x1b}, []byte(string(event.Rune()))...)
		} else {
			data = []byte(string(event.Rune()))
		}
	case tcell.KeyCtrlC:
		data = []byte{0x03}
	case tcell.KeyCtrlD:
		data = []byte{0x04}
	case tcell.KeyCtrlZ:
		data = []byte{0x1a}
	}
	if len(data) > 0 {
		_, _ = a.session.Write(data)
	}
}

func (a *App) HandleKey(event *tcell.EventKey) {
	if a.keyCapture != nil {
		a.handleBindingCapture(event)
		return
	}
	if a.keyTest {
		a.handleKeyTester(event)
		return
	}
	if a.dialog != nil {
		done, result := a.dialog.HandleKey(event)
		if done {
			callback := a.dialog.OnDone
			a.dialog = nil
			if callback != nil {
				callback(result)
			}
		}
		return
	}
	if a.shellVisible {
		a.handleShellKey(event)
		return
	}
	if a.findState != nil {
		if action, ok := a.keys.Lookup(keymap.Find, event); ok {
			a.dispatchFindAction(action)
			return
		}
		a.handleFindKey(event)
		return
	}
	if a.viewer != nil {
		if action, ok := a.keys.Lookup(keymap.Viewer, event); ok {
			a.dispatchViewerAction(action)
			return
		}
		a.handleViewerKey(event)
		return
	}
	if a.editor != nil {
		if action, ok := a.keys.Lookup(keymap.Editor, event); ok {
			a.dispatchEditorAction(action)
			return
		}
		a.handleEditorKey(event)
		return
	}
	if a.ctrlXPending {
		a.ctrlXPending = false
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 's':
				a.promptLink(false)
			case 'l':
				a.promptLink(true)
			case 'e':
				a.editLink()
			default:
				a.status = "Ctrl-X: s symbolic link, l hard link, e edit link"
			}
		}
		return
	}
	a.status = ""
	active := a.panels[a.active]
	if event.Key() == tcell.KeyEnter && a.cmd.Empty() {
		handled, err := active.Enter(context.Background())
		if err != nil {
			a.status = err.Error()
		} else if !handled {
			a.status = "Selected item is not a directory"
		}
		return
	}
	if event.Key() == tcell.KeyEnter && !a.cmd.Empty() {
		a.runCommand(a.cmd.Text(), event.Modifiers()&tcell.ModAlt != 0)
		return
	}
	if event.Key() == tcell.KeyCtrlY {
		a.cmd.SetText("")
		return
	}
	if event.Key() == tcell.KeyCtrlC && a.job != nil {
		_ = a.job.Kill()
		a.status = "Stopping command"
		return
	}
	if event.Key() == tcell.KeyRune && event.Modifiers()&tcell.ModAlt != 0 && (event.Rune() == 'p' || event.Rune() == 'n') {
		if a.cmdHistory != nil {
			if event.Rune() == 'p' {
				a.cmd.SetText(a.cmdHistory.Prev(a.cmd.Text()))
			} else {
				a.cmd.SetText(a.cmdHistory.Next(a.cmd.Text()))
			}
		}
		return
	}
	if a.commandLineOwns(event) {
		a.cmd.HandleKey(event)
		return
	}
	if action, ok := a.keys.Lookup(keymap.Panel, event); ok {
		a.dispatchPanelAction(action)
		return
	}
	switch event.Key() {
	case tcell.KeyCtrlX:
		a.ctrlXPending = true
		a.status = "Ctrl-X: s symbolic link, l hard link, e edit link"
	case tcell.KeyTab, tcell.KeyBacktab:
		a.active = 1 - a.active
	case tcell.KeyUp:
		active.MoveBy(-1)
	case tcell.KeyDown:
		active.MoveBy(1)
	case tcell.KeyPgUp:
		active.PageBy(-1)
	case tcell.KeyPgDn:
		active.PageBy(1)
	case tcell.KeyHome:
		active.Home()
	case tcell.KeyEnd:
		active.End()
	case tcell.KeyEnter, tcell.KeyRight:
		handled, err := active.Enter(context.Background())
		if err != nil {
			a.status = err.Error()
		} else if !handled {
			a.status = "Selected item is not a directory"
		} else {
			a.status = ""
		}
	case tcell.KeyLeft:
		if err := active.GoUp(context.Background()); err != nil {
			a.status = err.Error()
		} else {
			a.status = ""
		}
	case tcell.KeyRune:
		if event.Modifiers() == tcell.ModNone && event.Rune() >= ' ' {
			a.cmd.HandleKey(event)
		}
	}
}

func (a *App) dispatchPanelAction(action string) {
	active := a.panels[a.active]
	switch action {
	case "view":
		if entry, ok := active.Selected(); ok && entry.IsUp {
			a.openGitHistory()
		} else {
			a.openViewer()
		}
	case "edit":
		a.openEditor()
	case "copy":
		a.promptOperation("Copy")
	case "move":
		a.promptOperation("Move")
	case "rename":
		a.rename()
	case "create-link":
		a.promptLink(false)
	case "edit-link":
		a.editLink()
	case "mkdir":
		a.promptMkdir()
	case "delete":
		a.confirmDelete()
	case "quit":
		a.quit = true
	case "mark":
		active.ToggleMark()
	case "refresh":
		if err := active.Reload(context.Background()); err != nil {
			a.status = err.Error()
		} else {
			a.status = "Directory refreshed"
		}
	case "open-shell":
		a.toggleShell()
	case "output-log":
		a.dialog = dialog.NewList("Command output", a.output.Lines())
	case "find-files":
		a.promptFind()
	case "git-history":
		a.openGitHistory()
	case "menu":
		a.openSettings()
	case "help":
		a.openShortcutList()
	case "toggle-hidden":
		a.toggleHidden()
	case "sort-order":
		a.cycleSortOrder()
	case "sync-other-panel":
		other := a.panels[1-a.active]
		if err := other.SetDir(context.Background(), active.Cwd, ""); err != nil {
			a.status = err.Error()
		}
	default:
		a.status = "Action is not available yet: " + action
	}
}

func (a *App) openSettings() {
	hidden := "on"
	if !a.panels[0].Opt.ShowHidden {
		hidden = "off"
	}
	lineNumbers := "off"
	if a.lineNumbers {
		lineNumbers = "on"
	}
	items := []string{
		"Shortcuts…",
		"Key tester",
		"Show hidden files: " + hidden,
		"Sort order: " + sortLabel(a.panels[0].Opt.SortMode),
		"Line numbers: " + lineNumbers,
		"Save all settings",
	}
	a.dialog = dialog.NewMenu("Settings", items, func(result dialog.Result) {
		if !result.OK() {
			return
		}
		switch result.Selected {
		case 0:
			a.openShortcutList()
		case 1:
			a.keyTest = true
			a.keyTestText = "Press a key to see how the app receives it. Esc twice returns."
		case 2:
			a.toggleHidden()
			a.openSettings()
		case 3:
			a.cycleSortOrder()
			a.openSettings()
		case 4:
			a.lineNumbers = !a.lineNumbers
			value := "off"
			if a.lineNumbers {
				value = "on"
			}
			a.keys.SetSetting("line-numbers", value)
			if a.viewer != nil {
				a.viewer.LineNumber = a.lineNumbers
			}
			if a.editor != nil {
				a.editor.LineNumber = a.lineNumbers
			}
			a.openSettings()
		case 5:
			a.saveSettings()
		}
	})
}

func sortLabel(mode vfs.SortMode) string {
	switch mode {
	case vfs.SortByExt:
		return "Extension"
	case vfs.SortBySize:
		return "Size"
	case vfs.SortByTime:
		return "Time"
	default:
		return "Name"
	}
}

func (a *App) toggleHidden() {
	show := !a.panels[0].Opt.ShowHidden
	value := "off"
	if show {
		value = "on"
	}
	a.keys.SetSetting("hidden-files", value)
	for _, panel := range a.panels {
		panel.Opt.ShowHidden = show
		if err := panel.Reload(context.Background()); err != nil {
			a.status = err.Error()
		}
	}
}

func (a *App) cycleSortOrder() {
	next := (a.panels[0].Opt.SortMode + 1) % 4
	a.keys.SetSetting("sort-order", sortLabel(next))
	for _, panel := range a.panels {
		panel.Opt.SortMode = next
		if err := panel.Reload(context.Background()); err != nil {
			a.status = err.Error()
		}
	}
}

func (a *App) saveSettings() {
	if err := a.persistSettings(); err != nil {
		a.dialog = dialog.NewMessage("Save settings", []string{err.Error()}, true)
		return
	}
	a.status = "Settings saved"
}

func (a *App) persistSettings() error {
	if a.configPath == "" {
		return errors.New("cannot determine the settings file path")
	}
	return a.keys.Save(a.configPath)
}

func (a *App) openShortcutList() {
	actions := append([]keymap.Action(nil), keymap.Defaults...)
	items := make([]string, 0, len(actions))
	for _, action := range actions {
		bindings := a.keys.Bindings(action.Mode, action.Name)
		items = append(items, fmt.Sprintf("%-7s %-22s %s", action.Mode, action.Describe, strings.Join(bindings, ", ")))
	}
	a.dialog = dialog.NewMenu("Shortcuts", items, func(result dialog.Result) {
		if result.OK() && result.Selected < len(actions) {
			a.openShortcutActions(actions[result.Selected])
		}
	})
}

func (a *App) openShortcutActions(action keymap.Action) {
	items := []string{"Bind a key", "Unbind", "Restore defaults", "Cancel"}
	a.dialog = dialog.NewMenu("Shortcut: "+action.Describe, items, func(result dialog.Result) {
		if !result.OK() {
			return
		}
		switch result.Selected {
		case 0:
			a.keyCapture = &bindingCapture{mode: action.Mode, action: action.Name}
		case 1:
			a.keys.Unbind(action.Mode, action.Name)
			a.saveSettings()
		case 2:
			var bindings []keymap.Binding
			for _, spelling := range action.Defaults {
				binding, err := keymap.Parse(spelling)
				if err != nil {
					a.status = err.Error()
					return
				}
				bindings = append(bindings, binding)
			}
			if err := a.keys.Rebind(action.Mode, action.Name, bindings...); err != nil {
				a.dialog = dialog.NewMessage("Restore shortcut", []string{err.Error()}, true)
				return
			}
			a.saveSettings()
		}
	})
}

func (a *App) handleBindingCapture(event *tcell.EventKey) {
	capture := a.keyCapture
	a.keyCapture = nil
	if event.Key() == tcell.KeyEscape {
		a.status = "Shortcut capture cancelled"
		return
	}
	binding := keymap.FromEvent(event)
	previous := a.keys.Owner(capture.mode, binding)
	if err := a.keys.Rebind(capture.mode, capture.action, binding); err != nil {
		a.dialog = dialog.NewMessage("Bind shortcut", []string{err.Error()}, true)
		return
	}
	if previous != "" && previous != capture.action {
		a.status = fmt.Sprintf("%s now uses %s; %s lost that key", capture.action, binding.String(), previous)
	} else {
		a.status = fmt.Sprintf("%s bound to %s", capture.action, binding.String())
	}
	if err := a.persistSettings(); err != nil {
		a.dialog = dialog.NewMessage("Save settings", []string{err.Error()}, true)
	}
}

func (a *App) handleKeyTester(event *tcell.EventKey) {
	if event.Key() == tcell.KeyEscape {
		if a.keyTestEsc {
			a.keyTest = false
			a.keyTestEsc = false
			return
		}
		a.keyTestEsc = true
		a.keyTestText = "Esc — press Esc again to return"
		return
	}
	a.keyTestEsc = false
	action := "unbound"
	for _, mode := range []keymap.Mode{keymap.Panel, keymap.Viewer, keymap.Editor, keymap.Find} {
		if name, ok := a.keys.Lookup(mode, event); ok {
			action = string(mode) + ": " + name
			break
		}
	}
	a.keyTestText = fmt.Sprintf("%s  Key=%d Mod=%d Rune=%q", action, event.Key(), event.Modifiers(), event.Rune())
}

func (a *App) drawKeyTester(width, height int) {
	ui.TextCenter(a.screen, 0, 1, width, "Key tester — press any key", a.theme.Border)
	ui.TextCenter(a.screen, 0, height/2, width, a.keyTestText, a.theme.Selection)
	ui.TextCenter(a.screen, 0, height-2, width, "Esc Esc to leave", a.theme.HintLabel)
}

func (a *App) commandLineOwns(event *tcell.EventKey) bool {
	switch event.Key() {
	case tcell.KeyRune:
		return event.Modifiers()&tcell.ModAlt == 0 && !a.cmd.Empty()
	case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyDelete, tcell.KeyLeft, tcell.KeyRight,
		tcell.KeyHome, tcell.KeyEnd, tcell.KeyCtrlA, tcell.KeyCtrlE, tcell.KeyCtrlK,
		tcell.KeyCtrlW, tcell.KeyCtrlB, tcell.KeyCtrlF:
		return !a.cmd.Empty()
	}
	return false
}

func (a *App) runCommand(command string, interactive bool) {
	a.cmd.SetText("")
	if strings.HasPrefix(strings.TrimSpace(command), "cd ") || strings.TrimSpace(command) == "cd" {
		target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(command), "cd"))
		if target == "" {
			target, _ = os.UserHomeDir()
		} else if !a.panels[a.active].FS.IsAbs(target) {
			target = a.panels[a.active].FS.Join(a.panels[a.active].Cwd, target)
		}
		if err := a.panels[a.active].SetDir(context.Background(), target, ""); err != nil {
			a.status = err.Error()
		}
		return
	}
	if interactive {
		_ = a.screen.Suspend()
		err := shell.RunInteractive(context.Background(), shell.Default(), a.panels[a.active].Cwd, command)
		_ = a.screen.Resume()
		if err != nil {
			a.status = err.Error()
		}
		return
	}
	if a.history != nil {
		a.history.Add(history.Command, command)
		_ = a.history.Save()
		a.cmdHistory = a.history.Cursor(history.Command)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.commandCtx = cancel
	job, err := shell.Start(ctx, shell.Default(), a.panels[a.active].Cwd, command, a.output)
	if err != nil {
		cancel()
		a.commandCtx = nil
		a.status = err.Error()
		return
	}
	a.job = job
	a.status = "Command running (Ctrl-C to stop)"
	go func() {
		err := job.Wait()
		_ = a.screen.PostEvent(tcell.NewEventInterrupt(commandFinished{err: err}))
	}()
}

func (a *App) finishCommand(err error) {
	a.job = nil
	if a.commandCtx != nil {
		a.commandCtx()
		a.commandCtx = nil
	}
	a.status = fmt.Sprintf("Command finished (exit %d)", shell.ExitCode(err))
}

func (a *App) promptOperation(op string) {
	sources := a.panels[a.active].Targets()
	if len(sources) == 0 {
		a.status = "No files selected"
		return
	}
	destination := a.panels[1-a.active].Cwd
	a.dialog = dialog.NewInput(op, "Destination: ", destination, func(result dialog.Result) {
		if result.Cancelled || result.Text == "" {
			return
		}
		a.startOperation(op, sources, result.Text)
	})
}

func (a *App) startOperation(op string, sources []string, destination string) {
	srcPanel, dstPanel := a.panels[a.active], a.panels[1-a.active]
	if !dstPanel.FS.IsAbs(destination) {
		destination = dstPanel.FS.Join(dstPanel.Cwd, destination)
	}
	pairs, err := fileop.PlanFS(context.Background(), srcPanel.FS, sources, dstPanel.FS, destination)
	if err != nil {
		a.dialog = dialog.NewMessage(op, []string{err.Error()}, true)
		return
	}
	conflicts, err := fileop.ConflictsFS(context.Background(), srcPanel.FS, pairs, dstPanel.FS)
	if err != nil {
		a.dialog = dialog.NewMessage(op, []string{err.Error()}, true)
		return
	}
	policy := fileop.Fail
	if len(conflicts) > 0 {
		a.dialog = dialog.NewConfirm("Overwrite?", []string{fmt.Sprintf("%d destination(s) exist. Overwrite them?", len(conflicts))}, []string{"Overwrite", "Cancel"}, func(result dialog.Result) {
			if result.OK() {
				a.launchOperation(fileop.Operation{Op: op, Pairs: pairs, Policy: fileop.Overwrite, SrcFS: srcPanel.FS, DstFS: dstPanel.FS})
			}
		})
		return
	}
	a.launchOperation(fileop.Operation{Op: op, Pairs: pairs, Policy: policy, SrcFS: srcPanel.FS, DstFS: dstPanel.FS})
}

func (a *App) launchOperation(operation fileop.Operation) {
	ctx, cancel := context.WithCancel(context.Background())
	a.opCancel = cancel
	a.dialog = dialog.NewProgress(operation.Op, cancel)
	go func() {
		result := fileop.Run(ctx, operation, func(progress fileop.Progress) {
			_ = a.screen.PostEvent(tcell.NewEventInterrupt(operationProgress{progress: progress}))
		})
		_ = a.screen.PostEvent(tcell.NewEventInterrupt(operationFinished{result: result}))
	}()
}

func (a *App) finishOperation(result fileop.Result) {
	if a.opCancel != nil {
		a.opCancel()
		a.opCancel = nil
	}
	a.dialog = nil
	_ = a.panels[a.active].Reload(context.Background())
	_ = a.panels[1-a.active].Reload(context.Background())
	if err := result.Err(); err != nil {
		a.dialog = dialog.NewMessage("Operation complete", []string{err.Error()}, true)
	} else if result.Cancelled {
		a.status = "Operation cancelled"
	} else {
		a.status = fmt.Sprintf("%d item(s) completed", result.Items)
	}
}

func (a *App) promptMkdir() {
	a.dialog = dialog.NewInput("Create directory", "Name: ", "", func(result dialog.Result) {
		if !result.OK() || result.Text == "" {
			return
		}
		p := a.panels[a.active]
		name := result.Text
		if !p.FS.IsAbs(name) {
			name = p.FS.Join(p.Cwd, name)
		}
		if err := p.FS.Mkdir(context.Background(), name); err != nil {
			a.dialog = dialog.NewMessage("Create directory", []string{err.Error()}, true)
			return
		}
		_ = p.SetDir(context.Background(), p.Cwd, result.Text)
	})
}

func (a *App) confirmDelete() {
	p := a.panels[a.active]
	paths := p.Targets()
	if len(paths) == 0 {
		a.status = "No files selected"
		return
	}
	a.dialog = dialog.NewConfirm("Delete", []string{fmt.Sprintf("Delete %d selected item(s)?", len(paths))}, []string{"Delete", "Cancel"}, func(result dialog.Result) {
		if result.OK() {
			a.launchOperation(fileop.Operation{Op: "Delete", Paths: paths, SrcFS: p.FS})
		}
	})
}

func (a *App) rename() {
	p := a.panels[a.active]
	targets := p.Targets()
	if len(targets) != 1 {
		a.status = "Rename requires exactly one selected item"
		return
	}
	source := targets[0]
	entry, err := p.FS.Stat(context.Background(), source)
	if err != nil {
		a.dialog = dialog.NewMessage("Rename", []string{err.Error()}, true)
		return
	}
	a.dialog = dialog.NewInput("Rename", "New name: ", entry.Name, func(result dialog.Result) {
		if !result.OK() || result.Text == "" {
			return
		}
		destination := result.Text
		if !p.FS.IsAbs(destination) {
			destination = p.FS.Join(p.Cwd, destination)
		}
		if destination == source {
			return
		}
		rename := func() {
			if err := p.FS.Rename(context.Background(), source, destination); err != nil {
				a.dialog = dialog.NewMessage("Rename", []string{err.Error()}, true)
				return
			}
			_ = p.Reload(context.Background())
			a.status = "Renamed"
		}
		if _, err := p.FS.Stat(context.Background(), destination); err == nil {
			a.dialog = dialog.NewConfirm("Replace?", []string{"The destination already exists. Replace it?"}, []string{"Replace", "Cancel"}, func(result dialog.Result) {
				if result.OK() {
					rename()
				}
			})
		} else if !errors.Is(err, os.ErrNotExist) {
			a.dialog = dialog.NewMessage("Rename", []string{err.Error()}, true)
		} else {
			rename()
		}
	})
}

func (a *App) promptLink(hard bool) {
	p := a.panels[a.active]
	entry, ok := p.Selected()
	if !ok || entry.IsUp {
		a.status = "Select a source for the link"
		return
	}
	source := p.CurrentPath()
	if hard {
		if entry.IsDir {
			a.dialog = dialog.NewMessage("Hard link", []string{"Hard links to directories are not supported."}, true)
			return
		}
		if _, err := p.FS.Stat(context.Background(), source); err != nil {
			a.dialog = dialog.NewMessage("Hard link", []string{"The hard-link target must exist.", err.Error()}, true)
			return
		}
	}
	defaultDir := p.Cwd
	other := a.panels[1-a.active]
	if sameFilesystem(p.FS, other.FS) {
		defaultDir = other.Cwd
	}
	kind := "Symbolic link"
	if hard {
		kind = "Hard link"
	}
	a.dialog = dialog.NewInput(kind, "Link name: ", p.FS.Join(defaultDir, entry.Name), func(result dialog.Result) {
		if !result.OK() || result.Text == "" {
			return
		}
		link := result.Text
		if !p.FS.IsAbs(link) {
			link = p.FS.Join(p.Cwd, link)
		}
		var err error
		if hard {
			err = p.FS.Link(context.Background(), source, link)
		} else {
			err = p.FS.Symlink(context.Background(), source, link)
		}
		if err != nil {
			a.dialog = dialog.NewMessage(kind, []string{err.Error()}, true)
			return
		}
		_ = p.Reload(context.Background())
		a.status = kind + " created"
	})
}

func (a *App) editLink() {
	p := a.panels[a.active]
	entry, ok := p.Selected()
	if !ok || !entry.IsLink || entry.IsUp {
		a.status = "Select a symbolic link"
		return
	}
	oldPath := p.CurrentPath()
	target, err := p.FS.Readlink(context.Background(), oldPath)
	if err != nil {
		a.dialog = dialog.NewMessage("Edit link", []string{err.Error()}, true)
		return
	}
	a.dialog = dialog.NewInput("Edit symbolic link", "Target: ", target, func(targetResult dialog.Result) {
		if !targetResult.OK() || targetResult.Text == "" {
			return
		}
		a.dialog = dialog.NewInput("Edit symbolic link", "Link name: ", oldPath, func(pathResult dialog.Result) {
			if !pathResult.OK() || pathResult.Text == "" {
				return
			}
			newPath := pathResult.Text
			if !p.FS.IsAbs(newPath) {
				newPath = p.FS.Join(p.Cwd, newPath)
			}
			apply := func() {
				temp := p.FS.Join(p.FS.Dir(newPath), fmt.Sprintf(".bc-link-%d", time.Now().UnixNano()))
				if err := p.FS.Symlink(context.Background(), targetResult.Text, temp); err != nil {
					a.dialog = dialog.NewMessage("Edit link", []string{err.Error()}, true)
					return
				}
				if err := p.FS.Rename(context.Background(), temp, newPath); err != nil {
					_ = p.FS.Remove(context.Background(), temp)
					a.dialog = dialog.NewMessage("Edit link", []string{err.Error()}, true)
					return
				}
				if newPath != oldPath {
					if err := p.FS.Remove(context.Background(), oldPath); err != nil {
						a.dialog = dialog.NewMessage("Edit link", []string{err.Error()}, true)
						return
					}
				}
				_ = p.Reload(context.Background())
				a.status = "Link updated"
			}
			if newPath == oldPath {
				apply()
				return
			}
			if _, err := p.FS.Stat(context.Background(), newPath); err == nil {
				a.dialog = dialog.NewConfirm("Replace?", []string{"The destination already exists. Replace it?"}, []string{"Replace", "Cancel"}, func(result dialog.Result) {
					if result.OK() {
						apply()
					}
				})
			} else if !errors.Is(err, os.ErrNotExist) {
				a.dialog = dialog.NewMessage("Edit link", []string{err.Error()}, true)
			} else {
				apply()
			}
		})
	})
}

func sameFilesystem(a, b vfs.FS) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Name() == "" || b.Name() == "" {
		return a.Name() == "" && b.Name() == ""
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	return va.Type() == vb.Type() && va.Type().Comparable() && va.Interface() == vb.Interface()
}

func (a *App) openViewer() {
	p := a.panels[a.active]
	entry, ok := p.Selected()
	if !ok || entry.IsUp || entry.IsDir {
		a.status = "F3 requires a file"
		return
	}
	view, err := viewer.Open(context.Background(), p.FS, p.CurrentPath())
	if err != nil {
		a.dialog = dialog.NewMessage("View", []string{err.Error()}, true)
		return
	}
	view.LineNumber = a.lineNumbers
	a.viewer = view
}

func (a *App) openEditor() {
	p := a.panels[a.active]
	entry, ok := p.Selected()
	if !ok || entry.IsUp || entry.IsDir {
		a.status = "F4 requires a file"
		return
	}
	if entry.IsLink {
		a.editLink()
		return
	}
	opened, err := editor.Open(context.Background(), p.FS, p.CurrentPath())
	if errors.Is(err, editor.ErrBinary) {
		path := p.CurrentPath()
		a.dialog = dialog.NewConfirm("Binary file", []string{"Byte mode edits one byte per character without text conversion."}, []string{"Open bytes", "Cancel"}, func(result dialog.Result) {
			if !result.OK() {
				return
			}
			opened, err := editor.OpenBinary(context.Background(), p.FS, path)
			if err != nil {
				a.dialog = dialog.NewMessage("Edit", []string{err.Error()}, true)
				return
			}
			opened.LineNumber = a.lineNumbers
			a.editor = opened
		})
		return
	}
	if err != nil {
		a.dialog = dialog.NewMessage("Edit", []string{err.Error()}, true)
		return
	}
	opened.LineNumber = a.lineNumbers
	a.editor = opened
}

func (a *App) handleEditorKey(event *tcell.EventKey) {
	e := a.editor
	switch event.Key() {
	case tcell.KeyEscape:
		a.closeEditor()
	default:
		e.HandleKey(event)
	}
}

func (a *App) dispatchEditorAction(action string) {
	e := a.editor
	switch action {
	case "editor-save":
		if err := e.Save(context.Background()); err != nil {
			a.dialog = dialog.NewMessage("Save", []string{err.Error()}, true)
		} else {
			a.status = "Saved"
		}
	case "editor-quit":
		a.closeEditor()
	case "editor-search":
		a.dialog = dialog.NewInput("Search", "Pattern: ", "", func(result dialog.Result) {
			if result.OK() && result.Text != "" {
				e.Search(result.Text)
			}
		})
	case "editor-goto":
		a.dialog = dialog.NewInput("Go to line", "Line: ", fmt.Sprint(e.Y+1), func(result dialog.Result) {
			if result.OK() {
				var line int
				if _, err := fmt.Sscan(result.Text, &line); err != nil {
					a.status = "Enter a line number"
					return
				}
				e.Y = min(max(line-1, 0), len(e.Lines)-1)
				e.X = min(e.X, len(e.Lines[e.Y]))
			}
		})
	case "editor-delete-line":
		e.DeleteLine()
	case "editor-undo":
		e.Undo()
	case "editor-redo":
		e.Redo()
	case "editor-line-numbers":
		e.LineNumber = !e.LineNumber
	case "editor-select-start":
		e.StartSelection()
		a.status = "Selection started"
	case "editor-select-end":
		e.EndSelection()
		a.status = "Selection ended"
	case "editor-copy", "editor-cut":
		data, ok := e.Selection()
		if !ok || a.clipboard == nil {
			a.status = "No selection"
			return
		}
		if _, err := a.clipboard.Copy(data); err != nil {
			a.dialog = dialog.NewMessage("Clipboard", []string{err.Error()}, true)
			return
		}
		if action == "editor-cut" {
			e.DeleteSelection()
		}
		a.status = "Copied to " + a.clipboard.Describe()
	case "editor-paste":
		if a.clipboard == nil {
			return
		}
		data, err := a.clipboard.Paste()
		if err != nil {
			a.dialog = dialog.NewMessage("Clipboard", []string{err.Error()}, true)
			return
		}
		e.InsertBytes(data)
		a.status = "Pasted"
	default:
		a.status = "Action is not available yet: " + action
	}
}

func (a *App) closeEditor() {
	e := a.editor
	if !e.Changed {
		a.editor = nil
		return
	}
	a.dialog = dialog.NewConfirm("Unsaved changes", []string{"Save changes before leaving?"}, []string{"Save", "Discard", "Cancel"}, func(result dialog.Result) {
		switch result.Button {
		case 0:
			if err := e.Save(context.Background()); err != nil {
				a.dialog = dialog.NewMessage("Save", []string{err.Error()}, true)
				return
			}
			a.editor = nil
		case 1:
			a.editor = nil
		}
	})
}

func (a *App) openGitHistory() {
	p := a.panels[a.active]
	if p.FS.Name() != "" {
		a.dialog = dialog.NewMessage("Git history", []string{"Git history is available only for local panels."}, true)
		return
	}
	path := p.CurrentPath()
	if entry, ok := p.Selected(); !ok || entry.IsUp {
		path = p.Cwd
	}
	ctx := context.Background()
	root, err := git.Root(ctx, p.Cwd)
	if err != nil {
		a.dialog = dialog.NewMessage("Git history", []string{err.Error()}, true)
		return
	}
	commits, err := git.Log(ctx, root, path, 300)
	if err != nil {
		a.dialog = dialog.NewMessage("Git history", []string{err.Error()}, true)
		return
	}
	status, err := git.CurrentStatus(ctx, root)
	if err != nil {
		a.dialog = dialog.NewMessage("Git history", []string{err.Error()}, true)
		return
	}
	type choice struct {
		hash        string
		uncommitted bool
	}
	choices := make([]choice, 0, len(commits)+1)
	items := make([]string, 0, len(commits)+1)
	if len(status.Staged)+len(status.Modified)+len(status.Untracked) > 0 {
		items = append(items, "* working tree — uncommitted changes")
		choices = append(choices, choice{uncommitted: true})
	}
	for _, commit := range commits {
		items = append(items, fmt.Sprintf("%s  %s  %s", commit.Short, commit.When.Format("2006-01-02"), commit.Subject))
		choices = append(choices, choice{hash: commit.Hash})
	}
	if len(items) == 0 {
		a.dialog = dialog.NewMessage("Git history", []string{"No commits or working-tree changes."}, false)
		return
	}
	title := fmt.Sprintf("History: %s — %s", path, git.Describe(ctx, root))
	a.dialog = dialog.NewPicker(title, items, func(result dialog.Result) {
		if !result.OK() || result.Selected < 0 || result.Selected >= len(choices) {
			return
		}
		var data []byte
		var err error
		if choices[result.Selected].uncommitted {
			data, err = git.Diff(ctx, root, path)
		} else {
			data, err = git.Show(ctx, root, choices[result.Selected].hash, path)
		}
		if err != nil {
			a.dialog = dialog.NewMessage("Git diff", []string{err.Error()}, true)
			return
		}
		a.viewer = viewer.FromBytesAs(items[result.Selected], "commit.diff", data)
	})
}

type findState struct {
	fs        vfs.FS
	options   find.Options
	hits      []find.Hit
	selected  int
	searching bool
	cancel    context.CancelFunc
	err       error
}

type bindingCapture struct {
	mode   keymap.Mode
	action string
}

func (a *App) promptFind() {
	p := a.panels[a.active]
	a.dialog = dialog.NewInput("Find", "File name pattern: ", "*", func(pattern dialog.Result) {
		if !pattern.OK() {
			return
		}
		a.dialog = dialog.NewInput("Find", "Containing text (optional): ", "", func(content dialog.Result) {
			if !content.OK() {
				return
			}
			a.startFind(p.FS, find.Options{Root: p.Cwd, NamePattern: pattern.Text, Contains: content.Text})
		})
	})
}

func (a *App) startFind(fs vfs.FS, options find.Options) {
	ctx, cancel := context.WithCancel(context.Background())
	a.findState = &findState{fs: fs, options: options, searching: true, cancel: cancel}
	go func() {
		_, err := find.Search(ctx, fs, options, func(hit find.Hit) {
			_ = a.screen.PostEvent(tcell.NewEventInterrupt(findHitEvent{hit: hit}))
		})
		_ = a.screen.PostEvent(tcell.NewEventInterrupt(findDoneEvent{err: err}))
	}()
}

func (a *App) drawFind(width, height int) {
	title := fmt.Sprintf("Find: %s", a.findState.options.NamePattern)
	state := fmt.Sprintf("%d hits", len(a.findState.hits))
	if a.findState.searching {
		state += "  searching…"
	}
	if a.findState.err != nil && !errors.Is(a.findState.err, context.Canceled) {
		state += "  " + a.findState.err.Error()
	}
	ui.TextPadded(a.screen, 0, 0, width, title, a.theme.Border)
	ui.TextRight(a.screen, 0, 0, width, state, a.theme.HintLabel)
	rows := height - 2
	start := max(0, len(a.findState.hits)-rows)
	for row, hit := range a.findState.hits[start:] {
		label := hit.Path
		if hit.Line > 0 {
			label = fmt.Sprintf("%s:%d: %s", hit.Path, hit.Line, hit.Text)
		}
		style := a.theme.Panel
		if row+start == a.findState.selected {
			style = a.theme.Selection
		}
		ui.TextPadded(a.screen, 0, row+1, width, label, style)
	}
	if a.findState.err != nil && !errors.Is(a.findState.err, context.Canceled) {
		ui.TextPadded(a.screen, 0, height-2, width, a.findState.err.Error(), a.theme.Error)
	}
	a.drawHints(height)
}

func (a *App) handleFindKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyUp:
		a.findState.selected = max(0, a.findState.selected-1)
	case tcell.KeyDown:
		a.findState.selected = min(max(0, len(a.findState.hits)-1), a.findState.selected+1)
	}
}

func (a *App) dispatchFindAction(action string) {
	switch action {
	case "find-open":
		a.openFindHit()
	case "find-panelise":
		a.panelizeFind()
	case "find-again":
		a.closeFind()
		a.promptFind()
	case "find-stop":
		if a.findState.cancel != nil {
			a.findState.cancel()
		}
		a.findState.searching = false
	case "find-quit":
		a.closeFind()
	default:
		a.status = "Action is not available yet: " + action
	}
}

func (a *App) openFindHit() {
	if a.findState == nil || len(a.findState.hits) == 0 {
		return
	}
	hit := a.findState.hits[min(a.findState.selected, len(a.findState.hits)-1)]
	view, err := viewer.Open(context.Background(), a.findState.fs, a.findState.fs.Join(a.findState.options.Root, hit.Path))
	if err != nil {
		a.dialog = dialog.NewMessage("Find result", []string{err.Error()}, true)
		return
	}
	if hit.Line > 0 {
		view.Goto(hit.Line)
	}
	a.closeFind()
	a.viewer = view
}

func (a *App) panelizeFind() {
	state := a.findState
	if state == nil {
		return
	}
	seen := make(map[string]bool)
	entries := make([]vfs.Entry, 0, len(state.hits))
	for _, hit := range state.hits {
		if seen[hit.Path] {
			continue
		}
		seen[hit.Path] = true
		entry, err := state.fs.Stat(context.Background(), state.fs.Join(state.options.Root, hit.Path))
		if err != nil {
			entry = vfs.Entry{Name: hit.Path, SizeUnknown: true}
		}
		entry.Name = hit.Path
		entries = append(entries, entry)
	}
	panel := a.panels[a.active]
	panel.Panelize(state.options.Root, entries)
	a.closeFind()
}

func (a *App) closeFind() {
	if a.findState != nil && a.findState.cancel != nil {
		a.findState.cancel()
	}
	a.findState = nil
}

func (a *App) handleViewerKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyEscape:
		a.viewer = nil
	case tcell.KeyUp:
		a.viewer.Top = max(0, a.viewer.Top-1)
	case tcell.KeyDown:
		a.viewer.Top++
	case tcell.KeyPgUp:
		a.viewer.Top = max(0, a.viewer.Top-10)
	case tcell.KeyPgDn:
		a.viewer.Top += 10
	case tcell.KeyHome:
		a.viewer.Goto(1)
	case tcell.KeyEnd:
		if a.viewer.Mode == viewer.HexMode {
			a.viewer.Top = max(0, (len(a.viewer.Data)-1)/16)
		} else {
			a.viewer.Top = max(0, len(a.viewer.Lines)-1)
		}
	}
}

func (a *App) dispatchViewerAction(action string) {
	switch action {
	case "viewer-quit":
		a.viewer = nil
	case "viewer-wrap":
		a.viewer.Wrap = !a.viewer.Wrap
	case "viewer-hex":
		a.viewer.ToggleHex()
	case "viewer-raw":
		a.viewer.ToggleRaw()
	case "viewer-goto":
		a.promptViewerGoto()
	case "viewer-search":
		a.promptViewerSearch()
	case "viewer-search-next":
		a.viewer.NextHit(false)
	case "viewer-search-prev":
		a.viewer.NextHit(true)
	case "viewer-line-numbers":
		a.viewer.LineNumber = !a.viewer.LineNumber
	case "viewer-top":
		a.viewer.Goto(1)
	case "viewer-bottom":
		if a.viewer.Mode == viewer.HexMode {
			a.viewer.Top = max(0, (len(a.viewer.Data)-1)/16)
		} else {
			a.viewer.Top = max(0, len(a.viewer.Lines)-1)
		}
	default:
		a.status = "Action is not available yet: " + action
	}
}

func (a *App) promptViewerSearch() {
	a.dialog = dialog.NewInput("Search", "Pattern: ", a.viewer.Query, func(result dialog.Result) {
		if result.OK() && result.Text != "" && !a.viewer.Search(result.Text) {
			a.status = "Search: no match"
		}
	})
}

func (a *App) promptViewerGoto() {
	initial := fmt.Sprint(a.viewer.Top + 1)
	a.dialog = dialog.NewInput("Go to line", "Line: ", initial, func(result dialog.Result) {
		if !result.OK() {
			return
		}
		var line int
		if _, err := fmt.Sscan(result.Text, &line); err != nil {
			a.status = "Enter a line number"
			return
		}
		a.viewer.Goto(line)
	})
}

type operationProgress struct{ progress fileop.Progress }
type operationFinished struct{ result fileop.Result }
type commandFinished struct{ err error }
type shellOutputUpdate struct{}
type shellSessionFinished struct{}
type findHitEvent struct{ hit find.Hit }
type findDoneEvent struct{ err error }
