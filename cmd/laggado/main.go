// LAGGADO — Lightweight Automatic Game-route Grading And Dynamic Optimizer
//
// A personal ExitLag/NoPing alternative for Windows.
// Detects game connections, identifies servers, tests routes, picks the best path.
//
// Usage:
//   laggado scan              — detect active game connections
//   laggado analyze [--ip X]  — test routes to detected/specified server
//   laggado optimize          — choose best route and optionally apply tunnel
//   laggado status            — show current route + metrics
//   laggado watch [--process] — continuous monitoring mode
//   laggado config            — show/edit configuration
//   laggado servers           — list known servers from history
package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"laggado/internal/connmon"
	"laggado/internal/gamedet"
	"laggado/internal/geoip"
	"laggado/internal/relay"
	"laggado/internal/routetest"
	"laggado/internal/scorer"
	"laggado/internal/serverid"
	"laggado/internal/store"
	"laggado/internal/tunnel"
)

const version = "0.1.0"

var (
	dataDir  string
	db       *store.Database
	geoRes   *geoip.Resolver
	wg       *tunnel.WireGuard
)

func main() {
	// Setup data directory
	home, _ := os.UserHomeDir()
	dataDir = filepath.Join(home, ".laggado")

	var err error
	db, err = store.Open(dataDir)
	if err != nil {
		fatalf("Failed to open database: %v", err)
	}

	geoRes, err = geoip.NewResolver(filepath.Join(dataDir, "cache"))
	if err != nil {
		fatalf("Failed to init GeoIP: %v", err)
	}

	wgConfigDir := db.Config.WireGuardConfigDir
	if wgConfigDir == "" {
		wgConfigDir = filepath.Join(dataDir, "wireguard")
	}
	wg = tunnel.NewWireGuard(wgConfigDir)

	if len(os.Args) < 2 {
		// No args = interactive mode (supports double-click from Explorer)
		cmdInteractive()
		return
	}

	switch os.Args[1] {
	case "scan":
		cmdScan()
	case "analyze":
		cmdAnalyze()
	case "optimize":
		cmdOptimize()
	case "status":
		cmdStatus()
	case "watch":
		cmdWatch()
	case "servers":
		cmdServers()
	case "config":
		cmdConfig()
	case "tunnel":
		cmdTunnel()
	case "relays":
		cmdRelays()
	case "relay":
		cmdRelayServer()
	case "version":
		fmt.Printf("LAGGADO v%s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

// ─── INTERACTIVE MODE ────────────────────────────────────────────────────────
// Launched when user double-clicks the .exe from Explorer (no args).

func cmdInteractive() {
	fmt.Printf("╔══════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  LAGGADO v%s — Game Route Optimizer                ║\n", version)
	fmt.Printf("╚══════════════════════════════════════════════════════╝\n\n")

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("┌─ Menu ──────────────────────────────────────────────")
		fmt.Println("│  1. scan     — Detectar conexões de jogos ativas")
		fmt.Println("│  2. analyze  — Testar rotas para o servidor")
		fmt.Println("│  3. optimize — Escolher melhor rota")
		fmt.Println("│  4. status   — Mostrar status atual")
		fmt.Println("│  5. watch    — Monitoramento contínuo")
		fmt.Println("│  6. servers  — Servidores conhecidos")
		fmt.Println("│  7. config   — Configurações")
		fmt.Println("│  0. sair")
		fmt.Println("└─────────────────────────────────────────────────────")
		fmt.Print("\nEscolha: ")

		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		fmt.Println()

		switch line {
		case "1", "scan":
			cmdScan()
		case "2", "analyze":
			fmt.Print("IP do servidor (deixe vazio para auto-detectar): ")
			ip, _ := reader.ReadString('\n')
			ip = strings.TrimSpace(ip)
			if ip != "" {
				os.Args = []string{"laggado", "analyze", "--ip", ip}
			} else {
				os.Args = []string{"laggado", "analyze"}
			}
			cmdAnalyze()
		case "3", "optimize":
			cmdOptimize()
		case "4", "status":
			cmdStatus()
		case "5", "watch":
			fmt.Print("Nome do processo (ex: cs2.exe, deixe vazio para auto): ")
			proc, _ := reader.ReadString('\n')
			proc = strings.TrimSpace(proc)
			if proc != "" {
				os.Args = []string{"laggado", "watch", "--process", proc}
			} else {
				os.Args = []string{"laggado", "watch"}
			}
			cmdWatch()
		case "6", "servers":
			cmdServers()
		case "7", "config":
			os.Args = []string{"laggado", "config"}
			cmdConfig()
		case "0", "sair", "exit", "q":
			fmt.Println("Até mais!")
			return
		default:
			fmt.Printf("Opção inválida: %q\n", line)
		}
		fmt.Println()
	}
}

func printUsage() {
	fmt.Printf(`LAGGADO v%s — Lightweight Automatic Game-route Grading And Dynamic Optimizer

Usage: laggado <command> [options]

Commands:
  scan                Detect active game connections
  analyze [--ip X]    Test routes to detected or specified server
  optimize            Choose and apply best route
  status              Show current connection metrics
  watch [--process X] Continuous monitoring mode
  servers             List known servers from history
  config              Show/edit configuration
  tunnel              WireGuard tunnel management (genkey / configure / activate / stop / status)
  relays              Community relay network — connect without your own VPS
  relay               Run a relay node to contribute capacity to the community
  version             Show version
  help                Show this help

CS2 quick start — via community relay (no VPS needed):
  1. laggado relays list                         see available relays
  2. laggado relays connect --relay BR-SP-1 --target <cs2-server-ip>
     (CS2 server IP: open console, type "status", look for "server : X.X.X.X")

CS2 quick start — via your own VPS (Oracle Free Tier):
  laggado tunnel guide                           full setup guide
`, version)
}

// isElevated reports whether the process is running with administrator privileges.
// It probes a resource that requires elevation rather than parsing token structs,
// keeping the implementation dependency-free.
func isElevated() bool {
	f, err := os.Open(`\\.\PHYSICALDRIVE0`)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// requireElevated prints a clear error and exits if not running as administrator.
func requireElevated(action string) {
	if !isElevated() {
		fmt.Fprintf(os.Stderr,
			"ERROR: %s requires administrator privileges.\n"+
				"Right-click laggado.exe → 'Run as administrator', then try again.\n", action)
		os.Exit(1)
	}
}

// validateIP returns an error if s is not a valid IPv4 address.
func validateIP(s string) error {
	if ip := net.ParseIP(s); ip == nil || ip.To4() == nil {
		return fmt.Errorf("%q is not a valid IPv4 address", s)
	}
	return nil
}

// ─── SCAN ───────────────────────────────────────────────────────────────────

func cmdScan() {
	fmt.Println("=== LAGGADO — Scanning for game connections ===")
	fmt.Println()

	// Detect games
	games, err := gamedet.DetectGames()
	if err != nil {
		fmt.Printf("Warning: game detection error: %v\n", err)
	}

	if len(games) > 0 {
		fmt.Println("Detected game processes:")
		for _, g := range games {
			friendly := gamedet.FriendlyName(g.Name)
			fmt.Printf("  %-30s PID %d\n", friendly+" ("+g.Name+")", g.PID)
		}
		fmt.Println()
	} else {
		fmt.Println("No known game processes detected. Scanning all connections...")
		fmt.Println()
	}

	// Get all connections
	allConns, err := connmon.GetAllConnections()
	if err != nil {
		fatalf("Failed to get connections: %v", err)
	}

	// If games detected, filter to their PIDs
	var targetConns []connmon.Connection
	if len(games) > 0 {
		pidSet := make(map[uint32]bool)
		for _, g := range games {
			pidSet[g.PID] = true
		}
		for _, c := range allConns {
			if pidSet[c.PID] {
				targetConns = append(targetConns, c)
			}
		}
	} else {
		// Show all public connections
		for _, c := range allConns {
			if c.IsPublicRemote() {
				targetConns = append(targetConns, c)
			}
		}
	}

	if len(targetConns) == 0 {
		fmt.Println("No active game connections found.")
		fmt.Println("Tip: Start a game and join a server, then run 'laggado scan' again.")
		return
	}

	// Identify server candidates
	candidates := serverid.IdentifyAllServers(targetConns)
	if len(candidates) == 0 {
		fmt.Println("Could not identify any game server IPs.")
		return
	}

	fmt.Printf("Found %d connection candidates:\n\n", len(candidates))
	fmt.Printf("  %-4s %-18s %-7s %-6s %-8s %s\n", "Rank", "IP", "Port", "Proto", "Score", "Reason")
	fmt.Printf("  %-4s %-18s %-7s %-6s %-8s %s\n", "----", "------------------", "-------", "------", "--------", "------")

	limit := len(candidates)
	if limit > 15 {
		limit = 15
	}
	for i := 0; i < limit; i++ {
		c := candidates[i]
		fmt.Printf("  #%-3d %-18s %-7d %-6s %-8.1f %s\n",
			i+1, c.IP, c.Port, c.Protocol, c.Score, c.Reason)
	}

	// GeoIP for top candidate
	if len(candidates) > 0 {
		best := candidates[0]
		geo, err := geoRes.Lookup(best.IP)
		fmt.Println()
		if err == nil {
			fmt.Printf("Top server: %s → %s\n", best.IP, geo)

			// Record to database
			db.RecordServer(store.ServerRecord{
				IP:          best.IP.String(),
				Port:        best.Port,
				Protocol:    best.Protocol.String(),
				Country:     geo.Country,
				City:        geo.City,
				Region:      geo.Region,
				ISP:         geo.ISP,
			})
			db.Save()
		} else {
			fmt.Printf("Top server: %s (GeoIP lookup failed: %v)\n", best.IP, err)
		}
	}
}

// ─── ANALYZE ────────────────────────────────────────────────────────────────

func cmdAnalyze() {
	targetIP := ""

	// Parse --ip flag
	for i, arg := range os.Args[2:] {
		if arg == "--ip" && i+1 < len(os.Args[2:])-0 {
			if i+3 < len(os.Args) {
				targetIP = os.Args[i+3]
			}
		}
	}

	if targetIP == "" {
		// Auto-detect
		fmt.Println("No --ip specified, auto-detecting game server...")
		targetIP = autoDetectServer()
		if targetIP == "" {
			printDetectionHelp()
			return
		}
	}

	fmt.Printf("=== LAGGADO — Analyzing routes to %s ===\n\n", targetIP)

	// GeoIP
	ip := net.ParseIP(targetIP)
	if ip != nil {
		if geo, err := geoRes.Lookup(ip); err == nil {
			fmt.Printf("Server location: %s\n\n", geo)
		}
	}

	// Test routes
	tester := routetest.NewTester()
	tester.Count = db.Config.PingCount

	fmt.Printf("Testing %d routes (%d probes each)...\n\n",
		1+len(db.Config.VPSEndpoints), tester.Count)

	metrics := tester.MeasureAll(targetIP, db.Config.VPSEndpoints)

	if len(metrics) == 0 {
		fmt.Println("All route tests failed. Server may be unreachable.")
		return
	}

	// Score routes
	weights := db.Config.Weights
	scored := scorer.ScoreRoutes(metrics, weights)

	fmt.Printf("  %-4s %-20s %8s %8s %8s %8s %7s %8s\n",
		"Rank", "Route", "Avg(ms)", "Min(ms)", "Max(ms)", "Jit(ms)", "Loss%", "Score")
	fmt.Printf("  %-4s %-20s %8s %8s %8s %8s %7s %8s\n",
		"----", "--------------------", "--------", "--------", "--------", "--------", "-------", "--------")

	for _, s := range scored {
		m := s.Metrics
		fmt.Printf("  #%-3d %-20s %8.1f %8.1f %8.1f %8.1f %6.1f%% %8.1f\n",
			s.Rank, m.Via,
			float64(m.AvgLatency.Microseconds())/1000.0,
			float64(m.MinLatency.Microseconds())/1000.0,
			float64(m.MaxLatency.Microseconds())/1000.0,
			float64(m.Jitter.Microseconds())/1000.0,
			m.PacketLoss,
			s.Score,
		)

		// Record to database
		db.RecordLatency(targetIP, m.Via, m, s.Score)
	}

	fmt.Printf("\nBest route: %s (score: %.1f)\n", scored[0].Metrics.Via, scored[0].Score)
	fmt.Printf("Scoring: latency*%.1f + jitter*%.1f + loss*%.1f\n",
		weights.LatencyWeight, weights.JitterWeight, weights.LossWeight)

	db.Save()
}

// printDetectionHelp explains why auto-detection may fail (especially for CS2/UDP games)
// and guides the user to find the server IP manually.
func printDetectionHelp() {
	games, _ := gamedet.DetectGames()
	if len(games) > 0 {
		fmt.Println("Game(s) detected but no server IP found:")
		for _, g := range games {
			fmt.Printf("  %s (PID %d)\n", gamedet.FriendlyName(g.Name), g.PID)
		}
		fmt.Println()
		fmt.Println("Most games use UDP for traffic — the Windows connection table")
		fmt.Println("does not expose remote IPs for UDP sockets.")
		fmt.Println()
		fmt.Println("To find the server IP:")
		fmt.Println("  CS2:      open console (`) → type 'status' → look for 'server : X.X.X.X'")
		fmt.Println("  Valorant: open console → type 'cl_showpos 1' or check task manager network")
		fmt.Println("  Other:    use 'netstat -n' or 'Resource Monitor' → Network tab")
		fmt.Println()
		fmt.Println("Then run: laggado analyze --ip <server-ip>")
	} else {
		fmt.Println("No known game process found and no active game server connection detected.")
		fmt.Println("Start your game, join a server, then run this command again.")
		fmt.Println("Or specify the server IP directly: laggado analyze --ip <server-ip>")
	}
}

// ─── OPTIMIZE ───────────────────────────────────────────────────────────────

func cmdOptimize() {
	fmt.Println("=== LAGGADO — Optimizing route ===")
	fmt.Println()

	targetIP := autoDetectServer()
	if targetIP == "" {
		printDetectionHelp()
		return
	}

	fmt.Printf("Target server: %s\n", targetIP)

	// Test routes
	tester := routetest.NewTester()
	tester.Count = db.Config.PingCount
	metrics := tester.MeasureAll(targetIP, db.Config.VPSEndpoints)

	if len(metrics) == 0 {
		fmt.Println("All routes unreachable.")
		return
	}

	best := scorer.BestRoute(metrics, db.Config.Weights)
	if best == nil {
		fmt.Println("Could not determine best route.")
		return
	}

	fmt.Printf("Best route: %s (score: %.1f, latency: %.1fms, jitter: %.1fms, loss: %.1f%%)\n",
		best.Metrics.Via, best.Score,
		float64(best.Metrics.AvgLatency.Microseconds())/1000.0,
		float64(best.Metrics.Jitter.Microseconds())/1000.0,
		best.Metrics.PacketLoss)

	if best.Metrics.Via == "direct" {
		fmt.Println("\nDirect route is optimal. No tunnel needed.")
	} else {
		fmt.Printf("\nRoute via %s is better than direct.\n", best.Metrics.Via)
		peer, hasPeer := db.Config.WireGuardPeers[best.Metrics.Via]
		if !wg.IsAvailable() {
			fmt.Println("WireGuard not installed — get it from https://www.wireguard.com/install/")
			fmt.Printf("After installing, run: laggado tunnel activate --target %s --via %s\n", targetIP, best.Metrics.Via)
		} else if db.Config.WireGuardPrivateKey == "" {
			fmt.Println("WireGuard keys not generated yet.")
			fmt.Println("  1. Run: laggado tunnel genkey")
			fmt.Println("  2. Add your public key to the VPS WireGuard config")
			fmt.Printf("  3. Run: laggado tunnel configure --vps %s --peer-key <VPS_PUBKEY> --peer-addr <VPS_IP:PORT> --tunnel-addr 10.66.66.2/32 --gateway 10.66.66.1\n", best.Metrics.Via)
			fmt.Printf("  4. Run: laggado tunnel activate --target %s --via %s\n", targetIP, best.Metrics.Via)
		} else if !hasPeer {
			fmt.Printf("No WireGuard peer configured for %q.\n", best.Metrics.Via)
			fmt.Printf("Run: laggado tunnel configure --vps %s --peer-key <VPS_PUBKEY> --peer-addr <VPS_IP:PORT> --tunnel-addr 10.66.66.2/32 --gateway 10.66.66.1\n", best.Metrics.Via)
		} else {
			_ = peer
			fmt.Printf("Run: laggado tunnel activate --target %s --via %s\n", targetIP, best.Metrics.Via)
		}
	}

	db.Save()
}

// ─── STATUS ─────────────────────────────────────────────────────────────────

func cmdStatus() {
	fmt.Println("=== LAGGADO — Current Status ===")
	fmt.Println()

	// Show detected games
	games, _ := gamedet.DetectGames()
	if len(games) > 0 {
		fmt.Println("Active games:")
		for _, g := range games {
			fmt.Printf("  %s (%s) — PID %d\n", gamedet.FriendlyName(g.Name), g.Name, g.PID)
		}
		fmt.Println()
	} else {
		fmt.Println("No game processes running.")
		fmt.Println()
	}

	// Show current game connections
	if len(games) > 0 {
		for _, g := range games {
			conns, err := connmon.GetConnectionsByPID(g.PID)
			if err != nil {
				continue
			}
			candidates := serverid.IdentifyAllServers(conns)
			if len(candidates) > 0 {
				best := candidates[0]
				fmt.Printf("Game server: %s:%d (%s)\n", best.IP, best.Port, best.Protocol)
				if geo, err := geoRes.Lookup(best.IP); err == nil {
					fmt.Printf("  Location: %s\n", geo)
				}

				// Quick latency test
				tester := routetest.NewTester()
				tester.Count = 5 // Quick test
				m, err := tester.MeasureDirect(best.IP.String())
				if err == nil {
					fmt.Printf("  Latency: %.1fms (jitter: %.1fms, loss: %.1f%%)\n",
						float64(m.AvgLatency.Microseconds())/1000.0,
						float64(m.Jitter.Microseconds())/1000.0,
						m.PacketLoss)
				}
				fmt.Println()
			}
		}
	}

	// WireGuard status
	fmt.Printf("WireGuard: ")
	if wg.IsAvailable() {
		fmt.Println("installed")
	} else {
		fmt.Println("not found")
	}

	// Database stats
	servers := db.GetServers()
	fmt.Printf("Known servers: %d\n", len(servers))
	fmt.Printf("GeoIP cache: %d entries\n", geoRes.CacheSize())
	fmt.Printf("Data dir: %s\n", dataDir)
}

// ─── WATCH ──────────────────────────────────────────────────────────────────

func cmdWatch() {
	processFilter := ""
	for i, arg := range os.Args[2:] {
		if arg == "--process" && i+3 < len(os.Args) {
			processFilter = os.Args[i+3]
		}
	}

	interval := time.Duration(db.Config.ScanIntervalSec) * time.Second
	testInterval := time.Duration(db.Config.TestIntervalSec) * time.Second

	fmt.Println("=== LAGGADO — Watch Mode ===")
	if processFilter != "" {
		fmt.Printf("Watching process: %s\n", processFilter)
	}
	fmt.Printf("Scan interval: %s | Test interval: %s\n", interval, testInterval)
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	var lastServer string
	var lastTest time.Time

	for {
		var targetPID uint32
		var gameName string

		if processFilter != "" {
			pid, err := gamedet.FindProcessByName(processFilter)
			if err != nil {
				fmt.Printf("[%s] Waiting for %s...\n", timestamp(), processFilter)
				time.Sleep(interval)
				continue
			}
			targetPID = pid
			gameName = processFilter
		} else {
			games, _ := gamedet.DetectGames()
			if len(games) == 0 {
				fmt.Printf("[%s] No games detected. Waiting...\n", timestamp())
				time.Sleep(interval)
				continue
			}
			targetPID = games[0].PID
			gameName = games[0].Name
		}

		conns, err := connmon.GetConnectionsByPID(targetPID)
		if err != nil {
			time.Sleep(interval)
			continue
		}

		best := serverid.IdentifyGameServer(conns)
		if best == nil {
			fmt.Printf("[%s] %s running but no game server connection found\n", timestamp(), gameName)
			time.Sleep(interval)
			continue
		}

		serverStr := best.IP.String()
		isNew := serverStr != lastServer
		lastServer = serverStr

		if isNew {
			fmt.Printf("[%s] NEW SERVER: %s:%d (%s) for %s\n",
				timestamp(), best.IP, best.Port, best.Protocol, gameName)
			if geo, err := geoRes.Lookup(best.IP); err == nil {
				fmt.Printf("  Location: %s\n", geo)
			}
			lastTest = time.Time{} // Force a route test
		}

		// Periodic route test
		if time.Since(lastTest) > testInterval {
			tester := routetest.NewTester()
			tester.Count = 5
			m, err := tester.MeasureDirect(serverStr)
			if err == nil {
				fmt.Printf("[%s] %s → avg=%.1fms jitter=%.1fms loss=%.1f%%\n",
					timestamp(), serverStr,
					float64(m.AvgLatency.Microseconds())/1000.0,
					float64(m.Jitter.Microseconds())/1000.0,
					m.PacketLoss)

				db.RecordLatency(serverStr, "direct", m, 0)
				db.Save()
			}
			lastTest = time.Now()
		}

		time.Sleep(interval)
	}
}

// ─── SERVERS ────────────────────────────────────────────────────────────────

func cmdServers() {
	servers := db.GetServers()
	if len(servers) == 0 {
		fmt.Println("No servers in history. Run 'laggado scan' to detect servers.")
		return
	}

	fmt.Println("=== Known Game Servers ===")
	fmt.Println()
	fmt.Printf("  %-18s %-15s %-25s %-15s %-10s %s\n",
		"IP", "Country", "City", "ISP", "Times", "Last Seen")
	fmt.Printf("  %-18s %-15s %-25s %-15s %-10s %s\n",
		"------------------", "---------------", "-------------------------", "---------------", "----------", "---------")

	for _, s := range servers {
		fmt.Printf("  %-18s %-15s %-25s %-15s %-10d %s\n",
			s.IP, s.Country, s.City, truncate(s.ISP, 15), s.TimesFound,
			s.LastSeen.Format("2006-01-02 15:04"))
	}
}

// ─── CONFIG ─────────────────────────────────────────────────────────────────

func cmdConfig() {
	if len(os.Args) > 2 {
		switch os.Args[2] {
		case "add-vps":
			if len(os.Args) < 5 {
				fmt.Println("Usage: laggado config add-vps <name> <address> [port] [--extra-ms <N>]")
				fmt.Println()
				fmt.Println("  --extra-ms <N>  estimated VPS→game-server latency in ms")
				fmt.Println("                  run a ping from the VPS to your game server and set this value")
				fmt.Println("                  so the optimizer scores the FULL path (client→VPS + VPS→server)")
				fmt.Println()
				fmt.Println("Example:")
				fmt.Println("  laggado config add-vps BR-SP 200.100.50.1 22 --extra-ms 5")
				return
			}
			name := os.Args[3]
			addr := os.Args[4]

			// Validate address (IP or hostname)
			if net.ParseIP(addr) == nil {
				// Not an IP — check it looks like a hostname (basic sanity)
				if strings.ContainsAny(addr, " /\\;|&`$") {
					fmt.Fprintf(os.Stderr, "Invalid VPS address: %q\n", addr)
					return
				}
			}

			port := 22
			extraMs := 0.0

			// Parse remaining args: [port] [--extra-ms N]
			args := os.Args[5:]
			i := 0
			for i < len(args) {
				switch args[i] {
				case "--extra-ms":
					if i+1 < len(args) {
						i++
						if v, err := strconv.ParseFloat(args[i], 64); err == nil && v >= 0 {
							extraMs = v
						} else {
							fmt.Fprintf(os.Stderr, "Invalid --extra-ms value: %q\n", args[i])
							return
						}
					}
				default:
					if p, err := strconv.Atoi(args[i]); err == nil {
						if p < 1 || p > 65535 {
							fmt.Fprintf(os.Stderr, "Port must be between 1 and 65535, got %d\n", p)
							return
						}
						port = p
					}
				}
				i++
			}

			db.Config.VPSEndpoints = append(db.Config.VPSEndpoints, routetest.VPSEndpoint{
				Name:           name,
				Address:        addr,
				Port:           port,
				ExtraLatencyMs: extraMs,
			})
			db.Save()
			if extraMs > 0 {
				fmt.Printf("Added VPS endpoint: %s (%s:%d) [+%.0fms VPS→target]\n", name, addr, port, extraMs)
			} else {
				fmt.Printf("Added VPS endpoint: %s (%s:%d)\n", name, addr, port)
				fmt.Printf("Tip: measure ping from VPS to game server and set with --extra-ms for accurate full-path scoring.\n")
			}
			return

		case "remove-vps":
			if len(os.Args) < 4 {
				fmt.Println("Usage: laggado config remove-vps <name>")
				return
			}
			name := os.Args[3]
			var newList []routetest.VPSEndpoint
			found := false
			for _, v := range db.Config.VPSEndpoints {
				if v.Name == name {
					found = true
					continue
				}
				newList = append(newList, v)
			}
			if !found {
				fmt.Printf("VPS %q not found\n", name)
				return
			}
			db.Config.VPSEndpoints = newList
			db.Save()
			fmt.Printf("Removed VPS endpoint: %s\n", name)
			return

		case "set":
			if len(os.Args) < 5 {
				fmt.Println("Usage: laggado config set <key> <value>")
				fmt.Println("Keys: scan-interval, test-interval, ping-count, jitter-weight, loss-weight")
				return
			}
			key := os.Args[3]
			val := os.Args[4]
			switch key {
			case "scan-interval":
				n, _ := strconv.Atoi(val)
				if n > 0 {
					db.Config.ScanIntervalSec = n
				}
			case "test-interval":
				n, _ := strconv.Atoi(val)
				if n > 0 {
					db.Config.TestIntervalSec = n
				}
			case "ping-count":
				n, _ := strconv.Atoi(val)
				if n > 0 {
					db.Config.PingCount = n
				}
			case "jitter-weight":
				f, _ := strconv.ParseFloat(val, 64)
				if f >= 0 {
					db.Config.Weights.JitterWeight = f
				}
			case "loss-weight":
				f, _ := strconv.ParseFloat(val, 64)
				if f >= 0 {
					db.Config.Weights.LossWeight = f
				}
			default:
				fmt.Printf("Unknown config key: %s\n", key)
				return
			}
			db.Save()
			fmt.Printf("Set %s = %s\n", key, val)
			return
		}
	}

	// Show current config
	fmt.Println("=== LAGGADO Configuration ===")
	fmt.Println()
	fmt.Printf("  Scoring weights:\n")
	fmt.Printf("    Latency weight: %.1f\n", db.Config.Weights.LatencyWeight)
	fmt.Printf("    Jitter weight:  %.1f\n", db.Config.Weights.JitterWeight)
	fmt.Printf("    Loss weight:    %.1f\n", db.Config.Weights.LossWeight)
	fmt.Println()
	fmt.Printf("  Polling:\n")
	fmt.Printf("    Scan interval:  %ds\n", db.Config.ScanIntervalSec)
	fmt.Printf("    Test interval:  %ds\n", db.Config.TestIntervalSec)
	fmt.Printf("    Ping count:     %d\n", db.Config.PingCount)
	fmt.Println()
	fmt.Printf("  VPS endpoints (%d):\n", len(db.Config.VPSEndpoints))
	if len(db.Config.VPSEndpoints) == 0 {
		fmt.Println("    (none configured)")
		fmt.Println("    Add with: laggado config add-vps <name> <address> [port]")
	} else {
		for _, v := range db.Config.VPSEndpoints {
			fmt.Printf("    %s → %s:%d\n", v.Name, v.Address, v.Port)
		}
	}
	fmt.Println()
	fmt.Printf("  Data directory: %s\n", dataDir)
	fmt.Printf("  WireGuard: ")
	if wg.IsAvailable() {
		fmt.Println("available")
	} else {
		fmt.Println("not installed")
	}
}

// ─── COMMUNITY RELAYS (client) ───────────────────────────────────────────────

func cmdRelays() {
	sub := ""
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}

	relayClient := relay.NewClient("", filepath.Join(dataDir, "cache"))

	switch sub {
	case "list", "":
		cmdRelaysList(relayClient)
	case "connect":
		cmdRelaysConnect(relayClient)
	case "disconnect":
		cmdRelaysDisconnect(relayClient)
	default:
		fmt.Println("Usage: laggado relays <subcommand>")
		fmt.Println()
		fmt.Println("  list                             List available community relays")
		fmt.Println("  connect --relay <name> --target <ip>   Connect via community relay")
		fmt.Println("  disconnect                       Disconnect from current relay")
		fmt.Println()
		fmt.Println("How it works:")
		fmt.Println("  Community members run 'laggado relay serve' on their VPS/machine.")
		fmt.Println("  You connect through them — no VPS needed on your end.")
		fmt.Println("  Brazil player helps Portugal player; Portugal player helps Brazil player.")
		fmt.Println()
		fmt.Println("Want to contribute a relay node?")
		fmt.Println("  laggado relay serve --help")
	}
}

func cmdRelaysList(rc *relay.Client) {
	fmt.Println("=== LAGGADO — Community Relays ===")
	fmt.Println()
	fmt.Println("Fetching relay list...")

	rl, err := rc.FetchRelays()
	if err != nil {
		fmt.Printf("Could not fetch relay list: %v\n", err)
		fmt.Println("Check your internet connection or try again later.")
		return
	}

	if len(rl.Relays) == 0 {
		fmt.Println("No community relays available yet.")
		fmt.Println()
		fmt.Println("Be the first! Run a relay node and submit a PR:")
		fmt.Println("  laggado relay serve --help")
		return
	}

	fmt.Printf("Found %d relay node(s). Testing latency...\n\n", len(rl.Relays))

	results := rc.ProbeAll(rl)

	fmt.Printf("  %-4s %-22s %-6s %-6s %8s  %s\n", "Rank", "Name", "Region", "Country", "Latency", "Note")
	fmt.Printf("  %-4s %-22s %-6s %-6s %8s  %s\n", "----", "----------------------", "------", "-------", "--------", "----")

	for i, r := range results {
		latency := "timeout"
		if r.Reachable {
			latency = fmt.Sprintf("%.1fms", r.LatencyMS())
		}
		fmt.Printf("  #%-3d %-22s %-6s %-7s %8s  %s\n",
			i+1, r.Relay.Name, r.Relay.Region, r.Relay.Country, latency, r.Relay.Note)
	}

	fmt.Println()
	fmt.Println("To connect: laggado relays connect --relay <name> --target <game-server-ip>")
}

func cmdRelaysConnect(rc *relay.Client) {
	requireElevated("relays connect")

	var relayName, targetIP string
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--relay":
			if i+1 < len(args) {
				i++
				relayName = args[i]
			}
		case "--target":
			if i+1 < len(args) {
				i++
				targetIP = args[i]
			}
		}
	}

	if relayName == "" || targetIP == "" {
		fmt.Println("Usage: laggado relays connect --relay <name> --target <game-server-ip>")
		fmt.Println()
		fmt.Println("  --relay <name>   relay name from 'laggado relays list'")
		fmt.Println("  --target <ip>    game server IP (CS2: open console, type 'status')")
		return
	}

	if err := validateIP(targetIP); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return
	}

	if db.Config.WireGuardPrivateKey == "" {
		fmt.Println("No WireGuard keys. Generating now...")
		if !wg.IsAvailable() {
			fmt.Println("WireGuard not installed. Get it from: https://www.wireguard.com/install/")
			return
		}
		privKey, pubKey, err := wg.GenerateKeys()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR generating keys: %v\n", err)
			return
		}
		db.Config.WireGuardPrivateKey = privKey
		db.Config.WireGuardPublicKey = pubKey
		db.Save()
		fmt.Printf("Keys generated. Public key: %s\n\n", pubKey)
	}

	fmt.Println("Fetching relay list...")
	rl, err := rc.FetchRelays()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return
	}

	// Find the requested relay
	var selectedRelay *relay.RelayInfo
	for i := range rl.Relays {
		if strings.EqualFold(rl.Relays[i].Name, relayName) {
			selectedRelay = &rl.Relays[i]
			break
		}
	}
	if selectedRelay == nil {
		fmt.Printf("Relay %q not found in community list.\n", relayName)
		fmt.Println("Run 'laggado relays list' to see available relays.")
		return
	}

	fmt.Printf("Joining relay %s (%s, %s)...\n", selectedRelay.Name, selectedRelay.City, selectedRelay.Country)

	jr, err := rc.Join(*selectedRelay, db.Config.WireGuardPublicKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR joining relay: %v\n", err)
		fmt.Println()
		fmt.Println("Troubleshooting:")
		fmt.Println("  - Relay may be offline or at capacity")
		fmt.Println("  - Try a different relay: laggado relays list")
		return
	}

	fmt.Printf("Assigned tunnel IP: %s\n", jr.ClientIP)
	fmt.Printf("Relay endpoint: %s\n", jr.ServerEndpoint)

	// Activate WireGuard split tunnel using info from relay
	session, err := wg.ActivateSplitTunnel(
		db.Config.WireGuardPrivateKey,
		jr.ServerPublicKey,
		jr.ServerEndpoint,
		jr.ClientIPCIDR,
		targetIP,
		jr.GatewayIP,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR activating tunnel: %v\n", err)
		rc.Leave(*selectedRelay, db.Config.WireGuardPublicKey)
		return
	}

	// Save session for disconnect
	db.Config.ActiveTunnelTargetIP = session.ServerIP
	db.Config.ActiveTunnelGatewayIP = session.GatewayIP
	db.Save()

	// Quick latency test
	fmt.Println()
	fmt.Println("Tunnel active. Testing latency...")
	tester := routetest.NewTester()
	tester.Count = 5
	if m, err := tester.MeasureDirect(targetIP); err == nil {
		fmt.Printf("Latency via relay: avg=%.1fms jitter=%.1fms loss=%.1f%%\n",
			float64(m.AvgLatency.Microseconds())/1000.0,
			float64(m.Jitter.Microseconds())/1000.0,
			m.PacketLoss)
	}

	fmt.Println()
	fmt.Printf("Routing %s through %s.\n", targetIP, selectedRelay.Name)
	fmt.Println("To stop: laggado relays disconnect")
}

