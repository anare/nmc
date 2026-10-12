package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/anare/nmc/internal/syntax"
	"github.com/anare/nmc/internal/ui"
	"github.com/anare/nmc/internal/vfs"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

const (
	SizeLimit      = 16 << 20
	HighlightLimit = 128 << 10
	TabWidth       = 8
)

var ErrBinary = errors.New("file contains binary or invalid UTF-8 data")

type snapshot struct {
	lines [][]rune
	x, y  int
}

type Editor struct {
	FS             vfs.FS
	Path           string
	Lines          [][]rune
	X, Y           int
	Top, Left      int
	LineNumber     bool
	Binary         bool
	Changed        bool
	lexer          string
	undo           []snapshot
	redo           []snapshot
	saved          []byte
	coalescing     bool
	highlighted    [][]syntax.Span
	highlightReady bool
}

func Open(ctx context.Context, fs vfs.FS, path string) (*Editor, error) {
	return open(ctx, fs, path, false)
}

func OpenBinary(ctx context.Context, fs vfs.FS, path string) (*Editor, error) {
	return open(ctx, fs, path, true)
}

func open(ctx context.Context, fs vfs.FS, path string, binary bool) (*Editor, error) {
	if fs == nil {
		return nil, errors.New("no filesystem")
	}
	reader, err := fs.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, SizeLimit+1))
	closeErr := reader.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(data) > SizeLimit {
		return nil, fmt.Errorf("file exceeds editor limit of %d MiB", SizeLimit>>20)
	}
	if !binary && (bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)) {
		return nil, ErrBinary
	}
	e := &Editor{FS: fs, Path: path, Binary: binary}
	if binary {
		e.setBytes(data)
	} else {
		e.setText(data)
	}
	e.saved = append([]byte(nil), e.Bytes()...)
	return e, nil
}

func FromBytes(path string, data []byte) *Editor {
	e := &Editor{Path: path}
	e.setText(data)
	e.saved = append([]byte(nil), e.Bytes()...)
	return e
}

func (e *Editor) setText(data []byte) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	parts := strings.Split(text, "\n")
	e.Lines = make([][]rune, len(parts))
	for index, part := range parts {
		e.Lines[index] = []rune(part)
	}
	if len(e.Lines) == 0 {
		e.Lines = [][]rune{{}}
	}
}

func (e *Editor) setBytes(data []byte) {
	parts := bytes.Split(data, []byte{'\n'})
	e.Lines = make([][]rune, len(parts))
	for index, part := range parts {
		e.Lines[index] = make([]rune, len(part))
		for offset, value := range part {
			e.Lines[index][offset] = rune(value)
		}
	}
	if len(e.Lines) == 0 {
		e.Lines = [][]rune{{}}
	}
}

func (e *Editor) Bytes() []byte {
	var data []byte
	for index, line := range e.Lines {
		if index > 0 {
			data = append(data, '\n')
		}
		if e.Binary {
			for _, value := range line {
				if value < 0 || value > 255 {
					continue
				}
				data = append(data, byte(value))
			}
		} else {
			data = append(data, string(line)...)
		}
	}
	return data
}

func (e *Editor) snapshot() snapshot {
	lines := make([][]rune, len(e.Lines))
	for index, line := range e.Lines {
		lines[index] = append([]rune(nil), line...)
	}
	return snapshot{lines: lines, x: e.X, y: e.Y}
}

func (e *Editor) replace(start, count int, with ...[]rune) {
	if start < 0 {
		start = 0
	}
	if start > len(e.Lines) {
		start = len(e.Lines)
	}
	end := min(start+max(count, 0), len(e.Lines))
	next := make([][]rune, 0, len(e.Lines)-(end-start)+len(with))
	next = append(next, e.Lines[:start]...)
	for _, line := range with {
		next = append(next, append([]rune(nil), line...))
	}
	next = append(next, e.Lines[end:]...)
	if len(next) == 0 {
		next = [][]rune{{}}
	}
	e.Lines = next
	e.clamp()
}

