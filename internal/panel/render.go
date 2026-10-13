package panel

import (
	"fmt"
	"math"
	"strings"

	"github.com/anare/nmc/internal/ui"
	"github.com/anare/nmc/internal/vfs"
	"github.com/gdamore/tcell/v2"
)

type columns struct {
	name, size, modified int
}

func (p *Panel) Draw(screen tcell.Screen, theme ui.Theme) {
	if p.w <= 0 || p.h <= 0 {
		return
	}
	rect := ui.Rect{X: p.x, Y: p.y, W: p.w, H: p.h}
	ui.Fill(screen, rect, theme.Panel)
	ui.DrawBox(screen, rect, ui.Single, theme.Border)
	if p.w < 2 || p.h < 2 {
		return
	}

	title := p.Cwd
	if name := p.FS.Name(); name != "" {
		title = name + ":" + p.Cwd
	}
	ui.Text(screen, p.x+2, p.y, p.w-4, ui.TruncateLeft(title, p.w-4), theme.Border)

	rows := p.VisibleRows()
	innerWidth := p.w - 2
	scrollbar := rows > 0 && len(p.Entries) > rows && innerWidth > 2
	listWidth := innerWidth
	if scrollbar {
		listWidth--
	}
	layout := makeColumns(listWidth)

	headerY := p.y + 1
	drawColumns(screen, p.x+1, headerY, layout, theme.Border, "Name", "Size", "Modify time")
	for row := 0; row < rows; row++ {
		y := p.y + 2 + row
		index := p.Top + row
		if index >= len(p.Entries) {
			ui.Fill(screen, ui.Rect{X: p.x + 1, Y: y, W: innerWidth, H: 1}, theme.Panel)
			continue
		}
		entry := p.Entries[index]
		marked := !entry.IsUp && p.Marked[p.FS.Join(p.Cwd, entry.Name)]
		style := theme.EntryStyle(entry, marked, index == p.Cursor)
		ui.Fill(screen, ui.Rect{X: p.x + 1, Y: y, W: innerWidth, H: 1}, style)
		name := DisplayName(entry, layout.name)
		drawColumns(screen, p.x+1, y, layout, style, name, entry.SizeString(layout.size), entry.ModTime.Format("Jan _2 15:04"))
	}

	if scrollbar {
		drawScrollbar(screen, p.x+p.w-2, p.y+2, rows, len(p.Entries), p.Top, theme)
	}

	ui.HLine(screen, p.x, p.y+p.h-3, p.w, ui.Single, theme.Border)
	footer, footerStyle := p.footer(theme)
	ui.TextPadded(screen, p.x+1, p.y+p.h-2, innerWidth, ui.Truncate(footer, innerWidth), footerStyle)
}

func makeColumns(width int) columns {
	if width <= 0 {
		return columns{}
	}
	layout := columns{}
	remaining := width
	if width >= 32 {
		layout.modified = 12
		remaining -= layout.modified + 1
	}
	if remaining >= 19 {
		layout.size = 8
		remaining -= layout.size + 1
	}
	layout.name = max(0, remaining)
	return layout
}

func drawColumns(screen tcell.Screen, x, y int, layout columns, style tcell.Style, name, size, modified string) {
	ui.TextPadded(screen, x, y, layout.name, name, style)
	x += layout.name
	if layout.size > 0 {
		ui.Text(screen, x, y, 1, "│", style)
		x++
		ui.TextPadded(screen, x, y, layout.size, size, style)
		x += layout.size
	}
	if layout.modified > 0 {
		ui.Text(screen, x, y, 1, "│", style)
		x++
		ui.TextPadded(screen, x, y, layout.modified, modified, style)
	}
}

func drawScrollbar(screen tcell.Screen, x, y, rows, total, top int, theme ui.Theme) {
	if rows <= 0 || total <= rows {
		return
	}
	thumb := max(1, rows*rows/total)
	thumb = min(rows, thumb)
	start := 0
	if total > rows {
		start = int(math.Round(float64(top) * float64(rows-thumb) / float64(total-rows)))
	}
	for row := 0; row < rows; row++ {
		runeToDraw := '│'
		style := theme.Border
		if row >= start && row < start+thumb {
			runeToDraw = '█'
			style = theme.HintKey
		}
		ui.Text(screen, x, y+row, 1, string(runeToDraw), style)
	}
}

func (p *Panel) footer(theme ui.Theme) (string, tcell.Style) {
	if p.Err != nil {
		return p.Err.Error(), theme.BrokenLink
	}
	count := 0
	var bytes int64
	for _, entry := range p.Entries {
		if entry.IsUp {
			continue
		}
		if p.Marked[p.FS.Join(p.Cwd, entry.Name)] {
			count++
			bytes += entry.Size
		}
	}
	if count > 0 {
		noun := "files"
		if count == 1 {
			noun = "file"
		}
		return fmt.Sprintf("%d %s marked, %s bytes", count, noun, humanSize(bytes)), theme.Panel
	}
	entry, ok := p.Selected()
	if !ok {
		return "", theme.Panel
	}
	name := entry.Name
	switch {
	case entry.IsLink && entry.Broken:
		name += " -> (broken)"
	case entry.IsLink && entry.Target != "":
		name += " -> " + entry.Target
	case entry.IsHardLink():
		name += fmt.Sprintf("   %d links", entry.Links)
	default:
		name += "   " + strings.TrimSpace(entry.SizeString(8))
	}
	return name, theme.Panel
}

func humanSize(size int64) string {
	value := float64(size)
	unit := ""
	for _, next := range []string{"", "K", "M", "G", "T", "P"} {
		unit = next
		if value < 1024 || next == "P" {
			break
		}
		value /= 1024
	}
	if unit == "" {
		return fmt.Sprintf("%d", size)
	}
	if value < 10 {
		return fmt.Sprintf("%.1f%s", value, unit)
	}
	return fmt.Sprintf("%.0f%s", value, unit)
}

func DisplayName(entry vfs.Entry, width int) string {
	if width <= 0 {
		return ""
	}
	indicator := entry.Indicator()
	if indicator == "" {
		return ui.Truncate(entry.Name, width)
	}
	if width == 1 {
		return indicator
	}
	return ui.Truncate(entry.Name, width-1) + indicator
}
