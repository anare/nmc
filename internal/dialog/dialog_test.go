package dialog

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anare/nmc/internal/history"
	"github.com/anare/nmc/internal/ui"
	"github.com/gdamore/tcell/v2"
)

func TestInputDialogAcceptsAndCancels(t *testing.T) {
	dialog := NewInput("Path", "Path: ", "/tmp", nil)
	dialog.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	done, result := dialog.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !done || !result.OK() || result.Text != "/tmpx" {
		t.Fatalf("input result = (done %v, %#v)", done, result)
	}
	done, result = NewInput("Path", "Path: ", "", nil).HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if !done || !result.Cancelled {
		t.Fatalf("escape result = (done %v, %#v), want cancelled", done, result)
	}
}

func TestConfirmMovesFocusAndReturnsSelectedButton(t *testing.T) {
	d := NewConfirm("Delete", []string{"Delete selected file?"}, []string{"Delete", "Cancel"}, nil)
	d.HandleKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	done, result := d.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !done || result.Button != 1 || result.OK() {
		t.Fatalf("confirm result = (done %v, %#v)", done, result)
	}
}

func TestProgressDialogClampsAndEscapeRequestsCancellation(t *testing.T) {
	cancelled := false
	d := NewProgress("Copy", func() { cancelled = true })
	d.SetProgress(500, "copying")
	if d.Progress != 100 {
		t.Fatalf("progress = %d, want clamped 100", d.Progress)
	}
	done, _ := d.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if done || !cancelled {
		t.Fatalf("progress Escape done=%v cancelled=%v, want stay open and cancel", done, cancelled)
	}
}

func TestDialogDrawsCenteredAndWrapsLongText(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(60, 20)
	d := NewMessage("Notice", []string{"This is a long message that should wrap within the dialog window"}, false)
	d.Draw(screen, ui.DefaultTheme())
	var b strings.Builder
	for y := 0; y < 20; y++ {
		for x := 0; x < 60; x++ {
			r, _, _, _ := screen.GetContent(x, y)
			b.WriteRune(r)
		}
	}
	if !strings.Contains(b.String(), "Notice") || !strings.Contains(b.String(), "OK") {
		t.Fatalf("dialog rendering = %q", b.String())
	}
}

func TestTextInputEditingAndWordDeletion(t *testing.T) {
	input := NewTextInput("one two")
	input.HandleKey(tcell.NewEventKey(tcell.KeyCtrlW, 0, tcell.ModCtrl))
	if got := input.Text(); got != "one " {
		t.Fatalf("Ctrl-W result = %q, want %q", got, "one ")
	}
	input.HandleKey(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	input.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'X', tcell.ModNone))
	if got := input.Text(); got != "Xone " {
		t.Fatalf("insert result = %q, want %q", got, "Xone ")
	}
	input.HandleKey(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	input.HandleKey(tcell.NewEventKey(tcell.KeyCtrlK, 0, tcell.ModCtrl))
	if got := input.Text(); got != "" {
		t.Fatalf("Ctrl-K result = %q, want empty", got)
	}
}

func TestFormLetsTextFieldEditBeforeFocusMovesAndSpaceTogglesCheck(t *testing.T) {
	form := NewForm("Options", []Field{TextField("Path", "abc", nil), CheckField("Recursive", false)})
	if !form.HandleKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)) || form.Focused != 0 {
		t.Fatalf("Left did not edit focused text field")
	}
	form.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if form.Focused != 1 {
		t.Fatalf("focus after Down = %d, want checkbox", form.Focused)
	}
	form.HandleKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
	if !form.Result().Checked(1) {
		t.Fatal("Space did not toggle the checkbox")
	}
	form.HandleKey(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModShift))
	if form.Focused != 0 || form.Result().Value(0) != "abc" {
		t.Fatalf("form state after Shift-Tab = %#v", form.Result())
	}
}

func TestPickerFiltersAndReturnsSelectedItem(t *testing.T) {
	picker := NewPicker("Choose", []string{"alpha", "beta"}, nil)
	picker.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone))
	done, result := picker.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !done || result.Text != "beta" || result.Selected != 1 {
		t.Fatalf("picker result = done %v, %#v", done, result)
	}
	menu := NewMenu("Menu", []string{"alpha", "beta"}, nil)
	menu.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone))
	if menu.Selected != 0 {
		t.Fatalf("menu unexpectedly filtered to item %d", menu.Selected)
	}
}

