// Package routetest measures network quality to a target IP:
// latency (ICMP ping with TCP fallback), jitter, and packet loss.
package routetest

import (
	"fmt"
	"math"
	"net"
	"sort"
	"sync"
	"time"
)

// RouteMetrics holds the measured quality metrics for a route.
type RouteMetrics struct {
	Target      string        `json:"target"`
	Via         string        `json:"via"` // "direct" or VPS endpoint name
	AvgLatency  time.Duration `json:"avgLatencyMs"`
	MinLatency  time.Duration `json:"minLatencyMs"`
	MaxLatency  time.Duration `json:"maxLatencyMs"`
	Jitter      time.Duration `json:"jitterMs"`
	PacketLoss  float64       `json:"packetLossPct"` // 0.0–100.0
	Samples     int           `json:"samples"`
	MeasuredAt  time.Time     `json:"measuredAt"`
}

func (m RouteMetrics) String() string {
	return fmt.Sprintf("%-20s avg=%5.1fms min=%5.1fms max=%5.1fms jitter=%5.1fms loss=%.1f%% (%d samples)",
		m.Via,
		float64(m.AvgLatency.Microseconds())/1000.0,
		float64(m.MinLatency.Microseconds())/1000.0,
		float64(m.MaxLatency.Microseconds())/1000.0,
		float64(m.Jitter.Microseconds())/1000.0,
		m.PacketLoss,
		m.Samples,
	)
}

// VPSEndpoint represents an optional intermediate routing point.
type VPSEndpoint struct {
	Name           string  `json:"name"`                     // e.g., "EU-Frankfurt", "BR-SaoPaulo"
	Address        string  `json:"address"`                  // IP or hostname
	Port           int     `json:"port"`                     // TCP port for probing
	ExtraLatencyMs float64 `json:"extraLatencyMs,omitempty"` // known VPS→target latency (ms) to add for full-path scoring
}

// Tester performs route quality measurements.
type Tester struct {
	Timeout    time.Duration
	Count      int // number of ping probes per test
	Interval   time.Duration // delay between probes
}

// NewTester creates a route tester with sensible defaults.
func NewTester() *Tester {
	return &Tester{
		Timeout:  2 * time.Second,
		Count:    10,
		Interval: 200 * time.Millisecond,
	}
}

// MeasureDirect tests the direct route from this machine to the target IP.
func (t *Tester) MeasureDirect(targetIP string) (*RouteMetrics, error) {
	latencies, lost := t.tcpPingBatch(targetIP, 443, t.Count)
	if len(latencies) == 0 {
		// Fallback: try common game ports
		for _, port := range []int{27015, 80, 7777} {
			latencies, lost = t.tcpPingBatch(targetIP, port, t.Count)
			if len(latencies) > 0 {
				break
			}
		}
	}

	if len(latencies) == 0 {
		// Last resort: ICMP-style via raw connect to port 0 (will get RST but we measure RTT)
		latencies, lost = t.tcpPingBatch(targetIP, 80, t.Count)
	}

	return t.computeMetrics(targetIP, "direct", latencies, lost), nil
}

// MeasureViaVPS tests the route through an intermediate VPS.
// Measures Client→VPS latency. If vps.ExtraLatencyMs > 0, that value is added to
// represent the known VPS→Target latency, giving a full-path estimate for scoring.
// Without ExtraLatencyMs, the metric underestimates total latency — configure it via
// "laggado config add-vps <name> <addr> [port] --extra-ms <N>".
func (t *Tester) MeasureViaVPS(targetIP string, vps VPSEndpoint) (*RouteMetrics, error) {
	port := vps.Port
	if port == 0 {
		port = 22 // Default to SSH port for VPS probing
	}

	latencies, lost := t.tcpPingBatch(vps.Address, port, t.Count)
	m := t.computeMetrics(targetIP, vps.Name, latencies, lost)

	// Add the configured VPS→target latency offset so the score reflects the full path.
	if vps.ExtraLatencyMs > 0 && m.PacketLoss < 100 {
		extra := time.Duration(int64(vps.ExtraLatencyMs*1000)) * time.Microsecond
		m.AvgLatency += extra
		m.MinLatency += extra
		m.MaxLatency += extra
		// Jitter is unchanged — it reflects Client→VPS variation only.
	}

	return m, nil
}

// MeasureAll tests direct route and all provided VPS endpoints concurrently.
func (t *Tester) MeasureAll(targetIP string, vpsList []VPSEndpoint) []*RouteMetrics {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []*RouteMetrics

	// Direct route
	wg.Add(1)
	go func() {
		defer wg.Done()
		m, err := t.MeasureDirect(targetIP)
		if err == nil {
			mu.Lock()
			results = append(results, m)
			mu.Unlock()
		}
	}()

	// VPS routes
	for _, vps := range vpsList {
		vps := vps
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := t.MeasureViaVPS(targetIP, vps)
			if err == nil {
				mu.Lock()
				results = append(results, m)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return results
}

// tcpPingBatch performs multiple TCP connect probes and returns successful latencies and lost count.
func (t *Tester) tcpPingBatch(host string, port int, count int) ([]time.Duration, int) {
	var latencies []time.Duration
	lost := 0

	addr := fmt.Sprintf("%s:%d", host, port)
	for i := 0; i < count; i++ {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, t.Timeout)
		elapsed := time.Since(start)

		if err != nil {
			// Connection refused still gives us RTT (the RST came back)
			if isConnectionRefused(err) {
				latencies = append(latencies, elapsed)
			} else {
				lost++
			}
		} else {
			latencies = append(latencies, elapsed)
			conn.Close()
		}

		if i < count-1 {
			time.Sleep(t.Interval)
		}
	}

	return latencies, lost
}

func (t *Tester) computeMetrics(target, via string, latencies []time.Duration, lost int) *RouteMetrics {
	total := len(latencies) + lost
	if total == 0 {
		total = 1
	}

	m := &RouteMetrics{
		Target:     target,
		Via:        via,
		Samples:    len(latencies) + lost,
		PacketLoss: float64(lost) / float64(total) * 100.0,
		MeasuredAt: time.Now(),
	}

	if len(latencies) == 0 {
		m.PacketLoss = 100.0
		return m
	}

	// Jitter = average absolute difference between consecutive samples.
	// MUST be calculated BEFORE sorting: jitter measures temporal variation,
	// and sorting destroys the time-order of probes.
	if len(latencies) > 1 {
		var jitterSum float64
		for i := 1; i < len(latencies); i++ {
			diff := math.Abs(float64(latencies[i].Microseconds() - latencies[i-1].Microseconds()))
			jitterSum += diff
		}
		jitterUs := jitterSum / float64(len(latencies)-1)
		m.Jitter = time.Duration(int64(jitterUs)) * time.Microsecond
	}

	// Sort for min/max/avg statistics only.
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	m.MinLatency = latencies[0]
	m.MaxLatency = latencies[len(latencies)-1]

	var sum int64
	for _, l := range latencies {
		sum += l.Microseconds()
	}
	avgUs := sum / int64(len(latencies))
	m.AvgLatency = time.Duration(avgUs) * time.Microsecond

	return m
}

// isConnectionRefused checks if the error is a "connection refused" —
// which still tells us the RTT since we got a RST back.
func isConnectionRefused(err error) bool {
	if opErr, ok := err.(*net.OpError); ok {
		return opErr.Err.Error() == "connectex: No connection could be made because the target machine actively refused it." ||
			opErr.Err.Error() == "connect: connection refused"
	}
	return false
}
