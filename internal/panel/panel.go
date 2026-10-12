package panel

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/anare/nmc/internal/vfs"
)

const chromeRows = 5

type returnTo struct {
	fs   vfs.FS
	cwd  string
	name string
}

type Panel struct {
	FS      vfs.FS
	Cwd     string
	Entries []vfs.Entry
	Marked  map[string]bool
	Cursor  int
	Top     int
	Opt     vfs.Options
	Err     error

	x, y, w, h int
	memo       map[string]string
	prevFS     vfs.FS
	prevCwd    string
	back       []returnTo
	panelized  bool
}

func New(fs vfs.FS, cwd string, opt vfs.Options) (*Panel, error) {
	if fs == nil {
		return nil, os.ErrInvalid
	}
	p := &Panel{
		FS:     fs,
		Marked: make(map[string]bool),
		Opt:    opt,
		memo:   make(map[string]string),
	}
	if err := p.SetDir(context.Background(), cwd, ""); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Panel) SetRect(x, y, width, height int) {
	p.x, p.y, p.w, p.h = x, y, width, height
	p.clamp()
}

func (p *Panel) Rect() (x, y, width, height int) {
	return p.x, p.y, p.w, p.h
}

func (p *Panel) VisibleRows() int { return max(0, p.h-chromeRows) }

