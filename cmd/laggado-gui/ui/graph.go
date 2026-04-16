package ui

// NewConnectionGraph renders the live connection diagram:
//
//	[Você] ──── Xms ──── [Relay/ISP] ──── Xms ──── [Servidor]
//
// Implemented as a custom Fyne canvas widget using primitives.

import (
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"laggado/internal/ping"
)

// connectionGraphWidget is a custom widget that draws the topology.
type connectionGraphWidget struct {
	widget.BaseWidget

	localLabel  string
	relayLabel  string
	serverLabel string

	localPing  float64
	relayPing  float64
	totalPing  float64

	// history for sparkline (last 30 samples)
	pingHistory []float64
	maxHistory  int

	// stop channel for the live-ping goroutine
	stopCh   chan struct{}
	stopOnce sync.Once
}

func newConnectionGraphWidget() *connectionGraphWidget {
	g := &connectionGraphWidget{
		localLabel:  "Você",
		relayLabel:  "Internet / ISP",
		serverLabel: "Servidor",
		maxHistory:  30,
		stopCh:      make(chan struct{}),
	}
	g.ExtendBaseWidget(g)
	return g
}

func (g *connectionGraphWidget) Update(localPing, relayPing, total float64, relay, server string) {
	g.localPing = localPing
	g.relayPing = relayPing
	g.totalPing = total
	if relay != "" {
		g.relayLabel = relay
	}
	if server != "" {
		g.serverLabel = server
	}
	if total > 0 {
		g.pingHistory = append(g.pingHistory, total)
		if len(g.pingHistory) > g.maxHistory {
			g.pingHistory = g.pingHistory[1:]
		}
	}
	g.Refresh()
}

// Stop signals the live-ping goroutine to exit.
func (g *connectionGraphWidget) Stop() {
	g.stopOnce.Do(func() { close(g.stopCh) })
}

func (g *connectionGraphWidget) CreateRenderer() fyne.WidgetRenderer {
	const maxSpark = 30 // matches maxHistory

	// ── Pre-allocate ALL canvas objects once ──────────────────────
	bg := canvas.NewRectangle(ColorPanel)
	bg.CornerRadius = 10

	// 2 connection lines (solid, no animated dashes)
	lines := [2]*canvas.Line{
		{StrokeWidth: 2.5},
		{StrokeWidth: 2.5},
	}
	lines[0].StrokeColor = colorToNRGBA(ColorBorder)
	lines[1].StrokeColor = colorToNRGBA(ColorBorder)

	// 2 ping labels above the lines
	pingLabels := [2]*canvas.Text{
		canvas.NewText("—", colorToNRGBA(ColorTextDim)),
		canvas.NewText("—", colorToNRGBA(ColorTextDim)),
	}
	for _, l := range pingLabels {
		l.TextSize = 12
		l.TextStyle = fyne.TextStyle{Bold: true}
	}

	// 3 nodes: circle + icon + label
	nodeCircles := [3]*canvas.Circle{
		{StrokeColor: colorToNRGBA(ColorBorder), StrokeWidth: 1.5},
		{StrokeColor: colorToNRGBA(ColorBorder), StrokeWidth: 1.5},
		{StrokeColor: colorToNRGBA(ColorBorder), StrokeWidth: 1.5},
	}
	nodeIcons := [3]*canvas.Text{
		canvas.NewText("💻", color.White),
		canvas.NewText("🌐", color.White),
		canvas.NewText("🖥", color.White),
	}
	nodeLabels := [3]*canvas.Text{
		canvas.NewText("Você", colorToNRGBA(ColorTextSec)),
		canvas.NewText("Internet / ISP", colorToNRGBA(ColorTextSec)),
		canvas.NewText("Servidor", colorToNRGBA(ColorTextSec)),
	}
	for _, t := range nodeIcons {
		t.TextSize = 14
	}
	for _, t := range nodeLabels {
		t.TextSize = 11
	}

	// Sparkline segments (pre-allocated, hidden when unused)
	sparkSegs := make([]*canvas.Line, maxSpark)
	for i := range sparkSegs {
		sparkSegs[i] = &canvas.Line{StrokeWidth: 1.5, StrokeColor: colorToNRGBA(ColorTextDim)}
		sparkSegs[i].Hide()
	}
	curPingTxt := canvas.NewText("", colorToNRGBA(ColorTextDim))
	curPingTxt.TextSize = 10

	// Build stable objects slice
	objs := []fyne.CanvasObject{bg}
	for _, l := range lines {
		objs = append(objs, l)
	}
	for _, t := range pingLabels {
		objs = append(objs, t)
	}
	for i := 0; i < 3; i++ {
		objs = append(objs, nodeCircles[i], nodeIcons[i], nodeLabels[i])
	}
	for _, s := range sparkSegs {
		objs = append(objs, s)
	}
	objs = append(objs, curPingTxt)

	return &graphRenderer{
		g:           g,
		objects:     objs,
		bg:          bg,
		lines:       lines,
		pingLabels:  pingLabels,
		nodeCircles: nodeCircles,
		nodeIcons:   nodeIcons,
		nodeLabels:  nodeLabels,
		sparkSegs:   sparkSegs,
		curPingTxt:  curPingTxt,
	}
}

