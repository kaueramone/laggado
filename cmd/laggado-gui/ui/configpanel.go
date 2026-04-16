package ui

import (
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"laggado/internal/routetest"
)

func NewConfigPanel(state *AppState) fyne.CanvasObject {
	title := canvas.NewText(T("config.title"), ColorTextPrim)
	title.TextSize = 18
	title.TextStyle = fyne.TextStyle{Bold: true}

	// ── Scoring weights ──────────────────────────────────────────
	wTitle := canvas.NewText(T("config.weights.title"), ColorTextSec)
	wTitle.TextSize = 12

	latEntry := widget.NewEntry()
	latEntry.SetText(fmt.Sprintf("%.1f", state.DB.Config.Weights.LatencyWeight))
	jitEntry := widget.NewEntry()
	jitEntry.SetText(fmt.Sprintf("%.1f", state.DB.Config.Weights.JitterWeight))
	lossEntry := widget.NewEntry()
	lossEntry.SetText(fmt.Sprintf("%.1f", state.DB.Config.Weights.LossWeight))

	weightForm := container.NewGridWithColumns(6,
		canvas.NewText("Latência ×", ColorTextSec), latEntry,
		canvas.NewText("Jitter ×", ColorTextSec), jitEntry,
		canvas.NewText("Perda ×", ColorTextSec), lossEntry,
	)

	saveWeightsBtn := widget.NewButton("Salvar Pesos", func() {
		lat, _ := strconv.ParseFloat(latEntry.Text, 64)
		jit, _ := strconv.ParseFloat(jitEntry.Text, 64)
		loss, _ := strconv.ParseFloat(lossEntry.Text, 64)
		if lat > 0 {
			state.DB.Config.Weights.LatencyWeight = lat
		}
		if jit >= 0 {
			state.DB.Config.Weights.JitterWeight = jit
		}
		if loss >= 0 {
			state.DB.Config.Weights.LossWeight = loss
		}
		state.DB.Save()
		dialog.ShowInformation("Salvo", "Pesos atualizados.", state.Win)
	})

	// ── VPS endpoints ─────────────────────────────────────────────
	vpsTitle := canvas.NewText(T("config.vps.title"), ColorTextSec)
	vpsTitle.TextSize = 12

	vpsHint := canvas.NewText("", ColorTextDim) // replaced by makeDesc below
	_ = vpsHint

	vpsNameEntry := widget.NewEntry()
	vpsNameEntry.SetPlaceHolder("Nome  (ex: EU-Frankfurt)")
	vpsAddrEntry := widget.NewEntry()
	vpsAddrEntry.SetPlaceHolder("IP ou hostname")
	vpsPortEntry := widget.NewEntry()
	vpsPortEntry.SetText("22")

	addVPSBtn := widget.NewButton("+ Adicionar VPS", func() {
		name := vpsNameEntry.Text
		addr := vpsAddrEntry.Text
		port, _ := strconv.Atoi(vpsPortEntry.Text)
		if name == "" || addr == "" {
			dialog.ShowInformation("Erro", "Preencha nome e endereço.", state.Win)
			return
		}
		if port <= 0 {
			port = 22
		}
		state.DB.Config.VPSEndpoints = append(state.DB.Config.VPSEndpoints,
			routetest.VPSEndpoint{Name: name, Address: addr, Port: port})
		state.DB.Save()
		vpsNameEntry.SetText("")
		vpsAddrEntry.SetText("")
		dialog.ShowInformation("Adicionado", name+" adicionado com sucesso!", state.Win)
	})

	vpsForm := container.NewGridWithColumns(4,
		vpsNameEntry, vpsAddrEntry, vpsPortEntry, addVPSBtn,
	)

	// VPS list
	vpsList := container.NewVBox()
	var refreshVPSList func()
	refreshVPSList = func() {
		vpsList.Objects = nil
		for i, v := range state.DB.Config.VPSEndpoints {
			i, v := i, v
			label := canvas.NewText(fmt.Sprintf("%s  →  %s:%d", v.Name, v.Address, v.Port), ColorTextPrim)
			label.TextSize = 12
			removeBtn := widget.NewButton("✕", func() {
				eps := state.DB.Config.VPSEndpoints
				state.DB.Config.VPSEndpoints = append(eps[:i], eps[i+1:]...)
				state.DB.Save()
				refreshVPSList()
			})
			removeBtn.Importance = widget.DangerImportance
			bg := canvas.NewRectangle(ColorCard)
			bg.CornerRadius = 6
			row := container.NewStack(bg, container.NewPadded(
				container.NewBorder(nil, nil, nil, removeBtn, label),
			))
			vpsList.Add(row)
		}
		if len(state.DB.Config.VPSEndpoints) == 0 {
			vpsList.Add(canvas.NewText("Nenhum VPS configurado.", ColorTextDim))
		}
		vpsList.Refresh()
	}
	refreshVPSList()

	// ── Ping settings ─────────────────────────────────────────────
	pingTitle := canvas.NewText(T("config.ping.title"), ColorTextSec)
	pingTitle.TextSize = 12

	pingCountEntry := widget.NewEntry()
	pingCountEntry.SetText(strconv.Itoa(state.DB.Config.PingCount))
	scanIntEntry := widget.NewEntry()
	scanIntEntry.SetText(strconv.Itoa(state.DB.Config.ScanIntervalSec))

	pingForm := container.NewGridWithColumns(4,
		canvas.NewText("Probes por rota:", ColorTextSec), pingCountEntry,
		canvas.NewText("Intervalo de scan (s):", ColorTextSec), scanIntEntry,
	)

	savePingBtn := widget.NewButton("Salvar", func() {
		if n, err := strconv.Atoi(pingCountEntry.Text); err == nil && n > 0 {
			state.DB.Config.PingCount = n
		}
		if n, err := strconv.Atoi(scanIntEntry.Text); err == nil && n > 0 {
			state.DB.Config.ScanIntervalSec = n
		}
		state.DB.Save()
		dialog.ShowInformation("Salvo", "Configurações salvas.", state.Win)
	})

	// ── WireGuard info ────────────────────────────────────────────
	wgTitle := canvas.NewText(T("config.wg.title"), ColorTextSec)
	wgTitle.TextSize = 12

	wgStatus := "WireGuard não encontrado — instale em wireguard.com"
	// Check path
	wgStatusLabel := canvas.NewText(wgStatus, ColorTextDim)
	wgStatusLabel.TextSize = 11

	// ── Anti-cheat compatibility ──────────────────────────────────
	acTitle := canvas.NewText(T("config.ac.title"), ColorTextSec)
	acTitle.TextSize = 12

	acLines := []string{
		"✅  EAC (Easy Anti-Cheat) — compatível",
		"✅  VAC (Valve Anti-Cheat) — compatível",
		"✅  BattlEye — compatível",
		"✅  Vanguard (Riot) — compatível",
		"",
		"LAGGADO usa apenas:",
		"  • Windows API (GetExtendedTcpTable / UDP) — mesma técnica do netstat",
		"  • ping.exe nativo do Windows — sem privilégios especiais",
		"  • Sem drivers kernel  •  Sem injeção de DLL  •  Sem leitura de memória",
		"",
		"⚠  Use apenas fora de partidas ativas para alterar rotas.",
		"   Monitoramento passivo é seguro em qualquer momento.",
	}

	acBox := container.NewVBox()
	for _, line := range acLines {
		c := ColorTextSec
		if line == "" {
			acBox.Add(spacer(2))
			continue
		}
		if strings.HasPrefix(line, "⚠") {
			c = ColorYellow
		} else if strings.HasPrefix(line, "✅") {
			c = ColorGreen
		}
		t := canvas.NewText(line, c)
		t.TextSize = 11
		acBox.Add(t)
	}

	acBg := canvas.NewRectangle(ColorCard)
	acBg.CornerRadius = 8
	acCard := container.NewStack(acBg, container.NewPadded(acBox))

	// ── Data dir ─────────────────────────────────────────────────
	dataDirLabel := canvas.NewText("Diretório de dados: "+state.DataDir, ColorTextDim)
	dataDirLabel.TextSize = 10

	geoLabel := canvas.NewText(fmt.Sprintf("Cache GeoIP: %d entradas", state.GeoRes.CacheSize()), ColorTextDim)
	geoLabel.TextSize = 10

	// ── Descriptions ─────────────────────────────────────────────
	makeDesc := func(key string) fyne.CanvasObject {
		t := canvas.NewText(T(key), ColorTextDim)
		t.TextSize = 11
		return t
	}

	// ── Layout ───────────────────────────────────────────────────
	sep := func() fyne.CanvasObject { return container.NewPadded(widget.NewSeparator()) }

	content := container.NewVBox(
		container.NewPadded(title),
		sep(),
		section(wTitle, makeDesc("config.weights.desc"), weightForm, saveWeightsBtn),
		sep(),
		section(vpsTitle, makeDesc("config.vps.desc"), vpsForm, vpsList),
		sep(),
		section(pingTitle, makeDesc("config.ping.desc"), pingForm, savePingBtn),
		sep(),
		section(wgTitle, makeDesc("config.wg.desc"), wgStatusLabel),
		sep(),
		section(acTitle, makeDesc("config.ac.desc"), acCard),
		sep(),
		container.NewPadded(dataDirLabel),
		container.NewPadded(geoLabel),
	)

	return container.NewStack(
		canvas.NewRectangle(ColorBG),
		container.NewVScroll(container.NewPadded(content)),
	)
}

func section(objs ...fyne.CanvasObject) fyne.CanvasObject {
	box := container.NewVBox()
	for _, o := range objs {
		box.Add(container.NewPadded(o))
	}
	return box
}
