package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type listItem struct {
	name  string
	path  string
	isDir bool
}

type panel struct {
	name  string
	list  *tview.List
	path  string
	items []listItem
}

func newPanel(title string) *panel {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetBorder(true)
	l.SetTitle(" " + title + " ")
	l.SetMainTextColor(tcell.ColorWhite)
	return &panel{name: title, list: l}
}

func (p *panel) load(path string, selectIndex int) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return err
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	p.path = absPath
	p.items = p.items[:0]
	if parent := filepath.Dir(absPath); parent != absPath {
		p.items = append(p.items, listItem{name: "..", path: parent, isDir: true})
	}

	for _, e := range entries {
		fullPath := filepath.Join(absPath, e.Name())
		p.items = append(p.items, listItem{name: e.Name(), path: fullPath, isDir: e.IsDir()})
	}

	p.list.Clear()
	for _, item := range p.items {
		display := item.name
		if item.isDir && item.name != ".." {
			display += "/"
		}
		p.list.AddItem(display, "", 0, nil)
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
	return p.load(item.path, 0)
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

func (p *panel) updateTitle() {
	p.list.SetTitle(fmt.Sprintf(" %s: %s ", p.name, p.path))
}

type appState struct {
	app        *tview.Application
	panels     [2]*panel
	active     int
	status     *tview.TextView
	escPending bool
	escAt      time.Time
}

func (s *appState) setStatus(msg string) {
	s.status.SetText(" " + msg)
}

func (s *appState) activePanel() *panel {
	return s.panels[s.active]
}

func (s *appState) switchPanel() {
	s.active = 1 - s.active
	s.stylePanels()
	s.app.SetFocus(s.activePanel().list)
	s.setStatus("Switched panel")
}

func (s *appState) stylePanels() {
	for i, p := range s.panels {
		if i == s.active {
			p.list.SetBorderColor(tcell.ColorYellow)
		} else {
			p.list.SetBorderColor(tcell.ColorGray)
		}
		p.updateTitle()
	}
}

func (s *appState) handleFunctionKey(num int) {
	s.setStatus(fmt.Sprintf("F%d pressed", num))
}

func (s *appState) handleEscSequence(event *tcell.EventKey) bool {
	if !s.escPending {
		return false
	}
	if time.Since(s.escAt) > 700*time.Millisecond {
		s.escPending = false
		return false
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
	s.escPending = false
	return false
}

func (s *appState) keyHandler(event *tcell.EventKey) *tcell.EventKey {
	if s.handleEscSequence(event) {
		return nil
	}

	a := s.activePanel()

	switch event.Key() {
	case tcell.KeyESC:
		s.escPending = true
		s.escAt = time.Now()
		s.setStatus("ESC detected: press 1-0 for F1-F10")
		return event
	case tcell.KeyTAB:
		s.switchPanel()
		return nil
	case tcell.KeyBacktab:
		s.switchPanel()
		return nil
	case tcell.KeyUp:
		a.move(-1)
		return nil
	case tcell.KeyDown:
		a.move(1)
		return nil
	case tcell.KeyPgUp:
		a.pageMove(-1)
		return nil
	case tcell.KeyPgDn:
		a.pageMove(1)
		return nil
	case tcell.KeyHome:
		a.list.SetCurrentItem(0)
		return nil
	case tcell.KeyEnd:
		if len(a.items) > 0 {
			a.list.SetCurrentItem(len(a.items) - 1)
		}
		return nil
	case tcell.KeyRight:
		if err := a.goIntoSelected(); err != nil {
			s.setStatus("Error: " + err.Error())
		}
		return nil
	case tcell.KeyLeft:
		if err := a.goParentViaList(); err != nil {
			s.setStatus("Error: " + err.Error())
		}
		return nil
	case tcell.KeyEnter:
		if err := a.goIntoSelected(); err != nil {
			s.setStatus("Error: " + err.Error())
		}
		return nil
	case tcell.KeyF1, tcell.KeyF2, tcell.KeyF3, tcell.KeyF4, tcell.KeyF5,
		tcell.KeyF6, tcell.KeyF7, tcell.KeyF8, tcell.KeyF9, tcell.KeyF10:
		n := int(event.Key()-tcell.KeyF1) + 1
		s.handleFunctionKey(n)
		return nil
	}

	if event.Key() == tcell.KeyRune {
		s.escPending = false
	}

	return event
}

func run() error {
	app := tview.NewApplication()

	left := newPanel("Left")
	right := newPanel("Right")

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if err := left.load(cwd, 0); err != nil {
		return err
	}
	if err := right.load(cwd, 0); err != nil {
		return err
	}

	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" Tab: switch panel  Arrows/PgUp/PgDn/Home/End: navigate  ESC+1..0: F1..F10 ")
	status.SetBorder(true)
	status.SetTitle(" Keys ")

	panels := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(left.list, 0, 1, true).
		AddItem(right.list, 0, 1, false)

	root := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(panels, 0, 1, true).
		AddItem(status, 3, 0, false)

	state := &appState{
		app:    app,
		panels: [2]*panel{left, right},
		status: status,
	}
	state.stylePanels()

	app.SetInputCapture(state.keyHandler)
	return app.SetRoot(root, true).SetFocus(left.list).Run()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nmc failed:", err)
		os.Exit(1)
	}
}
