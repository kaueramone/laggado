package ui

import (
	"fmt"
	"image/color"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"laggado/internal/gamelib"
	"laggado/internal/store"
)


// NewConexoesPanel builds the main "Conexões" screen — equivalent to ExitLag's home view.
//
// Layout:
//   ┌────────────────────────────────────────────────────────────┐
//   │ Conexões                              [+ Adicionar Jogo]   │
//   ├────────────────────────────────────────────────────────────┤
//   │  [Icon] Nome do Jogo       Região  [toggle ON/OFF]  [×]  │
//   │  [Icon] Nome do Jogo       Região  [toggle ON/OFF]  [×]  │
//   ├────────────────────────────────────────────────────────────┤
//   │ Última Sessão                                              │
//   │  Duração   Ping Mín   Ping Méd   Jitter   Perda           │
//   └────────────────────────────────────────────────────────────┘
func NewConexoesPanel(state *AppState) fyne.CanvasObject {
	title := canvas.NewText("Conexões", ColorTextPrim)
	title.TextSize = 20
	title.TextStyle = fyne.TextStyle{Bold: true}

	addBtn := widget.NewButton("+ Adicionar Jogo", nil)
	addBtn.Importance = widget.MediumImportance

	header := container.NewBorder(nil, nil, nil, addBtn, title)

	// ── Connection list ───────────────────────────────────────────
	connList := container.NewVBox()

	var refreshList func()
	refreshList = func() {
		connList.Objects = nil
		conns := state.DB.GetConnections()
		if len(conns) == 0 {
			empty := canvas.NewText("Nenhuma conexão configurada. Clique em '+ Adicionar Jogo'.", ColorTextDim)
			empty.TextSize = 12
			connList.Add(container.NewCenter(container.NewPadded(empty)))
		}
		for _, gc := range conns {
			gc := gc
			connList.Add(makeConnectionCard(state, gc, refreshList))
		}
		connList.Refresh()
	}

	// ── Add game dialog ───────────────────────────────────────────
	addBtn.OnTapped = func() {
		showAddGameDialog(state, state.Win, func(gc store.GameConnection) {
			state.DB.AddConnection(gc)
			state.DB.Save()
			refreshList()
		})
	}

	refreshList()

	// ── Last session stats ────────────────────────────────────────
	sessionTitle := canvas.NewText("Dados da Última Sessão", ColorTextSec)
	sessionTitle.TextSize = 13
	sessionTitle.TextStyle = fyne.TextStyle{Bold: true}

	sessionCard := makeLastSessionCard(state)

	sep1 := widget.NewSeparator()
	sep2 := widget.NewSeparator()

	top := container.NewVBox(
		container.NewPadded(header),
		sep1,
	)
	bottom := container.NewVBox(
		sep2,
		container.NewPadded(sessionTitle),
		container.NewPadded(sessionCard),
	)

	return container.NewStack(
		canvas.NewRectangle(ColorBG),
		container.NewPadded(
			container.NewBorder(top, bottom, nil, nil,
				container.NewVScroll(container.NewPadded(connList)),
			),
		),
	)
}

