package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Tab identifiers
const (
	tabConexoes  = "conexoes"
	tabBiblioteca = "biblioteca"
	tabAnalisador = "analisador"
	tabConfig    = "config"
	tabColabore  = "colabore"
)

// NewMainView builds the ExitLag-style main layout.
//
//	┌──────────────────────────────────────────────────────┐
//	│ [Logo]                                                │
//	│ ──────────────────────────────────────────────────── │
//	│ [icon] Conexões        │  Content pane               │
//	│ [icon] Biblioteca      │                             │
//	│ [icon] Analisador de   │                             │
//	│        Rede            │                             │
//	│ [icon] Configurações   │                             │
//	│ ──────────────────────  │                             │
//	│ [icon] Colabore        │                             │
//	└──────────────────────────────────────────────────────┘
func NewMainView(state *AppState) fyne.CanvasObject {
	var panels [5]fyne.CanvasObject // conexoes, biblioteca, analisador, config, colabore
	contentStack := container.NewStack()

	currentTab := tabConexoes

	var navBtns [5]*sidebarBtn
	tabOrder := []string{tabConexoes, tabBiblioteca, tabAnalisador, tabConfig, tabColabore}

	selectTab := func(tab string) {
		// Stop graph goroutine when leaving the analyzer tab
		if tab != tabAnalisador {
			state.StopGraph()
		}

		currentTab = tab
		for i, t := range tabOrder {
			navBtns[i].setActive(t == tab)
		}

		idx := tabIndex(tab)
		// Re-create analyzer each time so goroutines start fresh
		if tab == tabAnalisador {
			panels[idx] = nil
		}
		if panels[idx] == nil {
			panels[idx] = buildPanel(state, tab)
		}
		contentStack.Objects = []fyne.CanvasObject{panels[idx]}
		contentStack.Refresh()
	}
	_ = currentTab

	// ── Sidebar ───────────────────────────────────────────────────
	sidebar := buildExitLagSidebar(state, selectTab, &navBtns)

	// Default panel
	panels[0] = buildPanel(state, tabConexoes)
	contentStack.Objects = []fyne.CanvasObject{panels[0]}
	navBtns[0].setActive(true)

	split := container.NewHSplit(sidebar, container.NewStack(canvas.NewRectangle(ColorBG), contentStack))
	split.SetOffset(0.22)
	return split
}

// ── Sidebar ───────────────────────────────────────────────────────────────

func buildExitLagSidebar(state *AppState, onSelect func(string), btns *[5]*sidebarBtn) fyne.CanvasObject {
	// Logo image
	logoImg := canvas.NewImageFromResource(LogoResource())
	logoImg.FillMode = canvas.ImageFillContain
	logoImg.SetMinSize(fyne.NewSize(160, 52))

	logoArea := container.NewCenter(container.NewPadded(logoImg))
	logoBg := canvas.NewRectangle(ColorPanel)
	logoBlock := container.NewStack(logoBg, container.NewPadded(logoArea))

	sep := widget.NewSeparator()

	// ── "Como funciona?" abaixo da logo ──────────────────────────
	howTitle := canvas.NewText(T("howworks.title"), ColorAccent)
	howTitle.TextSize = 11
	howTitle.TextStyle = fyne.TextStyle{Bold: true}

	step1 := canvas.NewText(T("howworks.step1"), ColorTextDim)
	step1.TextSize = 10
	step2 := canvas.NewText(T("howworks.step2"), ColorTextDim)
	step2.TextSize = 10
	step3 := canvas.NewText(T("howworks.step3"), ColorTextDim)
	step3.TextSize = 10

	howBg := canvas.NewRectangle(colorWithAlpha(ColorAccent, 12))
	howBg.CornerRadius = 8
	howBlock := container.NewStack(howBg, container.NewPadded(container.NewVBox(
		howTitle, spacer(2), step1, step2, step3,
	)))

	// Nav items (matching ExitLag's sidebar order)
	type navDef struct {
		icon, labelKey, id string
	}
	navItems := []navDef{
		{"🔗", "nav.conexoes", tabConexoes},
		{"📚", "nav.biblioteca", tabBiblioteca},
		{"📊", "nav.analisador", tabAnalisador},
		{"⚙", "nav.config", tabConfig},
	}

	navBox := container.NewVBox()
	for i, item := range navItems {
		i, item := i, item
		btn := newSidebarBtn(item.icon, T(item.labelKey), func() { onSelect(item.id) })
		btns[i] = btn
		navBox.Add(btn.obj)
	}

	// ── Lagger Network counter ────────────────────────────────────
	laggerCount := canvas.NewText("⚡ … Laggers", ColorAccent)
	laggerCount.TextSize = 12
	laggerCount.TextStyle = fyne.TextStyle{Bold: true}
	laggerCount.Alignment = fyne.TextAlignCenter

	laggerStatus := canvas.NewText(T("collab.lagger.join"), ColorTextDim)
	laggerStatus.TextSize = 10
	laggerStatus.Alignment = fyne.TextAlignCenter

	laggerBg := canvas.NewRectangle(colorWithAlpha(ColorAccent, 15))
	laggerBg.CornerRadius = 8

	laggerBlock := container.NewStack(laggerBg, container.NewPadded(container.NewVBox(
		container.NewCenter(laggerCount),
		container.NewCenter(laggerStatus),
	)))

	// Goroutine: atualiza contador e status a cada 60s
	go func() {
		update := func() {
			if state.Discovery == nil {
				return
			}
			count, err := state.Discovery.GetCount()
			if err == nil {
				laggerCount.Text = fmt.Sprintf("⚡ %d Laggers", count)
				if count > 0 {
					laggerCount.Color = ColorGreen
				} else {
					laggerCount.Color = ColorAccent
				}
			} else {
				laggerCount.Text = "⚡ — Laggers"
				laggerCount.Color = ColorTextDim
			}
			laggerCount.Refresh()

			if state.AmIALagger {
				laggerStatus.Text = T("collab.lagger.me")
				laggerStatus.Color = ColorGreen
			} else {
				laggerStatus.Text = T("collab.lagger.join")
				laggerStatus.Color = ColorTextDim
			}
			laggerStatus.Refresh()
		}
		update()
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			update()
		}
	}()

	// ── Seletor de idioma ─────────────────────────────────────────
	makeLangBtn := func(flag string, lang Lang) *widget.Button {
		btn := widget.NewButton(flag, func() {
			SetLang(lang)
			state.Language = lang
			state.Win.SetContent(container.NewStack(
				canvas.NewRectangle(ColorBG),
				NewMainView(state),
			))
		})
		btn.Importance = widget.LowImportance
		if CurrentLang() == lang {
			btn.Importance = widget.HighImportance
		}
		return btn
	}
	langRow := container.NewCenter(container.NewHBox(
		makeLangBtn("🇧🇷", LangPT),
		makeLangBtn("🇪🇸", LangES),
		makeLangBtn("🇺🇸", LangEN),
	))

	// Bottom separator + Colabore
	bottomSep := widget.NewSeparator()
	colaboreBtn := newSidebarBtn("💙", T("nav.colabore"), func() { onSelect(tabColabore) })
	btns[4] = colaboreBtn

	ver := canvas.NewText(T("app.version"), ColorTextDim)
	ver.TextSize = 9

	sidebarBg := canvas.NewRectangle(ColorPanel)

	sidebar := container.NewStack(sidebarBg, container.NewBorder(
		container.NewVBox(logoBlock, sep, container.NewPadded(howBlock), sep),
		container.NewVBox(
			widget.NewSeparator(),
			container.NewPadded(laggerBlock),
			container.NewPadded(langRow),
			bottomSep,
			colaboreBtn.obj,
			container.NewPadded(container.NewCenter(ver)),
		),
		nil, nil,
		container.NewPadded(navBox),
	))

	return sidebar
}

