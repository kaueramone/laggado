// Package store provides JSON-based persistence for detected servers,
// route history, and configuration.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"laggado/internal/routetest"
	"laggado/internal/scorer"
)

// ServerRecord stores information about a detected game server.
type ServerRecord struct {
	IP          string    `json:"ip"`
	Port        uint16    `json:"port"`
	Protocol    string    `json:"protocol"`
	GameProcess string    `json:"gameProcess"`
	Country     string    `json:"country"`
	City        string    `json:"city"`
	Region      string    `json:"region"`
	ISP         string    `json:"isp"`
	FirstSeen   time.Time `json:"firstSeen"`
	LastSeen    time.Time `json:"lastSeen"`
	TimesFound  int       `json:"timesFound"`
}

// LatencyRecord stores a historical latency measurement.
type LatencyRecord struct {
	ServerIP   string    `json:"serverIP"`
	Via        string    `json:"via"`
	AvgLatency float64   `json:"avgLatencyMs"`
	Jitter     float64   `json:"jitterMs"`
	Loss       float64   `json:"lossPct"`
	Score      float64   `json:"score"`
	Timestamp  time.Time `json:"timestamp"`
}

// GameConnection represents a per-game routing profile (ExitLag "Conexões" equivalent).
type GameConnection struct {
	GameID      int    `json:"gameId"`      // ExitLag app_id
	GameName    string `json:"gameName"`
	GameExe     string `json:"gameExe"`     // process name slug for gameservers.GetRoutes, e.g. "cs2"
	Enabled     bool   `json:"enabled"`     // route active toggle
	Region      string `json:"region"`      // "SA", "US", "EU", "ASIA", "AUTO"
	ServerAlias string `json:"serverAlias"` // specific relay alias if any
	// Last session stats
	LastSessionDuration string  `json:"lastSessionDuration"`
	LastAvgPing         int     `json:"lastAvgPing"`
	LastMinPing         int     `json:"lastMinPing"`
	LastMaxPing         int     `json:"lastMaxPing"`
	LastAvgJitter       int     `json:"lastAvgJitter"`
	LastPktLoss         float64 `json:"lastPktLoss"`
}

// WireGuardPeer holds the WireGuard relay configuration for one VPS.
type WireGuardPeer struct {
	VPSName       string `json:"vpsName"`
	PeerPublicKey string `json:"peerPublicKey"` // VPS's WireGuard public key
	PeerEndpoint  string `json:"peerEndpoint"`  // VPS_IP:51820
	TunnelAddress string `json:"tunnelAddress"` // client tunnel IP (e.g. "10.66.66.2/32")
	GatewayIP     string `json:"gatewayIP"`     // VPS tunnel gateway (e.g. "10.66.66.1")
}

