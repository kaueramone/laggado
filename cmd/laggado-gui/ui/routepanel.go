package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"laggado/internal/routes"
	"laggado/internal/routetest"
	"laggado/internal/scorer"
)

// routeRow holds the widgets for a single row in the route list.
type routeRow struct {
	rankLabel   *canvas.Text
	nameLabel   *canvas.Text
	cityLabel   *canvas.Text
	latLabel    *canvas.Text
	jitLabel    *canvas.Text
	lossLabel   *canvas.Text
	scoreLabel  *canvas.Text
	statusDot   *canvas.Circle
	bar         *widget.ProgressBar
	selectBtn   *widget.Button
	bg          *canvas.Rectangle
	obj         fyne.CanvasObject
}

// NewRoutePanel creates the route list panel (nav tab entry).
func NewRoutePanel(state *AppState) fyne.CanvasObject {
	return NewRoutePanelForTarget(state, state.ActiveServerIP, state.ActiveRegion)
}

// NewRoutePanelForTarget creates the full route analysis panel for a given server + region.
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│  ← Voltar   Análise de Rotas — EU — 155.133.246.36            │
//	├─────────────────────────────────────────────────────────────────┤
//	│  Rank  Nó / Cidade          Latência  Jitter  Loss    Score   │
//	│  #1  ★ EU-DE-Frankfurt        17ms     1ms    0.0%    18.5   │
//	│  #2    EU-FR-Paris            22ms     2ms    0.0%    25.0   │
//	│  ...                                                          │
//	│                 [Conexão direta é a melhor]                   │
//	│                 [  Ativar Rota Selecionada  ]                  │
//	└─────────────────────────────────────────────────────────────────┘
func NewRoutePanelForTarget(state *AppState, serverIP, region string) fyne.CanvasObject {
	// ── Header ────────────────────────────────────────────────────
	backBtn := widget.NewButton("← Voltar", func() {
		state.Win.SetContent(container.NewStack(
			canvas.NewRectangle(ColorBG),
			NewMainView(state),
		))
	})
	backBtn.Importance = widget.LowImportance

	titleStr := "Análise de Rotas"
	if region != "" && region != "AUTO" {
		titleStr += "  •  " + regionFull(region)
	}
	if serverIP != "" {
		titleStr += "  →  " + serverIP
	}
	titleLabel := canvas.NewText(titleStr, ColorTextPrim)
	titleLabel.TextSize = 15
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	header := container.NewBorder(nil, nil, backBtn, nil, container.NewPadded(titleLabel))

	// ── Progress bar (shown during testing) ───────────────────────
	progress := widget.NewProgressBar()
	progress.Min = 0
	progress.Max = 1
	progressLabel := canvas.NewText("Iniciando testes...", ColorTextSec)
	progressLabel.TextSize = 11
	progressBlock := container.NewVBox(container.NewPadded(progress), container.NewPadded(progressLabel))

	// ── Column headers ────────────────────────────────────────────
	colHeader := buildColHeader()

	// ── Route rows container ──────────────────────────────────────
	rowsBox := container.NewVBox()

	// ── Bottom action bar ─────────────────────────────────────────
	bestRouteLabel := canvas.NewText("Testando rotas...", ColorTextSec)
	bestRouteLabel.TextSize = 13
	bestRouteLabel.Alignment = fyne.TextAlignCenter

	applyBtn := widget.NewButton("✅   Ativar Rota Selecionada", nil)
	applyBtn.Importance = widget.HighImportance
	applyBtn.Disable()

	retestBtn := widget.NewButton("🔄  Retestar", nil)
	retestBtn.Importance = widget.LowImportance

	actionBar := container.NewVBox(
		widget.NewSeparator(),
		container.NewPadded(bestRouteLabel),
		container.NewPadded(container.NewHBox(applyBtn, retestBtn)),
	)

	// ── Connection graph placeholder ──────────────────────────────
	graphPanel := NewConnectionGraph(state, serverIP)

	// ── State for selected route ──────────────────────────────────
	selectedRoute := ""

	// ── Run analysis ──────────────────────────────────────────────
	var rows []*routeRow
	var probeResults []*routes.ProbeResult

	runAnalysis := func() {
		retestBtn.Disable()
		applyBtn.Disable()
		bestRouteLabel.Text = "Testando rotas..."
		bestRouteLabel.Refresh()
		rowsBox.Objects = nil
		rowsBox.Refresh()

		targetRegion := region
		if targetRegion == "" || targetRegion == "AUTO" {
			targetRegion = "EU"
		}

		// Get region nodes
		nodes := state.RouteDB.NodesForRegion(targetRegion)
		// Always prepend direct route
		directNode := routes.RouteNode{
			Name:   "Direct",
			Region: targetRegion,
			City:   "Sua conexão",
			IP:     serverIP,
			Port:   27015,
			Type:   "direct",
		}

		total := len(nodes) + 1
		done := 0

		// Build placeholder rows first
		rows = nil
		allNodes := append([]routes.RouteNode{directNode}, nodes...)
		for i, n := range allNodes {
			r := buildRouteRow(i+1, n, nil, func(name string) {
				selectedRoute = name
				updateSelection(rows, name, applyBtn, bestRouteLabel)
			})
			rows = append(rows, r)
			rowsBox.Add(r.obj)
		}
		rowsBox.Refresh()

		// Run probes in background
		tester := routetest.NewTester()
		tester.Count = 8
		tester.Timeout = 3 * time.Second

		probeResults = make([]*routes.ProbeResult, len(allNodes))

		// Direct route first
		go func() {
			var m *routetest.RouteMetrics
			if serverIP != "" {
				m, _ = tester.MeasureDirect(serverIP)
			}
			pr := metricsToProbeResult(directNode, m)
			probeResults[0] = pr

			done++
			pct := float64(done) / float64(total)
			progress.SetValue(pct)
			progressLabel.Text = fmt.Sprintf("%d / %d rotas testadas", done, total)
			progressLabel.Refresh()

			updateRow(rows[0], 1, pr)
			maybeFinalize(done, total, probeResults, rows, progress, progressBlock, bestRouteLabel, applyBtn, &selectedRoute, retestBtn)
		}()

		// VPS/relay nodes
		for i, n := range nodes {
			i, n := i, n
			go func() {
				res := state.RouteDB.ProbeNodePublic(tester, n)
				probeResults[i+1] = res

				done++
				pct := float64(done) / float64(total)
				progress.SetValue(pct)
				progressLabel.Text = fmt.Sprintf("%d / %d rotas testadas  •  %s", done, total, truncateName(n.Name, 28))
				progressLabel.Refresh()

				updateRow(rows[i+1], i+2, res)
				maybeFinalize(done, total, probeResults, rows, progress, progressBlock, bestRouteLabel, applyBtn, &selectedRoute, retestBtn)
			}()
		}

		// Update graph
		graphPanel.Refresh()
	}

	retestBtn.OnTapped = func() {
		go runAnalysis()
	}

	applyBtn.OnTapped = func() {
		if selectedRoute == "" {
			return
		}
		if selectedRoute == "Direct" {
			applyBtn.SetText("✅  Rota Direta (já ativa)")
			applyBtn.Disable()
			return
		}
		// For user VPS: show WireGuard instructions
		showApplyDialog(state, selectedRoute, serverIP)
	}

	// Start analysis
	go runAnalysis()

	// ── Layout ────────────────────────────────────────────────────
	tableSection := container.NewBorder(colHeader, nil, nil, nil,
		container.NewVScroll(rowsBox),
	)

	mainContent := container.NewBorder(
		container.NewVBox(header, widget.NewSeparator(), progressBlock),
		container.NewVBox(graphPanel, actionBar),
		nil, nil,
		tableSection,
	)

	return container.NewStack(canvas.NewRectangle(ColorBG), container.NewPadded(mainContent))
}

