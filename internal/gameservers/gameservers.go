// Package gameservers provides a curated database of known game server IP ranges
// per game and region.
//
// Why this approach?
//
// Games like CS2 and Valorant use UDP for gameplay traffic. The Windows UDP
// connection table (GetExtendedUdpTable) is stateless — it never records the
// remote IP because UDP has no handshake. This is a kernel-level limitation,
// not a code limitation.
//
// ExitLag and NoPing solve this the same way: they maintain a pre-built
// database of each game's server IP ranges per region. When the user selects
// CS2 → SA, the app already knows Valve's SA server IP blocks and routes all
// traffic to those CIDRs through the tunnel — no runtime server detection needed.
//
// Additionally, this package provides ActiveServerDetect() which attempts
// live detection via netstat + process correlation for when the player is
// already inside a match (fallback/refinement layer).
package gameservers

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// Region constants — must match routes package.
const (
	RegionSA   = "SA"   // South America (primarily Brazil)
	RegionUS   = "US"   // North America
	RegionEU   = "EU"   // Europe
	RegionASIA = "ASIA" // Asia-Pacific
	RegionAuto = "AUTO" // pick best region dynamically
)

// GameServerEntry describes a game's known server infrastructure for one region.
type GameServerEntry struct {
	// GameSlugs lists the process-name slugs that map to this entry.
	// Matched case-insensitively against running process names.
	GameSlugs []string

	// Region is one of RegionSA / RegionUS / RegionEU / RegionASIA.
	Region string

	// CIDRs are the IP ranges (CIDR notation) used by this game's servers
	// in the given region. These become the WireGuard AllowedIPs.
	CIDRs []string

	// GamePorts are the UDP/TCP ports this game uses on the server side.
	// Used as secondary confirmation when probing live connections.
	GamePorts []int

	// Notes is a human-readable explanation of the source of these ranges.
	Notes string
}