// AppConfig holds user-configurable settings.
type AppConfig struct {
	// Scoring weights
	Weights scorer.Weights `json:"weights"`

	// VPS endpoints for route testing
	VPSEndpoints []routetest.VPSEndpoint `json:"vpsEndpoints"`

	// Polling intervals
	ScanIntervalSec  int `json:"scanIntervalSec"`
	TestIntervalSec  int `json:"testIntervalSec"`
	PingCount        int `json:"pingCount"`

	// WireGuard
	WireGuardConfigDir     string                   `json:"wireguardConfigDir"`
	WireGuardPrivateKey    string                   `json:"wireguardPrivateKey,omitempty"`
	WireGuardPublicKey     string                   `json:"wireguardPublicKey,omitempty"`
	WireGuardPeers         map[string]WireGuardPeer `json:"wireguardPeers,omitempty"`
	// Last activated split tunnel (used by "tunnel stop" to clean up routes)
	ActiveTunnelTargetIP   string `json:"activeTunnelTargetIP,omitempty"`
	ActiveTunnelGatewayIP  string `json:"activeTunnelGatewayIP,omitempty"`

	// Per-game connection profiles
	Connections []GameConnection `json:"connections"`

	// Lagger Network
	// DiscoveryURL overrides the default Cloudflare Worker URL.
	// Leave empty to use the official LAGGADO registry.
	DiscoveryURL string `json:"discoveryUrl,omitempty"`
	// LaggerEnabled controls whether this node participates as a relay.
	// Defaults to true — every user contributes to the community by default.
	LaggerEnabled bool `json:"laggerEnabled"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() AppConfig {
	return AppConfig{
		Weights:            scorer.DefaultWeights(),
		VPSEndpoints:       nil,
		ScanIntervalSec:    5,
		TestIntervalSec:    30,
		PingCount:          10,
		WireGuardConfigDir: "",
		LaggerEnabled:      true, // opt-in by default — users contribute to the network
	}
}

// Database is the top-level data store.
type Database struct {
	mu       sync.RWMutex
	dir      string
	Servers  map[string]*ServerRecord           `json:"servers"`  // keyed by IP
	Latency  map[string][]LatencyRecord          `json:"latency"`  // keyed by serverIP
	History  map[string]*scorer.RouteHistory     `json:"history"`  // keyed by serverIP
	Config   AppConfig                           `json:"config"`
}

// Open loads (or creates) the database in the given directory.
func Open(dir string) (*Database, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db := &Database{
		dir:     dir,
		Servers: make(map[string]*ServerRecord),
		Latency: make(map[string][]LatencyRecord),
		History: make(map[string]*scorer.RouteHistory),
		Config:  DefaultConfig(),
	}

	db.load()
	return db, nil
}

// RecordServer adds or updates a detected server.
func (db *Database) RecordServer(rec ServerRecord) {
	db.mu.Lock()
	defer db.mu.Unlock()

	existing, ok := db.Servers[rec.IP]
	if ok {
		existing.LastSeen = time.Now()
		existing.TimesFound++
		// Update geo if it was empty
		if existing.Country == "" && rec.Country != "" {
			existing.Country = rec.Country
			existing.City = rec.City
			existing.Region = rec.Region
			existing.ISP = rec.ISP
		}
	} else {
		rec.FirstSeen = time.Now()
		rec.LastSeen = time.Now()
		rec.TimesFound = 1
		db.Servers[rec.IP] = &rec
	}
}

// RecordLatency stores a latency measurement.
func (db *Database) RecordLatency(serverIP string, via string, m *routetest.RouteMetrics, score float64) {
	db.mu.Lock()
	defer db.mu.Unlock()

	record := LatencyRecord{
		ServerIP:   serverIP,
		Via:        via,
		AvgLatency: float64(m.AvgLatency.Microseconds()) / 1000.0,
		Jitter:     float64(m.Jitter.Microseconds()) / 1000.0,
		Loss:       m.PacketLoss,
		Score:      score,
		Timestamp:  time.Now(),
	}

	history := db.Latency[serverIP]
	history = append(history, record)
	// Keep last 100 records per server
	if len(history) > 100 {
		history = history[len(history)-100:]
	}
	db.Latency[serverIP] = history
}

// AddConnection adds or updates a game connection profile.
func (db *Database) AddConnection(gc GameConnection) {
	db.mu.Lock()
	defer db.mu.Unlock()
	for i, c := range db.Config.Connections {
		if c.GameID == gc.GameID {
			db.Config.Connections[i] = gc
			return
		}
	}
	db.Config.Connections = append(db.Config.Connections, gc)
}

// RemoveConnection removes a game connection profile by game ID.
func (db *Database) RemoveConnection(gameID int) {
	db.mu.Lock()
	defer db.mu.Unlock()
	conns := db.Config.Connections[:0]
	for _, c := range db.Config.Connections {
		if c.GameID != gameID {
			conns = append(conns, c)
		}
	}
	db.Config.Connections = conns
}

// GetConnections returns all game connection profiles.
func (db *Database) GetConnections() []GameConnection {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return append([]GameConnection{}, db.Config.Connections...)
}

// GetServers returns all known servers.
func (db *Database) GetServers() []*ServerRecord {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var result []*ServerRecord
	for _, s := range db.Servers {
		result = append(result, s)
	}
	return result
}

// GetLatency returns latency history for a server.
func (db *Database) GetLatency(serverIP string) []LatencyRecord {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.Latency[serverIP]
}

// Save persists the database to disk.
func (db *Database) Save() error {
	db.mu.RLock()
	defer db.mu.RUnlock()

	// Save servers
	if err := writeJSON(filepath.Join(db.dir, "servers.json"), db.Servers); err != nil {
		return fmt.Errorf("save servers: %w", err)
	}

	// Save latency history
	if err := writeJSON(filepath.Join(db.dir, "latency.json"), db.Latency); err != nil {
		return fmt.Errorf("save latency: %w", err)
	}

	// Save route history
	if err := writeJSON(filepath.Join(db.dir, "history.json"), db.History); err != nil {
		return fmt.Errorf("save history: %w", err)
	}

	// Save config
	if err := writeJSON(filepath.Join(db.dir, "config.json"), db.Config); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	return nil
}

func (db *Database) load() {
	readJSON(filepath.Join(db.dir, "servers.json"), &db.Servers)
	readJSON(filepath.Join(db.dir, "latency.json"), &db.Latency)
	readJSON(filepath.Join(db.dir, "history.json"), &db.History)
	readJSON(filepath.Join(db.dir, "config.json"), &db.Config)
}

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readJSON(path string, v interface{}) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	json.Unmarshal(data, v)
}
