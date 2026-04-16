// Package relay implements the LAGGADO community relay network.
//
// Architecture:
//   - relay.json on GitHub lists available community relay nodes
//   - Clients fetch the list, probe latency, and auto-join the best relay
//   - Relay operators run "laggado relay serve" on any machine/VPS they own
//   - Dynamic peer registration: no pre-configuration needed per client
//
// Protocol:
//   GET  /info  → relay metadata + server WireGuard public key
//   POST /join  → client registers its WireGuard public key, gets assigned IP
//   DEL  /leave → client deregisters (optional cleanup)
//   GET  /peers → active peer count (public, for monitoring)
package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultRelayListURL is the community relay list hosted on GitHub.
// Community members submit PRs to add their relay nodes.
const DefaultRelayListURL = "https://raw.githubusercontent.com/laggado/laggado/main/relay.json"

const cacheTTL = 1 * time.Hour

// RelayInfo describes a single community relay node in relay.json.
type RelayInfo struct {
	Name     string   `json:"name"`     // e.g., "BR-SP-1"
	Region   string   `json:"region"`   // "SA", "EU", "US", "ASIA", "RU"
	Country  string   `json:"country"`  // ISO 2-letter code
	City     string   `json:"city"`
	APIURL   string   `json:"api"`      // e.g., "http://1.2.3.4:7734"
	WGPort   int      `json:"wgPort"`   // WireGuard port (default 51820)
	Games    []string `json:"games"`    // supported games; empty = all games
	Operator string   `json:"operator"` // community handle
	Note     string   `json:"note"`     // e.g., "Oracle Free Tier"
}

// RelayList is the top-level relay.json structure.
type RelayList struct {
	Version int         `json:"version"`
	Updated string      `json:"updated"`
	Relays  []RelayInfo `json:"relays"`
}

// ProbeResult holds measured latency for one relay.
type ProbeResult struct {
	Relay     RelayInfo
	Latency   time.Duration
	Reachable bool
	Error     string
}

func (p ProbeResult) LatencyMS() float64 {
	return float64(p.Latency.Microseconds()) / 1000.0
}

// JoinRequest is sent by the client to register its WireGuard public key.
type JoinRequest struct {
	ClientPublicKey string `json:"clientPublicKey"`
}

// JoinResponse contains the WireGuard config needed for the client to connect.
type JoinResponse struct {
	ClientIP        string `json:"clientIP"`        // assigned tunnel IP, e.g., "10.100.1.50"
	ClientIPCIDR    string `json:"clientIPCIDR"`    // e.g., "10.100.1.50/32"
	ServerPublicKey string `json:"serverPublicKey"` // relay's WireGuard public key
	ServerEndpoint  string `json:"serverEndpoint"`  // relay_ip:wgPort
	GatewayIP       string `json:"gatewayIP"`       // relay's tunnel gateway, e.g., "10.100.0.1"
}

// ─── CLIENT ──────────────────────────────────────────────────────────────────

// Client fetches, caches and interacts with the community relay list.
type Client struct {
	ListURL  string
	CacheDir string
	http     *http.Client
}

// NewClient creates a relay client.
func NewClient(listURL, cacheDir string) *Client {
	if listURL == "" {
		listURL = DefaultRelayListURL
	}
	return &Client{
		ListURL:  listURL,
		CacheDir: cacheDir,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchRelays returns the community relay list, using a local cache when fresh.
// Falls back to stale cache if the network is unreachable.
func (c *Client) FetchRelays() (*RelayList, error) {
	cachePath := filepath.Join(c.CacheDir, "relay_cache.json")

	if info, err := os.Stat(cachePath); err == nil {
		if time.Since(info.ModTime()) < cacheTTL {
			if rl := c.loadCache(cachePath); rl != nil {
				return rl, nil
			}
		}
	}

	resp, err := c.http.Get(c.ListURL)
	if err != nil {
		if rl := c.loadCache(cachePath); rl != nil {
			return rl, nil // stale cache as fallback
		}
		return nil, fmt.Errorf("fetch relay list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Non-200: fall back to stale cache or return empty list
		if rl := c.loadCache(cachePath); rl != nil {
			return rl, nil
		}
		return &RelayList{Version: 1, Relays: []RelayInfo{}}, nil
	}

	var rl RelayList
	if err := json.NewDecoder(resp.Body).Decode(&rl); err != nil {
		if rl := c.loadCache(cachePath); rl != nil {
			return rl, nil
		}
		return nil, fmt.Errorf("parse relay list: %w", err)
	}

	c.saveCache(cachePath, &rl)
	return &rl, nil
}

// ProbeAll measures latency to all relay API endpoints concurrently.
// Results are sorted: reachable first, then by ascending latency.
func (c *Client) ProbeAll(rl *RelayList) []ProbeResult {
	results := make([]ProbeResult, len(rl.Relays))
	var wg sync.WaitGroup
	for i, relay := range rl.Relays {
		i, relay := i, relay
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = c.probeRelay(relay)
		}()
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		if results[i].Reachable != results[j].Reachable {
			return results[i].Reachable
		}
		return results[i].Latency < results[j].Latency
	})
	return results
}

// Join registers the client's WireGuard public key with a relay and returns
// the WireGuard configuration needed to set up the split tunnel.
func (c *Client) Join(relay RelayInfo, clientPublicKey string) (*JoinResponse, error) {
	url := strings.TrimRight(relay.APIURL, "/") + "/join"
	body, _ := json.Marshal(JoinRequest{ClientPublicKey: clientPublicKey})

	resp, err := c.http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("join relay %s: %w", relay.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("join relay %s: HTTP %d", relay.Name, resp.StatusCode)
	}

	var jr JoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
		return nil, fmt.Errorf("parse join response: %w", err)
	}
	return &jr, nil
}

// Leave unregisters the client from a relay (best-effort cleanup).
func (c *Client) Leave(relay RelayInfo, clientPublicKey string) {
	url := strings.TrimRight(relay.APIURL, "/") + "/leave"
	body, _ := json.Marshal(JoinRequest{ClientPublicKey: clientPublicKey})
	req, err := http.NewRequest(http.MethodDelete, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func (c *Client) probeRelay(relay RelayInfo) ProbeResult {
	host := extractHost(relay.APIURL)
	if host == "" {
		return ProbeResult{Relay: relay, Error: "invalid API URL"}
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", host, 3*time.Second)
	latency := time.Since(start)

	if err != nil {
		if isConnRefused(err) {
			// Host is up but port closed — still a valid RTT
			return ProbeResult{Relay: relay, Latency: latency, Reachable: true}
		}
		return ProbeResult{Relay: relay, Error: err.Error()}
	}
	conn.Close()
	return ProbeResult{Relay: relay, Latency: latency, Reachable: true}
}

func (c *Client) loadCache(path string) *RelayList {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var rl RelayList
	if json.Unmarshal(data, &rl) != nil {
		return nil
	}
	return &rl
}

func (c *Client) saveCache(path string, rl *RelayList) {
	os.MkdirAll(filepath.Dir(path), 0755)
	if data, err := json.MarshalIndent(rl, "", "  "); err == nil {
		os.WriteFile(path, data, 0644)
	}
}

func extractHost(apiURL string) string {
	s := strings.TrimPrefix(apiURL, "https://")
	s = strings.TrimPrefix(s, "http://")
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "refused") || strings.Contains(msg, "actively refused")
}
