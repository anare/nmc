package dialog

import (
	"fmt"
	"strings"

	"github.com/anare/nmc/internal/history"
	"github.com/anare/nmc/internal/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

type Result struct {
	Button    int
	Text      string
	Selected  int
	Cancelled bool
}

func (r Result) OK() bool { return !r.Cancelled && r.Button == 0 }

type mode int

const (
	messageMode mode = iota
	confirmMode
	inputMode
	listMode
	progressMode
	pickerMode
)

type Dialog struct {
	Title         string
	Lines         []string
	Buttons       []string
	Error         bool
	Input         *TextInput
	Prompt        string
	Progress      int
	ProgressInfo  string
	Items         []string
	Selected      int
	Filter        bool
	FilterText    string
	Combo         bool
	ComboOpen     bool
	ComboItems    []string
	ComboSelected int
	Offset        int
	Focused       int
	mode          mode
	onCancel      func()
	OnDone        func(Result)
}

func NewMessage(title string, lines []string, isError bool) *Dialog {
	buttons := []string{"OK"}
	return &Dialog{Title: title, Lines: append([]string(nil), lines...), Buttons: buttons, Error: isError, mode: messageMode}
}

func NewList(title string, lines []string) *Dialog {
	dialog := NewMessage(title, lines, false)
	dialog.mode = listMode
	return dialog
}

func NewPicker(title string, items []string, onDone func(Result)) *Dialog {
	return &Dialog{Title: title, Items: append([]string(nil), items...), Buttons: []string{"Select", "Cancel"}, Filter: true, mode: pickerMode, OnDone: onDone}
}

func NewHistBrowser(title string, items []string, onDone func(Result)) *Dialog {
	return NewPicker(title, items, onDone)
}

func NewMenu(title string, items []string, onDone func(Result)) *Dialog {
	dialog := NewPicker(title, items, onDone)
	dialog.Filter = false
	return dialog
}

func NewConfirm(title string, lines, buttons []string, onDone func(Result)) *Dialog {
	if len(buttons) == 0 {
		buttons = []string{"Yes", "No"}
	}
	return &Dialog{Title: title, Lines: append([]string(nil), lines...), Buttons: append([]string(nil), buttons...), mode: confirmMode, OnDone: onDone}
}

func NewInput(title, prompt, initial string, onDone func(Result)) *Dialog {
	return &Dialog{Title: title, Prompt: prompt, Input: NewTextInput(initial), Buttons: []string{"OK", "Cancel"}, Focused: -1, mode: inputMode, OnDone: onDone}
}

func NewComboInput(title, prompt, initial string, cursor *history.Cursor, onDone func(Result)) *Dialog {
	dialog := NewInput(title, prompt, initial, onDone)
	dialog.Input.WithHistory(cursor)
	dialog.Combo = true
	if cursor != nil {
		dialog.ComboItems = cursor.Entries()
	}
	return dialog
}

func NewProgress(title string, onCancel func()) *Dialog {
	return &Dialog{Title: title, Buttons: []string{"Cancel"}, mode: progressMode, onCancel: onCancel}
}

func (d *Dialog) SetProgress(percent int, info string) {
	d.Progress = min(max(percent, 0), 100)
	d.ProgressInfo = info
}

func (d *Dialog) HandleKey(event *tcell.EventKey) (bool, Result) {
	cancel := Result{Button: -1, Cancelled: true}
	if d.mode == inputMode && d.Combo {
		if d.ComboOpen {
			switch event.Key() {
			case tcell.KeyEscape:
				d.ComboOpen = false
				return false, Result{}
			case tcell.KeyUp:
				d.ComboSelected = max(0, d.ComboSelected-1)
				return false, Result{}
			case tcell.KeyDown:
				d.ComboSelected = min(max(0, len(d.ComboItems)-1), d.ComboSelected+1)
				return false, Result{}
			case tcell.KeyEnter:
				if len(d.ComboItems) > 0 {
					d.Input.SetText(d.ComboItems[d.ComboSelected])
					return true, Result{Button: 0, Text: d.Input.Text()}
				}
				d.ComboOpen = false
				return false, Result{}
			case tcell.KeyCtrlD:
				if len(d.ComboItems) > 0 && d.Input.history != nil {
					_, _ = d.Input.history.Forget(d.ComboItems[d.ComboSelected])
					d.ComboItems = d.Input.history.Entries()
					d.filterComboItems()
					d.ComboSelected = min(d.ComboSelected, max(0, len(d.ComboItems)-1))
				}
				return false, Result{}
			}
		} else if event.Key() == tcell.KeyDown {
			d.filterComboItems()
			d.ComboSelected = 0
			d.ComboOpen = len(d.ComboItems) > 0
			return false, Result{}
		}
	}
	if d.mode == pickerMode && d.Filter {
		switch event.Key() {
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if len(d.FilterText) > 0 {
				d.FilterText = string([]rune(d.FilterText)[:len([]rune(d.FilterText))-1])
				d.Selected = 0
			}
			return false, Result{}
		case tcell.KeyRune:
			if event.Modifiers() == tcell.ModNone && event.Rune() >= ' ' {
				d.FilterText += string(event.Rune())
				d.Selected = 0
				return false, Result{}
			}
		}
	}
	switch event.Key() {
	case tcell.KeyEscape:
		if d.mode == progressMode {
			if d.onCancel != nil {
				d.onCancel()
			}
			return false, Result{}
		}
		return true, cancel
	case tcell.KeyTab, tcell.KeyBacktab:
		if d.mode == inputMode {
			if event.Key() == tcell.KeyTab {
				if d.Focused == len(d.Buttons)-1 {
					d.Focused = -1
				} else {
					d.Focused++
				}
			} else {
				if d.Focused <= 0 {
					d.Focused = -1
				} else {
					d.Focused--
				}
			}
			return false, Result{}
		}
		if len(d.Buttons) > 1 {
			d.Focused = (d.Focused + 1) % len(d.Buttons)
			if event.Key() == tcell.KeyBacktab {
				d.Focused = (d.Focused + len(d.Buttons) - 2) % len(d.Buttons)
			}
		}
		return false, Result{}
	case tcell.KeyUp, tcell.KeyPgUp:
		if d.mode == listMode {
			d.Offset = max(0, d.Offset-1)
			return false, Result{}
		}
		if d.mode == pickerMode {
			d.Selected = max(0, d.Selected-1)
			return false, Result{}
		}
	case tcell.KeyDown, tcell.KeyPgDn:
		if d.mode == listMode {
			d.Offset++
			return false, Result{}
		}
		if d.mode == pickerMode {
			d.Selected = min(max(0, len(d.visibleItems())-1), d.Selected+1)
			return false, Result{}
		}
	}

	if d.mode == inputMode && d.Focused == -1 && d.Input.HandleKey(event) {
		if d.Combo {
			d.filterComboItems()
			d.ComboSelected = 0
		}
		return false, Result{}
	}
	switch event.Key() {
	case tcell.KeyEnter:
		if d.mode == inputMode && d.Focused == -1 {
			return true, Result{Button: 0, Text: d.Input.Text()}
		}
		if d.mode == inputMode && d.Focused > 0 {
			return true, Result{Button: d.Focused, Text: d.Input.Text(), Cancelled: true}
		}
		if d.mode == pickerMode {
			items := d.visibleItems()
			if len(items) == 0 {
				return true, Result{Button: 1, Cancelled: true}
			}
			return true, Result{Button: 0, Selected: d.visibleItemIndices()[d.Selected], Text: items[d.Selected]}
		}
		return true, Result{Button: d.Focused}
	case tcell.KeyRune:
		if d.mode == inputMode {
			return false, Result{}
		}
		if d.mode == listMode && (event.Rune() == 'j' || event.Rune() == 'k') {
			if event.Rune() == 'j' {
				d.Offset++
			} else {
				d.Offset = max(0, d.Offset-1)
			}
			return false, Result{}
		}
	}
	return false, Result{}
}

func (d *Dialog) visibleItems() []string {
	var items []string
	for _, index := range d.visibleItemIndices() {
		items = append(items, d.Items[index])
	}
	return items
}

func (d *Dialog) visibleItemIndices() []int {
	var indices []int
	needle := strings.ToLower(d.FilterText)
	for index, item := range d.Items {
		if !d.Filter || needle == "" || strings.Contains(strings.ToLower(item), needle) {
			indices = append(indices, index)
		}
	}
	return indices
}

func (d *Dialog) filterComboItems() {
	if d.Input == nil || d.Input.history == nil {
		d.ComboItems = nil
		return
	}
	needle := strings.ToLower(d.Input.Text())
	d.ComboItems = d.Input.history.Entries()
	if needle == "" {
		return
	}
	filtered := d.ComboItems[:0]
	for _, item := range d.ComboItems {
		if strings.Contains(strings.ToLower(item), needle) {
			filtered = append(filtered, item)
		}
	}
	d.ComboItems = filtered
}

func (d *Dialog) Draw(screen tcell.Screen, theme ui.Theme) {
	screenWidth, screenHeight := screen.Size()
	var lines []string
	for _, line := range d.Lines {
		lines = append(lines, Wrap(line, max(1, screenWidth-8))...)
	}
	items := d.visibleItems()
	if d.mode == pickerMode {
		lines = append([]string(nil), items...)
	}
	footer := ""
	if d.mode == listMode && len(lines) > 0 {
		bodyRows := max(1, min(len(lines), screenHeight-7))
		d.Offset = min(d.Offset, max(0, len(lines)-bodyRows))
		footer = fmt.Sprintf("%d-%d of %d", d.Offset+1, min(d.Offset+bodyRows, len(lines)), len(lines))
		lines = lines[d.Offset:min(d.Offset+bodyRows, len(lines))]
	}

	longest := displayWidth(d.Title)
	for _, line := range lines {
		longest = max(longest, displayWidth(line))
	}
	if d.Prompt != "" {
		longest = max(longest, displayWidth(d.Prompt)+32)
	}
	if footer != "" {
		longest = max(longest, displayWidth(footer))
	}
	for _, button := range d.Buttons {
		longest = max(longest, displayWidth(button)+4)
	}
	width := min(72, max(44, longest+4))
	if screenWidth < width {
		width = screenWidth
	}
	height := min(screenHeight, max(7, len(lines)+6))
	if d.mode == inputMode {
		height = 8
		if d.ComboOpen {
			height = min(screenHeight, 8+min(8, len(d.ComboItems)))
		}
	}
	if d.mode == listMode {
		height = min(screenHeight, max(8, len(lines)+7))
		if footer != "" {
			height = min(screenHeight, len(lines)+7)
		}
	}
	x, y := (screenWidth-width)/2, (screenHeight-height)/2
	rect := ui.Rect{X: x, Y: y, W: width, H: height}
	ui.Fill(screen, rect, theme.Dialog)
	ui.DrawBox(screen, rect, ui.Double, theme.Border)
	titleStyle := theme.Border
	if d.Error {
		titleStyle = theme.Error
	}
	ui.Text(screen, x+2, y, width-4, ui.Truncate(d.Title, width-4), titleStyle)

	bodyStart := y + 2
	bodyWidth := max(0, width-4)
	for index, line := range lines {
		bodyY := bodyStart + index
		bodyBottom := y + height - 3
		if d.mode == listMode {
			bodyBottom = y + height - 5
		}
		if bodyY >= bodyBottom {
			break
		}
		ui.Text(screen, x+2, bodyY, bodyWidth, ui.Truncate(line, bodyWidth), theme.Dialog)
	}
	if d.mode == inputMode && y+height-4 > bodyStart {
		ui.Text(screen, x+2, y+4, min(bodyWidth, displayWidth(d.Prompt)), d.Prompt, theme.Dialog)
		d.Input.Draw(screen, x+2+displayWidth(d.Prompt), y+4, max(0, bodyWidth-displayWidth(d.Prompt)), theme.Dialog)
	}
	if d.mode == inputMode && d.ComboOpen {
		startY := y + 5
		rows := min(8, min(len(d.ComboItems), y+height-3-startY))
		for index := 0; index < rows; index++ {
			style := theme.Dialog
			if index == d.ComboSelected {
				style = theme.Selection
			}
			ui.TextPadded(screen, x+2, startY+index, bodyWidth, ui.Truncate(d.ComboItems[index], bodyWidth), style)
		}
	}
	if d.mode == progressMode {
		drawProgress(screen, x+2, bodyStart, bodyWidth, d.Progress, theme)
		if d.ProgressInfo != "" {
			ui.Text(screen, x+2, bodyStart+1, bodyWidth, ui.Truncate(d.ProgressInfo, bodyWidth), theme.Dialog)
		}
	}
	if d.mode == pickerMode {
		for index, item := range items {
			row := bodyStart + index
			if row >= y+height-3 {
				break
			}
			style := theme.Dialog
			if index == d.Selected {
				style = theme.Selection
			}
			ui.TextPadded(screen, x+2, row, bodyWidth, ui.Truncate(item, bodyWidth), style)
		}
	}
	if footer != "" {
		ui.TextRight(screen, x+2, y+height-3, bodyWidth, footer, theme.HintLabel)
	}

	if len(d.Buttons) > 0 {
		buttons := make([]string, len(d.Buttons))
		for index, button := range d.Buttons {
			if index == d.Focused {
				buttons[index] = "[" + button + "]"
			} else {
				buttons[index] = " " + button + " "
			}
		}
		buttonRow := strings.Join(buttons, "  ")
		ui.TextCenter(screen, x+1, y+height-2, width-2, buttonRow, theme.Selection)
	}
}

func drawProgress(screen tcell.Screen, x, y, width, percent int, theme ui.Theme) {
	if width <= 0 {
		return
	}
	percent = min(max(percent, 0), 100)
	barWidth := max(0, width-7)
	filled := min(max(barWidth*percent/100, 0), barWidth)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	line := fmt.Sprintf("%s %3d%%", bar, percent)
	ui.TextPadded(screen, x, y, width, line, theme.Dialog)
}

func Wrap(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var wrapped []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			wrapped = append(wrapped, "")
			continue
		}
		line := ""
		for _, word := range words {
			for displayWidth(word) > width {
				runes := []rune(word)
				cut, used := 0, 0
				for cut < len(runes) && used+max(0, runesWidth(runes[cut:cut+1])) <= width {
					used += runesWidth(runes[cut : cut+1])
					cut++
				}
				if cut == 0 {
					cut = 1
				}
				if line != "" {
					wrapped = append(wrapped, line)
					line = ""
				}
				wrapped = append(wrapped, string(runes[:cut]))
				word = string(runes[cut:])
			}
			if line == "" {
				line = word
			} else if displayWidth(line)+1+displayWidth(word) <= width {
				line += " " + word
			} else {
				wrapped = append(wrapped, line)
				line = word
			}
		}
		if line != "" {
			wrapped = append(wrapped, line)
		}
	}
	return wrapped
}

func runesWidth(runes []rune) int {
	width := 0
	graphemes := uniseg.NewGraphemes(string(runes))
	for graphemes.Next() {
		width += max(0, graphemes.Width())
	}
	return width
}