func (e *Editor) record(before snapshot) {
	e.undo = append(e.undo, before)
	e.redo = nil
	e.Changed = !bytes.Equal(e.Bytes(), e.saved)
	e.coalescing = false
	e.highlightReady = false
}

func (e *Editor) InsertRune(value rune) bool {
	if e.Binary && (value < 0 || value > 255) {
		return false
	}
	if !e.coalescing {
		e.undo = append(e.undo, e.snapshot())
		e.redo = nil
	}
	line := e.Lines[e.Y]
	next := make([]rune, 0, len(line)+1)
	next = append(next, line[:e.X]...)
	next = append(next, value)
	next = append(next, line[e.X:]...)
	e.Lines[e.Y] = next
	e.X++
	e.coalescing = true
	e.Changed = !bytes.Equal(e.Bytes(), e.saved)
	e.highlightReady = false
	return true
}

func (e *Editor) InsertText(text string) {
	for _, value := range strings.ReplaceAll(text, "\r\n", "\n") {
		if value == '\n' {
			e.Newline()
		} else {
			e.InsertRune(value)
		}
	}
}

func (e *Editor) Newline() {
	before := e.snapshot()
	line := e.Lines[e.Y]
	left, right := append([]rune(nil), line[:e.X]...), append([]rune(nil), line[e.X:]...)
	e.replace(e.Y, 1, left, right)
	e.Y++
	e.X = 0
	e.record(before)
}

func (e *Editor) Backspace() {
	if e.X > 0 {
		before := e.snapshot()
		line := e.Lines[e.Y]
		e.Lines[e.Y] = append(append([]rune(nil), line[:e.X-1]...), line[e.X:]...)
		e.X--
		e.record(before)
		return
	}
	if e.Y == 0 {
		return
	}
	before := e.snapshot()
	previous := len(e.Lines[e.Y-1])
	joined := append(append([]rune(nil), e.Lines[e.Y-1]...), e.Lines[e.Y]...)
	e.replace(e.Y-1, 2, joined)
	e.Y--
	e.X = previous
	e.record(before)
}

func (e *Editor) Delete() {
	line := e.Lines[e.Y]
	if e.X < len(line) {
		before := e.snapshot()
		e.Lines[e.Y] = append(append([]rune(nil), line[:e.X]...), line[e.X+1:]...)
		e.record(before)
		return
	}
	if e.Y+1 < len(e.Lines) {
		before := e.snapshot()
		joined := append(append([]rune(nil), line...), e.Lines[e.Y+1]...)
		e.replace(e.Y, 2, joined)
		e.record(before)
	}
}

func (e *Editor) DeleteLine() {
	before := e.snapshot()
	e.replace(e.Y, 1)
	e.record(before)
}

func (e *Editor) Undo() bool {
	if len(e.undo) == 0 {
		return false
	}
	e.redo = append(e.redo, e.snapshot())
	state := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.Lines, e.X, e.Y = state.lines, state.x, state.y
	e.coalescing = false
	e.highlightReady = false
	e.Changed = !bytes.Equal(e.Bytes(), e.saved)
	return true
}

func (e *Editor) Redo() bool {
	if len(e.redo) == 0 {
		return false
	}
	e.undo = append(e.undo, e.snapshot())
	state := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.Lines, e.X, e.Y = state.lines, state.x, state.y
	e.coalescing = false
	e.highlightReady = false
	e.Changed = !bytes.Equal(e.Bytes(), e.saved)
	return true
}

func (e *Editor) Search(query string) bool {
	if query == "" || len(e.Lines) == 0 {
		return false
	}
	startY, startX := e.Y, e.X
	needle := []rune(query)
	for offset := 0; offset < len(e.Lines); offset++ {
		y := (startY + offset) % len(e.Lines)
		runes := e.Lines[y]
		from := 0
		if y == startY {
			from = min(startX+1, len(runes))
		}
		for x := from; x+len(needle) <= len(runes); x++ {
			if strings.EqualFold(string(runes[x:x+len(needle)]), query) {
				e.X, e.Y = x, y
				return true
			}
		}
	}
	return false
}

