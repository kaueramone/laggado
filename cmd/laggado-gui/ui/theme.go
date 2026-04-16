package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"laggado/assets"
)

// Palette — dark gaming aesthetic, inspired by ExitLag's dark UI.
var (
	ColorBG        = color.NRGBA{R: 13, G: 14, B: 20, A: 255}    // #0D0E14 deepest bg
	ColorPanel     = color.NRGBA{R: 20, G: 22, B: 32, A: 255}    // #141620 panels
	ColorCard      = color.NRGBA{R: 26, G: 29, B: 42, A: 255}    // #1A1D2A cards
	ColorBorder    = color.NRGBA{R: 45, G: 50, B: 72, A: 255}    // #2D3248 borders
	ColorAccent    = color.NRGBA{R: 0, G: 212, B: 255, A: 255}   // #00D4FF cyan accent
	ColorAccent2   = color.NRGBA{R: 100, G: 65, B: 255, A: 255}  // #6441FF purple
	ColorGreen     = color.NRGBA{R: 0, G: 230, B: 118, A: 255}   // #00E676 good latency
	ColorYellow    = color.NRGBA{R: 255, G: 214, B: 0, A: 255}   // #FFD600 medium
	ColorRed       = color.NRGBA{R: 255, G: 59, B: 48, A: 255}   // #FF3B30 bad latency
	ColorTextPrim  = color.NRGBA{R: 230, G: 232, B: 245, A: 255} // #E6E8F5 primary text
	ColorTextSec   = color.NRGBA{R: 120, G: 128, B: 168, A: 255} // #7880A8 secondary text
	ColorTextDim   = color.NRGBA{R: 65, G: 72, B: 105, A: 255}   // #414869 dim text
	ColorButtonBG  = color.NRGBA{R: 0, G: 212, B: 255, A: 255}
	ColorButtonFG  = color.NRGBA{R: 0, G: 0, B: 0, A: 255}
)

// LatencyColor returns a color coding for a latency value in milliseconds.
func LatencyColor(ms float64) color.Color {
	switch {
	case ms <= 0:
		return ColorTextDim
	case ms < 60:
		return ColorGreen
	case ms < 120:
		return ColorYellow
	default:
		return ColorRed
	}
}

// LatencyLabel returns a text label for latency quality.
func LatencyLabel(ms float64) string {
	switch {
	case ms <= 0:
		return "—"
	case ms < 60:
		return "Excelente"
	case ms < 100:
		return "Bom"
	case ms < 150:
		return "Médio"
	default:
		return "Alto"
	}
}

// laggadoTheme is the custom Fyne theme.
type laggadoTheme struct{}

func DarkTheme() fyne.Theme { return &laggadoTheme{} }

func (t *laggadoTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return ColorBG
	case theme.ColorNameButton:
		return ColorCard
	case theme.ColorNameDisabledButton:
		return ColorPanel
	case theme.ColorNameForeground:
		return ColorTextPrim
	case theme.ColorNameDisabled:
		return ColorTextDim
	case theme.ColorNamePlaceHolder:
		return ColorTextSec
	case theme.ColorNamePrimary:
		return ColorAccent
	case theme.ColorNameFocus:
		return ColorAccent
	case theme.ColorNameHover:
		return ColorCard
	case theme.ColorNameInputBackground:
		return ColorPanel
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0, G: 212, B: 255, A: 60}
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 100}
	case theme.ColorNameScrollBar:
		return ColorBorder
	case theme.ColorNameSeparator:
		return ColorBorder
	case theme.ColorNameOverlayBackground:
		return ColorPanel
	case theme.ColorNameMenuBackground:
		return ColorPanel
	case theme.ColorNameHeaderBackground:
		return ColorPanel
	}
	return theme.DarkTheme().Color(name, variant)
}

func (t *laggadoTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DarkTheme().Font(style)
}

func (t *laggadoTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DarkTheme().Icon(name)
}

func (t *laggadoTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 13
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameSubHeadingText:
		return 15
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameScrollBar:
		return 4
	case theme.SizeNameScrollBarSmall:
		return 2
	case theme.SizeNameSeparatorThickness:
		return 1
	}
	return theme.DarkTheme().Size(name)
}

// AppIcon returns the application icon as a Fyne resource.
func AppIcon() fyne.Resource {
	return fyne.NewStaticResource("icon.png", assets.IconPNG)
}

// LogoResource returns the LAGGADO logo as a Fyne resource.
func LogoResource() fyne.Resource {
	return fyne.NewStaticResource("logo.png", assets.LogoPNG)
}
