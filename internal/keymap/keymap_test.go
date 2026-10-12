package keymap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestParseStringAndEventNormaliseCtrlAndShiftedFunctionKeys(t *testing.T) {
	for _, spelling := range []string{"F5", "Shift-F6", "Ctrl-O", "Alt-f", "Alt-Enter", "Ctrl-\\", "Space", "Comma"} {
		binding, err := Parse(spelling)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spelling, err)
		}
		roundTrip, err := Parse(binding.String())
		if err != nil || roundTrip != binding {
			t.Fatalf("%q -> %q -> %+v err=%v, original %+v", spelling, binding.String(), roundTrip, err, binding)
		}
	}
	shifted := FromEvent(tcell.NewEventKey(tcell.KeyF18, 0, tcell.ModNone))
	expected, _ := Parse("Shift-F6")
	if shifted != expected {
		t.Fatalf("F18 normalized to %+v, want %+v", shifted, expected)
	}
	control := FromEvent(tcell.NewEventKey(tcell.KeyCtrlO, 'o', tcell.ModCtrl))
	expected, _ = Parse("Ctrl-O")
	if control != expected {
		t.Fatalf("Ctrl-O normalized to %+v, want %+v", control, expected)
	}
}

func TestRebindRefusesReservedAndMovesBindingWithinMode(t *testing.T) {
	m := New()
	if err := m.Rebind(Panel, "copy", Binding{Key: tcell.KeyTab}); err == nil {
		t.Fatal("binding Tab was accepted")
	}
	if err := m.Rebind(Panel, "copy", Binding{Key: tcell.KeyRune, Rune: 'c'}); err != nil {
		t.Fatal(err)
	}
	action, ok := m.Lookup(Panel, tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone))
	if !ok || action != "copy" {
		t.Fatalf("rebound key action=%q ok=%v", action, ok)
	}
	if _, ok := m.Lookup(Panel, tcell.NewEventKey(tcell.KeyF5, 0, tcell.ModNone)); ok {
		t.Fatal("old copy key remains bound")
	}
}

func TestLoadSaveRetainSettingsAndOnlyWriteChangedBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bcrc")
	if err := os.WriteFile(path, []byte("[settings]\nhidden-files = on\n[panel]\ncopy = Ctrl-G\n[unknown]\nquit = F2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, problems := Load(path)
	if len(problems) != 1 {
		t.Fatalf("problems = %v", problems)
	}
	if got := m.Settings()["hidden-files"]; got != "on" {
		t.Fatalf("setting = %q", got)
	}
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "[settings]\nhidden-files = on\n\n[panel]\ncopy = Ctrl-G\n" {
		t.Fatalf("saved config = %q", data)
	}
}

func TestAliasesAndF1AreReserved(t *testing.T) {
	for _, spelling := range []string{"Ctrl-I", "Ctrl-H", "F1"} {
		binding, err := Parse(spelling)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spelling, err)
		}
		if reason, ok := Reserved(binding); !ok || reason == "" {
			t.Fatalf("%s not reserved", spelling)
		}
	}
}
