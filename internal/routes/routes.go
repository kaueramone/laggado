// Package routes maintains a database of regional routing test points.
// These are public anycast/CDN nodes used to measure path quality to different regions.
// Free equivalent of ExitLag's "route sync" — no proprietary relay needed for testing.
// If the user has their own VPS, it's added via config and used for actual tunneling.
package routes

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"laggado/internal/routetest"
)

// Region constants
const (
	RegionSA   = "SA"  // South America
	RegionUS   = "US"  // North America
	RegionEU   = "EU"  // Europe
	RegionASIA = "ASIA"
)

// RouteNode is a single testable endpoint in the route database.
type RouteNode struct {
	Name    string `json:"name"`
	Region  string `json:"region"`
	Country string `json:"country"`
	City    string `json:"city"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
	Type    string `json:"type"` // "anycast", "cdn", "vps", "user"
}

// ProbeResult holds measured quality for a route node.
type ProbeResult struct {
	Node       RouteNode     `json:"node"`
	Latency    time.Duration `json:"latencyMs"`
	Jitter     time.Duration `json:"jitterMs"`
	PacketLoss float64       `json:"packetLossPct"`
	Score      float64       `json:"score"`
	TestedAt   time.Time     `json:"testedAt"`
	Reachable  bool          `json:"reachable"`
}

func (r ProbeResult) LatencyMS() float64 {
	return float64(r.Latency.Microseconds()) / 1000.0
}
func (r ProbeResult) JitterMS() float64 {
	return float64(r.Jitter.Microseconds()) / 1000.0
}

// BuiltinNodes is the free route atlas — public anycast IPs by region.
// These are used to measure path quality (not for tunneling).
// For actual tunneling, the user adds their own VPS IPs via config.
var BuiltinNodes = []RouteNode{
	// ── South America ─────────────────────────────────────────────
	{Name: "SA-BR-Cloudflare", Region: RegionSA, Country: "BR", City: "São Paulo", IP: "104.18.0.1", Port: 80, Type: "anycast"},
	{Name: "SA-BR-Google", Region: RegionSA, Country: "BR", City: "São Paulo", IP: "142.251.129.46", Port: 80, Type: "anycast"},
	{Name: "SA-BR-Level3", Region: RegionSA, Country: "BR", City: "São Paulo", IP: "4.68.0.1", Port: 80, Type: "anycast"},
	{Name: "SA-AR-Cloudflare", Region: RegionSA, Country: "AR", City: "Buenos Aires", IP: "104.18.1.1", Port: 80, Type: "anycast"},
	{Name: "SA-CL-Cloudflare", Region: RegionSA, Country: "CL", City: "Santiago", IP: "104.18.2.1", Port: 80, Type: "anycast"},
	{Name: "SA-CO-Cloudflare", Region: RegionSA, Country: "CO", City: "Bogotá", IP: "104.18.3.1", Port: 80, Type: "anycast"},

	// ── North America ─────────────────────────────────────────────
	{Name: "US-East-Cloudflare", Region: RegionUS, Country: "US", City: "Ashburn VA", IP: "1.1.1.1", Port: 80, Type: "anycast"},
	{Name: "US-East-Google", Region: RegionUS, Country: "US", City: "Ashburn VA", IP: "8.8.8.8", Port: 80, Type: "anycast"},
	{Name: "US-East-Level3", Region: RegionUS, Country: "US", City: "New York", IP: "4.2.2.1", Port: 80, Type: "anycast"},
	{Name: "US-East-Valve", Region: RegionUS, Country: "US", City: "Virginia", IP: "208.78.164.1", Port: 27015, Type: "anycast"},
	{Name: "US-Central-Cloudflare", Region: RegionUS, Country: "US", City: "Dallas TX", IP: "1.1.1.2", Port: 80, Type: "anycast"},
	{Name: "US-West-Cloudflare", Region: RegionUS, Country: "US", City: "Los Angeles CA", IP: "1.1.1.3", Port: 80, Type: "anycast"},
	{Name: "US-West-Google", Region: RegionUS, Country: "US", City: "Los Angeles", IP: "8.8.4.4", Port: 80, Type: "anycast"},
	{Name: "CA-East-Cloudflare", Region: RegionUS, Country: "CA", City: "Toronto", IP: "104.19.0.1", Port: 80, Type: "anycast"},

	// ── Europe ────────────────────────────────────────────────────
	{Name: "EU-DE-Cloudflare", Region: RegionEU, Country: "DE", City: "Frankfurt", IP: "104.20.0.1", Port: 80, Type: "anycast"},
	{Name: "EU-DE-Google", Region: RegionEU, Country: "DE", City: "Frankfurt", IP: "142.250.185.206", Port: 80, Type: "anycast"},
	{Name: "EU-NL-Cloudflare", Region: RegionEU, Country: "NL", City: "Amsterdam", IP: "104.21.0.1", Port: 80, Type: "anycast"},
	{Name: "EU-FR-Cloudflare", Region: RegionEU, Country: "FR", City: "Paris", IP: "104.22.0.1", Port: 80, Type: "anycast"},
	{Name: "EU-GB-Cloudflare", Region: RegionEU, Country: "GB", City: "London", IP: "104.23.0.1", Port: 80, Type: "anycast"},
	{Name: "EU-SE-Cloudflare", Region: RegionEU, Country: "SE", City: "Stockholm", IP: "104.24.0.1", Port: 80, Type: "anycast"},
	{Name: "EU-PT-Cloudflare", Region: RegionEU, Country: "PT", City: "Lisbon", IP: "104.25.0.1", Port: 80, Type: "anycast"},
	{Name: "EU-ES-Cloudflare", Region: RegionEU, Country: "ES", City: "Madrid", IP: "104.26.0.1", Port: 80, Type: "anycast"},
	{Name: "EU-ES-Valve", Region: RegionEU, Country: "ES", City: "Madrid", IP: "155.133.246.1", Port: 27015, Type: "anycast"},

	// ── Asia ──────────────────────────────────────────────────────
	{Name: "ASIA-JP-Cloudflare", Region: RegionASIA, Country: "JP", City: "Tokyo", IP: "104.27.0.1", Port: 80, Type: "anycast"},
	{Name: "ASIA-SG-Cloudflare", Region: RegionASIA, Country: "SG", City: "Singapore", IP: "104.28.0.1", Port: 80, Type: "anycast"},
	{Name: "ASIA-KR-Cloudflare", Region: RegionASIA, Country: "KR", City: "Seoul", IP: "104.29.0.1", Port: 80, Type: "anycast"},
	{Name: "ASIA-HK-Cloudflare", Region: RegionASIA, Country: "HK", City: "Hong Kong", IP: "104.30.0.1", Port: 80, Type: "anycast"},
	{Name: "ASIA-AU-Cloudflare", Region: RegionASIA, Country: "AU", City: "Sydney", IP: "104.31.0.1", Port: 80, Type: "anycast"},
}

// RegionEmoji maps region codes to flag/emoji for display.
var RegionEmoji = map[string]string{
	RegionSA:   "🌎",
	RegionUS:   "🌎",
	RegionEU:   "🌍",
	RegionASIA: "🌏",
}

// Database manages route nodes and their measured results.
type Database struct {
	mu      sync.RWMutex
	nodes   []RouteNode
	results map[string]*ProbeResult // keyed by node Name
	dataDir string
}

// NewDatabase creates a route database, merging builtin nodes with user-configured ones.
func NewDatabase(dataDir string, userNodes []routetest.VPSEndpoint) *Database {
	db := &Database{
		dataDir: dataDir,
		results: make(map[string]*ProbeResult),
	}

	// Start with builtin nodes
	db.nodes = append(db.nodes, BuiltinNodes...)

	// Add user VPS nodes
	for _, vps := range userNodes {
		port := vps.Port
		if port == 0 {
			port = 22
		}
		db.nodes = append(db.nodes, RouteNode{
			Name:    vps.Name,
			Region:  guessRegion(vps.Name),
			Country: guessCountry(vps.Name),
			City:    vps.Name,
			IP:      vps.Address,
			Port:    port,
			Type:    "user",
		})
	}

	db.loadResults()
	return db
}

// TotalNodes returns count of all known route nodes.
func (db *Database) TotalNodes() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.nodes)
}

// ProbeAll measures latency to all nodes with bounded concurrency (max 3 at a time).
// Uses a single fast TCP connect per node (no repeated probes) to minimise
// network interference with games loading in the background.
func (db *Database) ProbeAll(progressFn func(done, total int, name string)) {
	const maxConcurrent = 3

	nodes := db.Nodes()
	total := len(nodes)
	tester := routetest.NewTester()
	tester.Count = 1   // single probe — just establish reachability
	tester.Timeout = 1500 * time.Millisecond

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0

	for _, node := range nodes {
		node := node
		wg.Add(1)
		sem <- struct{}{} // acquire slot
		go func() {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			result := db.probeNode(tester, node)

			db.mu.Lock()
			db.results[node.Name] = result
			db.mu.Unlock()

			mu.Lock()
			done++
			d := done
			mu.Unlock()

			if progressFn != nil {
				progressFn(d, total, node.Name)
			}
		}()
	}
	wg.Wait()
	db.saveResults()
}

// ProbeRegion probes only nodes in the given region.
func (db *Database) ProbeRegion(region string, progressFn func(done, total int, name string)) []*ProbeResult {
	var regionNodes []RouteNode
	for _, n := range db.nodes {
		if n.Region == region {
			regionNodes = append(regionNodes, n)
		}
	}

	tester := routetest.NewTester()
	tester.Count = 8
	tester.Timeout = 3 * time.Second

	var mu sync.Mutex
	var wg sync.WaitGroup
	var results []*ProbeResult
	done := 0
	total := len(regionNodes)

	for _, node := range regionNodes {
		node := node
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := db.probeNode(tester, node)

			mu.Lock()
			db.results[node.Name] = result
			results = append(results, result)
			done++
			d := done
			mu.Unlock()

			if progressFn != nil {
				progressFn(d, total, node.Name)
			}
		}()
	}
	wg.Wait()

	// Sort by score
	sort.Slice(results, func(i, j int) bool {
		if !results[i].Reachable {
			return false
		}
		if !results[j].Reachable {
			return true
		}
		return results[i].Score < results[j].Score
	})

	db.saveResults()
	return results
}

// BestForRegion returns the best cached result for a region.
func (db *Database) BestForRegion(region string) *ProbeResult {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var best *ProbeResult
	for name, r := range db.results {
		if !r.Reachable {
			continue
		}
		// Find node for this result
		for _, n := range db.nodes {
			if n.Name == name && n.Region == region {
				if best == nil || r.Score < best.Score {
					best = r
				}
			}
		}
	}
	return best
}

// GetResult returns the cached probe result for a node.
func (db *Database) GetResult(nodeName string) *ProbeResult {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.results[nodeName]
}

// Nodes returns all nodes (copy).
func (db *Database) Nodes() []RouteNode {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return append([]RouteNode{}, db.nodes...)
}

// NodesForRegion returns nodes filtered by region.
func (db *Database) NodesForRegion(region string) []RouteNode {
	db.mu.RLock()
	defer db.mu.RUnlock()
	var result []RouteNode
	for _, n := range db.nodes {
		if n.Region == region {
			result = append(result, n)
		}
	}
	return result
}

// ProbeNodePublic is the exported version for use by the GUI package.
func (db *Database) ProbeNodePublic(tester *routetest.Tester, node RouteNode) *ProbeResult {
	return db.probeNode(tester, node)
}

func (db *Database) probeNode(tester *routetest.Tester, node RouteNode) *ProbeResult {
	addr := fmt.Sprintf("%s:%d", node.IP, node.Port)
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, tester.Timeout)
	elapsed := time.Since(start)

	if err != nil {
		// Connection refused = host is up, RTT is valid
		if isConnRefused(err) {
			ms := float64(elapsed.Milliseconds())
			return &ProbeResult{
				Node:      node,
				Latency:   elapsed,
				Reachable: true,
				Score:     ms,
				TestedAt:  time.Now(),
			}
		}
		return &ProbeResult{
			Node:      node,
			Reachable: false,
			Score:     999999,
			TestedAt:  time.Now(),
		}
	}
	conn.Close()

	// tester.Count == 1 means quick-scan mode (used at startup splash).
	// Skip MeasureDirect to avoid flooding the network with probe traffic.
	if tester.Count <= 1 {
		ms := float64(elapsed.Milliseconds())
		return &ProbeResult{
			Node:      node,
			Latency:   elapsed,
			Reachable: true,
			Score:     ms,
			TestedAt:  time.Now(),
		}
	}

	// Full measurement: multiple probes for jitter calculation.
	m, _ := tester.MeasureDirect(node.IP)
	if m == nil {
		ms := float64(elapsed.Milliseconds())
		return &ProbeResult{
			Node:      node,
			Latency:   elapsed,
			Reachable: true,
			Score:     ms,
			TestedAt:  time.Now(),
		}
	}

	score := float64(m.AvgLatency.Milliseconds()) +
		1.5*float64(m.Jitter.Milliseconds()) +
		25.0*m.PacketLoss

	return &ProbeResult{
		Node:       node,
		Latency:    m.AvgLatency,
		Jitter:     m.Jitter,
		PacketLoss: m.PacketLoss,
		Score:      score,
		Reachable:  true,
		TestedAt:   time.Now(),
	}
}

func (db *Database) saveResults() {
	db.mu.RLock()
	data, _ := json.MarshalIndent(db.results, "", "  ")
	db.mu.RUnlock()
	os.MkdirAll(db.dataDir, 0755)
	os.WriteFile(filepath.Join(db.dataDir, "route_results.json"), data, 0644)
}

func (db *Database) loadResults() {
	data, err := os.ReadFile(filepath.Join(db.dataDir, "route_results.json"))
	if err != nil {
		return
	}
	db.mu.Lock()
	json.Unmarshal(data, &db.results)
	db.mu.Unlock()
}

func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return contains(s, "refused") || contains(s, "actively refused")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func guessRegion(name string) string {
	n := toLower(name)
	if containsStr(n, "br") || containsStr(n, "sa") || containsStr(n, "sao") ||
		containsStr(n, "brazil") || containsStr(n, "arg") || containsStr(n, "chile") {
		return RegionSA
	}
	if containsStr(n, "us") || containsStr(n, "na") || containsStr(n, "american") ||
		containsStr(n, "dallas") || containsStr(n, "virginia") || containsStr(n, "chicago") {
		return RegionUS
	}
	if containsStr(n, "eu") || containsStr(n, "frankfurt") || containsStr(n, "amsterdam") ||
		containsStr(n, "london") || containsStr(n, "paris") || containsStr(n, "berlin") {
		return RegionEU
	}
	if containsStr(n, "asia") || containsStr(n, "tokyo") || containsStr(n, "singapore") ||
		containsStr(n, "seoul") || containsStr(n, "sydney") {
		return RegionASIA
	}
	return RegionUS
}

func guessCountry(name string) string {
	n := toLower(name)
	if containsStr(n, "br") || containsStr(n, "brazil") {
		return "BR"
	}
	if containsStr(n, "de") || containsStr(n, "frankfurt") || containsStr(n, "berlin") {
		return "DE"
	}
	if containsStr(n, "nl") || containsStr(n, "amsterdam") {
		return "NL"
	}
	if containsStr(n, "jp") || containsStr(n, "tokyo") {
		return "JP"
	}
	return "US"
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