func cmdRelaysDisconnect(rc *relay.Client) {
	requireElevated("relays disconnect")

	session := &tunnel.TunnelSession{
		TunnelName: "laggado",
		ServerIP:   db.Config.ActiveTunnelTargetIP,
		GatewayIP:  db.Config.ActiveTunnelGatewayIP,
		Mask:       "255.255.255.255",
	}

	if err := wg.DeactivateSplitTunnel(session); err != nil {
		fmt.Printf("Note: %v\n", err)
	}

	db.Config.ActiveTunnelTargetIP = ""
	db.Config.ActiveTunnelGatewayIP = ""
	db.Save()

	fmt.Println("Disconnected from relay.")
}

// ─── RELAY SERVER (for community contributors) ────────────────────────────────

func cmdRelayServer() {
	sub := ""
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}

	switch sub {
	case "serve", "start":
		cmdRelayServe()
	default:
		fmt.Println("=== LAGGADO — Relay Server ===")
		fmt.Println()
		fmt.Println("Contribute your machine/VPS as a community relay node.")
		fmt.Println("Other LAGGADO users in different regions will be able to")
		fmt.Println("route their game traffic through you — for free.")
		fmt.Println()
		fmt.Println("Usage: laggado relay serve [options]")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  --wg-interface <name>    WireGuard interface (default: wg0)")
		fmt.Println("  --wg-port <port>         WireGuard listen port (default: 51820)")
		fmt.Println("  --api-port <port>         Management API port (default: 7734)")
		fmt.Println("  --subnet <CIDR>          Client IP pool (default: 10.100.1.0/24)")
		fmt.Println("  --gateway <IP>           Tunnel gateway IP (default: 10.100.1.1)")
		fmt.Println("  --public-ip <IP>         Your public IP (auto-detected if omitted)")
		fmt.Println()
		fmt.Println("Requirements:")
		fmt.Println("  1. WireGuard installed and wg0 interface set up")
		fmt.Println("  2. Port UDP/51820 open on firewall")
		fmt.Println("  3. Port TCP/7734 open on firewall (management API)")
		fmt.Println("  4. IP forwarding enabled: net.ipv4.ip_forward=1")
		fmt.Println()
		fmt.Println("Setup guide: laggado tunnel guide")
		fmt.Println()
		fmt.Println("After starting, submit a PR to add your relay to the community list:")
		fmt.Println("  https://github.com/laggado/laggado/blob/main/relay.json")
	}
}

