package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
)

const (
	ThemeMidnightCommander = "midnight-commander"
	ThemeDark              = "dark"
	ThemeLight             = "light"
)

func DefaultTheme() Theme {
	theme, _ := ThemeByName(ThemeMidnightCommander)
	return theme
}

func ThemeNames() []string {
	return []string{ThemeMidnightCommander, ThemeDark, ThemeLight}
}

func ThemeLabel(name string) string {
	switch name {
	case ThemeMidnightCommander:
		return "Midnight Commander"
	case ThemeDark:
		return "Dark"
	case ThemeLight:
		return "Light"
	default:
		return name
	}
}

func ThemeByName(name string) (Theme, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ThemeMidnightCommander:
		return makeTheme(
			ThemeMidnightCommander,
			tcell.NewRGBColor(0, 0, 170),
			tcell.NewRGBColor(0, 0, 170),
			tcell.NewRGBColor(0, 0, 128),
			tcell.NewRGBColor(0, 170, 170),
			tcell.ColorBlack,
		), true
	case ThemeDark:
		return makeTheme(
			ThemeDark,
			tcell.NewRGBColor(24, 26, 32),
			tcell.NewRGBColor(32, 35, 43),
			tcell.NewRGBColor(40, 43, 52),
			tcell.NewRGBColor(100, 110, 128),
			tcell.ColorWhite,
		), true
	case ThemeLight:
		return makeTheme(
			ThemeLight,
			tcell.NewRGBColor(238, 240, 244),
			tcell.ColorWhite,
			tcell.NewRGBColor(248, 249, 251),
			tcell.NewRGBColor(80, 95, 120),
			tcell.ColorWhite,
		), true
	default:
		return Theme{}, false
	}
}

func makeTheme(name string, background, panel, dialog, accent, selectionFG tcell.Color) Theme {
	base := func(color tcell.Color) tcell.Style {
		return tcell.StyleDefault.Foreground(color).Background(panel)
	}
	bold := func(color tcell.Color) tcell.Style { return base(color).Bold(true) }
	white := tcell.ColorWhite
	buttonFG := tcell.ColorBlack
	buttonBG := tcell.NewRGBColor(0, 170, 170)
	hotkeyFG := tcell.ColorYellow
	yellow := tcell.ColorYellow
	green := tcell.ColorGreen
	red := tcell.ColorRed
	cyan := tcell.ColorAqua
	magenta := tcell.ColorFuchsia
	silver := tcell.ColorSilver
	if name == ThemeLight {
		white = tcell.NewRGBColor(35, 42, 54)
		buttonFG = tcell.ColorWhite
		buttonBG = tcell.NewRGBColor(25, 65, 125)
		hotkeyFG = tcell.ColorYellow
		yellow = tcell.NewRGBColor(145, 85, 0)
		green = tcell.NewRGBColor(0, 110, 55)
		red = tcell.NewRGBColor(170, 25, 25)
		cyan = tcell.NewRGBColor(0, 105, 145)
		magenta = tcell.NewRGBColor(130, 35, 145)
		silver = tcell.NewRGBColor(70, 78, 90)
	} else if name == ThemeDark {
		buttonFG = tcell.ColorWhite
		buttonBG = tcell.NewRGBColor(70, 80, 100)
		hotkeyFG = tcell.NewRGBColor(255, 220, 100)
	}
	button := tcell.StyleDefault.Foreground(buttonFG).Background(buttonBG)
	return Theme{
		Name:       name,
		Background: tcell.StyleDefault.Background(background).Foreground(white),
		Panel:      tcell.StyleDefault.Background(panel).Foreground(white),
		Border:     base(accent),
		Dialog:     tcell.StyleDefault.Background(dialog).Foreground(white),
		Error:      tcell.StyleDefault.Background(red).Foreground(tcell.ColorWhite).Bold(true),
		Selection:  tcell.StyleDefault.Background(accent).Foreground(selectionFG),
		Marked:     base(yellow).Bold(true),
		HintKey:    base(yellow).Bold(true),
		HintLabel:  base(white),
		HintBar:    tcell.StyleDefault.Foreground(white).Background(panel),
		HintButton: button,
		HintHotkey: button.Foreground(hotkeyFG).Bold(true),
		Directory:  bold(white),
		Executable: bold(green),
		Symlink:    base(cyan),
		BrokenLink: bold(red),
		Special:    base(tcell.NewRGBColor(180, 150, 0)),
		Archive:    base(magenta),
		Image:      base(tcell.NewRGBColor(147, 112, 219)),
		Media:      base(tcell.NewRGBColor(255, 140, 0)),
		Source:     base(tcell.NewRGBColor(135, 206, 250)),
		Document:   base(tcell.NewRGBColor(210, 180, 140)),
		Data:       base(tcell.NewRGBColor(240, 230, 140)),
		Temporary:  base(tcell.NewRGBColor(128, 128, 128)),
		Plain:      base(silver),
	}
}
