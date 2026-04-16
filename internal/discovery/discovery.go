// Package discovery implements the LAGGADO Lagger Network client.
//
// When the app opens, every user is a potential Lagger — a community relay node.
// This package handles:
//
//   - Registering this machine as a Lagger (POST /register)
//   - Sending heartbeats every 2 minutes to stay visible
//   - Gracefully leaving when the app closes (DELETE /leave)
//   - Querying active Laggers by region (GET /laggers)
//   - Fetching the global Lagger count for the UI counter (GET /count)
//
// The discovery backend is a Cloudflare Worker (zero cost, ~20 Laggers free tier).
// Endpoint: configurable, defaults to the official LAGGADO Worker URL.
package discovery

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultWorkerURL is the official LAGGADO Lagger registry.
// Replace with your own Cloudflare Worker URL after deployment.
const DefaultWorkerURL = "https://laggado-laggers.kaueramone.workers.dev"

// HeartbeatInterval is how often a Lagger sends a presence update.
// Must be less than half of server-side TTL (5 minutes) to avoid expiry.
const HeartbeatInterval = 2 * time.Minute

// ── Types ─────────────────────────────────────────────────────────────────────

// LaggerInfo describes an active Lagger node returned by the registry.
type LaggerInfo struct {
	ID       string `json:"id"`
	Region   string `json:"region"`
	Endpoint string `json:"endpoint"` // "IP:PORT" for WireGuard (e.g. "1.2.3.4:51820")
	RelayAPI string `json:"relayApi"` // "http://IP:PORT" for relay HTTP API (e.g. "http://1.2.3.4:7735")
	City     string `json:"city,omitempty"`
	Country  string `json:"country,omitempty"`
	LastSeen int64  `json:"lastSeen"` // Unix ms
}

// LaggerList is the response from GET /laggers.
type LaggerList struct {
	Laggers []LaggerInfo `json:"laggers"`
	Count   int          `json:"count"`
	Ts      int64        `json:"ts"`
}

// CountResponse is the response from GET /count.
type CountResponse struct {
	Count int   `json:"count"`
	Ts    int64 `json:"ts"`
}

// ── Client ────────────────────────────────────────────────────────────────────

// Client manages this node's participation in the Lagger Network.
type Client struct {
	workerURL string
	dataDir   string
	http      *http.Client

	mu          sync.Mutex
	laggerID    string // stable UUID persisted to disk
	isLagger    bool   // true when registered and active
	heartbeatCh chan struct{}
	stopCh      chan struct{}

	// Set before calling Register()
	WgPublicKey string
	Endpoint    string // "IP:WGPort" after UPnP mapping
	RelayAPI    string // "http://IP:7735" — this node's relay HTTP API URL
	Region      string
	City        string
	Country     string
	Version     string
}

// NewClient creates a discovery client.
// dataDir is used to persist the lagger ID across restarts.
// workerURL can be empty to use DefaultWorkerURL.
func NewClient(workerURL, dataDir string) *Client {
	if workerURL == "" {
		workerURL = DefaultWorkerURL
	}
	return &Client{
		workerURL:   workerURL,
		dataDir:     dataDir,
		http:        &http.Client{Timeout: 10 * time.Second},
		heartbeatCh: make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
	}
}

// LaggerID returns this node's stable identity, loading or generating it.
func (c *Client) LaggerID() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.laggerID != "" {
		return c.laggerID
	}

	// Try loading from disk
	idFile := filepath.Join(c.dataDir, "lagger_id")
	if data, err := os.ReadFile(idFile); err == nil && len(data) >= 8 {
		c.laggerID = string(data)
		return c.laggerID
	}

	// Generate new ID
	b := make([]byte, 16)
	rand.Read(b)
	c.laggerID = hex.EncodeToString(b)

	os.MkdirAll(c.dataDir, 0755)
	os.WriteFile(idFile, []byte(c.laggerID), 0600)

	return c.laggerID
}