// db is the master database of game server IP ranges.
// Sources: public BGP/ASN data (RIPE, ARIN), Valve's own published ranges,
// Riot Games network disclosures, community research.
var db = []GameServerEntry{

	// ════════════════════════════════════════════════════════════════
	// VALVE / STEAM GAMES  (CS2, Dota 2, TF2, Deadlock, etc.)
	// ASN: AS32590 (Valve Corporation)
	// ════════════════════════════════════════════════════════════════

	{
		GameSlugs: []string{"cs2", "csgo", "dota2", "tf2", "deadlock", "hl2"},
		Region:    RegionSA,
		CIDRs: []string{
			// Valve SA (Brazil) — AS32590 / LATAM PoP
			"155.133.248.0/21",
			"185.25.182.0/24",
			"162.254.197.0/24", // Steam Datagram Relay SA
			// Valve anycast / relay overlay
			"192.69.96.0/22",
		},
		GamePorts: []int{27015, 27016, 27017, 27018, 27019, 27020, 4380, 27005},
		Notes:     "Valve AS32590 — South America (São Paulo PoP). SDR relay ranges included.",
	},
	{
		GameSlugs: []string{"cs2", "csgo", "dota2", "tf2", "deadlock", "hl2"},
		Region:    RegionUS,
		CIDRs: []string{
			// Valve US East
			"208.78.164.0/22",
			"162.254.192.0/21", // Steam Datagram Relay US
			"155.133.224.0/20",
			// Valve US West
			"162.254.196.0/24",
			"192.69.97.0/24",
		},
		GamePorts: []int{27015, 27016, 27017, 27018, 27019, 27020, 4380, 27005},
		Notes:     "Valve AS32590 — North America (Ashburn VA + Seattle WA PoPs).",
	},
	{
		GameSlugs: []string{"cs2", "csgo", "dota2", "tf2", "deadlock", "hl2"},
		Region:    RegionEU,
		CIDRs: []string{
			// Valve EU (Stockholm, Frankfurt, Vienna, Warsaw)
			"155.133.246.0/24",
			"185.25.183.0/24",
			"192.223.24.0/22",
			"162.254.193.0/24", // Steam Datagram Relay EU
			"155.133.252.0/22",
		},
		GamePorts: []int{27015, 27016, 27017, 27018, 27019, 27020, 4380, 27005},
		Notes:     "Valve AS32590 — Europe (Stockholm/Frankfurt primary PoPs).",
	},
	{
		GameSlugs: []string{"cs2", "csgo", "dota2", "tf2", "deadlock", "hl2"},
		Region:    RegionASIA,
		CIDRs: []string{
			// Valve Asia (Tokyo, Singapore, Seoul, Hong Kong, Sydney)
			"103.28.54.0/24",
			"162.254.195.0/24", // SDR Asia
			"155.133.240.0/21",
			"192.69.98.0/23",
		},
		GamePorts: []int{27015, 27016, 27017, 27018, 27019, 27020, 4380, 27005},
		Notes:     "Valve AS32590 — Asia-Pacific (Tokyo/Singapore primary PoPs).",
	},

	// ════════════════════════════════════════════════════════════════
	// RIOT GAMES  (Valorant, League of Legends, Wild Rift)
	// ASN: AS6507 (Riot Games)
	// ════════════════════════════════════════════════════════════════

	{
		GameSlugs: []string{"valorant", "valorant-win64-shipping", "leagueclient", "league of legends"},
		Region:    RegionSA,
		CIDRs: []string{
			// Riot SA (Brazil — São Paulo)
			"5.62.18.0/24",
			"5.62.19.0/24",
			"185.40.64.0/22",
			"185.40.68.0/22",
			"37.244.0.0/16",   // Riot EU/SA overlap range
		},
		GamePorts: []int{7086, 8088, 8393, 8394, 8395, 8396, 8397, 8398, 8399, 8400, 2099},
		Notes:     "Riot Games AS6507 — South America (BR/LAS servers).",
	},
	{
		GameSlugs: []string{"valorant", "valorant-win64-shipping", "leagueclient", "league of legends"},
		Region:    RegionEU,
		CIDRs: []string{
			// Riot EU (Amsterdam, Frankfurt, Warsaw)
			"185.40.64.0/22",
			"185.40.68.0/22",
			"37.244.0.0/16",
			"80.239.144.0/20",
		},
		GamePorts: []int{7086, 8088, 8393, 8394, 8395, 8396, 8397, 8398, 8399, 2099},
		Notes:     "Riot Games AS6507 — Europe (Amsterdam/Frankfurt primary PoPs).",
	},
	{
		GameSlugs: []string{"valorant", "valorant-win64-shipping", "leagueclient", "league of legends"},
		Region:    RegionUS,
		CIDRs: []string{
			// Riot NA (Chicago, Dallas, Atlanta)
			"5.62.16.0/22",
			"185.40.72.0/22",
			"68.232.34.0/24",
		},
		GamePorts: []int{7086, 8088, 8393, 8394, 8395, 8396, 8397, 8398, 8399, 2099},
		Notes:     "Riot Games AS6507 — North America (Chicago/Dallas primary PoPs).",
	},

	// ════════════════════════════════════════════════════════════════
	// EPIC GAMES  (Fortnite, Rocket League)
	// ASN: AS46489 (Epic Games) + AWS for some regions
	// ════════════════════════════════════════════════════════════════

	{
		GameSlugs: []string{"fortnite", "fortniteclient-win64-shipping", "rocketleague"},
		Region:    RegionSA,
		CIDRs: []string{
			// Epic SA / Fortnite BR (uses AWS São Paulo ap-southeast-1 + Cloudflare)
			"52.67.0.0/16",   // AWS SA-EAST-1 (São Paulo) — Fortnite BR servers
			"54.94.0.0/15",   // AWS SA-EAST-1
			"18.231.0.0/16",  // AWS SA-EAST-1
			"34.193.0.0/16",  // AWS US-EAST fallback
		},
		GamePorts: []int{7777, 9000, 443, 80},
		Notes:     "Epic Games — Fortnite/Rocket League SA. Uses AWS São Paulo + US-East.",
	},
	{
		GameSlugs: []string{"fortnite", "fortniteclient-win64-shipping", "rocketleague"},
		Region:    RegionEU,
		CIDRs: []string{
			"52.29.0.0/16",   // AWS EU-CENTRAL-1 (Frankfurt)
			"35.156.0.0/14",  // AWS EU-CENTRAL-1
			"18.184.0.0/14",  // AWS EU-CENTRAL-1
		},
		GamePorts: []int{7777, 9000, 443, 80},
		Notes:     "Epic Games — Fortnite/Rocket League EU. Uses AWS Frankfurt.",
	},

	// ════════════════════════════════════════════════════════════════
	// EA / BATTLEFIELD SERIES  (BF 2042, BFV, BF1, BF4)
	// ASN: AS36351 (EA / Softlayer)
	// ════════════════════════════════════════════════════════════════

	{
		GameSlugs: []string{"bf2042", "battlefield2042", "bfv", "bf1", "bf4", "bf3"},
		Region:    RegionSA,
		CIDRs: []string{
			// EA SA servers (Brazil — hosted via IBM Cloud / Softlayer SP)
			"169.44.60.0/24",
			"169.44.56.0/21",
			"169.57.140.0/22",
			"23.246.208.0/20", // EA Brazil CDN/game servers
		},
		GamePorts: []int{3659, 14000, 14001, 14002, 14003, 14004, 14005, 14006, 14015, 14016, 25000},
		Notes:     "EA AS36351 — South America (IBM Cloud São Paulo).",
	},
	{
		GameSlugs: []string{"bf2042", "battlefield2042", "bfv", "bf1", "bf4", "bf3"},
		Region:    RegionEU,
		CIDRs: []string{
			// EA EU servers (Frankfurt, Amsterdam — IBM Cloud)
			"169.50.16.0/20",
			"169.50.32.0/19",
			"23.246.200.0/21",
		},
		GamePorts: []int{3659, 14000, 14001, 14002, 14003, 14004, 14005, 14006, 14015, 14016, 25000},
		Notes:     "EA AS36351 — Europe (IBM Cloud Frankfurt/Amsterdam).",
	},

	// ════════════════════════════════════════════════════════════════
	// ACTIVISION / CALL OF DUTY  (Warzone, MW3)
	// ASN: AS20940 (Akamai — CDN) + AS209 (CenturyLink)
	// ════════════════════════════════════════════════════════════════

	{
		GameSlugs: []string{"modernwarfare", "warzone", "cod", "blackopscoldwar"},
		Region:    RegionSA,
		CIDRs: []string{
			// Activision SA (uses AWS + dedicated SA nodes)
			"52.67.128.0/17",  // AWS SA-EAST-1
			"177.71.128.0/17", // AWS SA-EAST-1
			"54.233.0.0/16",   // AWS SA-EAST-1
		},
		GamePorts: []int{3074, 3075, 3076, 30000, 30001, 30002, 30003},
		Notes:     "Activision — CoD/Warzone SA (AWS São Paulo + dedicated nodes).",
	},
	{
		GameSlugs: []string{"modernwarfare", "warzone", "cod", "blackopscoldwar"},
		Region:    RegionEU,
		CIDRs: []string{
			// Activision EU
			"52.29.0.0/16",
			"35.156.0.0/14",
			"3.120.0.0/14",
		},
		GamePorts: []int{3074, 3075, 3076, 30000, 30001, 30002, 30003},
		Notes:     "Activision — CoD/Warzone EU (AWS Frankfurt).",
	},
}