func cmdRelayServe() {
	// Parse flags
	wgInterface := "wg0"
	wgPort := 51820
	apiPort := 7734
	subnet := "10.100.1.0/24"
	gateway := "10.100.1.1"
	publicIP := ""

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--wg-interface":
			if i+1 < len(args) {
				i++
				wgInterface = args[i]
			}
		case "--wg-port":
			if i+1 < len(args) {
				i++
				fmt.Sscanf(args[i], "%d", &wgPort)
			}
		case "--api-port":
			if i+1 < len(args) {
				i++
				fmt.Sscanf(args[i], "%d", &apiPort)
			}
		case "--subnet":
			if i+1 < len(args) {
				i++
				subnet = args[i]
			}
		case "--gateway":
			if i+1 < len(args) {
				i++
				gateway = args[i]
			}
		case "--public-ip":
			if i+1 < len(args) {
				i++
				publicIP = args[i]
			}
		}
	}

	// Auto-detect public IP if not provided
	if publicIP == "" {
		publicIP = detectPublicIP()
		if publicIP == "" {
			fmt.Println("Could not auto-detect public IP. Use --public-ip <IP>")
			return
		}
		fmt.Printf("Detected public IP: %s\n", publicIP)
	}

	// Get WireGuard public key
	if !wg.IsAvailable() {
		fmt.Println("WireGuard not installed. Get it from: https://www.wireguard.com/install/")
		return
	}

	serverPubKey := getWGInterfaceKey(wgInterface)
	if serverPubKey == "" {
		fmt.Printf("Could not get public key for WireGuard interface %q.\n", wgInterface)
		fmt.Printf("Make sure wg0 is up: wg show %s\n", wgInterface)
		return
	}

	endpoint := fmt.Sprintf("%s:%d", publicIP, wgPort)
	apiAddr := fmt.Sprintf("0.0.0.0:%d", apiPort)

	srv, err := relay.NewServer(wgInterface, wg.WgExe, serverPubKey, endpoint, gateway, subnet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return
	}

	fmt.Println("=== LAGGADO Relay Server ===")
	fmt.Println()
	fmt.Printf("WireGuard interface: %s\n", wgInterface)
	fmt.Printf("Server endpoint:     %s\n", endpoint)
	fmt.Printf("Server public key:   %s\n", serverPubKey)
	fmt.Printf("Client subnet:       %s\n", subnet)
	fmt.Printf("Management API:      http://%s\n", apiAddr)
	fmt.Println()
	fmt.Println("Add this to relay.json to share with the community:")
	fmt.Printf(`  {
    "name": "XX-CITY-1",
    "region": "SA",
    "country": "BR",
    "city": "São Paulo",
    "api": "http://%s:%d",
    "wgPort": %d,
    "operator": "your-github-handle",
    "note": "Oracle Free Tier"
  }`+"\n", publicIP, apiPort, wgPort)
	fmt.Println()
	fmt.Println("Listening for connections... (Ctrl+C to stop)")

	if err := srv.Serve(apiAddr); err != nil {
		fmt.Fprintf(os.Stderr, "Relay server error: %v\n", err)
	}
}

