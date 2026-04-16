package ui

import (
	"fmt"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"laggado/internal/gamedet"
	"laggado/internal/gamelib"
	"laggado/internal/store"
)

// NewBibliotecaPanel builds the game library browser.
func NewBibliotecaPanel(state *AppState) fyne.CanvasObject {
	title := canvas.NewText(T("lib.title"), ColorTextPrim)
	title.TextSize = 20
	title.TextStyle = fyne.TextStyle{Bold: true}

	countLabel := canvas.NewText(fmt.Sprintf(T("lib.available"), len(gamelib.All())), ColorTextDim)
	countLabel.TextSize = 11

	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder(T("lib.search"))

	// ── Detected game banner ──────────────────────────────────────
	detectedBanner := buildDetectedGameBanner(state)

	// ── Game list (VBox — sem widget.List para evitar type assertions) ──
	gameVBox := container.NewVBox()

	buildList := func(games []gamelib.Game) {
		gameVBox.Objects = nil
		for _, g := range games {
			g := g

			iconImg := canvas.NewImageFromResource(placeholderGameIcon(g.ID))
			iconImg.FillMode = canvas.ImageFillContain
			iconImg.SetMinSize(fyne.NewSize(36, 36))
			if g.IconPath != "" {
				if raw, err := os.ReadFile(g.IconPath); err == nil {
					iconImg.Resource = fyne.NewStaticResource(fmt.Sprintf("g%d.png", g.ID), raw)
				}
			}

			nameText := canvas.NewText(g.Name, ColorTextPrim)
			nameText.TextSize = 13

			addBtn := widget.NewButton(T("lib.add"), func() {
				state.DB.AddConnection(store.GameConnection{
					GameID:   g.ID,
					GameName: g.Name,
					GameExe:  gameNameToExeSlug(g.Name),
					Enabled:  true,
					Region:   "AUTO",
				})
				state.DB.Save()
			})
			addBtn.Importance = widget.LowImportance

			bg := canvas.NewRectangle(ColorCard)
			bg.CornerRadius = 8

			row := container.NewStack(bg, container.NewPadded(
				container.NewBorder(nil, nil, iconImg, addBtn, nameText),
			))
			gameVBox.Add(row)
		}
		gameVBox.Refresh()
	}

	all := gamelib.All()
	if len(all) > 200 {
		all = all[:200]
	}
	buildList(all)

	searchEntry.OnChanged = func(query string) {
		results := gamelib.Search(query)
		if len(results) > 200 {
			results = results[:200]
		}
		countLabel.Text = fmt.Sprintf(T("lib.found"), len(results))
		countLabel.Refresh()
		buildList(results)
	}

	header := container.NewVBox(
		container.NewBorder(nil, nil, title, nil, countLabel),
		container.NewPadded(searchEntry),
		widget.NewSeparator(),
	)

	topSection := container.NewVBox(
		container.NewPadded(header),
		detectedBanner,
	)

	return container.NewStack(
		canvas.NewRectangle(ColorBG),
		container.NewBorder(
			topSection,
			nil, nil, nil,
			container.NewVScroll(container.NewPadded(gameVBox)),
		),
	)
}

// buildDetectedGameBanner cria um card que detecta o jogo aberto e oferece criar rota.
func buildDetectedGameBanner(state *AppState) fyne.CanvasObject {
	bannerContainer := container.NewVBox()

	refresh := func() {
		bannerContainer.Objects = nil

		games, err := gamedet.DetectGames()
		if err != nil || len(games) == 0 {
			bannerContainer.Refresh()
			return
		}

		// Tenta encontrar o jogo na gamelib
		g := gamelib.FindByExeName(games[0].Name)
		if g == nil {
			bannerContainer.Refresh()
			return
		}

		// Já está nas conexões?
		for _, c := range state.DB.GetConnections() {
			if c.GameID == g.ID {
				bannerContainer.Refresh()
				return
			}
		}

		// Mostra o banner
		icon := canvas.NewImageFromResource(placeholderGameIcon(g.ID))
		icon.FillMode = canvas.ImageFillContain
		icon.SetMinSize(fyne.NewSize(40, 40))

		detTitle := canvas.NewText(T("lib.detected")+": "+g.Name, ColorAccent)
		detTitle.TextSize = 14
		detTitle.TextStyle = fyne.TextStyle{Bold: true}

		detSub := canvas.NewText(T("lib.detected.sub"), ColorTextSec)
		detSub.TextSize = 11

		addBtn := widget.NewButton(T("lib.detected.btn"), func() {
			state.DB.AddConnection(store.GameConnection{
				GameID:   g.ID,
				GameName: g.Name,
				GameExe:  gameNameToExeSlug(g.Name),
				Enabled:  true,
				Region:   "AUTO",
			})
			state.DB.Save()
			bannerContainer.Objects = nil
			bannerContainer.Refresh()
		})
		addBtn.Importance = widget.HighImportance

		bg := canvas.NewRectangle(colorWithAlpha(ColorAccent, 20))
		bg.CornerRadius = 10

		content := container.NewBorder(nil, nil, icon, addBtn,
			container.NewVBox(detTitle, detSub),
		)

		bannerContainer.Add(container.NewStack(bg, container.NewPadded(content)))
		bannerContainer.Refresh()
	}

	refresh()

	// Re-detecta a cada 10 segundos
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refresh()
		}
	}()

	return bannerContainer
}