// makeConnectionCard builds a single game connection row with a real Connect button.
func makeConnectionCard(state *AppState, gc store.GameConnection, refresh func()) fyne.CanvasObject {
	// Game icon
	iconImg := canvas.NewImageFromResource(placeholderGameIcon(gc.GameID))
	iconImg.FillMode = canvas.ImageFillContain
	iconImg.SetMinSize(fyne.NewSize(48, 48))

	g := gamelib.ByID(gc.GameID)
	if g != nil && g.IconPath != "" {
		if raw, err := os.ReadFile(g.IconPath); err == nil {
			iconImg.Resource = fyne.NewStaticResource(fmt.Sprintf("g%d.png", gc.GameID), raw)
		}
	}

	// Name
	nameLabel := canvas.NewText(gc.GameName, ColorTextPrim)
	nameLabel.TextSize = 14
	nameLabel.TextStyle = fyne.TextStyle{Bold: true}

	regionLabel := canvas.NewText(regionEmoji(gc.Region)+"  "+regionName(gc.Region), ColorTextSec)
	regionLabel.TextSize = 11

	// Status label (shown during connect flow and when connected)
	statusLabel := canvas.NewText("", ColorTextDim)
	statusLabel.TextSize = 10

	// Determine initial connected state for this card
	isThisConnected := state.IsConnectedTo(gc.GameID)
	if isThisConnected {
		statusLabel.Text = "● CONECTADO"
		statusLabel.Color = ColorGreen
	}

	// Region selector
	regions := []string{"AUTO", "SA", "US", "EU", "ASIA"}
	regionSelect := widget.NewSelect(regions, func(r string) {
		gc.Region = r
		state.DB.AddConnection(gc)
		state.DB.Save()
		regionLabel.Text = regionEmoji(r) + "  " + regionName(r)
		regionLabel.Refresh()
	})
	regionSelect.Selected = gc.Region

	// Connect / Disconnect button
	connectBtn := widget.NewButton("▶  Conectar", nil)
	connectBtn.Importance = widget.HighImportance

	if isThisConnected {
		connectBtn.SetText("■  Desconectar")
		connectBtn.Importance = widget.DangerImportance
	}

	// Remove button
	removeBtn := widget.NewButton("✕", func() {
		// If this game is connected, disconnect first
		if state.IsConnectedTo(gc.GameID) {
			StopConnect(state, func(_ string) {}, func(_ error) {
				state.DB.RemoveConnection(gc.GameID)
				state.DB.Save()
				refresh()
			})
			return
		}
		state.DB.RemoveConnection(gc.GameID)
		state.DB.Save()
		refresh()
	})
	removeBtn.Importance = widget.DangerImportance

	// ── Connect button logic ───────────────────────────────────────────────────
	var connecting bool

	connectBtn.OnTapped = func() {
		// ── DISCONNECT flow ────────────────────────────────────────────
		if state.IsConnectedTo(gc.GameID) {
			connectBtn.SetText("Desconectando…")
			connectBtn.Disable()
			statusLabel.Color = ColorTextDim

			StopConnect(state,
				func(msg string) {
					statusLabel.Text = msg
					statusLabel.Refresh()
				},
				func(err error) {
					connectBtn.Enable()
					if err != nil {
						statusLabel.Text = "Erro: " + err.Error()
						statusLabel.Color = ColorRed
					} else {
						statusLabel.Text = ""
						connectBtn.SetText("▶  Conectar")
						connectBtn.Importance = widget.HighImportance
					}
					statusLabel.Refresh()
					connectBtn.Refresh()
				},
			)
			return
		}

		// ── CONNECT flow ───────────────────────────────────────────────
		if connecting {
			return
		}
		connecting = true
		connectBtn.SetText("Conectando…")
		connectBtn.Disable()
		regionSelect.Disable()
		statusLabel.Text = "Iniciando…"
		statusLabel.Color = ColorAccent
		statusLabel.Refresh()

		StartConnect(state, gc,
			// onStatus
			func(msg string) {
				statusLabel.Text = msg
				statusLabel.Refresh()
			},
			// onDone
			func(err error) {
				connecting = false
				connectBtn.Enable()
				regionSelect.Enable()

				if err != nil {
					statusLabel.Text = "✗ " + err.Error()
					statusLabel.Color = ColorRed
					connectBtn.SetText("▶  Conectar")
					connectBtn.Importance = widget.HighImportance
					// Show full error in a dialog so the user can read it
					dialog.ShowError(err, state.Win)
				} else {
					statusLabel.Text = "● CONECTADO"
					statusLabel.Color = ColorGreen
					connectBtn.SetText("■  Desconectar")
					connectBtn.Importance = widget.DangerImportance
				}
				statusLabel.Refresh()
				connectBtn.Refresh()
			},
		)
	}

	// ── Layout ────────────────────────────────────────────────────────────────
	nameRow := container.NewVBox(nameLabel, regionLabel, spacer(2), statusLabel)
	left := container.NewHBox(iconImg, container.NewPadded(nameRow))
	right := container.NewHBox(container.NewPadded(regionSelect), connectBtn, removeBtn)

	row := container.NewBorder(nil, nil, left, right)

	bg := canvas.NewRectangle(ColorCard)
	bg.CornerRadius = 10

	return container.NewStack(bg, container.NewPadded(row))
}