func detectPublicIP() string {
	client := &http.Client{Timeout: 5 * time.Second}
	for _, url := range []string{
		"https://api.ipify.org",
		"https://ipv4.icanhazip.com",
		"https://checkip.amazonaws.com",
	} {
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		buf := make([]byte, 64)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		ip := strings.TrimSpace(string(buf[:n]))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

func getWGInterfaceKey(iface string) string {
	if wg.WgExe == "" {
		return ""
	}
	out, err := exec.Command(wg.WgExe, "show", iface, "public-key").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ─── TUNNEL ─────────────────────────────────────────────────────────────────

func cmdTunnel() {
	sub := ""
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}

	switch sub {
	case "genkey", "gen-key":
		cmdTunnelGenKey()
	case "configure", "config", "setup":
		cmdTunnelConfigure()
	case "activate", "start":
		cmdTunnelActivate()
	case "stop", "deactivate":
		cmdTunnelStop()
	case "status":
		cmdTunnelStatus()
	case "vps-config":
		cmdTunnelVPSConfig()
	case "guide":
		cmdTunnelGuide()
	default:
		fmt.Println("=== LAGGADO — Tunnel Management ===")
		fmt.Println()
		fmt.Println("Usage: laggado tunnel <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  genkey                                         Generate WireGuard key pair")
		fmt.Println("  configure --vps <name> --peer-key <KEY>")
		fmt.Println("            --peer-addr <IP:PORT>")
		fmt.Println("            --tunnel-addr <CIDR> --gateway <IP>  Configure WireGuard peer for a VPS")
		fmt.Println("  activate --target <IP> --via <vps-name>        Activate split tunnel for game server")
		fmt.Println("  stop                                           Stop active tunnel and remove routes")
		fmt.Println("  status                                         Show WireGuard status")
		fmt.Println()
		fmt.Println("Quick start for CS2 (Portugal → São Paulo):")
		fmt.Println("  1. laggado tunnel guide             (full free VPS setup guide)")
		fmt.Println("  2. laggado tunnel genkey            (generate your WireGuard keys)")
		fmt.Println("  3. laggado tunnel vps-config        (generate server-side config to paste on VPS)")
		fmt.Println("  4. laggado tunnel configure --vps BR-SP --peer-key <VPS_PUBKEY> --peer-addr <VPS_IP>:51820 --tunnel-addr 10.66.66.2/32 --gateway 10.66.66.1")
		fmt.Println("  5. laggado tunnel activate --target <cs2-server-ip> --via BR-SP")
		fmt.Println("  6. laggado tunnel stop   (when done)")
	}
}

func cmdTunnelGenKey() {
	fmt.Println("=== LAGGADO — Generate WireGuard Keys ===")
	fmt.Println()

	if !wg.IsAvailable() {
		fmt.Println("WireGuard is not installed.")
		fmt.Println("Download and install it from: https://www.wireguard.com/install/")
		return
	}

	// Check if keys already exist
	if db.Config.WireGuardPrivateKey != "" {
		fmt.Println("WireGuard keys already generated.")
		fmt.Printf("Public key: %s\n", db.Config.WireGuardPublicKey)
		fmt.Println()
		fmt.Println("To regenerate, delete wireguardPrivateKey from ~/.laggado/config.json first.")
		return
	}

	privKey, pubKey, err := wg.GenerateKeys()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return
	}

	db.Config.WireGuardPrivateKey = privKey
	db.Config.WireGuardPublicKey = pubKey
	if err := db.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Could not save keys: %v\n", err)
	}

	fmt.Println("Keys generated and saved.")
	fmt.Println()
	fmt.Printf("Your PUBLIC KEY (add this to your VPS WireGuard config):\n")
	fmt.Printf("  %s\n", pubKey)
	fmt.Println()
	fmt.Println("VPS WireGuard [Peer] section example:")
	fmt.Printf("[Peer]\nPublicKey = %s\nAllowedIPs = 10.66.66.2/32\n", pubKey)
	fmt.Println()
	fmt.Println("Next step: laggado tunnel configure --vps <name> --peer-key <VPS_PUBKEY> ...")
}

