package ui

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"laggado/internal/connmon"
	"laggado/internal/gamedet"
	"laggado/internal/serverid"
	"laggado/internal/steamapi"
)

// NewGamePanel builds the game detection + region selection panel.
func NewGamePanel(state *AppState) fyne.CanvasObject {
	// ── Header ────────────────────────────────────────────────────
	title := canvas.NewText("Jogo Detectado", ColorTextPrim)
	title.TextSize = 18
	title.TextStyle = fyne.TextStyle{Bold: true}

	scanBtn := widget.NewButton("🔍  Escanear", nil)
	scanBtn.Importance = widget.MediumImportance
	header := container.NewBorder(nil, nil, nil, scanBtn, title)

	// ── Cover image ───────────────────────────────────────────────
	coverRes := placeholderCover()
	coverImg := canvas.NewImageFromResource(coverRes)
	coverImg.FillMode = canvas.ImageFillContain
	coverImg.SetMinSize(fyne.NewSize(230, 107))

	coverBg := canvas.NewRectangle(ColorCard)
	coverBg.CornerRadius = 8
	coverContainer := container.NewStack(coverBg, container.NewPadded(coverImg))

	// ── Game info labels ──────────────────────────────────────────
	gameName := canvas.NewText("Nenhum jogo detectado", ColorTextPrim)
	gameName.TextSize = 20
	gameName.TextStyle = fyne.TextStyle{Bold: true}

	gamePlatformLabel := canvas.NewText("Inicie um jogo e clique Escanear", ColorTextSec)
	gamePlatformLabel.TextSize = 12

	serverLabel := canvas.NewText("Servidor: —", ColorTextSec)
	serverLabel.TextSize = 12

	geoLabel := canvas.NewText("—", ColorTextSec)
	geoLabel.TextSize = 12

	pingDot := canvas.NewCircle(ColorTextDim)
	pingDot.Resize(fyne.NewSize(10, 10))

	pingVal := canvas.NewText("— ms", ColorTextSec)
	pingVal.TextSize = 16
	pingVal.TextStyle = fyne.TextStyle{Bold: true}

	pingQuality := canvas.NewText("", ColorTextDim)
	pingQuality.TextSize = 11

	pingRow := container.NewHBox(pingDot, container.NewPadded(pingVal), pingQuality)

	infoCol := container.NewVBox(
		gameName,
		gamePlatformLabel,
		spacer(10),
		serverLabel,
		geoLabel,
		spacer(6),
		pingRow,
	)

	gameCard := container.NewHBox(coverContainer, container.NewPadded(infoCol))
	gameCardBg := canvas.NewRectangle(ColorCard)
	gameCardBg.CornerRadius = 10
	gameCardFull := container.NewStack(gameCardBg, container.NewPadded(gameCard))

	// ── Region selector ───────────────────────────────────────────
	regionTitle := canvas.NewText("Região do Servidor de Destino", ColorTextSec)
	regionTitle.TextSize = 12

	selectedRegion := "AUTO"

	type regionDef struct{ code, emoji, label string }
	regions := []regionDef{
		{"AUTO", "🎯", "Auto"},
		{"SA", "🌎", "Sul América"},
		{"US", "🌎", "América do Norte"},
		{"EU", "🌍", "Europa"},
		{"ASIA", "🌏", "Ásia / Oceania"},
	}

	regionBtns := make([]*widget.Button, len(regions))
	regionRow := container.NewHBox()

	for i, r := range regions {
		i, r := i, r
		btn := widget.NewButton(r.emoji+"  "+r.label, nil)
		btn.Importance = widget.LowImportance
		btn.OnTapped = func() {
			selectedRegion = r.code
			state.ActiveRegion = r.code
			for j, rb := range regionBtns {
				if j == i {
					rb.Importance = widget.HighImportance
				} else {
					rb.Importance = widget.LowImportance
				}
				rb.Refresh()
			}
		}
		regionBtns[i] = btn
		regionRow.Add(btn)
	}
	regionBtns[0].Importance = widget.HighImportance

	regionBlock := container.NewVBox(
		regionTitle,
		spacer(4),
		regionRow,
	)

	// ── Optimize button ───────────────────────────────────────────
	optimizeBtn := widget.NewButton("⚡   OTIMIZAR ROTA", nil)
	optimizeBtn.Importance = widget.HighImportance

	optimizeProgress := widget.NewProgressBarInfinite()
	optimizeProgress.Hide()
	optimizeStatus := canvas.NewText("", ColorTextSec)
	optimizeStatus.TextSize = 11

	// ── Scan logic ────────────────────────────────────────────────
	updateGame := func() {
		games, _ := gamedet.DetectGames()
		if len(games) == 0 {
			gameName.Text = "Nenhum jogo detectado"
			gamePlatformLabel.Text = "Inicie um jogo e clique em Escanear"
			serverLabel.Text = "Servidor: —"
			geoLabel.Text = "—"
			pingVal.Text = "— ms"
			pingVal.Color = ColorTextSec
			pingDot.FillColor = ColorTextDim
			pingQuality.Text = ""
			coverImg.Resource = placeholderCover()
			state.ActiveGameExe, state.ActiveGameName, state.ActiveServerIP = "", "", ""
			for _, o := range []fyne.CanvasObject{gameName, gamePlatformLabel, serverLabel, geoLabel, pingVal, pingDot, pingQuality, coverImg} {
				o.Refresh()
			}
			return
		}

		g := games[0]
		state.ActiveGameExe = g.Name
		state.ActiveGameName = gamedet.FriendlyName(g.Name)
		gameName.Text = state.ActiveGameName
		gameName.Refresh()

		meta := steamapi.GetGameMeta(g.Name, state.ActiveGameName)
		platStr := platformLabel(meta.Platform)
		if meta.SteamID > 0 {
			platStr += fmt.Sprintf("  •  App ID %d", meta.SteamID)
		}
		platStr += "  •  " + g.Name
		gamePlatformLabel.Text = platStr
		gamePlatformLabel.Refresh()

		// Cover art (async)
		go func() {
			defer func() { recover() }()
			info, err := state.CoverFetcher.GetGameInfo(g.Name, state.ActiveGameName)
			if err == nil && info.CoverPath != "" {
				if raw, err2 := os.ReadFile(info.CoverPath); err2 == nil {
					res := fyne.NewStaticResource(filepath.Base(info.CoverPath), raw)
					coverImg.Resource = res
					coverImg.Refresh()
				}
			}
		}()

		// Detect server
		conns, _ := connmon.GetConnectionsByPID(g.PID)
		best := serverid.IdentifyGameServer(conns)
		if best != nil {
			state.ActiveServerIP = best.IP.String()
			serverLabel.Text = "Servidor: " + best.IP.String() + fmt.Sprintf("  •  porta %d  •  %s", best.Port, best.Protocol)
			serverLabel.Refresh()

			// GeoIP (async)
			go func() {
				defer func() { recover() }()
				geo, err := state.GeoRes.Lookup(best.IP)
				if err == nil {
					geoLabel.Text = fmt.Sprintf("%s, %s  (%s)  •  %s", geo.City, geo.Country, geo.CountryCode, truncateName(geo.ISP, 30))
					geoLabel.Refresh()
					if selectedRegion == "AUTO" {
						detected := geoToRegion(geo.CountryCode)
						state.ActiveRegion = detected
						for j, r := range regions {
							if r.code == detected {
								regionBtns[j].Importance = widget.HighImportance
							} else if r.code != "AUTO" {
								regionBtns[j].Importance = widget.LowImportance
							}
							regionBtns[j].Refresh()
						}
					}
				}
			}()

			// Quick ping (3 TCP probes)
			go func() {
				defer func() { recover() }()
				addr := net.JoinHostPort(best.IP.String(), fmt.Sprintf("%d", best.Port))
				var total int64
				hits := 0
				for i := 0; i < 3; i++ {
					t0 := time.Now()
					conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
					elapsed := time.Since(t0)
					if err == nil {
						conn.Close()
						total += elapsed.Microseconds()
						hits++
					} else if strings.Contains(err.Error(), "refused") {
						total += elapsed.Microseconds()
						hits++
					}
					time.Sleep(100 * time.Millisecond)
				}
				if hits > 0 {
					ms := float64(total/int64(hits)) / 1000.0
					pingVal.Text = fmt.Sprintf("%.0f ms", ms)
					pingVal.Color = LatencyColor(ms)
					pingDot.FillColor = LatencyColor(ms)
					pingQuality.Text = LatencyLabel(ms)
					pingQuality.Color = LatencyColor(ms)
				} else {
					pingVal.Text = "timeout"
					pingVal.Color = ColorRed
					pingDot.FillColor = ColorRed
				}
				for _, o := range []fyne.CanvasObject{pingVal, pingDot, pingQuality} {
					o.Refresh()
				}
			}()
		} else {
			serverLabel.Text = "Servidor: aguardando conexão..."
			serverLabel.Refresh()
		}
	}

	scanBtn.OnTapped = func() {
		scanBtn.Disable()
		go func() {
			defer func() {
				recover()
				scanBtn.Enable()
			}()
			updateGame()
		}()
	}

	// ── Optimize action ───────────────────────────────────────────
	optimizeBtn.OnTapped = func() {
		if state.ActiveServerIP == "" && state.ActiveGameExe == "" {
			dialog.ShowInformation("Aviso", "Escaneie um jogo ativo primeiro.", state.Win)
			return
		}

		optimizeBtn.Disable()
		optimizeProgress.Show()
		optimizeStatus.Text = "Preparando análise de rotas..."
		optimizeStatus.Refresh()

		go func() {
			defer func() {
				optimizeBtn.Enable()
				optimizeProgress.Hide()
				optimizeStatus.Text = ""
				optimizeStatus.Refresh()
			}()

			region := state.ActiveRegion
			if region == "" || region == "AUTO" {
				region = "EU"
			}

			rp := NewRoutePanelForTarget(state, state.ActiveServerIP, region)
			state.Win.SetContent(container.NewStack(
				canvas.NewRectangle(ColorBG),
				rp,
			))
		}()
	}

	// Auto-scan on open
	go func() {
		defer func() { recover() }()
		time.Sleep(300 * time.Millisecond)
		updateGame()
	}()

	// ── Final layout ──────────────────────────────────────────────
	sep1 := widget.NewSeparator()
	sep2 := widget.NewSeparator()

	optimizeRow := container.NewVBox(
		container.NewPadded(optimizeBtn),
		container.NewHBox(optimizeProgress, optimizeStatus),
	)

	content := container.NewVBox(
		container.NewPadded(header),
		sep1,
		container.NewPadded(gameCardFull),
		spacer(4),
		sep2,
		container.NewPadded(regionBlock),
		spacer(4),
		container.NewPadded(optimizeRow),
	)

	return container.NewStack(canvas.NewRectangle(ColorBG), container.NewPadded(content))
}

