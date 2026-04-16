package ui

// lagger.go — tenta colocar este PC como um Lagger (relay node) automaticamente.
//
// Sequência:
//  1. Verifica WireGuard instalado
//  2. Gera/carrega chaves WireGuard do servidor (persistidas em DataDir)
//  3. Instala interface WireGuard "laggado-srv" como serviço Windows
//  4. Tenta UPnP para obter IP público e abrir portas — fallback: ipify
//  5. Sobe o relay HTTP na porta 7735
//  6. Registra no Cloudflare Worker (discovery)
//  7. Envia heartbeats a cada 2 min automaticamente (pelo discovery.Client)
//  8. Atualiza state.AmIALagger = true
//
// Se qualquer etapa falhar, o app continua normalmente no modo cliente.
// O usuário não vê erros — só vê "Modo cliente" no status da sidebar.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"laggado/internal/discovery"
	"laggado/internal/relay"
	"laggado/internal/upnp"
)

const (
	laggerSrvInterface = "laggado-srv"
	laggerWGPort       = 51820
	laggerRelayPort    = 7735
	laggerGatewayIP    = "10.100.0.1"
	laggerSubnet       = "10.100.1.0/24"
	laggerTunnelAddr   = "10.100.0.1/24"
)

// StartLaggerMode tenta registrar este PC como Lagger em background.
// Falha silenciosamente — o app nunca trava por causa disso.
func StartLaggerMode(state *AppState) {
	go func() {
		if err := tryBecomeLagger(state); err != nil {
			// Falha silenciosa: usuário fica no modo cliente
			_ = err
		}
	}()
}

func tryBecomeLagger(state *AppState) error {
	// ── 1. WireGuard disponível? ──────────────────────────────────────────────
	if state.WG == nil || !state.WG.IsAvailable() {
		return fmt.Errorf("WireGuard não instalado")
	}

	// ── 2. Chaves do servidor ─────────────────────────────────────────────────
	privKey, pubKey, err := loadOrGenerateServerKeys(state.DataDir, state.WG.WgExe)
	if err != nil {
		return fmt.Errorf("chaves servidor: %w", err)
	}

	// ── 3. Instala interface WireGuard servidor ("laggado-srv") ───────────────
	confPath, err := writeServerWGConf(state.DataDir, privKey)
	if err != nil {
		return fmt.Errorf("conf wg servidor: %w", err)
	}

	// Remove tunnel anterior se existir (ignora erro)
	if state.WG.WireGuardExe != "" {
		exec.Command(state.WG.WireGuardExe, "/uninstalltunnelservice", laggerSrvInterface).Run()
		time.Sleep(800 * time.Millisecond)
		if err := installServerTunnel(state.WG.WireGuardExe, confPath); err != nil {
			return fmt.Errorf("instalar tunnel servidor: %w", err)
		}
		time.Sleep(1500 * time.Millisecond)
	}

	// ── 4. IP público + UPnP ─────────────────────────────────────────────────
	publicIP, geoCity, geoCountry := resolvePublicPresence()
	if publicIP == "" {
		// Não tem IP público acessível — fica só como cliente
		return fmt.Errorf("IP público não detectado")
	}

	// ── 5. Relay HTTP server ──────────────────────────────────────────────────
	srvPubKey := pubKey
	endpoint := fmt.Sprintf("%s:%d", publicIP, laggerWGPort)
	relayAPIURL := fmt.Sprintf("http://%s:%d", publicIP, laggerRelayPort)
	apiListen := fmt.Sprintf("0.0.0.0:%d", laggerRelayPort)

	relaySrv, err := relay.NewServer(
		laggerSrvInterface,
		state.WG.WgExe,
		srvPubKey,
		endpoint,
		laggerGatewayIP,
		laggerSubnet,
	)
	if err != nil {
		return fmt.Errorf("criar relay server: %w", err)
	}

	go func() {
		_ = relaySrv.Serve(apiListen)
	}()
	time.Sleep(300 * time.Millisecond)

	// Confirma que a porta está respondendo antes de registrar
	if !portOpen(apiListen) {
		return fmt.Errorf("relay server não respondeu em %s", apiListen)
	}

	// ── 6. Registra no Worker ─────────────────────────────────────────────────
	workerURL := state.DB.Config.DiscoveryURL
	disc := state.Discovery
	if disc == nil {
		disc = discovery.NewClient(workerURL, state.DataDir)
	}

	region := state.DB.Config.PreferredRegion
	if region == "" {
		region = "SA"
	}

	disc.WgPublicKey = srvPubKey
	disc.Endpoint    = endpoint
	disc.RelayAPI    = relayAPIURL
	disc.Region      = region
	disc.City        = geoCity
	disc.Country     = geoCountry
	disc.Version     = "0.2.0"

	ctx := context.Background()
	if err := disc.Register(ctx); err != nil {
		return fmt.Errorf("registrar Lagger: %w", err)
	}

	// ── 7. Atualiza estado ────────────────────────────────────────────────────
	state.AmIALagger = true
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func loadOrGenerateServerKeys(dataDir, wgExe string) (priv, pub string, err error) {
	privFile := filepath.Join(dataDir, "lagger_wg.key")
	pubFile  := filepath.Join(dataDir, "lagger_wg.pub")

	// Tenta carregar do disco
	if privBytes, err := os.ReadFile(privFile); err == nil {
		if pubBytes, err := os.ReadFile(pubFile); err == nil {
			return strings.TrimSpace(string(privBytes)), strings.TrimSpace(string(pubBytes)), nil
		}
	}

	// Gera par novo
	privOut, err := exec.Command(wgExe, "genkey").Output()
	if err != nil {
		return "", "", fmt.Errorf("wg genkey: %w", err)
	}
	privKey := strings.TrimSpace(string(privOut))

	cmd := exec.Command(wgExe, "pubkey")
	cmd.Stdin = strings.NewReader(privKey + "\n")
	pubOut, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("wg pubkey: %w", err)
	}
	pubKey := strings.TrimSpace(string(pubOut))

	os.MkdirAll(dataDir, 0755)
	os.WriteFile(privFile, []byte(privKey), 0600)
	os.WriteFile(pubFile,  []byte(pubKey),  0644)

	return privKey, pubKey, nil
}

