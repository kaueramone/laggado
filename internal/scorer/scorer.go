// Package scorer implements route scoring and selection logic.
// score = latency + (jitter_weight * jitter) + (loss_weight * packet_loss)
// Lower score = better route.
package scorer

import (
	"fmt"
	"sort"
	"time"

	"laggado/internal/routetest"
)

// Weights for the scoring formula. Tuned for gaming where latency is king
// but packet loss is catastrophic.
type Weights struct {
	LatencyWeight float64 `json:"latencyWeight"` // multiplier for avg latency (ms)
	JitterWeight  float64 `json:"jitterWeight"`  // multiplier for jitter (ms)
	LossWeight    float64 `json:"lossWeight"`    // multiplier for packet loss (%)
}

// DefaultWeights returns sensible defaults for gaming traffic.
func DefaultWeights() Weights {
	return Weights{
		LatencyWeight: 1.0,
		JitterWeight:  1.5,
		LossWeight:    25.0, // 1% loss = +25 to score; loss is very bad for gaming
	}
}

// ScoredRoute pairs a route measurement with its computed score.
type ScoredRoute struct {
	Metrics *routetest.RouteMetrics
	Score   float64
	Rank    int
}

func (s ScoredRoute) String() string {
	return fmt.Sprintf("#%d [score=%.1f] %s", s.Rank, s.Score, s.Metrics)
}

// ScoreRoutes computes a score for each measured route and returns them sorted (best first).
func ScoreRoutes(metrics []*routetest.RouteMetrics, w Weights) []ScoredRoute {
	scored := make([]ScoredRoute, len(metrics))

	for i, m := range metrics {
		latencyMs := float64(m.AvgLatency.Microseconds()) / 1000.0
		jitterMs := float64(m.Jitter.Microseconds()) / 1000.0
		lossPct := m.PacketLoss

		score := (w.LatencyWeight * latencyMs) +
			(w.JitterWeight * jitterMs) +
			(w.LossWeight * lossPct)

		// Penalize routes with 100% loss heavily
		if lossPct >= 100.0 {
			score = 999999.0
		}

		scored[i] = ScoredRoute{
			Metrics: m,
			Score:   score,
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score < scored[j].Score
	})

	for i := range scored {
		scored[i].Rank = i + 1
	}

	return scored
}

// BestRoute returns the top-scored route, or nil if no routes.
func BestRoute(metrics []*routetest.RouteMetrics, w Weights) *ScoredRoute {
	scored := ScoreRoutes(metrics, w)
	if len(scored) == 0 {
		return nil
	}
	return &scored[0]
}

// ShouldSwitch determines if switching from current to candidate route is worthwhile.
// Requires a minimum improvement threshold to avoid route flapping.
func ShouldSwitch(current, candidate *ScoredRoute, minImprovement float64) bool {
	if current == nil {
		return true
	}
	if candidate == nil {
		return false
	}
	improvement := current.Score - candidate.Score
	return improvement > minImprovement
}

// RouteHistory tracks historical route scores for learning.
type RouteHistory struct {
	ServerIP string            `json:"serverIP"`
	Scores   map[string][]RouteSnapshot `json:"scores"` // via -> history
}

// RouteSnapshot is a point-in-time route measurement.
type RouteSnapshot struct {
	Score      float64   `json:"score"`
	AvgLatency float64   `json:"avgLatencyMs"`
	Jitter     float64   `json:"jitterMs"`
	Loss       float64   `json:"lossPct"`
	Timestamp  time.Time `json:"timestamp"`
}

// RecordScore adds a measurement to the history, keeping at most maxEntries.
func (h *RouteHistory) RecordScore(via string, s ScoredRoute, maxEntries int) {
	if h.Scores == nil {
		h.Scores = make(map[string][]RouteSnapshot)
	}

	snapshot := RouteSnapshot{
		Score:      s.Score,
		AvgLatency: float64(s.Metrics.AvgLatency.Microseconds()) / 1000.0,
		Jitter:     float64(s.Metrics.Jitter.Microseconds()) / 1000.0,
		Loss:       s.Metrics.PacketLoss,
		Timestamp:  time.Now(),
	}

	history := h.Scores[via]
	history = append(history, snapshot)
	if len(history) > maxEntries {
		history = history[len(history)-maxEntries:]
	}
	h.Scores[via] = history
}

// BestHistorical returns the via name with the lowest average historical score.
func (h *RouteHistory) BestHistorical() string {
	bestVia := ""
	bestAvg := float64(999999)

	for via, snapshots := range h.Scores {
		if len(snapshots) == 0 {
			continue
		}
		var sum float64
		for _, s := range snapshots {
			sum += s.Score
		}
		avg := sum / float64(len(snapshots))
		if avg < bestAvg {
			bestAvg = avg
			bestVia = via
		}
	}

	return bestVia
}