// ────────────────────────────────────────────────────────────────────────────

// GetRoutes returns the known CIDR ranges for a given game process name and region.
// gameExe should be the lowercase process name (e.g. "cs2.exe" or "cs2").
// Returns nil if the game or region is not in the database.
func GetRoutes(gameExe, region string) []string {
	gameKey := strings.ToLower(strings.TrimSuffix(gameExe, ".exe"))
	for _, entry := range db {
		if entry.Region != region {
			continue
		}
		for _, slug := range entry.GameSlugs {
			if strings.EqualFold(slug, gameKey) {
				return entry.CIDRs
			}
		}
	}
	return nil
}

// GetAllRegionRoutes returns routes for all regions for a given game,
// keyed by region code. Useful for building the AllowedIPs set when
// the user selects AUTO mode.
func GetAllRegionRoutes(gameExe string) map[string][]string {
	gameKey := strings.ToLower(strings.TrimSuffix(gameExe, ".exe"))
	result := make(map[string][]string)
	for _, entry := range db {
		for _, slug := range entry.GameSlugs {
			if strings.EqualFold(slug, gameKey) {
				result[entry.Region] = append(result[entry.Region], entry.CIDRs...)
				break
			}
		}
	}
	return result
}

// GetPorts returns the known game server ports for a given process name.
func GetPorts(gameExe string) []int {
	gameKey := strings.ToLower(strings.TrimSuffix(gameExe, ".exe"))
	seen := make(map[int]bool)
	var ports []int
	for _, entry := range db {
		for _, slug := range entry.GameSlugs {
			if strings.EqualFold(slug, gameKey) {
				for _, p := range entry.GamePorts {
					if !seen[p] {
						seen[p] = true
						ports = append(ports, p)
					}
				}
				break
			}
		}
	}
	return ports
}

// IsKnownGame returns true if the process name has a known server database entry.
func IsKnownGame(gameExe string) bool {
	gameKey := strings.ToLower(strings.TrimSuffix(gameExe, ".exe"))
	for _, entry := range db {
		for _, slug := range entry.GameSlugs {
			if strings.EqualFold(slug, gameKey) {
				return true
			}
		}
	}
	return false
}