// graphRenderer draws the graph using stable, pre-allocated canvas primitives.
// Objects() returns a fixed slice; Refresh() only updates properties.
type graphRenderer struct {
	g           *connectionGraphWidget
	objects     []fyne.CanvasObject

	bg          *canvas.Rectangle
	lines       [2]*canvas.Line
	pingLabels  [2]*canvas.Text
	nodeCircles [3]*canvas.Circle
	nodeIcons   [3]*canvas.Text
	nodeLabels  [3]*canvas.Text
	sparkSegs   []*canvas.Line
	curPingTxt  *canvas.Text
}

func (r *graphRenderer) Layout(size fyne.Size) {}
func (r *graphRenderer) MinSize() fyne.Size    { return fyne.NewSize(600, 140) }
func (r *graphRenderer) Destroy()              {}
func (r *graphRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *graphRenderer) Refresh() {
	g := r.g

	const (
		nodeY = float32(65)
		w     = float32(700)
		nodeR = float32(18)
	)
	nodeXs := [3]float32{w * 0.12, w * 0.50, w * 0.88}

	// ── Connection lines ──────────────────────────────────────────
	pings := [2]float64{g.localPing, g.relayPing}
	for i := 0; i < 2; i++ {
		x1 := nodeXs[i] + nodeR
		x2 := nodeXs[i+1] - nodeR
		midX := (x1 + x2) / 2

		ms := pings[i]
		lc := colorToNRGBA(ColorBorder)
		if ms > 0 {
			lc = colorToNRGBA(LatencyColor(ms))
		}
		r.lines[i].StrokeColor = lc
		r.lines[i].Position1 = fyne.NewPos(x1, nodeY)
		r.lines[i].Position2 = fyne.NewPos(x2, nodeY)

		var txt string
		if ms > 0 {
			txt = fmt.Sprintf("%.0fms", ms)
		} else {
			txt = "—"
		}
		r.pingLabels[i].Text = txt
		r.pingLabels[i].Color = lc
		r.pingLabels[i].Move(fyne.NewPos(midX-20, nodeY-22))
	}

	// ── Nodes ─────────────────────────────────────────────────────
	nodePings := [3]float64{0, g.localPing, g.totalPing}
	nodeTextLabels := [3]string{g.localLabel, g.relayLabel, g.serverLabel}

	for i, x := range nodeXs {
		dotColor := colorToNRGBA(ColorCard)
		if nodePings[i] > 0 {
			dotColor = colorToNRGBA(LatencyColor(nodePings[i]))
		} else if i == 0 {
			dotColor = colorToNRGBA(ColorAccent)
		}
		r.nodeCircles[i].FillColor = dotColor
		r.nodeCircles[i].Move(fyne.NewPos(x-nodeR, nodeY-nodeR))
		r.nodeCircles[i].Resize(fyne.NewSize(nodeR*2, nodeR*2))

		r.nodeIcons[i].Move(fyne.NewPos(x-9, nodeY-10))

		lbl := truncateName(nodeTextLabels[i], 20)
		r.nodeLabels[i].Text = lbl
		r.nodeLabels[i].Move(fyne.NewPos(x-float32(len(lbl))*3.2, nodeY+nodeR+5))
	}

	// ── Sparkline ─────────────────────────────────────────────────
	hist := g.pingHistory
	const (
		sparkX = float32(20)
		sparkY = float32(128)
		sparkH = float32(18)
	)
	sparkW := w - 40

	// Hide all segments first, then show the ones we need
	for _, s := range r.sparkSegs {
		s.Hide()
	}
	r.curPingTxt.Text = ""

	if len(hist) > 1 {
		maxPing := float64(1)
		for _, v := range hist {
			if v > maxPing {
				maxPing = v
			}
		}
		n := len(hist)
		for i := 1; i < n && i < len(r.sparkSegs)+1; i++ {
			x1 := sparkX + float32(i-1)*sparkW/float32(n-1)
			x2 := sparkX + float32(i)*sparkW/float32(n-1)
			y1 := sparkY - float32(hist[i-1]/maxPing)*sparkH
			y2 := sparkY - float32(hist[i]/maxPing)*sparkH

			seg := r.sparkSegs[i-1]
			seg.StrokeColor = colorToNRGBA(LatencyColor(hist[i]))
			seg.Position1 = fyne.NewPos(x1, y1)
			seg.Position2 = fyne.NewPos(x2, y2)
			seg.Show()
		}

		last := hist[len(hist)-1]
		r.curPingTxt.Text = fmt.Sprintf("Ping atual: %.0fms", last)
		r.curPingTxt.Color = colorToNRGBA(LatencyColor(last))
		r.curPingTxt.Move(fyne.NewPos(sparkX, sparkY-sparkH-12))
	}
}

