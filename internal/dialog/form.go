package dialog

import (
	"github.com/anare/nmc/internal/history"
	"github.com/anare/nmc/internal/ui"
	"github.com/gdamore/tcell/v2"
)

type Field struct {
	Label   string
	Input   *TextInput
	Check   bool
	IsCheck bool
}

func TextField(label, initial string, cursor *history.Cursor) Field {
	return Field{Label: label, Input: NewTextInput(initial).WithHistory(cursor)}
}

func CheckField(label string, on bool) Field {
	return Field{Label: label, Check: on, IsCheck: true}
}

type FormResult struct {
	Values []string
	Checks []bool
}

func (r FormResult) Value(index int) string {
	if index < 0 || index >= len(r.Values) {
		return ""
	}
	return r.Values[index]
}

func (r FormResult) Checked(index int) bool {
	return index >= 0 && index < len(r.Checks) && r.Checks[index]
}

type Form struct {
	Title   string
	Fields  []Field
	Focused int
}

func NewForm(title string, fields []Field) *Form {
	copied := append([]Field(nil), fields...)
	return &Form{Title: title, Fields: copied}
}

func (f *Form) HandleKey(event *tcell.EventKey) bool {
	if len(f.Fields) == 0 {
		return false
	}
	field := &f.Fields[f.Focused]
	if field.IsCheck {
		if event.Key() == tcell.KeyRune && event.Rune() == ' ' {
			field.Check = !field.Check
			return true
		}
	} else if field.Input != nil && field.Input.HandleKey(event) {
		return true
	}
	switch event.Key() {
	case tcell.KeyTab, tcell.KeyDown:
		f.Focused = (f.Focused + 1) % len(f.Fields)
		return true
	case tcell.KeyBacktab, tcell.KeyUp:
		f.Focused = (f.Focused + len(f.Fields) - 1) % len(f.Fields)
		return true
	}
	return false
}

func (f *Form) Result() FormResult {
	result := FormResult{Values: make([]string, len(f.Fields)), Checks: make([]bool, len(f.Fields))}
	for index, field := range f.Fields {
		if field.IsCheck {
			result.Checks[index] = field.Check
		} else if field.Input != nil {
			result.Values[index] = field.Input.Text()
		}
	}
	return result
}

func (f *Form) Draw(screen tcell.Screen, rect ui.Rect, theme ui.Theme) {
	ui.Fill(screen, rect, theme.Dialog)
	ui.DrawBox(screen, rect, ui.Double, theme.Border)
	ui.Text(screen, rect.X+2, rect.Y, rect.W-4, ui.Truncate(f.Title, rect.W-4), theme.Border)
	labelWidth := 0
	for _, field := range f.Fields {
		labelWidth = max(labelWidth, displayWidth(field.Label)+2)
	}
	for index, field := range f.Fields {
		y := rect.Y + 2 + index
		if y >= rect.Bottom()-1 {
			break
		}
		style := theme.Dialog
		if index == f.Focused {
			style = theme.Selection
		}
		ui.TextPadded(screen, rect.X+2, y, labelWidth, field.Label, style)
		if field.IsCheck {
			value := "[ ]"
			if field.Check {
				value = "[x]"
			}
			ui.Text(screen, rect.X+2+labelWidth, y, 3, value, style)
		} else if field.Input != nil {
			field.Input.Draw(screen, rect.X+2+labelWidth, y, rect.W-labelWidth-4, style)
		}
	}
}
