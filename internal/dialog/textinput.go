package dialog

import (
	"strings"
	"unicode"

	"github.com/anare/nmc/internal/history"
	"github.com/anare/nmc/internal/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

type TextInput struct {
	text    []rune
	cursor  int
	history *history.Cursor
}

func NewTextInput(initial string) *TextInput {
	runes := []rune(initial)
	return &TextInput{text: runes, cursor: len(runes)}
}

func (i *TextInput) Text() string { return string(i.text) }
func (i *TextInput) Empty() bool  { return len(i.text) == 0 }
func (i *TextInput) Cursor() int  { return i.cursor }

func (i *TextInput) WithHistory(cursor *history.Cursor) *TextInput {
	i.history = cursor
	return i
}

func (i *TextInput) SetText(value string) {
	i.text = []rune(value)
	i.cursor = len(i.text)
}

func (i *TextInput) HandleKey(event *tcell.EventKey) bool {
	if event.Key() == tcell.KeyRune && event.Modifiers()&tcell.ModAlt != 0 && i.history != nil {
		switch event.Rune() {
		case 'p':
			i.SetText(i.history.Prev(i.Text()))
			return true
		case 'n':
			i.SetText(i.history.Next(i.Text()))
			return true
		}
	}
	if event.Key() == tcell.KeyRune && event.Modifiers()&tcell.ModAlt == 0 {
		i.text = append(i.text, 0)
		copy(i.text[i.cursor+1:], i.text[i.cursor:])
		i.text[i.cursor] = event.Rune()
		i.cursor++
		return true
	}
	switch event.Key() {
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if i.cursor > 0 {
			i.text = append(i.text[:i.cursor-1], i.text[i.cursor:]...)
			i.cursor--
		}
		return true
	case tcell.KeyDelete:
		if i.cursor < len(i.text) {
			i.text = append(i.text[:i.cursor], i.text[i.cursor+1:]...)
		}
		return true
	case tcell.KeyLeft:
		if event.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
			i.cursor = i.wordBoundary(-1)
		} else if i.cursor > 0 {
			i.cursor--
		}
		return true
	case tcell.KeyRight:
		if event.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
			i.cursor = i.wordBoundary(1)
		} else if i.cursor < len(i.text) {
			i.cursor++
		}
		return true
	case tcell.KeyHome:
		i.cursor = 0
		return true
	case tcell.KeyEnd:
		i.cursor = len(i.text)
		return true
	case tcell.KeyCtrlA:
		i.cursor = 0
		return true
	case tcell.KeyCtrlE:
		i.cursor = len(i.text)
		return true
	case tcell.KeyCtrlK:
		i.text = i.text[:i.cursor]
		return true
	case tcell.KeyCtrlW:
		end := i.cursor
		for i.cursor > 0 && !isWord(i.text[i.cursor-1]) {
			i.cursor--
		}
		for i.cursor > 0 && isWord(i.text[i.cursor-1]) {
			i.cursor--
		}
		i.text = append(i.text[:i.cursor], i.text[end:]...)
		return true
	case tcell.KeyCtrlB:
		i.cursor = i.wordBoundary(-1)
		return true
	case tcell.KeyCtrlF:
		i.cursor = i.wordBoundary(1)
		return true
	default:
		return false
	}
}

func (i *TextInput) Draw(screen tcell.Screen, x, y, width int, style tcell.Style) {
	if width <= 0 {
		return
	}
	visible, cursorX := i.visible(width)
	ui.TextPadded(screen, x, y, width, visible, style)
	caretX := min(max(cursorX, 0), width-1)
	screen.SetContent(x+caretX, y, ' ', nil, style.Reverse(true))
}

func (i *TextInput) visible(width int) (string, int) {
	type cluster struct {
		text       string
		start, end int
		width      int
		cell       int
	}
	var clusters []cluster
	graphemes := uniseg.NewGraphemes(string(i.text))
	start := 0
	cell := 0
	cursorCell := 0
	for graphemes.Next() {
		text := graphemes.Str()
		runeCount := len([]rune(text))
		clusterWidth := max(0, graphemes.Width())
		clusters = append(clusters, cluster{text: text, start: start, end: start + runeCount, width: clusterWidth, cell: cell})
		if i.cursor >= start && i.cursor <= start+runeCount {
			cursorCell = cell
			if i.cursor == start+runeCount {
				cursorCell += clusterWidth
			}
		}
		start += runeCount
		cell += clusterWidth
	}
	left := max(0, cursorCell-width+1)
	var visible strings.Builder
	used := 0
	cursorX := cursorCell - left
	for _, part := range clusters {
		if part.end <= i.cursor && i.cursor == part.end {
			cursorX = used + part.width - left
		}
		if part.cell >= left && part.cell+part.width <= left+width {
			visible.WriteString(part.text)
		}
		used += part.width
	}
	return visible.String(), cursorX
}

func (i *TextInput) wordBoundary(direction int) int {
	position := i.cursor
	if direction < 0 {
		for position > 0 && !isWord(i.text[position-1]) {
			position--
		}
		for position > 0 && isWord(i.text[position-1]) {
			position--
		}
		return position
	}
	for position < len(i.text) && isWord(i.text[position]) {
		position++
	}
	for position < len(i.text) && !isWord(i.text[position]) {
		position++
	}
	return position
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func displayWidth(value string) int {
	width := 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		width += max(0, graphemes.Width())
	}
	return width
}
