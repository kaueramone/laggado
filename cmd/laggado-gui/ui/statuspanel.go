package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"laggado/internal/connmon"
	"laggado/internal/gamedet"
	"laggado/internal/ping"
	"laggado/internal/serverid"
)

// NewStatusPanel shows live connection stats: ping, jitter, loss over time.
func NewStatusPanel(state *AppState) fyne.CanvasObject {
	title := canvas.NewText(T("status.title"), ColorTextPrim)
	title.TextSize = 18
	title.TextStyle = fyne.TextStyle{Bold: true}

	// ── Stat cards com descrição ──────────────────────────────────
	pingVal := canvas.NewText("— ms", ColorTextPrim)
	pingVal.TextSize = 22
	pingVal.TextStyle = fyne.TextStyle{Bold: true}

	jitterVal := canvas.NewText("— ms", ColorTextPrim)
	jitterVal.TextSize = 22
	jitterVal.TextStyle = fyne.TextStyle{Bold: true}

	lossVal := canvas.NewText("—", ColorTextPrim)
	lossVal.TextSize = 22
	lossVal.TextStyle = fyne.TextStyle{Bold: true}

	routeVal := canvas.NewText("Direta", ColorAccent)
	routeVal.TextSize = 18
	routeVal.TextStyle = fyne.TextStyle{Bold: true}

	makeStatCard := func(labelKey, descKey string, val *canvas.Text) fyne.CanvasObject {
		lbl := canvas.NewText(T(labelKey), ColorTextDim)
		lbl.TextSize = 11
		lbl.TextStyle = fyne.TextStyle{Bold: true}
		desc := canvas.NewText(T(descKey), ColorTextDim)
		desc.TextSize = 9
		bg := canvas.NewRectangle(ColorCard)
		bg.CornerRadius = 8
		return container.NewStack(bg, container.NewPadded(container.NewVBox(
			lbl,
			spacer(2),
			val,
			spacer(4),
			desc,
		)))
	}

	statsRow := container.NewGridWithColumns(4,
		makeStatCard("status.ping", "status.ping.desc", pingVal),
		makeStatCard("status.jitter", "status.jitter.desc", jitterVal),
		makeStatCard("status.loss", "status.loss.desc", lossVal),
		makeStatCard("status.route", "status.route.desc", routeVal),
	)

	// ── Graph ─────────────────────────────────────────────────────
	graph := NewConnectionGraph(state, state.ActiveServerIP)

	// ── Recent sessions ──────────────────────────────────────────
	sessionTitle := canvas.NewText(T("status.sessions"), ColorTextSec)
	sessionTitle.TextSize = 12

	servers := state.DB.GetServers()
	rows := container.NewVBox()
	shown := 0
	for _, s := range servers {
		if shown >= 5 {
			break
		}
		row := container.NewGridWithColumns(4,
			labelText(s.IP, ColorTextPrim, 12),
			labelText(s.Country+" / "+s.City, ColorTextSec, 11),
			labelText(s.GameProcess, ColorTextDim, 11),
			labelText(s.LastSeen.Format("02/01 15:04"), ColorTextDim, 11),
		)
		bg := canvas.NewRectangle(ColorCard)
		bg.CornerRadius = 6
		rows.Add(container.NewStack(bg, container.NewPadded(row)))
		shown++
	}
	if shown == 0 {
		rows.Add(labelText(T("status.nosession"), ColorTextDim, 12))
	}

	// ── Live monitor ─────────────────────────────────────────────
	monHint := canvas.NewText(T("status.monitor.hint"), ColorTextDim)
	monHint.TextSize = 10

	monitoring := false
	monBtn := widget.NewButton(T("status.monitor.start"), nil)
	monBtn.Importance = widget.HighImportance

	stopCh := make(chan struct{}, 1)

	monBtn.OnTapped = func() {
		if !monitoring {
			monitoring = true
			monBtn.SetText(T("status.monitor.stop"))
			monBtn.Importance = widget.DangerImportance
			go liveMonitor(state, stopCh, pingVal, jitterVal, lossVal)
		} else {
			monitoring = false
			stopCh <- struct{}{}
			monBtn.SetText(T("status.monitor.start"))
			monBtn.Importance = widget.HighImportance
		}
	}

	monitorBg := canvas.NewRectangle(ColorCard)
	monitorBg.CornerRadius = 8
	monitorBlock := container.NewStack(monitorBg, container.NewPadded(
		container.NewBorder(nil, nil, nil, monBtn,
			container.NewVBox(monHint),
		),
	))

	// ── Layout ───────────────────────────────────────────────────
	content := container.NewVBox(
		container.NewPadded(title),
		widget.NewSeparator(),
		container.NewPadded(statsRow),
		container.NewPadded(graph),
		container.NewPadded(monitorBlock),
		widget.NewSeparator(),
		container.NewPadded(sessionTitle),
		container.NewPadded(rows),
	)

	return container.NewStack(canvas.NewRectangle(ColorBG), container.NewVScroll(container.NewPadded(content)))
}

func liveMonitor(state *AppState, stop chan struct{}, pingV, jitV, lossV *canvas.Text) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var latencies []float64

	probe := func(targetIP string) {
		// Use Windows ping.exe — works for UDP-only game servers (CS2, BF, RL, etc.)
		res, err := ping.Probe(targetIP, 4, 1500)
		if err != nil && res.AvgMs == 0 {
			return
		}
		if res.AvgMs == 0 && res.Loss >= 1.0 {
			pingV.Text = "timeout"
			pingV.Color = ColorRed
			pingV.Refresh()
			lossV.Text = "100%"
			lossV.Color = ColorRed
			lossV.Refresh()
			return
		}

		latencies = append(latencies, res.AvgMs)
		if len(latencies) > 20 {
			latencies = latencies[1:]
		}

		avg := mean(latencies)
		jit := jitter(latencies)

		pingV.Text = fmt.Sprintf("%.0f ms", avg)
		pingV.Color = LatencyColor(avg)
		pingV.Refresh()

		jitV.Text = fmt.Sprintf("%.0f ms", jit)
		jitV.Color = LatencyColor(jit * 3)
		jitV.Refresh()

		lossPct := res.Loss * 100
		lossV.Text = fmt.Sprintf("%.1f%%", lossPct)
		if lossPct < 1 {
			lossV.Color = ColorGreen
		} else if lossPct < 5 {
			lossV.Color = ColorYellow
		} else {
			lossV.Color = ColorRed
		}
		lossV.Refresh()
	}

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}

		// Detect server IP if not already known
		targetIP := state.ActiveServerIP
		if targetIP == "" {
			games, _ := gamedet.DetectGames()
			if len(games) > 0 {
				conns, _ := connmon.GetConnectionsByPID(games[0].PID)
				if best := serverid.IdentifyGameServer(conns); best != nil {
					targetIP = best.IP.String()
					state.ActiveServerIP = targetIP
				}
			}
		}
		if targetIP == "" {
			continue
		}

		go probe(targetIP)
	}
}


func labelText(s string, c color.Color, size float32) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	return t
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var s float64
	for _, v := range vals {
		s += v
	}
	return s / float64(len(vals))
}

func jitter(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	var s float64
	for i := 1; i < len(vals); i++ {
		d := vals[i] - vals[i-1]
		if d < 0 {
			d = -d
		}
		s += d
	}
	return s / float64(len(vals)-1)
}

