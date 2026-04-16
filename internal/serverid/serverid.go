// Package serverid identifies the most likely game server IP from a set of connections.
// It filters out CDNs, telemetry, update servers, and picks the primary game server
// based on connection characteristics.
package serverid

import (
	"net"
	"strings"

	"laggado/internal/connmon"
)

// cdnAndTelemetryASNPrefixes are IP prefixes commonly used by CDNs, telemetry, etc.
// We filter these out to avoid misidentifying a game server.
var knownNonGamePrefixes = []net.IPNet{
	// Akamai CDN
	parseCIDR("23.0.0.0/12"),
	parseCIDR("104.64.0.0/10"),
	parseCIDR("184.24.0.0/13"),
	// Cloudflare
	parseCIDR("104.16.0.0/12"),
	parseCIDR("172.64.0.0/13"),
	parseCIDR("1.1.1.0/24"),
	// Google / YouTube
	parseCIDR("142.250.0.0/15"),
	parseCIDR("172.217.0.0/16"),
	parseCIDR("216.58.0.0/16"),
	parseCIDR("34.0.0.0/10"),
	// Amazon CloudFront / AWS CDN (NOT AWS game servers)
	parseCIDR("13.32.0.0/15"),
	parseCIDR("13.224.0.0/14"),
	parseCIDR("99.84.0.0/16"),
	parseCIDR("143.204.0.0/16"),
	// Microsoft / Azure (telemetry, updates — not game servers)
	parseCIDR("40.64.0.0/10"),
	// Fastly CDN
	parseCIDR("151.101.0.0/16"),
	// Apple telemetry
	parseCIDR("17.0.0.0/8"),
	// EA CDN / Akamai (game downloads, not game servers)
	// Note: EA game SERVERS use separate IPs (not filtered here)
}

// knownTelemetryPorts are ports commonly used for telemetry/analytics, not game traffic.
var knownTelemetryPorts = map[uint16]bool{
	80:   true,
	443:  true,
	8443: true,
}

// gameServerPorts are ports commonly associated with game servers.
// Sourced from ExitLag reverse engineering + official documentation.
var gameServerPorts = map[uint16]bool{
	// ── Valve / Steam (CS2, Dota 2, TF2) ──
	27015: true, 27016: true, 27017: true, 27018: true, 27019: true, 27020: true,
	// Steam P2P / matchmaking relay
	4380: true, 27005: true, 27036: true,

	// ── EA / Battlefield series ──
	// Battlefield 1, V, 2042 use UDP in these ranges
	3659:  true, // EA ProtoSSL
	14000: true, 14001: true, 14002: true, 14003: true, 14004: true,
	14005: true, 14006: true, 14007: true, 14008: true, 14009: true,
	14010: true, 14011: true, 14012: true, 14013: true, 14014: true,
	14015: true, 14016: true,
	25000: true, 25001: true, 25002: true, 25003: true, // BF2042 alt ports
	// EA App / Origin TCP ports
	9960:  true, 9961: true, 9962: true, 9963: true, 9964: true,

	// ── Epic / Rocket League (Psyonix) ──
	7777: true, 7778: true, 7779: true, 7780: true, // Unreal Engine default
	7787: true,

	// ── Riot Games (Valorant, LoL) ──
	8088: true, 8393: true, 8394: true, 8395: true,
	2099: true, // LoL XMPP

	// ── Activision / Battle.net ──
	1119: true, // Battle.net auth
	3724: true, // WoW/Blizzard
	6113: true, // Battle.net chat
	1120: true, 1818: true,

	// ── Generic game/Unreal Engine ──
	9000: true, 9001: true, 9002: true,
	10000: true, 10001: true, 10002: true,
}

func parseCIDR(s string) net.IPNet {
	_, n, _ := net.ParseCIDR(s)
	return *n
}

// ServerCandidate represents a potential game server with scoring metadata.
type ServerCandidate struct {
	IP         net.IP
	Port       uint16
	Protocol   connmon.Protocol
	Score      float64 // higher = more likely to be the game server
	Reason     string
}