func (e *Editor) Save(ctx context.Context) error {
	if e.FS == nil {
		return errors.New("editor has no filesystem")
	}
	entry, err := e.FS.Stat(ctx, e.Path)
	mode := os.FileMode(0o644)
	if err == nil && entry.Mode.Perm() != 0 {
		mode = entry.Mode.Perm()
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp := e.FS.Join(e.FS.Dir(e.Path), fmt.Sprintf(".bc-save-%d", os.Getpid()))
	writer, err := e.FS.Create(ctx, tmp, mode)
	if err != nil {
		return err
	}
	if _, err = writer.Write(e.Bytes()); err != nil {
		_ = writer.Close()
		_ = e.FS.Remove(context.Background(), tmp)
		return err
	}
	if err = writer.Close(); err != nil {
		_ = e.FS.Remove(context.Background(), tmp)
		return err
	}
	if err = e.FS.Rename(ctx, tmp, e.Path); err != nil {
		_ = e.FS.Remove(context.Background(), tmp)
		return err
	}
	e.Changed = false
	e.saved = append(e.saved[:0], e.Bytes()...)
	return nil
}

func (e *Editor) HandleKey(event *tcell.EventKey) bool {
	switch event.Key() {
	case tcell.KeyLeft:
		e.coalescing = false
		if e.X > 0 {
			e.X--
		}
	case tcell.KeyRight:
		e.coalescing = false
		if e.X < len(e.Lines[e.Y]) {
			e.X++
		}
	case tcell.KeyUp:
		e.coalescing = false
		e.Y = max(0, e.Y-1)
	case tcell.KeyDown:
		e.coalescing = false
		e.Y = min(len(e.Lines)-1, e.Y+1)
	case tcell.KeyHome, tcell.KeyCtrlA:
		e.coalescing = false
		e.X = 0
	case tcell.KeyEnd, tcell.KeyCtrlE:
		e.coalescing = false
		e.X = len(e.Lines[e.Y])
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		e.Backspace()
	case tcell.KeyDelete:
		e.Delete()
	case tcell.KeyEnter:
		e.Newline()
	case tcell.KeyTab:
		e.coalescing = false
		e.InsertRune('\t')
	case tcell.KeyCtrlZ:
		e.Undo()
	case tcell.KeyCtrlY:
		e.Redo()
	case tcell.KeyCtrlB:
		e.coalescing = false
		e.word(-1)
	case tcell.KeyCtrlF:
		e.coalescing = false
		e.word(1)
	case tcell.KeyRune:
		if event.Modifiers()&tcell.ModCtrl != 0 {
			return false
		}
		return e.InsertRune(event.Rune())
	default:
		e.coalescing = false
		return false
	}
	e.clamp()
	return true
}

func (e *Editor) word(direction int) {
	line := e.Lines[e.Y]
	if direction < 0 {
		for e.X > 0 && !wordRune(line[e.X-1]) {
			e.X--
		}
		for e.X > 0 && wordRune(line[e.X-1]) {
			e.X--
		}
		return
	}
	for e.X < len(line) && wordRune(line[e.X]) {
		e.X++
	}
	for e.X < len(line) && !wordRune(line[e.X]) {
		e.X++
	}
}

func (e *Editor) clamp() {
	if len(e.Lines) == 0 {
		e.Lines = [][]rune{{}}
	}
	e.Y = min(max(0, e.Y), len(e.Lines)-1)
	e.X = min(max(0, e.X), len(e.Lines[e.Y]))
}

func wordRune(value rune) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func (e *Editor) Draw(screen tcell.Screen, rect ui.Rect, theme ui.Theme) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	ui.Fill(screen, rect, theme.Background)
	ui.DrawBox(screen, rect, ui.Double, theme.Border)
	title := e.Path
	if e.Changed {
		title += " *"
	}
	ui.Text(screen, rect.X+2, rect.Y, rect.W-4, ui.TruncateLeft(title, rect.W-4), theme.Border)
	numberWidth := 0
	if e.LineNumber {
		numberWidth = len(fmt.Sprint(len(e.Lines))) + 1
	}
	rows := max(0, rect.H-2)
	for row := 0; row < rows; row++ {
		lineIndex := e.Top + row
		if lineIndex >= len(e.Lines) {
			break
		}
		if numberWidth > 0 {
			ui.Text(screen, rect.X+1, rect.Y+1+row, numberWidth, fmt.Sprintf("%*d ", numberWidth-1, lineIndex+1), theme.HintLabel)
		}
		left := e.Left
		if lineIndex == e.Y {
			cursorCell := e.displayColumn(e.Lines[lineIndex], e.X)
			if cursorCell < left {
				left = cursorCell
			}
			if cursorCell >= left+rect.W-numberWidth-2 {
				left = cursorCell - (rect.W - numberWidth - 3)
			}
			e.Left = max(0, left)
		}
		e.drawEditorLine(screen, rect.X+1+numberWidth, rect.Y+1+row, rect.W-numberWidth-2, left, lineIndex, theme)
	}
	cursorRow := e.Y - e.Top
	if cursorRow >= 0 && cursorRow < rows {
		cell := e.displayColumn(e.Lines[e.Y], e.X) - e.Left
		if cell >= 0 && cell < rect.W-numberWidth-2 {
			screen.SetContent(rect.X+1+numberWidth+cell, rect.Y+1+cursorRow, ' ', nil, theme.Selection)
		}
	}
}

