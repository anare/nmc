package viewer

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/anare/nmc/internal/syntax"
	"github.com/anare/nmc/internal/ui"
	"github.com/anare/nmc/internal/vfs"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

const SizeLimit = 32 << 20

type Mode int

const (
	TextMode Mode = iota
	RawMode
	HexMode
)

type Viewer struct {
	Name       string
	LexerName  string
	Data       []byte
	Lines      []string
	Highlights [][]syntax.Span
	Mode       Mode
	Wrap       bool
	LineNumber bool
	Top        int
	Left       int
	Query      string
	Hits       []int
	Hit        int
	Truncated  bool
}

func Open(ctx context.Context, fs vfs.FS, path string) (*Viewer, error) {
	if fs == nil {
		return nil, errors.New("no filesystem")
	}
	reader, err := fs.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, SizeLimit+1))
	if err != nil {
		return nil, err
	}
	truncated := len(data) > SizeLimit
	if truncated {
		data = data[:SizeLimit]
	}
	viewer := FromBytes(path, data)
	viewer.Truncated = truncated
	return viewer, nil
}

func FromBytes(name string, data []byte) *Viewer {
	data = append([]byte(nil), data...)
	viewer := &Viewer{Name: name, Data: data, Wrap: true}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		viewer.Mode = HexMode
	} else {
		viewer.Lines = strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		if highlighted, language, ok := syntax.Highlight(name, string(data), "monokai"); ok {
			viewer.Highlights = highlighted
			viewer.LexerName = language
		}
	}
	return viewer
}

func FromBytesAs(name, lexerName string, data []byte) *Viewer {
	viewer := FromBytes(name, data)
	if viewer.Mode != HexMode {
		viewer.LexerName = lexerName
		if highlighted, language, ok := syntax.Highlight(lexerName, string(data), "monokai"); ok {
			viewer.Highlights = highlighted
			if language != "" {
				viewer.LexerName = language
			}
		}
	}
	return viewer
}

func (v *Viewer) Status() string {
	if v.Truncated {
		return fmt.Sprintf("File truncated at %d MiB", SizeLimit>>20)
	}
	if v.Query != "" && len(v.Hits) == 0 {
		return "Search: no match"
	}
	if v.Query != "" && len(v.Hits) > 0 {
		return fmt.Sprintf("Match %d of %d", v.Hit+1, len(v.Hits))
	}
	return ""
}

func (v *Viewer) ToggleRaw() {
	if v.Mode == HexMode {
		return
	}
	if v.Mode == RawMode {
		v.Mode = TextMode
	} else {
		v.Mode = RawMode
	}
}

func (v *Viewer) ToggleHex() {
	if v.Mode == HexMode {
		v.Mode = TextMode
	} else {
		v.Mode = HexMode
	}
}

func (v *Viewer) Search(query string) bool {
	v.Query = query
	v.Hits = nil
	v.Hit = 0
	if query == "" {
		return false
	}
	lower := strings.ToLower(query)
	if v.Mode == HexMode {
		for offset := 0; offset+len(query) <= len(v.Data); offset++ {
			if bytes.EqualFold(v.Data[offset:offset+len(query)], []byte(query)) {
				v.Hits = append(v.Hits, offset)
			}
		}
	} else {
		for index, line := range v.Lines {
			if strings.Contains(strings.ToLower(line), lower) {
				v.Hits = append(v.Hits, index)
			}
		}
	}
	v.goToHit()
	return len(v.Hits) > 0
}

func (v *Viewer) NextHit(reverse bool) bool {
	if len(v.Hits) == 0 {
		return false
	}
	if reverse {
		v.Hit = (v.Hit + len(v.Hits) - 1) % len(v.Hits)
	} else {
		v.Hit = (v.Hit + 1) % len(v.Hits)
	}
	v.goToHit()
	return true
}

func (v *Viewer) goToHit() {
	if len(v.Hits) == 0 {
		return
	}
	if v.Mode == HexMode {
		v.Top = v.Hits[v.Hit] / 16
	} else {
		v.Top = v.Hits[v.Hit]
	}
}

func (v *Viewer) Goto(line int) {
	maximum := len(v.Lines) - 1
	if v.Mode == HexMode {
		maximum = max(0, (len(v.Data)-1)/16)
	}
	v.Top = min(max(line-1, 0), max(0, maximum))
}