// makeLastSessionCard builds the session stats row.
func makeLastSessionCard(state *AppState) fyne.CanvasObject {
	// Try to find last session from connections
	conns := state.DB.GetConnections()

	var lastDur, lastGame string
	var lastMin, lastAvg, lastJitter int
	var lastLoss float64

	for _, c := range conns {
		if c.LastAvgPing > 0 {
			lastDur = c.LastSessionDuration
			lastGame = c.GameName
			lastMin = c.LastMinPing
			lastAvg = c.LastAvgPing
			lastJitter = c.LastAvgJitter
			lastLoss = c.LastPktLoss
			break
		}
	}

	makeStatBox := func(label, val string, valColor color.Color) fyne.CanvasObject { //nolint:unparam
		lbl := canvas.NewText(label, ColorTextDim)
		lbl.TextSize = 10
		v := canvas.NewText(val, valColor)
		v.TextSize = 18
		v.TextStyle = fyne.TextStyle{Bold: true}
		bg := canvas.NewRectangle(ColorCard)
		bg.CornerRadius = 8
		return container.NewStack(bg, container.NewVBox(
			container.NewPadded(lbl),
			container.NewPadded(v),
		))
	}

	var gameInfo string
	if lastGame != "" {
		gameInfo = lastGame + "  •  " + lastDur
	} else {
		gameInfo = "Nenhuma sessão ainda — escaneie um jogo"
	}

	gameLbl := canvas.NewText(gameInfo, ColorTextSec)
	gameLbl.TextSize = 11

	pingColor := func(ms int) color.Color {
		return LatencyColor(float64(ms))
	}

	statsRow := container.NewGridWithColumns(5,
		makeStatBox("Duração", lastDur, ColorTextPrim),
		makeStatBox("Ping Mín", fmt.Sprintf("%d ms", lastMin), pingColor(lastMin)),
		makeStatBox("Ping Médio", fmt.Sprintf("%d ms", lastAvg), pingColor(lastAvg)),
		makeStatBox("Jitter Médio", fmt.Sprintf("%d ms", lastJitter), LatencyColor(float64(lastJitter)*3)),
		makeStatBox("Perda", fmt.Sprintf("%.1f%%", lastLoss), func() color.Color {
			if lastLoss < 1 {
				return ColorGreen
			} else if lastLoss < 5 {
				return ColorYellow
			}
			return ColorRed
		}()),
	)

	bg := canvas.NewRectangle(ColorPanel)
	bg.CornerRadius = 10

	return container.NewStack(bg, container.NewPadded(container.NewVBox(
		gameLbl,
		spacer(4),
		statsRow,
	)))
}

// showAddGameDialog shows a search dialog to pick a game from the library.
func showAddGameDialog(state *AppState, win fyne.Window, onAdd func(store.GameConnection)) {
	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Pesquisar jogo...")

	resultList := widget.NewList(
		func() int { return 0 },
		func() fyne.CanvasObject {
			return container.NewHBox(
				canvas.NewText("", ColorTextPrim),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {},
	)

	var filtered []gamelib.Game
	var selectedGame *gamelib.Game

	updateList := func(query string) {
		filtered = gamelib.Search(query)
		if len(filtered) > 100 {
			filtered = filtered[:100]
		}
		resultList.Length = func() int { return len(filtered) }
		resultList.CreateItem = func() fyne.CanvasObject {
			t := canvas.NewText("", ColorTextPrim)
			t.TextSize = 13
			return t
		}
		resultList.UpdateItem = func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < len(filtered) {
				o.(*canvas.Text).Text = filtered[i].Name
				o.(*canvas.Text).Refresh()
			}
		}
		resultList.Refresh()
	}

	searchEntry.OnChanged = updateList
	updateList("")

	resultList.OnSelected = func(id widget.ListItemID) {
		if id < len(filtered) {
			g := filtered[id]
			selectedGame = &g
		}
	}

	regionSelect := widget.NewSelect([]string{"AUTO", "SA", "US", "EU", "ASIA"}, nil)
	regionSelect.Selected = "AUTO"

	content := container.NewVBox(
		searchEntry,
		widget.NewLabel("Região:"),
		regionSelect,
		container.NewPadded(resultList),
	)

	d := dialog.NewCustomConfirm("Adicionar Jogo", "Adicionar", "Cancelar", content, func(ok bool) {
		if !ok || selectedGame == nil {
			return
		}
		onAdd(store.GameConnection{
			GameID:   selectedGame.ID,
			GameName: selectedGame.Name,
			GameExe:  gameNameToExeSlug(selectedGame.Name), // mapped process slug for routing
			Enabled:  true,
			Region:   regionSelect.Selected,
		})
	}, win)
	d.Resize(fyne.NewSize(480, 500))
	d.Show()
}

// ── Helpers ───────────────────────────────────────────────────────────────

func regionEmoji(code string) string {
	switch code {
	case "SA":
		return "🌎"
	case "US":
		return "🌎"
	case "EU":
		return "🌍"
	case "ASIA":
		return "🌏"
	default:
		return "🎯"
	}
}

func regionName(code string) string {
	switch code {
	case "SA":
		return "Sul América"
	case "US":
		return "América do Norte"
	case "EU":
		return "Europa"
	case "ASIA":
		return "Ásia / Oceania"
	default:
		return "Automático"
	}
}

func placeholderGameIcon(id int) fyne.Resource {
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48">
  <rect width="48" height="48" rx="8" fill="#1A1D2A"/>
  <text x="24" y="32" font-size="22" text-anchor="middle">🎮</text>
  <text x="24" y="46" font-size="6" fill="#414869" text-anchor="middle">%d</text>
</svg>`, id)
	return fyne.NewStaticResource(fmt.Sprintf("gicon%d.svg", id), []byte(svg))
}
