package ui

import (
	"fmt"
	"os"
	"strings"
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

	// ── Steam/Epic installed games banner ─────────────────────────
	installedBanner := buildInstalledGamesBanner(state)

	// ── Game list — using widget.List for proper sizing/scroll ────
	var filtered []gamelib.Game
	all := gamelib.All()
	if len(all) > 300 {
		all = all[:300]
	}
	filtered = all

	gameList := widget.NewList(
		func() int { return len(filtered) },
		func() fyne.CanvasObject {
			icon := canvas.NewImageFromResource(placeholderGameIcon(0))
			icon.FillMode = canvas.ImageFillContain
			icon.SetMinSize(fyne.NewSize(32, 32))
			name := canvas.NewText("Game Name", ColorTextPrim)
			name.TextSize = 13
			btn := widget.NewButton(T("lib.add"), func() {})
			btn.Importance = widget.LowImportance
			return container.NewBorder(nil, nil, icon, btn, name)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(filtered) {
				return
			}
			g := filtered[id]
			row := obj.(*fyne.Container)
			icon := row.Objects[1].(*canvas.Image)
			name := row.Objects[0].(*canvas.Text)
			btn := row.Objects[2].(*widget.Button)

			name.Text = g.Name
			name.Refresh()

			icon.Resource = placeholderGameIcon(g.ID)
			if g.IconPath != "" {
				if raw, err := os.ReadFile(g.IconPath); err == nil {
					icon.Resource = fyne.NewStaticResource(fmt.Sprintf("g%d.png", g.ID), raw)
				}
			}
			icon.Refresh()

			btn.OnTapped = func() {
				state.DB.AddConnection(store.GameConnection{
					GameID:   g.ID,
					GameName: g.Name,
					GameExe:  gameNameToExeSlug(g.Name),
					Enabled:  true,
					Region:   "AUTO",
				})
				state.DB.Save()
			}
		},
	)
	_ = gameList // SetMinSize not available on widget.List; sizing handled by Border layout

	updateList := func(query string) {
		if strings.TrimSpace(query) == "" {
			filtered = gamelib.All()
			if len(filtered) > 300 {
				filtered = filtered[:300]
			}
		} else {
			results := gamelib.Search(query)
			if len(results) > 300 {
				results = results[:300]
			}
			filtered = results
		}
		countLabel.Text = fmt.Sprintf(T("lib.found"), len(filtered))
		countLabel.Refresh()
		gameList.Refresh()
	}

	searchEntry.OnChanged = updateList

	header := container.NewVBox(
		container.NewBorder(nil, nil, title, nil, countLabel),
		container.NewPadded(searchEntry),
		widget.NewSeparator(),
	)

	topSection := container.NewVBox(
		container.NewPadded(header),
		detectedBanner,
		installedBanner,
	)

	return container.NewStack(
		canvas.NewRectangle(ColorBG),
		container.NewBorder(
			topSection,
			nil, nil, nil,
			container.NewPadded(gameList),
		),
	)
}

// buildDetectedGameBanner detects running game processes and offers to add.
func buildDetectedGameBanner(state *AppState) fyne.CanvasObject {
	bannerContainer := container.NewVBox()

	refresh := func() {
		bannerContainer.Objects = nil

		games, err := gamedet.DetectGames()
		if err != nil || len(games) == 0 {
			bannerContainer.Refresh()
			return
		}

		g := gamelib.FindByExeName(games[0].Name)
		if g == nil {
			bannerContainer.Refresh()
			return
		}

		for _, c := range state.DB.GetConnections() {
			if c.GameID == g.ID {
				bannerContainer.Refresh()
				return
			}
		}

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

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refresh()
		}
	}()

	return bannerContainer
}

// buildInstalledGamesBanner scans Steam/Epic directories for installed games
// that are in the LAGGADO library and shows quick-add buttons.
func buildInstalledGamesBanner(state *AppState) fyne.CanvasObject {
	found := scanInstalledGames()
	if len(found) == 0 {
		return container.NewVBox()
	}

	// Filter out already-added games
	var toShow []gamelib.Game
	existing := map[int]bool{}
	for _, c := range state.DB.GetConnections() {
		existing[c.GameID] = true
	}
	for _, g := range found {
		if !existing[g.ID] {
			toShow = append(toShow, g)
		}
	}
	if len(toShow) == 0 {
		return container.NewVBox()
	}

	headerTxt := canvas.NewText(fmt.Sprintf("🎮  %d jogos instalados encontrados — clique para adicionar", len(toShow)), ColorTextSec)
	headerTxt.TextSize = 11

	rows := container.NewVBox(container.NewPadded(headerTxt))
	for _, g := range toShow {
		g := g
		nameTxt := canvas.NewText(g.Name, ColorTextPrim)
		nameTxt.TextSize = 12
		addBtn := widget.NewButton("+ Adicionar", func() {
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
		bg := canvas.NewRectangle(colorWithAlpha(ColorGreen, 10))
		bg.CornerRadius = 6
		row := container.NewStack(bg, container.NewPadded(
			container.NewBorder(nil, nil, nil, addBtn, nameTxt),
		))
		rows.Add(row)
	}

	bg := canvas.NewRectangle(ColorCard)
	bg.CornerRadius = 10
	return container.NewStack(bg, container.NewPadded(rows))
}

// scanInstalledGames looks for installed games in Steam and Epic directories.
func scanInstalledGames() []gamelib.Game {
	var dirs []string

	// Common Steam library paths
	steamPaths := []string{
		`C:\Program Files (x86)\Steam\steamapps\common`,
		`C:\Program Files\Steam\steamapps\common`,
		`D:\Steam\steamapps\common`,
		`D:\SteamLibrary\steamapps\common`,
		`E:\Steam\steamapps\common`,
		`E:\SteamLibrary\steamapps\common`,
	}

	// Common Epic Games paths
	epicPaths := []string{
		`C:\Program Files\Epic Games`,
		`C:\Program Files (x86)\Epic Games`,
		`D:\Epic Games`,
		`E:\Epic Games`,
	}

	dirs = append(dirs, steamPaths...)
	dirs = append(dirs, epicPaths...)

	seen := map[int]bool{}
	var results []gamelib.Game

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			g := gamelib.FindByExeName(entry.Name())
			if g != nil && !seen[g.ID] {
				seen[g.ID] = true
				results = append(results, *g)
			}
		}
	}

	return results
}
