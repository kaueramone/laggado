package ui

import (
	"sync"

	"fyne.io/fyne/v2"

	"laggado/internal/discovery"
	"laggado/internal/geoip"
	"laggado/internal/routes"
	"laggado/internal/steamapi"
	"laggado/internal/store"
	"laggado/internal/tunnel"
)

// AppState holds all shared application state passed between screens.
type AppState struct {
	App          fyne.App
	Win          fyne.Window
	DB           *store.Database
	GeoRes       *geoip.Resolver
	RouteDB      *routes.Database
	CoverFetcher *steamapi.Fetcher
	Discovery    *discovery.Client  // Lagger Network client
	WG           *tunnel.WireGuard  // WireGuard manager
	DataDir      string

	// Current session state
	ActiveGameExe  string
	ActiveGameName string
	ActiveServerIP string
	ActiveRegion   string // "SA", "US", "EU", "ASIA"

	// Active tunnel (set by connect.go, cleared on disconnect)
	connMu       sync.Mutex
	ActiveTunnel *tunnel.TunnelSession  // nil when not connected
	ActiveConn   *store.GameConnection  // game currently being tunneled

	// Lagger Network state
	LaggerCount int  // updated periodically from GET /count
	AmIALagger  bool // true when this node is registered as a Lagger

	// Graph lifecycle — call to stop the connection graph goroutines.
	graphStopMu sync.Mutex
	graphStop   func()

	// Language / i18n
	Language Lang // LangPT, LangES, LangEN
}

// IsConnected returns true if a tunnel is currently active.
func (s *AppState) IsConnected() bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.ActiveTunnel != nil
}

// IsConnectedTo returns true if the tunnel is active for this game connection.
func (s *AppState) IsConnectedTo(gameID int) bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.ActiveTunnel != nil && s.ActiveConn != nil && s.ActiveConn.GameID == gameID
}

// RegisterGraphStop registers a function that stops the current connection
// graph's goroutines. Any previously registered stop function is called first.
func (s *AppState) RegisterGraphStop(stop func()) {
	s.graphStopMu.Lock()
	defer s.graphStopMu.Unlock()
	if s.graphStop != nil {
		s.graphStop()
	}
	s.graphStop = stop
}

// StopGraph stops any running connection graph goroutines.
func (s *AppState) StopGraph() {
	s.graphStopMu.Lock()
	defer s.graphStopMu.Unlock()
	if s.graphStop != nil {
		s.graphStop()
		s.graphStop = nil
	}
}