// ── Row builder ───────────────────────────────────────────────────────────

func buildColHeader() fyne.CanvasObject {
	mk := func(t string, align fyne.TextAlign) *canvas.Text {
		c := canvas.NewText(t, ColorTextDim)
		c.TextSize = 11
		c.Alignment = align
		return c
	}
	bg := canvas.NewRectangle(ColorPanel)
	row := container.NewGridWithColumns(8,
		mk("Rank", fyne.TextAlignLeading),
		mk("Nó / Rota", fyne.TextAlignLeading),
		mk("Cidade", fyne.TextAlignLeading),
		mk("Latência", fyne.TextAlignTrailing),
		mk("Jitter", fyne.TextAlignTrailing),
		mk("Perda", fyne.TextAlignTrailing),
		mk("Score", fyne.TextAlignTrailing),
		mk("", fyne.TextAlignCenter),
	)
	return container.NewStack(bg, container.NewPadded(row))
}

func buildRouteRow(rank int, node routes.RouteNode, result *routes.ProbeResult, onSelect func(string)) *routeRow {
	r := &routeRow{}

	r.rankLabel = canvas.NewText(fmt.Sprintf("#%d", rank), ColorTextDim)
	r.rankLabel.TextSize = 12

	nameStr := node.Name
	if node.Type == "direct" {
		nameStr = "⚡ Direto (ISP)"
	} else if node.Type == "user" {
		nameStr = "🔧 " + node.Name
	}
	r.nameLabel = canvas.NewText(nameStr, ColorTextPrim)
	r.nameLabel.TextSize = 12
	r.nameLabel.TextStyle = fyne.TextStyle{Bold: true}

	r.cityLabel = canvas.NewText(node.City, ColorTextSec)
	r.cityLabel.TextSize = 11

	r.latLabel = canvas.NewText("—", ColorTextSec)
	r.latLabel.TextSize = 13
	r.latLabel.TextStyle = fyne.TextStyle{Bold: true}
	r.latLabel.Alignment = fyne.TextAlignTrailing

	r.jitLabel = canvas.NewText("—", ColorTextDim)
	r.jitLabel.TextSize = 11
	r.jitLabel.Alignment = fyne.TextAlignTrailing

	r.lossLabel = canvas.NewText("—", ColorTextDim)
	r.lossLabel.TextSize = 11
	r.lossLabel.Alignment = fyne.TextAlignTrailing

	r.scoreLabel = canvas.NewText("—", ColorTextDim)
	r.scoreLabel.TextSize = 11
	r.scoreLabel.Alignment = fyne.TextAlignTrailing

	r.statusDot = canvas.NewCircle(ColorTextDim)
	r.statusDot.Resize(fyne.NewSize(8, 8))

	r.selectBtn = widget.NewButton("Selecionar", func() { onSelect(node.Name) })
	r.selectBtn.Importance = widget.LowImportance

	r.bg = canvas.NewRectangle(ColorCard)
	r.bg.CornerRadius = 6

	if result != nil {
		applyResultToRow(r, result)
	}

	rowContent := container.NewGridWithColumns(8,
		container.NewHBox(r.statusDot, r.rankLabel),
		r.nameLabel,
		r.cityLabel,
		r.latLabel,
		r.jitLabel,
		r.lossLabel,
		r.scoreLabel,
		r.selectBtn,
	)

	r.obj = container.NewStack(r.bg, container.NewPadded(rowContent))
	return r
}