func (e *Editor) displayLine(line []rune) string {
	var result strings.Builder
	column := 0
	for _, value := range line {
		if value == '\t' {
			spaces := TabWidth - column%TabWidth
			result.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		} else {
			result.WriteRune(value)
			column++
		}
	}
	return result.String()
}

func (e *Editor) displayColumn(line []rune, position int) int {
	column := 0
	for _, value := range line[:min(position, len(line))] {
		if value == '\t' {
			column += TabWidth - column%TabWidth
		} else {
			column += max(0, runewidth.RuneWidth(value))
		}
	}
	return column
}

func (e *Editor) drawEditorLine(screen tcell.Screen, x, y, width, left, lineIndex int, theme ui.Theme) {
	if width <= 0 {
		return
	}
	highlighted := e.Highlighted()
	if lineIndex >= len(highlighted) {
		ui.Text(screen, x, y, width, dropCells(e.displayLine(e.Lines[lineIndex]), left), theme.Panel)
		return
	}
	column := 0
	drawn := 0
	for _, span := range highlighted[lineIndex] {
		startColumn := column
		style := theme.Panel
		if span.Foreground.IsSet() {
			style = style.Foreground(tcell.NewRGBColor(int32(span.Foreground.Red()), int32(span.Foreground.Green()), int32(span.Foreground.Blue())))
		}
		if span.Bold {
			style = style.Bold(true)
		}
		var display strings.Builder
		for _, value := range span.Text {
			if value == '\t' {
				spaces := TabWidth - column%TabWidth
				display.WriteString(strings.Repeat(" ", spaces))
				column += spaces
			} else {
				display.WriteRune(value)
				column += max(0, runewidth.RuneWidth(value))
			}
		}
		text := dropCells(display.String(), max(0, left-startColumn))
		used := ui.Text(screen, x+drawn, y, width-drawn, text, style)
		drawn += used
		if drawn >= width {
			return
		}
	}
}

func displayWidth(text string) int {
	width := 0
	for _, value := range text {
		width += max(0, runewidth.RuneWidth(value))
	}
	return width
}

func dropCells(text string, cells int) string {
	if cells <= 0 {
		return text
	}
	var result strings.Builder
	width := 0
	for _, value := range text {
		runeWidth := max(0, runewidth.RuneWidth(value))
		if width+runeWidth > cells {
			result.WriteRune(value)
		}
		width += runeWidth
	}
	return result.String()
}

func (e *Editor) Highlighted() [][]syntax.Span {
	if e.highlightReady {
		return e.highlighted
	}
	e.highlightReady = true
	if e.Binary || len(e.Bytes()) > HighlightLimit {
		e.highlighted = nil
		return e.highlighted
	}
	lines, _, ok := syntax.Highlight(e.Path, string(e.Bytes()), "monokai")
	if !ok {
		e.highlighted = nil
		return e.highlighted
	}
	e.highlighted = lines
	return e.highlighted
}