// Register announces this node to the registry and starts the heartbeat loop.
// Must have WgPublicKey, Endpoint, and Region set before calling.
// Safe to call multiple times — idempotent.
func (c *Client) Register(ctx context.Context) error {
	c.mu.Lock()
	if c.WgPublicKey == "" || c.Endpoint == "" || c.Region == "" {
		c.mu.Unlock()
		return fmt.Errorf("discovery: WgPublicKey, Endpoint and Region must be set before Register()")
	}
	if c.isLagger {
		c.mu.Unlock()
		return nil // already registered
	}
	c.mu.Unlock()

	if err := c.sendRegister(); err != nil {
		return err
	}

	c.mu.Lock()
	c.isLagger = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	go c.heartbeatLoop()
	return nil
}

// Leave gracefully removes this node from the registry and stops the heartbeat.
func (c *Client) Leave() {
	c.mu.Lock()
	if !c.isLagger {
		c.mu.Unlock()
		return
	}
	c.isLagger = false
	close(c.stopCh)
	c.mu.Unlock()

	// Best-effort DELETE — ignore errors (we're shutting down)
	body, _ := json.Marshal(map[string]string{"id": c.LaggerID()})
	req, err := http.NewRequest(http.MethodDelete, c.workerURL+"/leave", bytes.NewReader(body))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}
}

// WorkerURL returns the registry URL being used.
func (c *Client) WorkerURL() string { return c.workerURL }

// IsLagger returns true if this node is currently registered and active.
func (c *Client) IsLagger() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isLagger
}

// GetLaggers returns active Laggers for the given region.
// region can be "" to get all regions.
func (c *Client) GetLaggers(region string) ([]LaggerInfo, error) {
	url := c.workerURL + "/laggers"
	if region != "" {
		url += "?region=" + region
	}

	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("discovery: get laggers: %w", err)
	}
	defer resp.Body.Close()

	var list LaggerList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("discovery: decode laggers: %w", err)
	}

	// Exclude ourselves from the list (we can't relay through ourselves)
	selfID := c.LaggerID()
	result := make([]LaggerInfo, 0, len(list.Laggers))
	for _, l := range list.Laggers {
		if l.ID != selfID {
			result = append(result, l)
		}
	}
	return result, nil
}

// GetCount returns the total number of active Laggers worldwide.
// Used for the UI counter badge ("⚡ 247 Laggers online").
func (c *Client) GetCount() (int, error) {
	resp, err := c.http.Get(c.workerURL + "/count")
	if err != nil {
		return 0, fmt.Errorf("discovery: get count: %w", err)
	}
	defer resp.Body.Close()

	var cr CountResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return 0, fmt.Errorf("discovery: decode count: %w", err)
	}
	return cr.Count, nil
}

// ── Heartbeat loop ────────────────────────────────────────────────────────────

func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			// Re-register = refresh the KV TTL on the server side
			if err := c.sendRegister(); err != nil {
				// Log but don't crash — network might be temporarily unavailable.
				// The server TTL (5min) gives us buffer before we're removed.
				_ = err
			}
		}
	}
}

func (c *Client) sendRegister() error {
	c.mu.Lock()
	payload := map[string]interface{}{
		"id":          c.LaggerID(),
		"region":      c.Region,
		"wgPublicKey": c.WgPublicKey,
		"endpoint":    c.Endpoint,
	}
	if c.RelayAPI != "" {
		payload["relayApi"] = c.RelayAPI
	}
	if c.City != "" {
		payload["city"] = c.City
	}
	if c.Country != "" {
		payload["country"] = c.Country
	}
	if c.Version != "" {
		payload["version"] = c.Version
	}
	c.mu.Unlock()

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("discovery: marshal register: %w", err)
	}

	resp, err := c.http.Post(c.workerURL+"/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discovery: register: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discovery: register returned HTTP %d", resp.StatusCode)
	}
	return nil
}