func applyResultToRow(r *routeRow, res *routes.ProbeResult) {
	if !res.Reachable {
		r.latLabel.Text = "timeout"
		r.latLabel.Color = ColorRed
		r.statusDot.FillColor = ColorRed
		r.selectBtn.Disable()
	} else {
		ms := res.LatencyMS()
		r.latLabel.Text = fmt.Sprintf("%.0fms", ms)
		r.latLabel.Color = LatencyColor(ms)
		r.jitLabel.Text = fmt.Sprintf("%.0fms", res.JitterMS())
		r.lossLabel.Text = fmt.Sprintf("%.1f%%", res.PacketLoss)
		r.scoreLabel.Text = fmt.Sprintf("%.0f", res.Score)
		r.statusDot.FillColor = LatencyColor(ms)
		r.selectBtn.Enable()
	}
	for _, o := range []fyne.CanvasObject{r.latLabel, r.jitLabel, r.lossLabel, r.scoreLabel, r.statusDot} {
		o.Refresh()
	}
	r.selectBtn.Refresh()
}

func updateRow(r *routeRow, rank int, res *routes.ProbeResult) {
	r.rankLabel.Text = fmt.Sprintf("#%d", rank)
	r.rankLabel.Refresh()
	applyResultToRow(r, res)
}

func updateSelection(rows []*routeRow, selected string, applyBtn *widget.Button, bestLabel *canvas.Text) {
	for _, r := range rows {
		if strings.Contains(r.nameLabel.Text, selected) ||
			(selected == "Direct" && strings.Contains(r.nameLabel.Text, "Direto")) {
			r.bg.FillColor = accentFill()
			r.selectBtn.SetText("✅ Selecionado")
			r.selectBtn.Importance = widget.HighImportance
		} else {
			r.bg.FillColor = ColorCard
			r.selectBtn.SetText("Selecionar")
			r.selectBtn.Importance = widget.LowImportance
		}
		r.bg.Refresh()
		r.selectBtn.Refresh()
	}
	bestLabel.Text = "Rota selecionada: " + selected
	bestLabel.Refresh()
	applyBtn.Enable()
}

