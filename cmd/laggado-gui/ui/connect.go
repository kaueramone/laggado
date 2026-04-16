package ui

// connect.go — full Lagger Network connect/disconnect flow.
//
// Sequence:
//   1. Get game server CIDRs from gameservers.GetRoutes(exe, region)
//   2. Query active Laggers from discovery.GetLaggers(region)
//   3. Probe latency to each Lagger endpoint
//   4. Call POST /join on the best Lagger's relay HTTP API
//   5. Activate WireGuard split tunnel with the game server CIDRs
//
// All network/blocking work runs in goroutines; UI callbacks are safe to call
// from any goroutine (Fyne handles synchronization internally).

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"laggado/internal/discovery"
	"laggado/internal/gameservers"
	"laggado/internal/relay"
	"laggado/internal/store"
)

const (
	defaultRelayPort = 7735    // LAGGADO relay HTTP API port
	probeTimeout     = 3 * time.Second
)

// StartConnect begins the full connect sequence in a background goroutine.
//
//   - onStatus(msg) — called multiple times with human-readable progress text
//   - onDone(err)   — called exactly once on completion; err is nil on success
func StartConnect(state *AppState, gc store.GameConnection, onStatus func(string), onDone func(error)) {
	go func() {
		err := runConnect(state, gc, onStatus)
		onDone(err)
	}()
}

// StopConnect tears down the active tunnel in a background goroutine.
func StopConnect(state *AppState, onStatus func(string), onDone func(error)) {
	go func() {
		state.connMu.Lock()
		tun := state.ActiveTunnel
		state.ActiveTunnel = nil
		state.ActiveConn = nil
		state.connMu.Unlock()

		if tun == nil {
			onDone(nil)
			return
		}

		onStatus("Desconectando...")

		if state.WG != nil {
			if err := state.WG.DeactivateSplitTunnel(tun); err != nil {
				// Report but don't block; tunnel service might already be gone
				onStatus("Aviso ao desconectar: " + err.Error())
			}
		}

		onStatus("Desconectado")
		onDone(nil)
	}()
}

// ── Internal connect flow ─────────────────────────────────────────────────────