func (v *Viewer) Draw(screen tcell.Screen, rect ui.Rect, theme ui.Theme) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	ui.Fill(screen, rect, theme.Background)
	ui.DrawBox(screen, rect, ui.Double, theme.Border)
	title := v.Name
	if v.LexerName != "" && v.Mode == TextMode {
		title += " [" + v.LexerName + "]"
	}
	ui.Text(screen, rect.X+2, rect.Y, rect.W-4, ui.TruncateLeft(title, rect.W-4), theme.Border)
	rows := max(0, rect.H-2)
	contentY := rect.Y + 1
	if v.Mode == HexMode {
		v.drawHex(screen, rect.X+1, contentY, rect.W-2, rows, theme)
	} else {
		v.drawText(screen, rect.X+1, contentY, rect.W-2, rows, theme)
	}
	if v.Truncated {
		ui.TextPadded(screen, rect.X+1, rect.Bottom()-1, rect.W-2, v.Status(), theme.HintKey)
	}
}

func (v *Viewer) drawText(screen tcell.Screen, x, y, width, rows int, theme ui.Theme) {
	numberWidth := 0
	if v.LineNumber {
		numberWidth = len(fmt.Sprint(len(v.Lines))) + 1
	}
	textWidth := max(0, width-numberWidth)
	lineIndex := v.Top
	drawn := 0
	for drawn < rows && lineIndex < len(v.Lines) {
		prefix := ""
		if numberWidth > 0 {
			prefix = fmt.Sprintf("%*d ", numberWidth-1, lineIndex+1)
			ui.Text(screen, x, y+drawn, numberWidth, prefix, theme.HintLabel)
		}
		line := v.Lines[lineIndex]
		if v.Wrap {
			wrapped := wrapDisplay(line, textWidth)
			for _, part := range wrapped {
				if drawn >= rows {
					break
				}
				ui.Text(screen, x+numberWidth, y+drawn, textWidth, part, theme.Panel)
				drawn++
			}
		} else {
			text := line
			if v.Left > 0 {
				text = dropDisplay(text, v.Left)
			}
			v.drawLine(screen, x+numberWidth, y+drawn, textWidth, lineIndex, text, theme)
			drawn++
		}
		lineIndex++
	}
}

func (v *Viewer) drawLine(screen tcell.Screen, x, y, width, lineIndex int, line string, theme ui.Theme) {
	if v.Mode == RawMode || lineIndex >= len(v.Highlights) {
		ui.Text(screen, x, y, width, line, theme.Panel)
		return
	}
	offset := 0
	for _, span := range v.Highlights[lineIndex] {
		style := theme.Panel
		if span.Foreground.IsSet() {
			style = style.Foreground(tcell.NewRGBColor(int32(span.Foreground.Red()), int32(span.Foreground.Green()), int32(span.Foreground.Blue())))
		}
		if span.Bold {
			style = style.Bold(true)
		}
		used := ui.Text(screen, x+offset, y, width-offset, span.Text, style)
		offset += used
		if offset >= width {
			break
		}
	}
}

func (v *Viewer) drawHex(screen tcell.Screen, x, y, width, rows int, theme ui.Theme) {
	for row := 0; row < rows; row++ {
		offset := (v.Top + row) * 16
		if offset >= len(v.Data) {
			break
		}
		end := min(offset+16, len(v.Data))
		chunk := v.Data[offset:end]
		ascii := make([]byte, len(chunk))
		for index, value := range chunk {
			if value >= 32 && value < 127 {
				ascii[index] = value
			} else {
				ascii[index] = '.'
			}
		}
		line := fmt.Sprintf("%08x  %-47s  %s", offset, hex.EncodeToString(chunk), ascii)
		ui.Text(screen, x, y+row, width, line, theme.Panel)
	}
}

func wrapDisplay(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	var result []string
	var line strings.Builder
	used := 0
	for _, r := range text {
		runeWidth := max(0, runewidth.RuneWidth(r))
		if used+runeWidth > width {
			result = append(result, line.String())
			line.Reset()
			used = 0
		}
		line.WriteRune(r)
		used += runeWidth
	}
	result = append(result, line.String())
	return result
}

func dropDisplay(text string, cells int) string {
	used := 0
	for index, r := range text {
		runeWidth := max(0, runewidth.RuneWidth(r))
		if used+runeWidth > cells {
			return text[index:]
		}
		used += runeWidth
	}
	return ""
}