func cmdTunnelConfigure() {
	fmt.Println("=== LAGGADO — Configure WireGuard Peer ===")
	fmt.Println()

	var vpsName, peerKey, peerAddr, tunnelAddr, gateway string

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--vps":
			if i+1 < len(args) {
				i++
				vpsName = args[i]
			}
		case "--peer-key":
			if i+1 < len(args) {
				i++
				peerKey = args[i]
			}
		case "--peer-addr":
			if i+1 < len(args) {
				i++
				peerAddr = args[i]
			}
		case "--tunnel-addr":
			if i+1 < len(args) {
				i++
				tunnelAddr = args[i]
			}
		case "--gateway":
			if i+1 < len(args) {
				i++
				gateway = args[i]
			}
		}
	}

	if vpsName == "" || peerKey == "" || peerAddr == "" {
		fmt.Println("Usage: laggado tunnel configure --vps <name> --peer-key <KEY> --peer-addr <IP:PORT>")
		fmt.Println("                                [--tunnel-addr <CIDR>] [--gateway <IP>]")
		fmt.Println()
		fmt.Println("  --vps <name>          VPS name (must match a configured VPS endpoint)")
		fmt.Println("  --peer-key <KEY>      VPS WireGuard public key")
		fmt.Println("  --peer-addr <IP:PORT> VPS IP and WireGuard port (default: 51820)")
		fmt.Println("  --tunnel-addr <CIDR>  Client tunnel IP (default: 10.66.66.2/32)")
		fmt.Println("  --gateway <IP>        VPS tunnel gateway (default: 10.66.66.1)")
		return
	}

	// Apply defaults
	if tunnelAddr == "" {
		tunnelAddr = "10.66.66.2/32"
	}
	if gateway == "" {
		gateway = "10.66.66.1"
	}
	// Add default WireGuard port if not specified
	if !strings.Contains(peerAddr, ":") {
		peerAddr = peerAddr + ":51820"
	}

	if db.Config.WireGuardPrivateKey == "" {
		fmt.Println("No WireGuard keys found. Run: laggado tunnel genkey")
		return
	}

	if db.Config.WireGuardPeers == nil {
		db.Config.WireGuardPeers = make(map[string]store.WireGuardPeer)
	}
	db.Config.WireGuardPeers[vpsName] = store.WireGuardPeer{
		VPSName:       vpsName,
		PeerPublicKey: peerKey,
		PeerEndpoint:  peerAddr,
		TunnelAddress: tunnelAddr,
		GatewayIP:     gateway,
	}

	if err := db.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR saving config: %v\n", err)
		return
	}

	fmt.Printf("VPS peer configured: %s\n", vpsName)
	fmt.Printf("  Peer endpoint:  %s\n", peerAddr)
	fmt.Printf("  Client tunnel:  %s\n", tunnelAddr)
	fmt.Printf("  Gateway:        %s\n", gateway)
	fmt.Println()
	fmt.Println("Next: laggado tunnel activate --target <game-server-ip> --via " + vpsName)
}

