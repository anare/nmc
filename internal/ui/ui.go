package ui

import (
	"strings"

	"github.com/anare/nmc/internal/vfs"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

type Rect struct {
	X, Y, W, H int
}

func (r Rect) Right() int  { return r.X + r.W }
func (r Rect) Bottom() int { return r.Y + r.H }

func Text(s tcell.Screen, x, y, maxw int, str string, style tcell.Style) int {
	if maxw <= 0 || y < 0 {
		return 0
	}
	screenW, screenH := s.Size()
	if y >= screenH {
		return 0
	}
	limit := min(maxw, screenW-x)
	if limit <= 0 {
		return 0
	}

	used := 0
	graphemes := uniseg.NewGraphemes(str)
	for graphemes.Next() {
		cluster := graphemes.Runes()
		width := graphemes.Width()
		if width <= 0 {
			continue
		}
		if used+width > limit {
			break
		}
		cellX := x + used
		if cellX >= 0 && cellX+width <= screenW {
			if len(cluster) > 0 {
				s.SetContent(cellX, y, cluster[0], cluster[1:], style)
			}
		}
		used += width
	}
	return used
}

func TextPadded(s tcell.Screen, x, y, width int, str string, style tcell.Style) int {
	used := Text(s, x, y, width, str, style)
	Fill(s, Rect{X: x + used, Y: y, W: width - used, H: 1}, style)
	return used
}

func TextRight(s tcell.Screen, x, y, width int, str string, style tcell.Style) int {
	str = TruncateLeft(str, width)
	used := displayWidth(str)
	return Text(s, x+width-used, y, used, str, style)
}

func TextCenter(s tcell.Screen, x, y, width int, str string, style tcell.Style) int {
	str = Truncate(str, width)
	used := displayWidth(str)
	return Text(s, x+(width-used)/2, y, used, str, style)
}

func Truncate(str string, maxw int) string {
	if maxw <= 0 {
		return ""
	}
	if displayWidth(str) <= maxw {
		return str
	}
	if maxw == 1 {
		return "~"
	}
	var b strings.Builder
	used := 0
	graphemes := uniseg.NewGraphemes(str)
	for graphemes.Next() {
		width := graphemes.Width()
		if width < 0 {
			continue
		}
		if used+width > maxw-1 {
			break
		}
		b.WriteString(graphemes.Str())
		used += width
	}
	b.WriteByte('~')
	return b.String()
}

func TruncateLeft(str string, maxw int) string {
	if maxw <= 0 {
		return ""
	}
	if displayWidth(str) <= maxw {
		return str
	}
	if maxw == 1 {
		return "~"
	}
	type textCluster struct {
		text  string
		width int
	}
	var clusters []textCluster
	graphemes := uniseg.NewGraphemes(str)
	for graphemes.Next() {
		clusters = append(clusters, textCluster{text: graphemes.Str(), width: graphemes.Width()})
	}
	start := len(clusters)
	used := 0
	for start > 0 {
		width := clusters[start-1].width
		if width < 0 {
			start--
			continue
		}
		if used+width > maxw-1 {
			break
		}
		used += width
		start--
	}
	var b strings.Builder
	b.WriteByte('~')
	for _, cluster := range clusters[start:] {
		b.WriteString(cluster.text)
	}
	return b.String()
}

func Fill(s tcell.Screen, rect Rect, style tcell.Style) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	screenW, screenH := s.Size()
	x0, y0 := max(0, rect.X), max(0, rect.Y)
	x1, y1 := min(screenW, rect.Right()), min(screenH, rect.Bottom())
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			s.SetContent(x, y, ' ', nil, style)
		}
	}
}

type BoxChars struct {
	TL, TR, BL, BR rune
	H, V           rune
	LT, RT, TT, BT rune
}

var (
	Single = BoxChars{'┌', '┐', '└', '┘', '─', '│', '├', '┤', '┬', '┴'}
	Double = BoxChars{'╔', '╗', '╚', '╝', '═', '║', '╠', '╣', '╦', '╩'}
)