// ── sidebarBtn ────────────────────────────────────────────────────────────

type sidebarBtn struct {
	obj    fyne.CanvasObject
	bg     *canvas.Rectangle
	icon   *canvas.Text
	label  *canvas.Text
	active bool
}

func newSidebarBtn(icon, label string, onTap func()) *sidebarBtn {
	sb := &sidebarBtn{}

	sb.bg = canvas.NewRectangle(color.Transparent)
	sb.bg.CornerRadius = 8

	sb.icon = canvas.NewText(icon, ColorTextSec)
	sb.icon.TextSize = 16

	sb.label = canvas.NewText(label, ColorTextSec)
	sb.label.TextSize = 13

	row := container.NewHBox(
		container.NewPadded(sb.icon),
		sb.label,
	)

	tap := widget.NewButton("", onTap)
	tap.Importance = widget.LowImportance

	// Stack: bg → row content → invisible tap button
	sb.obj = container.NewStack(
		sb.bg,
		container.NewPadded(row),
		tap,
	)
	return sb
}

func (sb *sidebarBtn) setActive(active bool) {
	sb.active = active
	if active {
		sb.bg.FillColor = colorWithAlpha(ColorGreen, 25)
		sb.icon.Color = ColorGreen
		sb.label.Color = ColorTextPrim
		sb.label.TextStyle = fyne.TextStyle{Bold: true}
	} else {
		sb.bg.FillColor = color.Transparent
		sb.icon.Color = ColorTextSec
		sb.label.Color = ColorTextSec
		sb.label.TextStyle = fyne.TextStyle{}
	}
	sb.bg.Refresh()
	sb.icon.Refresh()
	sb.label.Refresh()
}

// ── Panel factory ─────────────────────────────────────────────────────────

func buildPanel(state *AppState, tab string) fyne.CanvasObject {
	switch tab {
	case tabConexoes:
		return NewConexoesPanel(state)
	case tabBiblioteca:
		return NewBibliotecaPanel(state)
	case tabAnalisador:
		return NewStatusPanel(state)
	case tabConfig:
		return NewConfigPanel(state)
	case tabColabore:
		return NewColaborePanel(state)
	}
	return canvas.NewRectangle(ColorBG)
}

func tabIndex(tab string) int {
	switch tab {
	case tabConexoes:
		return 0
	case tabBiblioteca:
		return 1
	case tabAnalisador:
		return 2
	case tabConfig:
		return 3
	case tabColabore:
		return 4
	}
	return 0
}