func cmdTunnelActivate() {
	requireElevated("tunnel activate")

	fmt.Println("=== LAGGADO — Activate Split Tunnel ===")
	fmt.Println()

	var targetIP, viaName string
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target":
			if i+1 < len(args) {
				i++
				targetIP = args[i]
			}
		case "--via":
			if i+1 < len(args) {
				i++
				viaName = args[i]
			}
		}
	}

	if targetIP == "" || viaName == "" {
		fmt.Println("Usage: laggado tunnel activate --target <game-server-ip> --via <vps-name>")
		fmt.Println()
		fmt.Println("  --target <IP>    Game server IP (find it in CS2 console: type 'status')")
		fmt.Println("  --via <name>     VPS name to route through (must have WireGuard peer configured)")
		return
	}

	if err := validateIP(targetIP); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return
	}

	if !wg.IsAvailable() {
		fmt.Println("WireGuard not installed. Get it from: https://www.wireguard.com/install/")
		return
	}

	if db.Config.WireGuardPrivateKey == "" {
		fmt.Println("No WireGuard keys. Run: laggado tunnel genkey")
		return
	}

	peer, ok := db.Config.WireGuardPeers[viaName]
	if !ok {
		fmt.Printf("No WireGuard peer configured for %q.\n", viaName)
		fmt.Printf("Run: laggado tunnel configure --vps %s --peer-key <KEY> --peer-addr <IP:PORT>\n", viaName)
		return
	}

	fmt.Printf("Routing %s through %s (%s)...\n", targetIP, viaName, peer.PeerEndpoint)

	session, err := wg.ActivateSplitTunnel(
		db.Config.WireGuardPrivateKey,
		peer.PeerPublicKey,
		peer.PeerEndpoint,
		peer.TunnelAddress,
		targetIP,
		peer.GatewayIP,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		fmt.Println()
		fmt.Println("Troubleshooting:")
		fmt.Println("  - Make sure you're running as Administrator")
		fmt.Println("  - Verify WireGuard server is running on the VPS")
		fmt.Println("  - Check that the VPS public key and endpoint are correct")
		return
	}

	// Save session info so "tunnel stop" can clean up the route.
	db.Config.ActiveTunnelTargetIP = session.ServerIP
	db.Config.ActiveTunnelGatewayIP = session.GatewayIP
	db.Save()

	// Quick latency test after tunnel comes up
	fmt.Println()
	fmt.Println("Tunnel active. Testing latency...")
	tester := routetest.NewTester()
	tester.Count = 5
	if m, err := tester.MeasureDirect(targetIP); err == nil {
		fmt.Printf("Latency via tunnel: avg=%.1fms jitter=%.1fms loss=%.1f%%\n",
			float64(m.AvgLatency.Microseconds())/1000.0,
			float64(m.Jitter.Microseconds())/1000.0,
			m.PacketLoss)
	}

	fmt.Println()
	fmt.Printf("Split tunnel active: all traffic to %s routes through %s.\n", targetIP, viaName)
	fmt.Println("To stop: laggado tunnel stop")
}

