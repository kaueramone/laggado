// Package geoip resolves IP addresses to geographic locations.
// Uses ip-api.com (free, no API key, 45 req/min limit) with aggressive local caching.
package geoip

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// GeoResult holds geographic information for an IP address.
type GeoResult struct {
	IP          string  `json:"ip"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	Region      string  `json:"regionName"`
	City        string  `json:"city"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	ISP         string  `json:"isp"`
	Org         string  `json:"org"`
	AS          string  `json:"as"`
	CachedAt    int64   `json:"cachedAt"`
}

func (g GeoResult) String() string {
	return fmt.Sprintf("%s, %s, %s (%s) [%s]", g.City, g.Region, g.Country, g.CountryCode, g.ISP)
}

// ipAPIResponse matches the ip-api.com JSON response format.
type ipAPIResponse struct {
	Status      string  `json:"status"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	Region      string  `json:"region"`
	RegionName  string  `json:"regionName"`
	City        string  `json:"city"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	ISP         string  `json:"isp"`
	Org         string  `json:"org"`
	AS          string  `json:"as"`
	Query       string  `json:"query"`
	Message     string  `json:"message"`
}

// Resolver performs GeoIP lookups with caching.
type Resolver struct {
	mu        sync.RWMutex
	cache     map[string]*GeoResult
	cacheFile string
	cacheTTL  time.Duration
	client    *http.Client
}

// NewResolver creates a new GeoIP resolver with the given cache file path.
func NewResolver(cacheDir string) (*Resolver, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	r := &Resolver{
		cache:     make(map[string]*GeoResult),
		cacheFile: filepath.Join(cacheDir, "geoip_cache.json"),
		cacheTTL:  24 * time.Hour * 7, // 7-day cache
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}

	r.loadCache()
	return r, nil
}

// Lookup resolves an IP to its geographic location.
// Returns cached result if available and fresh, otherwise queries ip-api.com.
func (r *Resolver) Lookup(ip net.IP) (*GeoResult, error) {
	ipStr := ip.String()

	// Check cache first
	r.mu.RLock()
	if cached, ok := r.cache[ipStr]; ok {
		age := time.Since(time.Unix(cached.CachedAt, 0))
		if age < r.cacheTTL {
			r.mu.RUnlock()
			return cached, nil
		}
	}
	r.mu.RUnlock()

	// Query ip-api.com
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,message,country,countryCode,region,regionName,city,lat,lon,isp,org,as,query", ipStr)
	resp, err := r.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("geoip lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("geoip rate limited (45 req/min)")
	}

	var apiResp ipAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode geoip response: %w", err)
	}

	if apiResp.Status != "success" {
		return nil, fmt.Errorf("geoip lookup failed: %s", apiResp.Message)
	}

	result := &GeoResult{
		IP:          apiResp.Query,
		Country:     apiResp.Country,
		CountryCode: apiResp.CountryCode,
		Region:      apiResp.RegionName,
		City:        apiResp.City,
		Lat:         apiResp.Lat,
		Lon:         apiResp.Lon,
		ISP:         apiResp.ISP,
		Org:         apiResp.Org,
		AS:          apiResp.AS,
		CachedAt:    time.Now().Unix(),
	}

	// Store in cache
	r.mu.Lock()
	r.cache[ipStr] = result
	r.mu.Unlock()

	r.saveCache()
	return result, nil
}

// BulkLookup resolves multiple IPs, respecting rate limits.
func (r *Resolver) BulkLookup(ips []net.IP) map[string]*GeoResult {
	results := make(map[string]*GeoResult)
	for _, ip := range ips {
		result, err := r.Lookup(ip)
		if err != nil {
			continue
		}
		results[ip.String()] = result
		// Small delay to respect rate limits
		time.Sleep(100 * time.Millisecond)
	}
	return results
}

func (r *Resolver) loadCache() {
	data, err := os.ReadFile(r.cacheFile)
	if err != nil {
		return // No cache file yet, that's fine
	}

	var cache map[string]*GeoResult
	if err := json.Unmarshal(data, &cache); err != nil {
		return
	}

	r.mu.Lock()
	r.cache = cache
	r.mu.Unlock()
}

func (r *Resolver) saveCache() {
	r.mu.RLock()
	data, err := json.MarshalIndent(r.cache, "", "  ")
	r.mu.RUnlock()

	if err != nil {
		return
	}
	os.WriteFile(r.cacheFile, data, 0644)
}

// CacheSize returns the number of cached GeoIP entries.
func (r *Resolver) CacheSize() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache)
}