func runConnect(state *AppState, gc store.GameConnection, onStatus func(string)) error {
	// Resolve region
	region := gc.Region
	if region == "" || region == "AUTO" {
		region = "SA"
	}

	// ── Step 1: Game server CIDRs ─────────────────────────────────────────────
	gameExe := resolveGameExe(gc)
	onStatus(fmt.Sprintf("Buscando rotas para %s (%s)…", gc.GameName, region))

	cidrs := gameservers.GetRoutes(gameExe, region)
	if len(cidrs) == 0 {
		return fmt.Errorf("%s não tem rotas conhecidas para a região %s.\n"+
			"Jogo suportado? Verifique em Biblioteca.", gc.GameName, region)
	}
	onStatus(fmt.Sprintf("  ✓ %d CIDRs encontrados para %s %s", len(cidrs), gameExe, region))

	// ── Step 2: Discover active Laggers ──────────────────────────────────────
	onStatus(fmt.Sprintf("Procurando Laggers ativos na região %s…", region))

	if state.Discovery == nil {
		return fmt.Errorf("discovery client não inicializado")
	}
	laggers, err := state.Discovery.GetLaggers(region)
	if err != nil {
		return fmt.Errorf("erro ao buscar Laggers: %w", err)
	}
	if len(laggers) == 0 {
		return fmt.Errorf("nenhum Lagger disponível na região %s 😔\n"+
			"Convide amigos para instalar o LAGGADO — cada instalação é um Lagger!\n"+
			"Enquanto isso, adicione um VPS próprio em Configurações.", region)
	}
	onStatus(fmt.Sprintf("  ✓ %d Lagger(s) encontrado(s) — testando latência…", len(laggers)))

	// ── Step 3: Probe latency ─────────────────────────────────────────────────
	type probeResult struct {
		lagger  discovery.LaggerInfo
		latency time.Duration
	}
	probed := make([]probeResult, 0, len(laggers))
	for _, l := range laggers {
		lat := probeLagger(l.Endpoint)
		if lat > 0 {
			probed = append(probed, probeResult{l, lat})
		}
	}

	if len(probed) == 0 {
		return fmt.Errorf("nenhum Lagger alcançável — verifique sua conexão ou firewall")
	}

	sort.Slice(probed, func(i, j int) bool {
		return probed[i].latency < probed[j].latency
	})

	best := probed[0]
	location := strings.TrimSpace(best.lagger.City + " " + best.lagger.Country)
	if location == "" {
		location = "desconhecido"
	}
	onStatus(fmt.Sprintf("  ✓ Melhor Lagger: %s — %d ms", location, best.latency.Milliseconds()))

	// ── Step 4: WireGuard key generation ─────────────────────────────────────
	if state.WG == nil {
		return fmt.Errorf("WireGuard manager não inicializado")
	}
	if !state.WG.IsAvailable() {
		return fmt.Errorf("WireGuard não instalado.\nBaixe em https://www.wireguard.com/install/ e reinicie o LAGGADO.")
	}

	onStatus("Gerando chaves WireGuard…")
	privateKey, publicKey, err := state.WG.GenerateKeys()
	if err != nil {
		return fmt.Errorf("gerar chaves WireGuard: %w", err)
	}

	// ── Step 5: Join best Lagger via relay HTTP API ───────────────────────────
	relayURL := laggerRelayAPI(best.lagger)
	if relayURL == "" {
		return fmt.Errorf("Lagger %s não expõe uma API relay — endpoint inválido", location)
	}
	onStatus(fmt.Sprintf("Conectando ao Lagger em %s…", relayURL))

	relayClient := relay.NewClient("", state.DataDir)
	joinResp, err := relayClient.Join(relay.RelayInfo{APIURL: relayURL}, publicKey)
	if err != nil {
		// Try next best Lagger if available
		if len(probed) > 1 {
			onStatus(fmt.Sprintf("  Lagger principal falhou (%v) — tentando próximo…", err))
			best = probed[1]
			relayURL = laggerRelayAPI(best.lagger)
			joinResp, err = relayClient.Join(relay.RelayInfo{APIURL: relayURL}, publicKey)
		}
		if err != nil {
			return fmt.Errorf("falha ao conectar ao Lagger: %w\n"+
				"O Lagger pode ter saído. Tente novamente em instantes.", err)
		}
	}
	onStatus("  ✓ Handshake WireGuard concluído")

	// ── Step 6: Activate split tunnel ─────────────────────────────────────────
	onStatus(fmt.Sprintf("Ativando split tunnel (%d rotas)…", len(cidrs)))

	session, err := state.WG.ActivateSplitTunnelCIDRs(
		privateKey,
		joinResp.ServerPublicKey,
		joinResp.ServerEndpoint,
		joinResp.ClientIPCIDR,
		cidrs,
		joinResp.GatewayIP,
	)
	if err != nil {
		return fmt.Errorf("ativar túnel WireGuard: %w\n"+
			"Certifique-se de estar executando como Administrador.", err)
	}

	// ── Store active session ───────────────────────────────────────────────────
	state.connMu.Lock()
	state.ActiveTunnel = session
	gcCopy := gc
	state.ActiveConn = &gcCopy
	state.connMu.Unlock()

	laggerDesc := location
	if best.lagger.Country != "" {
		laggerDesc = best.lagger.Country
		if best.lagger.City != "" {
			laggerDesc = best.lagger.City + ", " + best.lagger.Country
		}
	}
	onStatus(fmt.Sprintf("✓ Conectado! %s → %s via Lagger em %s (%d ms)",
		gc.GameName, region, laggerDesc, best.latency.Milliseconds()))

	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// resolveGameExe returns the process-name slug used by gameservers.GetRoutes.
// Priority: gc.GameExe (if it's a known game) → re-derive from game name.
// This protects against stale/wrong slugs stored in the database before fixes.
func resolveGameExe(gc store.GameConnection) string {
	if gc.GameExe != "" && gameservers.IsKnownGame(gc.GameExe) {
		return gc.GameExe
	}
	// Stored slug not recognized — always re-derive from the display name
	return gameNameToExeSlug(gc.GameName)
}

// gameNameToExeSlug maps common game display names to their exe slug.
// The gameservers package uses these slugs to look up CIDRs.
//
// NOTE: slugToName() converts hyphens to spaces, so we must match BOTH forms:
// "Counter-Strike 2" and "Counter Strike 2" both occur depending on the source.
func gameNameToExeSlug(name string) string {
	// Normalize: lowercase + collapse hyphens/underscores to spaces
	n := strings.ToLower(name)
	n = strings.ReplaceAll(n, "-", " ")
	n = strings.ReplaceAll(n, "_", " ")

	switch {
	case strings.Contains(n, "counter strike 2") || strings.Contains(n, "cs2") ||
		strings.Contains(n, "counter strike global") || strings.Contains(n, "csgo"):
		return "cs2"
	case strings.Contains(n, "counter strike"):
		return "cs2"
	case strings.Contains(n, "dota 2") || strings.Contains(n, "dota2"):
		return "dota2"
	case strings.Contains(n, "deadlock"):
		return "deadlock"
	case strings.Contains(n, "team fortress"):
		return "tf2"
	case strings.Contains(n, "valorant"):
		return "valorant"
	case strings.Contains(n, "league of legends"):
		return "leagueclient"
	case strings.Contains(n, "fortnite"):
		return "fortnite"
	case strings.Contains(n, "rocket league"):
		return "rocketleague"
	case strings.Contains(n, "battlefield 2042") || strings.Contains(n, "bf2042"):
		return "bf2042"
	case strings.Contains(n, "battlefield"):
		return "bf2042"
	case strings.Contains(n, "warzone") || strings.Contains(n, "call of duty") ||
		strings.Contains(n, "modern warfare"):
		return "modernwarfare"
	case strings.Contains(n, "apex legends") || strings.Contains(n, "apex"):
		return "r5apex"
	case strings.Contains(n, "pubg") || strings.Contains(n, "playerunknown"):
		return "pubg"
	case strings.Contains(n, "overwatch"):
		return "overwatch"
	case strings.Contains(n, "rainbow six") || strings.Contains(n, "r6"):
		return "rainbowsix"
	case strings.Contains(n, "rust"):
		return "rust"
	case strings.Contains(n, "minecraft"):
		return "minecraft"
	}
	// Fallback: use first word of game name lowercased
	parts := strings.Fields(n)
	if len(parts) > 0 {
		return parts[0]
	}
	return n
}

// laggerRelayAPI derives the relay HTTP API URL from a LaggerInfo.
// Uses RelayAPI if registered; otherwise derives from WireGuard endpoint IP + default port.
func laggerRelayAPI(l discovery.LaggerInfo) string {
	if l.RelayAPI != "" {
		return l.RelayAPI
	}
	// Derive: same IP as WireGuard endpoint, but relay HTTP port
	host, _, err := net.SplitHostPort(l.Endpoint)
	if err != nil || host == "" {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", host, defaultRelayPort)
}

// probeLagger measures TCP round-trip time to the Lagger's WireGuard endpoint.
// Returns 0 if unreachable.
func probeLagger(endpoint string) time.Duration {
	if endpoint == "" {
		return 0
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", endpoint, probeTimeout)
	elapsed := time.Since(start)
	if err != nil {
		return 0
	}
	conn.Close()
	return elapsed
}