func cmdTunnelStop() {
	requireElevated("tunnel stop")

	fmt.Println("=== LAGGADO — Stop Tunnel ===")
	fmt.Println()

	if !wg.IsAvailable() {
		fmt.Println("WireGuard not installed.")
		return
	}

	// Use the session info saved during activate for accurate route cleanup.
	session := &tunnel.TunnelSession{
		TunnelName: "laggado",
		ServerIP:   db.Config.ActiveTunnelTargetIP,
		GatewayIP:  db.Config.ActiveTunnelGatewayIP,
		Mask:       "255.255.255.255",
	}

	if err := wg.DeactivateSplitTunnel(session); err != nil {
		// Non-fatal: report but continue (tunnel interface may already be down)
		fmt.Printf("Note: %v\n", err)
	}

	// Clear saved session
	db.Config.ActiveTunnelTargetIP = ""
	db.Config.ActiveTunnelGatewayIP = ""
	db.Save()

	fmt.Println("Tunnel stopped and routes removed.")
}

// cmdTunnelVPSConfig prints the server-side WireGuard config ready to paste on the VPS.
// The user's public key (generated with "tunnel genkey") is embedded in the [Peer] block.
func cmdTunnelVPSConfig() {
	if db.Config.WireGuardPublicKey == "" {
		fmt.Println("No keys generated yet. Run: laggado tunnel genkey")
		return
	}

	fmt.Println("=== WireGuard Server Config (paste this on your VPS) ===")
	fmt.Println()
	fmt.Println("Save as /etc/wireguard/wg0.conf on the VPS, then run:")
	fmt.Println("  sudo wg-quick up wg0")
	fmt.Println("  sudo systemctl enable wg-quick@wg0")
	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────────")
	fmt.Println("# Generate VPS private key first:")
	fmt.Println("# wg genkey | tee /etc/wireguard/privatekey | wg pubkey > /etc/wireguard/publickey")
	fmt.Println("# cat /etc/wireguard/privatekey   ← paste as PrivateKey below")
	fmt.Println("# cat /etc/wireguard/publickey    ← use in 'laggado tunnel configure --peer-key'")
	fmt.Println()
	fmt.Printf(`[Interface]
PrivateKey = <VPS_PRIVATE_KEY>
Address = 10.66.66.1/24
ListenPort = 51820
PostUp   = iptables -A FORWARD -i wg0 -j ACCEPT; iptables -t nat -A POSTROUTING -o $(ip route | awk '/default/{print $5;exit}') -j MASQUERADE
PostDown = iptables -D FORWARD -i wg0 -j ACCEPT; iptables -t nat -D POSTROUTING -o $(ip route | awk '/default/{print $5;exit}') -j MASQUERADE

[Peer]
# LAGGADO client (your Windows machine)
PublicKey = %s
AllowedIPs = 10.66.66.2/32
`, db.Config.WireGuardPublicKey)
	fmt.Println("─────────────────────────────────────────────────────")
	fmt.Println()
	fmt.Println("Also enable IP forwarding on the VPS:")
	fmt.Println("  echo 'net.ipv4.ip_forward=1' | sudo tee -a /etc/sysctl.conf")
	fmt.Println("  sudo sysctl -p")
	fmt.Println()
	fmt.Println("After setting up, run on client:")
	fmt.Println("  laggado tunnel configure --vps BR-SP \\")
	fmt.Println("    --peer-key <VPS_PUBLIC_KEY> \\")
	fmt.Println("    --peer-addr <VPS_IP>:51820 \\")
	fmt.Println("    --tunnel-addr 10.66.66.2/32 \\")
	fmt.Println("    --gateway 10.66.66.1")
}