func platformLabel(platform string) string {
	switch platform {
	case "steam":
		return "Steam"
	case "epic":
		return "Epic Games"
	case "ea":
		return "EA App"
	case "blizzard":
		return "Battle.net"
	default:
		return "Outro"
	}
}

func geoToRegion(countryCode string) string {
	sa := map[string]bool{"BR": true, "AR": true, "CL": true, "CO": true, "PE": true, "UY": true, "PY": true, "BO": true, "EC": true, "VE": true}
	eu := map[string]bool{"DE": true, "FR": true, "GB": true, "NL": true, "SE": true, "NO": true, "FI": true, "DK": true, "IT": true, "ES": true, "PT": true, "PL": true, "AT": true, "CH": true, "BE": true, "RU": true, "UA": true, "TR": true}
	asia := map[string]bool{"JP": true, "KR": true, "CN": true, "SG": true, "HK": true, "TW": true, "AU": true, "NZ": true, "IN": true, "TH": true}
	cc := strings.ToUpper(countryCode)
	if sa[cc] {
		return "SA"
	}
	if eu[cc] {
		return "EU"
	}
	if asia[cc] {
		return "ASIA"
	}
	return "US"
}

func placeholderCover() fyne.Resource {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 460 215">
  <rect width="460" height="215" rx="8" fill="#1A1D2A"/>
  <text x="230" y="100" font-family="sans-serif" font-size="52" fill="#2D3248" text-anchor="middle">🎮</text>
  <text x="230" y="150" font-family="sans-serif" font-size="14" fill="#414869" text-anchor="middle">Nenhum jogo detectado</text>
</svg>`
	return fyne.NewStaticResource("placeholder.svg", []byte(svg))
}