// ── Public factory ────────────────────────────────────────────────────────

// NewConnectionGraph creates the graph panel with a live-updating widget.
// The graph's internal goroutines are stopped automatically via g.stopCh
// when StopAnimation is called. Callers that navigate away should call
// g.StopAnimation(); here we tie the stop to state.GraphStop so the
// mainview panel lifecycle can stop it when switching tabs.
func NewConnectionGraph(state *AppState, serverIP string) fyne.CanvasObject {
	g := newConnectionGraphWidget()
	g.serverLabel = "Servidor"
	if serverIP != "" {
		g.serverLabel = serverIP
	}

	// Register stop hook so tab-switching tears down the goroutine.
	state.RegisterGraphStop(g.Stop)

	// Live ping goroutine — uses ICMP via ping.exe (works with UDP-only servers).
	go func() {
		defer func() { recover() }()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-g.stopCh:
				return
			case <-ticker.C:
			}

			ip := state.ActiveServerIP
			if ip == "" {
				continue
			}

			res, err := ping.Probe(ip, 4, 1500)
			if err != nil && res.AvgMs == 0 {
				continue
			}
			if res.AvgMs > 0 {
				g.Update(res.AvgMs, 0, res.AvgMs, "Internet / ISP", ip)
			}
		}
	}()

	title := canvas.NewText("Conexão em Tempo Real", ColorTextSec)
	title.TextSize = 11

	bg := canvas.NewRectangle(ColorPanel)
	bg.CornerRadius = 10

	return container.NewStack(bg, container.NewVBox(
		container.NewPadded(title),
		container.NewPadded(g),
	))
}

// ── Helpers ───────────────────────────────────────────────────────────────

func colorToNRGBA(c color.Color) color.NRGBA {
	if n, ok := c.(color.NRGBA); ok {
		return n
	}
	r, g, b, a := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