func TestComboInputHistoryDropdownFilterAndForget(t *testing.T) {
	store, err := history.Open(t.TempDir() + "/history")
	if err != nil {
		t.Fatal(err)
	}
	store.Add(history.Host, "alpha.example")
	store.Add(history.Host, "beta.example")
	cursor := store.Cursor(history.Host)
	d := NewComboInput("Connect", "Host: ", "", cursor, nil)
	d.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if !d.ComboOpen || len(d.ComboItems) != 2 {
		t.Fatalf("dropdown state = open:%v items:%v", d.ComboOpen, d.ComboItems)
	}
	d.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	d.HandleKey(tcell.NewEventKey(tcell.KeyCtrlD, 0, tcell.ModNone))
	if got := cursor.Entries(); len(got) != 1 || got[0] != "beta.example" {
		t.Fatalf("history after forgetting selection = %v", got)
	}
	done, result := d.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !done || result.Text != "beta.example" {
		t.Fatalf("selection = done:%v result:%+v", done, result)
	}
}

func TestPickerFiltersByWholeTypedQuery(t *testing.T) {
	picker := NewPicker("History", []string{"alpha", "beta", "gamma"}, nil)
	picker.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	done, result := picker.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !done || result.Text != "beta" || result.Selected != 1 {
		t.Fatalf("filtered picker result = done %v, %#v", done, result)
	}
}

func TestPickerAndMenuScrollSelectedItemsIntoView(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(60, 10)

	items := make([]string, 30)
	for index := range items {
		items[index] = fmt.Sprintf("item-%02d", index)
	}
	for _, picker := range []*Dialog{NewPicker("Choose", items, nil), NewMenu("Menu", items, nil)} {
		for range 21 {
			picker.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
		}
		screen.Clear()
		picker.Draw(screen, ui.DefaultTheme())
		if picker.Selected < picker.Offset || picker.Selected >= picker.Offset+picker.visibleRows {
			t.Fatalf("selected item %d outside visible range %d..%d", picker.Selected, picker.Offset, picker.Offset+picker.visibleRows)
		}
		if !strings.Contains(dialogScreenText(screen), items[picker.Selected]) {
			t.Fatalf("selected item %q is missing from the rendered menu", items[picker.Selected])
		}
	}
}

func TestComboHistoryScrollsSelectedItemIntoView(t *testing.T) {
	store, err := history.Open(t.TempDir() + "/history")
	if err != nil {
		t.Fatal(err)
	}
	for index := range 20 {
		store.Add(history.Host, fmt.Sprintf("host-%02d", index))
	}
	d := NewComboInput("Connect", "Host: ", "", store.Cursor(history.Host), nil)
	d.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	for range 12 {
		d.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	}

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(60, 10)
	d.Draw(screen, ui.DefaultTheme())
	if d.ComboSelected < d.ComboOffset || d.ComboSelected >= d.ComboOffset+d.visibleRows {
		t.Fatalf("combo selection %d outside visible range %d..%d", d.ComboSelected, d.ComboOffset, d.ComboOffset+d.visibleRows)
	}
	if !strings.Contains(dialogScreenText(screen), d.ComboItems[d.ComboSelected]) {
		t.Fatalf("selected history item %q is missing from the rendered dropdown", d.ComboItems[d.ComboSelected])
	}
}

func TestListDialogPagesThroughVisibleRows(t *testing.T) {
	lines := make([]string, 20)
	for index := range lines {
		lines[index] = fmt.Sprintf("line-%02d", index)
	}
	d := NewList("Output", lines)
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(60, 10)
	d.Draw(screen, ui.DefaultTheme())
	d.HandleKey(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone))
	screen.Clear()
	d.Draw(screen, ui.DefaultTheme())
	if d.Offset != 3 || !strings.Contains(dialogScreenText(screen), "line-03") {
		t.Fatalf("paged list offset=%d rendered=%q", d.Offset, dialogScreenText(screen))
	}
}

func TestWrapFitsEveryResultWithinWidth(t *testing.T) {
	for _, line := range Wrap("a long message with界wide text", 8) {
		if displayWidth(line) > 8 {
			t.Fatalf("wrapped line %q has width %d", line, displayWidth(line))
		}
	}
}

func dialogScreenText(screen tcell.SimulationScreen) string {
	width, height := screen.Size()
	var result strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, _, _, _ := screen.GetContent(x, y)
			if r != 0 {
				result.WriteRune(r)
			}
		}
	}
	return result.String()
}
