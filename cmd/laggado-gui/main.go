package main

import (
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"

	"laggado/internal/discovery"
	"laggado/internal/geoip"
	"laggado/internal/routes"
	"laggado/internal/steamapi"
	"laggado/internal/store"
	"laggado/internal/tunnel"
	"laggado/cmd/laggado-gui/ui"
)

func main() {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".laggado")

	// Bootstrap data layer
	db, _ := store.Open(dataDir)
	geoRes, _ := geoip.NewResolver(filepath.Join(dataDir, "cache"))
	routeDB := routes.NewDatabase(dataDir, db.Config.VPSEndpoints)
	coverFetcher := steamapi.NewFetcher(filepath.Join(dataDir, "covers"))

	// Lagger Network — every user that opens the app is a potential Lagger.
	// The Worker URL can be overridden via config for self-hosted deployments.
	workerURL := db.Config.DiscoveryURL // "" = uses DefaultWorkerURL
	disc := discovery.NewClient(workerURL, dataDir)

	// WireGuard manager — auto-detects installed wg.exe / wireguard.exe.
	wg := tunnel.NewWireGuard(filepath.Join(dataDir, "wg"))

	a := app.New()
	a.Settings().SetTheme(ui.DarkTheme())

	w := a.NewWindow("LAGGADO")
	w.SetIcon(ui.AppIcon())
	w.Resize(fyne.NewSize(1000, 680))
	w.SetFixedSize(false)
	w.CenterOnScreen()

	// Wire up app state
	state := &ui.AppState{
		App:          a,
		Win:          w,
		DB:           db,
		GeoRes:       geoRes,
		RouteDB:      routeDB,
		CoverFetcher: coverFetcher,
		Discovery:    disc,
		WG:           wg,
		DataDir:      dataDir,
	}

	// Gracefully leave the Lagger Network and tear down any active tunnel when the window closes.
	w.SetOnClosed(func() {
		if disc.IsLagger() {
			disc.Leave()
		}
		if state.IsConnected() {
			// Best-effort tunnel cleanup on exit
			ui.StopConnect(state, func(_ string) {}, func(_ error) {})
		}
	})

	// Start with splash → then main view
	splash := ui.NewSplashScreen(state, func() {
		w.SetContent(ui.NewMainView(state))
		w.SetTitle("LAGGADO — Game Route Optimizer")

		// Tenta tornar este PC um Lagger (relay node) em background.
		// Falha silenciosamente se WireGuard não estiver disponível ou UPnP falhar.
		ui.StartLaggerMode(state)
	})

	w.SetContent(splash)
	w.SetMaster()

	// Apply dark window chrome on Windows
	applyWindowsStyle(w)

	a.Settings().SetTheme(theme.DarkTheme())
	w.ShowAndRun()
}
