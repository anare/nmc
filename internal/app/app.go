package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/anare/nmc/internal/panel"
	"github.com/anare/nmc/internal/ui"
	"github.com/anare/nmc/internal/vfs"
	"github.com/gdamore/tcell/v2"
)

const (
	minWidth  = 40
	minHeight = 10
)

type App struct {
	screen tcell.Screen
	panels [2]*panel.Panel
	active int
	theme  ui.Theme
	status string
	quit   bool
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
	return &App{screen: screen, panels: [2]*panel.Panel{left, right}, theme: ui.DefaultTheme()}, nil
}

func (a *App) Run() error {
	if err := a.screen.Init(); err != nil {
		return err
	}
	defer a.screen.Fini()
	for !a.quit {
		a.Draw()
		switch event := a.screen.PollEvent().(type) {
		case *tcell.EventKey:
			a.HandleKey(event)
		case *tcell.EventResize:
			a.screen.Sync()
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

	panelHeight := height - 2
	leftWidth := width / 2
	rightWidth := width - leftWidth
	a.panels[0].SetRect(0, 0, leftWidth, panelHeight)
	a.panels[1].SetRect(leftWidth, 0, rightWidth, panelHeight)
	a.panels[0].Draw(a.screen, a.theme)
	a.panels[1].Draw(a.screen, a.theme)

	statusStyle := a.theme.Panel
	if a.panels[a.active].Err != nil {
		statusStyle = a.theme.BrokenLink
		if a.status == "" {
			a.status = a.panels[a.active].Err.Error()
		}
	}
	ui.TextPadded(a.screen, 0, height-2, width, strings.TrimSpace(a.status), statusStyle)
	var hints [10]string
	hints[9] = "Quit"
	ui.DrawHintBar(a.screen, height-1, width, a.theme, hints)
	a.screen.Show()
}

func (a *App) HandleKey(event *tcell.EventKey) {
	a.status = ""
	active := a.panels[a.active]
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
	}
}