func (p *Panel) SetDir(ctx context.Context, dir, selectName string) error {
	if !p.FS.IsAbs(dir) {
		if p.Cwd != "" {
			dir = p.FS.Join(p.Cwd, dir)
		}
		if p.FS.Name() == "" {
			var err error
			dir, err = filepath.Abs(dir)
			if err != nil {
				p.Err = err
				return err
			}
		}
	}
	if p.FS.Name() == "" {
		dir = filepath.Clean(dir)
	}

	entries, err := p.FS.List(ctx, dir)
	if err != nil {
		p.Err = err
		return err
	}
	if p.Cwd != "" && p.Cwd != dir {
		if entry, ok := p.Selected(); ok {
			p.memo[p.Cwd] = entry.Name
		}
		p.Marked = make(map[string]bool)
	}
	if selectName == "" {
		selectName = p.memo[dir]
	}
	vfs.Sort(entries, p.Opt.SortMode)
	if !p.Opt.ShowHidden {
		filtered := entries[:0]
		for _, entry := range entries {
			if entry.IsUp || !strings.HasPrefix(entry.Name, ".") {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}

	p.Cwd = dir
	p.Entries = entries
	p.panelized = false
	p.Err = nil
	p.Cursor, p.Top = 0, 0
	for i, entry := range entries {
		if entry.Name == selectName {
			p.Cursor = i
			break
		}
	}
	p.clamp()
	return nil
}

func (p *Panel) Reload(ctx context.Context) error {
	if p.panelized {
		return p.SetDir(ctx, p.Cwd, "")
	}
	name := ""
	if entry, ok := p.Selected(); ok {
		name = entry.Name
	}
	return p.SetDir(ctx, p.Cwd, name)
}

func (p *Panel) Selected() (vfs.Entry, bool) {
	if p.Cursor < 0 || p.Cursor >= len(p.Entries) {
		return vfs.Entry{}, false
	}
	return p.Entries[p.Cursor], true
}

func (p *Panel) CurrentPath() string {
	entry, ok := p.Selected()
	if !ok {
		return ""
	}
	if entry.IsUp {
		return p.FS.Dir(p.Cwd)
	}
	return p.FS.Join(p.Cwd, entry.Name)
}

func (p *Panel) MoveBy(delta int) {
	if len(p.Entries) == 0 {
		return
	}
	p.Cursor = min(max(p.Cursor+delta, 0), len(p.Entries)-1)
	p.clamp()
}

func (p *Panel) MoveTo(index int) {
	if len(p.Entries) == 0 {
		p.Cursor, p.Top = 0, 0
		return
	}
	p.Cursor = min(max(index, 0), len(p.Entries)-1)
	p.clamp()
}

func (p *Panel) PageBy(pages int) {
	step := max(1, p.VisibleRows()-1)
	p.MoveBy(pages * step)
}

func (p *Panel) Home() { p.MoveTo(0) }
func (p *Panel) End()  { p.MoveTo(len(p.Entries) - 1) }

func (p *Panel) ToggleMark() {
	entry, ok := p.Selected()
	if !ok {
		return
	}
	if !entry.IsUp {
		name := p.FS.Join(p.Cwd, entry.Name)
		if p.Marked[name] {
			delete(p.Marked, name)
		} else {
			p.Marked[name] = true
		}
	}
	p.MoveBy(1)
}

func (p *Panel) Targets() []string {
	if len(p.Marked) > 0 {
		paths := make([]string, 0, len(p.Marked))
		for _, entry := range p.Entries {
			if entry.IsUp {
				continue
			}
			name := p.FS.Join(p.Cwd, entry.Name)
			if p.Marked[name] {
				paths = append(paths, name)
			}
		}
		return paths
	}
	if path := p.CurrentPath(); path != "" {
		if entry, ok := p.Selected(); ok && !entry.IsUp {
			return []string{path}
		}
	}
	return nil
}

func (p *Panel) Enter(ctx context.Context) (bool, error) {
	entry, ok := p.Selected()
	if !ok || !entry.IsDir {
		return false, nil
	}
	if entry.IsUp {
		return true, p.GoUp(ctx)
	}
	return true, p.SetDir(ctx, p.FS.Join(p.Cwd, entry.Name), "")
}

func (p *Panel) GoUp(ctx context.Context) error {
	parent := p.FS.Dir(p.Cwd)
	if parent == p.Cwd {
		if p.Nested() {
			return p.LeaveFS()
		}
		if p.prevFS != nil {
			return p.DisconnectRemote()
		}
		return nil
	}
	return p.SetDir(ctx, parent, base(p.FS, p.Cwd))
}

func (p *Panel) ToggleMarkAt(index int) {
	p.MoveTo(index)
	p.ToggleMark()
}

func (p *Panel) ConnectRemote(fs vfs.FS, dir string) error {
	oldFS, oldCwd := p.FS, p.Cwd
	p.prevFS, p.prevCwd = oldFS, oldCwd
	p.FS = fs
	p.Cwd = ""
	if err := p.SetDir(context.Background(), dir, ""); err != nil {
		p.FS, p.Cwd = oldFS, oldCwd
		p.prevFS, p.prevCwd = nil, ""
		return err
	}
	return nil
}

func (p *Panel) DisconnectRemote() error {
	if p.prevFS == nil {
		return nil
	}
	oldFS, oldCwd := p.FS, p.Cwd
	fs, cwd := p.prevFS, p.prevCwd
	p.FS, p.Cwd = fs, ""
	if err := p.SetDir(context.Background(), cwd, ""); err != nil {
		p.FS, p.Cwd = oldFS, oldCwd
		return err
	}
	p.prevFS, p.prevCwd = nil, ""
	return nil
}

func (p *Panel) EnterFS(fs vfs.FS, dir, backTo string) error {
	p.back = append(p.back, returnTo{fs: p.FS, cwd: p.Cwd, name: backTo})
	oldFS, oldCwd := p.FS, p.Cwd
	p.FS, p.Cwd = fs, ""
	if err := p.SetDir(context.Background(), dir, ""); err != nil {
		p.FS, p.Cwd = oldFS, oldCwd
		p.back = p.back[:len(p.back)-1]
		return err
	}
	return nil
}

func (p *Panel) LeaveFS() error {
	if len(p.back) == 0 {
		return nil
	}
	returnTo := p.back[len(p.back)-1]
	oldFS, oldCwd := p.FS, p.Cwd
	p.FS, p.Cwd = returnTo.fs, ""
	if err := p.SetDir(context.Background(), returnTo.cwd, returnTo.name); err != nil {
		p.FS, p.Cwd = oldFS, oldCwd
		return err
	}
	p.back = p.back[:len(p.back)-1]
	return nil
}

func (p *Panel) Nested() bool { return len(p.back) > 0 }

func (p *Panel) Panelize(root string, entries []vfs.Entry) {
	p.Cwd = root
	p.Entries = append([]vfs.Entry(nil), entries...)
	p.Marked = make(map[string]bool)
	vfs.Sort(p.Entries, p.Opt.SortMode)
	p.Cursor, p.Top = 0, 0
	p.panelized = true
	p.Err = nil
	p.clamp()
}

func base(fs vfs.FS, name string) string {
	if fs.Name() == "" {
		return filepath.Base(name)
	}
	return path.Base(name)
}

func (p *Panel) clamp() {
	if len(p.Entries) == 0 {
		p.Cursor, p.Top = 0, 0
		return
	}
	p.Cursor = min(max(p.Cursor, 0), len(p.Entries)-1)
	rows := p.VisibleRows()
	if rows <= 0 {
		p.Top = p.Cursor
		return
	}
	if p.Cursor < p.Top {
		p.Top = p.Cursor
	}
	if p.Cursor >= p.Top+rows {
		p.Top = p.Cursor - rows + 1
	}
	p.Top = min(max(p.Top, 0), max(0, len(p.Entries)-rows))
}
