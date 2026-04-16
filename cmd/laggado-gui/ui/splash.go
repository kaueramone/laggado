package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// NewSplashScreen creates the loading screen shown at startup.
// It syncs the route database (equivalent to ExitLag's 964-route preload).
// When done it calls onReady.
func NewSplashScreen(state *AppState, onReady func()) fyne.CanvasObject {
	// ── Logo / title ──────────────────────────────────────────────
	logoImg := canvas.NewImageFromResource(LogoResource())
	logoImg.FillMode = canvas.ImageFillContain
	logoImg.SetMinSize(fyne.NewSize(320, 100))

	sub := canvas.NewText("Game Route Optimizer", ColorTextSec)
	sub.TextSize = 14
	sub.Alignment = fyne.TextAlignCenter

	// ── Status label ──────────────────────────────────────────────
	statusLabel := canvas.NewText("Inicializando...", ColorTextSec)
	statusLabel.TextSize = 12
	statusLabel.Alignment = fyne.TextAlignCenter

	// ── Progress bar ──────────────────────────────────────────────
	progress := widget.NewProgressBar()
	progress.Min = 0
	progress.Max = 1
	progress.Value = 0

	progressContainer := container.NewPadded(progress)

	// ── Route count badge ─────────────────────────────────────────
	total := state.RouteDB.TotalNodes()
	routeCount := canvas.NewText(
		fmt.Sprintf("Sincronizando %d rotas...", total),
		ColorTextDim,
	)
	routeCount.TextSize = 11
	routeCount.Alignment = fyne.TextAlignCenter

	// ── Version ───────────────────────────────────────────────────
	ver := canvas.NewText("v0.2.0  •  freeware  •  sem servidores de terceiros", ColorTextDim)
	ver.TextSize = 10
	ver.Alignment = fyne.TextAlignCenter

	// ── Layout ────────────────────────────────────────────────────
	content := container.NewVBox(
		spacer(60),
		container.NewCenter(logoImg),
		spacer(4),
		sub,
		spacer(40),
		container.NewPadded(progressContainer),
		spacer(4),
		statusLabel,
		spacer(4),
		routeCount,
		spacer(40),
		ver,
	)

	bg := canvas.NewRectangle(ColorBG)
	centered := container.NewStack(
		bg,
		container.NewCenter(
			container.NewWithoutLayout(
				container.NewPadded(
					container.NewVBox(content),
				),
			),
		),
	)
	_ = centered

	// Wrap in a max-width center column
	col := container.NewGridWithColumns(3,
		spacer(1),
		container.NewPadded(content),
		spacer(1),
	)

	root := container.NewStack(bg, col)

	// ── Background sync goroutine ─────────────────────────────────
	go func() {
		// Phase 1: Initialize
		setStatus(statusLabel, routeCount, progress, "Carregando configurações...", 0.05, "")
		time.Sleep(300 * time.Millisecond)

		// Phase 2: Route probe (the "964 routes" equivalent)
		// We probe our builtin atlas in parallel
		done := 0
		total := state.RouteDB.TotalNodes()

		setStatus(statusLabel, routeCount, progress,
			fmt.Sprintf("Sincronizando %d rotas de teste...", total),
			0.1, fmt.Sprintf("0 / %d", total))

		state.RouteDB.ProbeAll(func(d, t int, name string) {
			done = d
			pct := 0.1 + 0.75*float32(d)/float32(t)
			label := fmt.Sprintf("Testando: %s", truncateName(name, 32))
			countStr := fmt.Sprintf("%d / %d rotas verificadas", d, t)
			setStatus(statusLabel, routeCount, progress, label, pct, countStr)
		})
		_ = done

		// Phase 3: Finalize
		setStatus(statusLabel, routeCount, progress, "Detectando jogos ativos...", 0.90, "")
		time.Sleep(200 * time.Millisecond)

		setStatus(statusLabel, routeCount, progress, "Pronto!", 1.0, "")
		time.Sleep(400 * time.Millisecond)

		// Transition to main view
		onReady()
	}()

	return root
}

func setStatus(statusLabel *canvas.Text, countLabel *canvas.Text, bar *widget.ProgressBar, status string, pct float32, count string) {
	// Fyne UI updates must happen on the main goroutine via Refresh
	statusLabel.Text = status
	statusLabel.Refresh()
	if count != "" {
		countLabel.Text = count
		countLabel.Refresh()
	}
	bar.SetValue(float64(pct))
}

func spacer(h float32) fyne.CanvasObject {
	s := canvas.NewRectangle(ColorBG)
	s.SetMinSize(fyne.NewSize(1, h))
	return s
}

func truncateName(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
