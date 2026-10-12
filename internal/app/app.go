package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/anare/nmc/internal/dialog"
	"github.com/anare/nmc/internal/fileop"
	"github.com/anare/nmc/internal/history"
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
	screen     tcell.Screen
	panels     [2]*panel.Panel
	active     int
	theme      ui.Theme
	status     string
	quit       bool
	dialog     *dialog.Dialog
	cmd        *dialog.TextInput
	output     *shell.Buffer
	opCancel   context.CancelFunc
	viewer     *viewer.Viewer
	history    *history.Store
	cmdHistory *history.Cursor
	job        *shell.Job
	commandCtx context.CancelFunc
}

func New(screen tcell.Screen, fs vfs.FS, cwd string) (*App, error) {
	if screen == nil || fs == nil {
		return nil, os.ErrInvalid
	}
	options := vfs.Options{ShowHidden: true}
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
		cmd: dialog.NewTextInput(""), output: shell.NewBuffer(1 << 20),
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
	if a.viewer != nil {
		hints[1], hints[2], hints[3], hints[4], hints[6], hints[7] = "Wrap", "Quit", "Hex", "Copy", "Search", "Raw"
	} else {
		hints[2], hints[3], hints[4], hints[5], hints[6], hints[7], hints[9] = "View", "Copy", "Move", "Mkdir", "Delete", "Refresh", "Quit"
	}
	ui.DrawHintBar(a.screen, height-1, width, a.theme, hints)
}

func (a *App) HandleKey(event *tcell.EventKey) {
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
	if a.viewer != nil {
		a.handleViewerKey(event)
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
	if event.Key() == tcell.KeyRune && event.Modifiers()&tcell.ModAlt != 0 && event.Rune() == 'o' {
		a.dialog = dialog.NewList("Command output", a.output.Lines())
		return
	}
	if a.commandLineOwns(event) {
		a.cmd.HandleKey(event)
		return
	}
	switch event.Key() {
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
	case tcell.KeyCtrlT, tcell.KeyInsert:
		active.ToggleMark()
	case tcell.KeyCtrlR:
		if err := active.Reload(context.Background()); err != nil {
			a.status = err.Error()
		} else {
			a.status = "Directory refreshed"
		}
	case tcell.KeyF10, tcell.KeyCtrlQ:
		a.quit = true
	case tcell.KeyF5:
		a.promptOperation("Copy")
	case tcell.KeyF6:
		a.promptOperation("Move")
	case tcell.KeyF7:
		a.promptMkdir()
	case tcell.KeyF8:
		a.confirmDelete()
	case tcell.KeyF3:
		a.openViewer()
	case tcell.KeyRune:
		if event.Modifiers() == tcell.ModNone && event.Rune() >= ' ' {
			a.cmd.HandleKey(event)
		}
	}
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
	a.viewer = view
}

func (a *App) handleViewerKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyEscape, tcell.KeyF3, tcell.KeyF10:
		a.viewer = nil
	case tcell.KeyF2:
		a.viewer.Wrap = !a.viewer.Wrap
	case tcell.KeyF4:
		a.viewer.ToggleHex()
	case tcell.KeyF8:
		a.viewer.ToggleRaw()
	case tcell.KeyF5:
		a.promptViewerGoto()
	case tcell.KeyF7:
		a.promptViewerSearch()
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
	case tcell.KeyRune:
		if event.Modifiers() == tcell.ModNone {
			switch event.Rune() {
			case 'q':
				a.viewer = nil
			case 'w':
				a.viewer.Wrap = !a.viewer.Wrap
			case 'h':
				a.viewer.ToggleHex()
			case 'r':
				a.viewer.ToggleRaw()
			case 'n':
				a.viewer.NextHit(false)
			case 'N':
				a.viewer.NextHit(true)
			case 'g':
				a.viewer.Goto(1)
			case 'G':
				if a.viewer.Mode == viewer.HexMode {
					a.viewer.Top = max(0, (len(a.viewer.Data)-1)/16)
				} else {
					a.viewer.Top = max(0, len(a.viewer.Lines)-1)
				}
			case '/':
				a.promptViewerSearch()
			}
		}
	}
	if event.Modifiers()&tcell.ModAlt != 0 && event.Rune() == 'n' {
		a.viewer.LineNumber = !a.viewer.LineNumber
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