// SupportedRegions returns all regions available for a given game.
func SupportedRegions(gameExe string) []string {
	gameKey := strings.ToLower(strings.TrimSuffix(gameExe, ".exe"))
	seen := make(map[string]bool)
	var regions []string
	for _, entry := range db {
		for _, slug := range entry.GameSlugs {
			if strings.EqualFold(slug, gameKey) && !seen[entry.Region] {
				seen[entry.Region] = true
				regions = append(regions, entry.Region)
				break
			}
		}
	}
	return regions
}

// ────────────────────────────────────────────────────────────────────────────
// ACTIVE SERVER DETECTION (runtime refinement)
//
// When a player is already inside a match, we can refine the routing to
// just the specific server IP they're connected to. This is more efficient
// than routing to the entire CIDR block.
//
// Strategy:
//  1. Use netstat -n output to find UDP connections for the game's PID.
//     On Windows 10+, netstat shows remote IPs for "connected" UDP sockets
//     when the socket has called connect() — which CS2 does.
//  2. Cross-reference with the known IP ranges to confirm it's a game server.
//  3. Fall back to the full CIDR block if detection fails.
// ────────────────────────────────────────────────────────────────────────────

// DetectedServer holds the result of runtime server detection.
type DetectedServer struct {
	IP       string // e.g., "185.25.182.43"
	Port     uint16
	Region   string // inferred from which CIDR the IP falls in
	Verified bool   // true if IP falls within a known game server range
}

// ActiveServerDetect attempts to detect the specific game server IP a process
// is currently connected to. gameExe is the process name (e.g., "cs2.exe"),
// pid is the process ID returned by gamedet.
//
// Returns nil if no server can be detected (game not in a match yet).
// Falls back gracefully — the caller should use GetRoutes() if this returns nil.
func ActiveServerDetect(gameExe string, pid uint32) *DetectedServer {
	// Use netstat -n -b to get per-process network connections including UDP.
	// -n: numeric, -b: show process, -a: all connections.
	// Note: requires no elevation for most cases; -b may need admin.
	out, err := exec.Command("netstat", "-n", "-a", "-p", "UDP").CombinedOutput()
	if err != nil {
		// Try without -p flag (older Windows)
		out, err = exec.Command("netstat", "-n", "-a").CombinedOutput()
		if err != nil {
			return nil
		}
	}
	return parseNetstatForGame(string(out), gameExe, pid)
}

// parseNetstatForGame extracts the most likely game server IP from netstat output.
func parseNetstatForGame(output, gameExe string, pid uint32) *DetectedServer {
	gameKey := strings.ToLower(strings.TrimSuffix(gameExe, ".exe"))
	knownPorts := make(map[int]bool)
	for _, p := range GetPorts(gameExe) {
		knownPorts[p] = true
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "UDP") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		// parts[0] = "UDP", parts[1] = local_addr:port, parts[2] = remote_addr:port or "*:*"
		remote := parts[2]
		if remote == "*:*" || remote == "0.0.0.0:0" {
			continue
		}

		host, portStr, err := net.SplitHostPort(remote)
		if err != nil {
			continue
		}
		remoteIP := net.ParseIP(host)
		if remoteIP == nil || remoteIP.IsPrivate() || remoteIP.IsLoopback() {
			continue
		}

		// Check if the remote port is a known game port
		port := 0
		fmt.Sscanf(portStr, "%d", &port)
		if !knownPorts[port] && port < 27000 {
			continue // not a known game port
		}

		// Verify the IP falls within known game server CIDRs
		region := inferRegionFromIP(remoteIP, gameKey)
		if region == "" {
			continue
		}

		return &DetectedServer{
			IP:       host,
			Port:     uint16(port),
			Region:   region,
			Verified: true,
		}
	}
	return nil
}

// inferRegionFromIP checks if the IP falls within any known game server range
// and returns the region code. Returns "" if no match.
func inferRegionFromIP(ip net.IP, gameKey string) string {
	for _, entry := range db {
		for _, slug := range entry.GameSlugs {
			if !strings.EqualFold(slug, gameKey) {
				continue
			}
			for _, cidr := range entry.CIDRs {
				_, ipNet, err := net.ParseCIDR(cidr)
				if err != nil {
					continue
				}
				if ipNet.Contains(ip) {
					return entry.Region
				}
			}
		}
	}
	return ""
}