func DrawBox(s tcell.Screen, rect Rect, chars BoxChars, style tcell.Style) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	if rect.W == 1 {
		VLine(s, rect.X, rect.Y, rect.H, chars, style)
		return
	}
	if rect.H == 1 {
		HLine(s, rect.X, rect.Y, rect.W, chars, style)
		return
	}
	put(s, rect.X, rect.Y, chars.TL, style)
	put(s, rect.Right()-1, rect.Y, chars.TR, style)
	put(s, rect.X, rect.Bottom()-1, chars.BL, style)
	put(s, rect.Right()-1, rect.Bottom()-1, chars.BR, style)
	for x := rect.X + 1; x < rect.Right()-1; x++ {
		put(s, x, rect.Y, chars.H, style)
		put(s, x, rect.Bottom()-1, chars.H, style)
	}
	for y := rect.Y + 1; y < rect.Bottom()-1; y++ {
		put(s, rect.X, y, chars.V, style)
		put(s, rect.Right()-1, y, chars.V, style)
	}
}

func HLine(s tcell.Screen, x, y, width int, chars BoxChars, style tcell.Style) {
	if width <= 0 {
		return
	}
	for offset := 0; offset < width; offset++ {
		ch := chars.H
		if width > 1 && offset == 0 {
			ch = chars.LT
		} else if width > 1 && offset == width-1 {
			ch = chars.RT
		}
		put(s, x+offset, y, ch, style)
	}
}

func VLine(s tcell.Screen, x, y, height int, chars BoxChars, style tcell.Style) {
	if height <= 0 {
		return
	}
	for offset := 0; offset < height; offset++ {
		ch := chars.V
		if height > 1 && offset == 0 {
			ch = chars.TT
		} else if height > 1 && offset == height-1 {
			ch = chars.BT
		}
		put(s, x, y+offset, ch, style)
	}
}

func put(s tcell.Screen, x, y int, ch rune, style tcell.Style) {
	width, height := s.Size()
	if x >= 0 && x < width && y >= 0 && y < height {
		s.SetContent(x, y, ch, nil, style)
	}
}

func displayWidth(str string) int {
	width := 0
	graphemes := uniseg.NewGraphemes(str)
	for graphemes.Next() {
		if clusterWidth := graphemes.Width(); clusterWidth > 0 {
			width += clusterWidth
		}
	}
	return width
}

type Theme struct {
	Name       string
	Background tcell.Style
	Panel      tcell.Style
	Border     tcell.Style
	Dialog     tcell.Style
	Error      tcell.Style
	Selection  tcell.Style
	Marked     tcell.Style
	HintKey    tcell.Style
	HintLabel  tcell.Style
	Directory  tcell.Style
	Executable tcell.Style
	Symlink    tcell.Style
	BrokenLink tcell.Style
	Special    tcell.Style
	Archive    tcell.Style
	Image      tcell.Style
	Media      tcell.Style
	Source     tcell.Style
	Document   tcell.Style
	Data       tcell.Style
	Temporary  tcell.Style
	Plain      tcell.Style
}

func (t Theme) FileTypeStyle(entry vfs.Entry) tcell.Style {
	switch entry.Class() {
	case vfs.ClassDir:
		return t.Directory
	case vfs.ClassExec:
		return t.Executable
	case vfs.ClassLink:
		return t.Symlink
	case vfs.ClassBroken:
		return t.BrokenLink
	case vfs.ClassSpecial:
		return t.Special
	case vfs.ClassArchive:
		return t.Archive
	case vfs.ClassImage:
		return t.Image
	case vfs.ClassMedia:
		return t.Media
	case vfs.ClassSource:
		return t.Source
	case vfs.ClassDoc:
		return t.Document
	case vfs.ClassData:
		return t.Data
	case vfs.ClassTemp:
		return t.Temporary
	case vfs.ClassPlain:
		return t.Plain
	default:
		return t.Plain
	}
}

func (t Theme) EntryStyle(entry vfs.Entry, marked, cursor bool) tcell.Style {
	style := t.FileTypeStyle(entry)
	if cursor {
		style = t.Selection
	}
	if marked {
		fg, _, _ := t.Marked.Decompose()
		style = style.Foreground(fg).Bold(true)
	}
	return style
}

func DrawHintBar(s tcell.Screen, y, width int, theme Theme, labels [10]string) {
	if width <= 0 {
		return
	}
	base, extra := width/len(labels), width%len(labels)
	x := 0
	for i, label := range labels {
		slot := base
		if i < extra {
			slot++
		}
		if slot == 0 {
			continue
		}
		key := string(rune('1' + i))
		if i == 9 {
			key = "0"
		}
		keyWidth := Text(s, x, y, min(slot, 2), key, theme.HintKey)
		TextPadded(s, x+keyWidth, y, slot-keyWidth, label, theme.HintLabel)
		x += slot
	}
}