// IdentifyGameServer picks the most likely game server from a list of connections.
// It uses heuristics: UDP > TCP, known game ports > unknown, non-CDN > CDN.
func IdentifyGameServer(conns []connmon.Connection) *ServerCandidate {
	var candidates []ServerCandidate

	for _, c := range conns {
		if !c.IsPublicRemote() {
			continue
		}

		// Skip LISTEN and TIME_WAIT states
		if c.State == "LISTEN" || c.State == "TIME_WAIT" || c.State == "CLOSED" {
			continue
		}

		score := 0.0
		reasons := []string{}

		// UDP connections are more likely game traffic
		if c.Protocol == connmon.UDP {
			score += 30
			reasons = append(reasons, "UDP")
		}

		// TCP ESTABLISHED is relevant
		if c.Protocol == connmon.TCP && c.State == "ESTABLISHED" {
			score += 10
			reasons = append(reasons, "TCP-EST")
		}

		// Known game ports get a big boost
		if gameServerPorts[c.RemotePort] {
			score += 40
			reasons = append(reasons, "game-port")
		}

		// HTTPS/HTTP ports are usually telemetry or web APIs
		if knownTelemetryPorts[c.RemotePort] {
			score -= 20
			reasons = append(reasons, "web-port")
		}

		// CDN/telemetry IPs get penalized
		if isKnownNonGame(c.RemoteIP) {
			score -= 30
			reasons = append(reasons, "CDN/telemetry")
		}

		// High ports (>1024) on UDP are very likely game traffic
		if c.Protocol == connmon.UDP && c.RemotePort > 1024 && !knownTelemetryPorts[c.RemotePort] {
			score += 15
			reasons = append(reasons, "high-port-UDP")
		}

		if score > 0 {
			candidates = append(candidates, ServerCandidate{
				IP:       c.RemoteIP,
				Port:     c.RemotePort,
				Protocol: c.Protocol,
				Score:    score,
				Reason:   strings.Join(reasons, ", "),
			})
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Pick highest scoring candidate
	best := &candidates[0]
	for i := 1; i < len(candidates); i++ {
		if candidates[i].Score > best.Score {
			best = &candidates[i]
		}
	}
	return best
}

// IdentifyAllServers returns all viable game server candidates, sorted by score descending.
func IdentifyAllServers(conns []connmon.Connection) []ServerCandidate {
	var candidates []ServerCandidate

	seen := make(map[string]bool)
	for _, c := range conns {
		if !c.IsPublicRemote() {
			continue
		}
		if c.State == "LISTEN" || c.State == "TIME_WAIT" || c.State == "CLOSED" {
			continue
		}

		key := c.RemoteIP.String()
		if seen[key] {
			continue
		}
		seen[key] = true

		score := 0.0
		reasons := []string{}

		if c.Protocol == connmon.UDP {
			score += 30
			reasons = append(reasons, "UDP")
		}
		if c.Protocol == connmon.TCP && c.State == "ESTABLISHED" {
			score += 10
			reasons = append(reasons, "TCP-EST")
		}
		if gameServerPorts[c.RemotePort] {
			score += 40
			reasons = append(reasons, "game-port")
		}
		if knownTelemetryPorts[c.RemotePort] {
			score -= 20
			reasons = append(reasons, "web-port")
		}
		if isKnownNonGame(c.RemoteIP) {
			score -= 30
			reasons = append(reasons, "CDN/telemetry")
		}
		if c.Protocol == connmon.UDP && c.RemotePort > 1024 && !knownTelemetryPorts[c.RemotePort] {
			score += 15
			reasons = append(reasons, "high-port-UDP")
		}

		candidates = append(candidates, ServerCandidate{
			IP:       c.RemoteIP,
			Port:     c.RemotePort,
			Protocol: c.Protocol,
			Score:    score,
			Reason:   strings.Join(reasons, ", "),
		})
	}

	// Sort descending by score (simple insertion sort — list is small)
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].Score > candidates[j-1].Score; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}

	return candidates
}

func isKnownNonGame(ip net.IP) bool {
	for _, prefix := range knownNonGamePrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}