func writeServerWGConf(dataDir, privKey string) (string, error) {
	confDir := filepath.Join(dataDir, "wg")
	os.MkdirAll(confDir, 0755)
	confPath := filepath.Join(confDir, laggerSrvInterface+".conf")

	content := fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s\nListenPort = %d\n",
		privKey, laggerTunnelAddr, laggerWGPort)

	if err := os.WriteFile(confPath, []byte(content), 0600); err != nil {
		return "", err
	}
	return confPath, nil
}

func installServerTunnel(wireGuardExe, confPath string) error {
	out, err := exec.Command(wireGuardExe, "/installtunnelservice", confPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, out)
	}
	return nil
}

// resolvePublicPresence tenta obter IP público via UPnP primeiro, depois ipify.
// Retorna publicIP, city, country (city/country vazios se não obtidos via GeoIP).
func resolvePublicPresence() (publicIP, city, country string) {
	// Tenta UPnP
	if gw, err := upnp.Discover(); err == nil {
		if ip, err := gw.GetExternalIP(); err == nil && ip != "" {
			// Abre portas no roteador
			localIP, _ := upnp.LocalIP()
			if localIP != "" {
				gw.AddPortMapping("UDP", laggerWGPort,    localIP, laggerWGPort,    "LAGGADO WireGuard", 0)
				gw.AddPortMapping("TCP", laggerRelayPort, localIP, laggerRelayPort, "LAGGADO Relay API", 0)
			}
			publicIP = ip
		}
	}

	// Fallback: ipify
	if publicIP == "" {
		client := &http.Client{Timeout: 5 * time.Second}
		for _, u := range []string{
			"https://api.ipify.org",
			"https://ipv4.icanhazip.com",
		} {
			if resp, err := client.Get(u); err == nil {
				buf := make([]byte, 64)
				n, _ := resp.Body.Read(buf)
				resp.Body.Close()
				ip := strings.TrimSpace(string(buf[:n]))
				if ip != "" {
					publicIP = ip
					break
				}
			}
		}
	}

	return publicIP, city, country
}

// portOpen verifica se a porta está ouvindo (TCP) antes de registrar.
func portOpen(addr string) bool {
	// Tenta HTTP GET /info no relay server local
	url := "http://" + addr + "/info"
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}