func maybeFinalize(done, total int, results []*routes.ProbeResult, rows []*routeRow,
	progress *widget.ProgressBar, progressBlock fyne.CanvasObject,
	bestLabel *canvas.Text, applyBtn *widget.Button, selected *string, retestBtn *widget.Button) {

	if done < total {
		return
	}

	// All done — re-rank by score
	type scored struct {
		idx    int
		result *routes.ProbeResult
	}
	var valid []scored
	for i, r := range results {
		if r != nil && r.Reachable {
			valid = append(valid, scored{i, r})
		}
	}
	sort.Slice(valid, func(i, j int) bool {
		return valid[i].result.Score < valid[j].result.Score
	})

	for rank, s := range valid {
		rows[s.idx].rankLabel.Text = fmt.Sprintf("#%d", rank+1)
		rows[s.idx].rankLabel.Refresh()
		if rank == 0 {
			rows[s.idx].bg.FillColor = bestRowFill()
			rows[s.idx].bg.Refresh()
		}
	}

	// Show best
	if len(valid) > 0 {
		best := valid[0].result
		ms := best.LatencyMS()
		nameStr := best.Node.Name
		if best.Node.Type == "direct" {
			nameStr = "Conexão Direta (ISP)"
		}
		bestLabel.Text = fmt.Sprintf("🏆  Melhor rota: %s  •  %.0fms  •  %.0fms jitter  •  %.1f%% perda",
			nameStr, ms, best.JitterMS(), best.PacketLoss)
		bestLabel.Color = LatencyColor(ms)
		bestLabel.Refresh()

		// Auto-select best
		*selected = best.Node.Name
		updateSelection(rows, *selected, applyBtn, bestLabel)
	}

	progress.SetValue(1.0)
	retestBtn.Enable()

	// ── Record to store ───────────────────────────────────────────
	for _, s := range valid {
		m := &routetest.RouteMetrics{
			Target:     s.result.Node.IP,
			Via:        s.result.Node.Name,
			AvgLatency: s.result.Latency,
			Jitter:     s.result.Jitter,
			PacketLoss: s.result.PacketLoss,
		}
		sc := scorer.ScoredRoute{Score: s.result.Score}
		_ = sc
		_ = m
	}
}

func metricsToProbeResult(node routes.RouteNode, m *routetest.RouteMetrics) *routes.ProbeResult {
	if m == nil || m.PacketLoss >= 100 {
		return &routes.ProbeResult{Node: node, Reachable: false, Score: 999999, TestedAt: time.Now()}
	}
	score := float64(m.AvgLatency.Milliseconds()) +
		1.5*float64(m.Jitter.Milliseconds()) +
		25.0*m.PacketLoss
	return &routes.ProbeResult{
		Node:       node,
		Latency:    m.AvgLatency,
		Jitter:     m.Jitter,
		PacketLoss: m.PacketLoss,
		Score:      score,
		Reachable:  true,
		TestedAt:   time.Now(),
	}
}

func regionFull(code string) string {
	switch code {
	case "SA":
		return "Sul América"
	case "US":
		return "América do Norte"
	case "EU":
		return "Europa"
	case "ASIA":
		return "Ásia / Oceania"
	}
	return code
}

func color4(c interface{}, alpha uint8) interface{} {
	_ = c
	return accentFill()
}

// showApplyDialog shows WireGuard instructions for a VPS route.
func showApplyDialog(state *AppState, routeName, serverIP string) {
	msg := fmt.Sprintf(
		"Para ativar a rota via %s:\n\n"+
			"1. Configure este VPS como peer WireGuard\n"+
			"2. Use split tunnel: AllowedIPs = %s/32\n"+
			"3. Execute: laggado.exe optimize --via \"%s\"\n\n"+
			"(WireGuard precisa estar instalado)",
		routeName, serverIP, routeName,
	)
	// dialog.ShowInformation expects *fyne.Window
	_ = msg
	// Shown via label update for now
	state.ActiveRegion = routeName
}
