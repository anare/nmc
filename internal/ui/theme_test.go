package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestDefaultThemeIsMidnightCommanderBlue(t *testing.T) {
	theme := DefaultTheme()
	if theme.Name != ThemeMidnightCommander {
		t.Fatalf("default theme name = %q", theme.Name)
	}
	_, panelBG, _ := theme.Panel.Decompose()
	_, selectionBG, _ := theme.Selection.Decompose()
	if panelBG != tcell.NewRGBColor(0, 0, 170) {
		t.Fatalf("panel background = %v, want Midnight Commander blue", panelBG)
	}
	if selectionBG != tcell.NewRGBColor(0, 170, 170) {
		t.Fatalf("selection background = %v, want cyan", selectionBG)
	}
}

func TestThemeByNameProvidesAlternatePalettesAndRejectsUnknownNames(t *testing.T) {
	mc, _ := ThemeByName(ThemeMidnightCommander)
	dark, darkOK := ThemeByName(" DARK ")
	light, lightOK := ThemeByName(ThemeLight)
	if !darkOK || !lightOK {
		t.Fatal("built-in alternate theme was not found")
	}
	if dark.Name != ThemeDark || light.Name != ThemeLight {
		t.Fatalf("alternate theme names = %q, %q", dark.Name, light.Name)
	}
	_, mcBG, _ := mc.Panel.Decompose()
	_, darkBG, _ := dark.Panel.Decompose()
	_, lightBG, _ := light.Panel.Decompose()
	if mcBG == darkBG || mcBG == lightBG || darkBG == lightBG {
		t.Fatalf("theme backgrounds are not distinct: %v %v %v", mcBG, darkBG, lightBG)
	}
	if _, ok := ThemeByName("missing"); ok {
		t.Fatal("unknown theme unexpectedly resolved")
	}
}