// cmdTunnelGuide prints the full free VPS setup guide using Oracle Cloud Always Free Tier.
func cmdTunnelGuide() {
	fmt.Println(`
╔══════════════════════════════════════════════════════════════════════════╗
║  LAGGADO — Guia de Setup Gratuito: Oracle Cloud + WireGuard + CS2       ║
║  Meta: Portugal → CS2 São Paulo em <130ms (grátis para sempre)          ║
╚══════════════════════════════════════════════════════════════════════════╝

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 PASSO 1 — Criar conta Oracle Cloud (gratuita para sempre)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 1. Acessa: cloud.oracle.com → "Start for free"
 2. Cria conta (pede cartão mas NÃO debita — apenas verificação)
 3. IMPORTANTE: escolhe Home Region = "Brazil East (São Paulo)"
    Não podes mudar depois!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 PASSO 2 — Criar a VM em São Paulo
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 1. Menu → Compute → Instances → Create Instance
 2. Configurações:
    - Shape: VM.Standard.E2.1.Micro (Always Free)
    - Image: Ubuntu 22.04 (ou Debian 11)
    - Region/AD: sa-saopaulo-1 (São Paulo)
    - Adiciona a tua chave SSH pública
 3. Network: deixa o Security List padrão
 4. Create Instance → aguarda 2-3 minutos

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 PASSO 3 — Abrir porta UDP 51820 no Oracle Cloud
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 1. Na VM → Primary VNIC → Subnet → Security List
 2. Add Ingress Rule:
    - Source CIDR: 0.0.0.0/0
    - IP Protocol: UDP
    - Destination Port: 51820
 3. Também no firewall do Ubuntu (dentro da VM):
    sudo iptables -I INPUT -p udp --dport 51820 -j ACCEPT
    (ou: sudo ufw allow 51820/udp)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 PASSO 4 — Instalar e configurar WireGuard na VM
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 SSH na VM (ssh ubuntu@<IP_DA_VM>) e corre:

   sudo apt update && sudo apt install -y wireguard

   # Gerar chaves do servidor
   wg genkey | sudo tee /etc/wireguard/server_private.key | \
     wg pubkey | sudo tee /etc/wireguard/server_public.key

   # Ver chaves (guarda o PUBLIC KEY — vais precisar no LAGGADO)
   cat /etc/wireguard/server_public.key

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 PASSO 5 — Configurar o LAGGADO (Windows)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 No Windows, com LAGGADO:

   laggado tunnel genkey
   → mostra o teu PUBLIC KEY → usa no passo 6

   laggado tunnel vps-config
   → gera o ficheiro wg0.conf para colar na VM

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 PASSO 6 — Criar /etc/wireguard/wg0.conf na VM
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Cole o output de "laggado tunnel vps-config" em /etc/wireguard/wg0.conf
 e substitua <VPS_PRIVATE_KEY> pelo conteúdo de:
   cat /etc/wireguard/server_private.key

   sudo wg-quick up wg0
   sudo systemctl enable wg-quick@wg0

   # Ativar IP forwarding
   echo 'net.ipv4.ip_forward=1' | sudo tee -a /etc/sysctl.conf
   sudo sysctl -p

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 PASSO 7 — Ligar o LAGGADO ao VPS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   # Adicionar VPS (--extra-ms 5 = latência Oracle→Valve em SP)
   laggado config add-vps BR-SP <IP_DA_VM> 22 --extra-ms 5

   # Configurar peer WireGuard
   laggado tunnel configure \
     --vps BR-SP \
     --peer-key <VPS_PUBLIC_KEY> \
     --peer-addr <IP_DA_VM>:51820 \
     --tunnel-addr 10.66.66.2/32 \
     --gateway 10.66.66.1

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 PASSO 8 — Usar com CS2
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   1. Abre CS2 → conecta num servidor São Paulo
   2. Abre a consola (tecla ~ ou F10) → digita: status
      → procura "server : X.X.X.X" — esse é o IP do servidor
   3. Corre LAGGADO como Administrador:
      laggado tunnel activate --target X.X.X.X --via BR-SP
   4. Verifica o ping no CS2 → deve estar em 115-130ms
   5. Para parar: laggado tunnel stop

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 RESULTADO ESPERADO
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   Sem LAGGADO:  ~250ms (roteamento genérico PT → SP)
   Com LAGGADO:  ~120-130ms (PT → Oracle SP → Valve SP)
   ExitLag:      ~115-127ms (infraestrutura dedicada GCORE/EdgeUno)

   Custo: 0€/mês (Oracle Cloud Always Free Tier)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 PROBLEMA? Execute para diagnosticar:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   laggado analyze --ip <IP_SERVIDOR_CS2>
   laggado tunnel status
`)
}

func cmdTunnelStatus() {
	fmt.Println("=== LAGGADO — Tunnel Status ===")
	fmt.Println()

	if !wg.IsAvailable() {
		fmt.Println("WireGuard: not installed")
		fmt.Println("Download from: https://www.wireguard.com/install/")
		return
	}

	fmt.Println("WireGuard: installed")
	fmt.Println()

	status, err := wg.Status("laggado")
	if err != nil {
		fmt.Println("No active laggado tunnel.")
	} else {
		fmt.Println("Active tunnel (laggado):")
		fmt.Println(status)
	}

	if db.Config.WireGuardPublicKey != "" {
		fmt.Printf("Client public key: %s\n", db.Config.WireGuardPublicKey)
	} else {
		fmt.Println("No keys generated. Run: laggado tunnel genkey")
	}

	if len(db.Config.WireGuardPeers) > 0 {
		fmt.Printf("\nConfigured peers (%d):\n", len(db.Config.WireGuardPeers))
		for name, p := range db.Config.WireGuardPeers {
			fmt.Printf("  %-20s %s  tunnel: %s  gw: %s\n",
				name, p.PeerEndpoint, p.TunnelAddress, p.GatewayIP)
		}
	} else {
		fmt.Println("\nNo WireGuard peers configured.")
		fmt.Println("Run: laggado tunnel configure --vps <name> ...")
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func autoDetectServer() string {
	games, _ := gamedet.DetectGames()

	var allConns []connmon.Connection
	if len(games) > 0 {
		for _, g := range games {
			conns, err := connmon.GetConnectionsByPID(g.PID)
			if err == nil {
				allConns = append(allConns, conns...)
			}
		}
	} else {
		conns, err := connmon.GetAllConnections()
		if err == nil {
			allConns = conns
		}
	}

	best := serverid.IdentifyGameServer(allConns)
	if best == nil {
		return ""
	}
	return best.IP.String()
}

func timestamp() string {
	return time.Now().Format("15:04:05")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}

