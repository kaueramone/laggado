// laggado-lagger — LAGGADO community relay node daemon.
//
// Compilar para Linux (a partir do Windows):
//   set GOOS=linux
//   set GOARCH=amd64
//   set CGO_ENABLED=0
//   go build -ldflags="-s -w" -o dist/laggado-lagger ./cmd/laggado-lagger
//
// Uso no VPS:
//   sudo ./laggado-lagger --region SA --public-ip <SEU_IP>
//
// Requisitos no VPS (Ubuntu/Debian):
//   sudo apt install wireguard
//   sudo ip link add wg0 type wireguard
//   wg genkey | tee /etc/wireguard/server.key | wg pubkey > /etc/wireguard/server.pub
//   sudo ip address add 10.100.0.1/24 dev wg0
//   sudo wg set wg0 listen-port 51820 private-key /etc/wireguard/server.key
//   sudo ip link set wg0 up
//   sudo sysctl -w net.ipv4.ip_forward=1
//   # Firewall: abrir UDP/51820 e TCP/7735
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"laggado/internal/discovery"
	"laggado/internal/relay"
)

const version = "0.2.0"

func main() {
	// ── Parse flags ───────────────────────────────────────────────────────────
	cfg := struct {
		wgInterface string
		wgPort      int
		apiPort     int
		subnet      string
		gateway     string
		publicIP    string
		region      string
		city        string
		country     string
		workerURL   string
	}{
		wgInterface: "wg0",
		wgPort:      51820,
		apiPort:     7735, // must match defaultRelayPort in connect.go
		subnet:      "10.100.1.0/24",
		gateway:     "10.100.0.1",
		region:      "SA",
		country:     "BR",
	}

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--wg-interface":
			cfg.wgInterface = next(args, &i)
		case "--wg-port":
			fmt.Sscanf(next(args, &i), "%d", &cfg.wgPort)
		case "--api-port":
			fmt.Sscanf(next(args, &i), "%d", &cfg.apiPort)
		case "--subnet":
			cfg.subnet = next(args, &i)
		case "--gateway":
			cfg.gateway = next(args, &i)
		case "--public-ip":
			cfg.publicIP = next(args, &i)
		case "--region":
			cfg.region = next(args, &i)
		case "--city":
			cfg.city = next(args, &i)
		case "--country":
			cfg.country = next(args, &i)
		case "--worker-url":
			cfg.workerURL = next(args, &i)
		case "--help", "-h":
			printHelp()
			os.Exit(0)
		}
	}

	// ── Auto-detect public IP ─────────────────────────────────────────────────
	if cfg.publicIP == "" {
		fmt.Print("Detectando IP público... ")
		cfg.publicIP = detectPublicIP()
		if cfg.publicIP == "" {
			fatalf("Não foi possível detectar o IP público. Use --public-ip <IP>")
		}
		fmt.Println(cfg.publicIP)
	}

	// ── Read WireGuard server public key ──────────────────────────────────────
	serverPubKey := getWGPubKey(cfg.wgInterface)
	if serverPubKey == "" {
		fatalf("Não foi possível ler a chave pública do WireGuard interface %q.\n"+
			"Verifique: sudo wg show %s", cfg.wgInterface, cfg.wgInterface)
	}

	endpoint := fmt.Sprintf("%s:%d", cfg.publicIP, cfg.wgPort)
	relayAPIURL := fmt.Sprintf("http://%s:%d", cfg.publicIP, cfg.apiPort)
	apiListen := fmt.Sprintf("0.0.0.0:%d", cfg.apiPort)

	// ── Start relay HTTP server ───────────────────────────────────────────────
	srv, err := relay.NewServer(
		cfg.wgInterface,
		"wg", // wg command is in PATH on Linux after apt install wireguard
		serverPubKey,
		endpoint,
		cfg.gateway,
		cfg.subnet,
	)
	if err != nil {
		fatalf("Erro ao criar relay server: %v", err)
	}

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║      LAGGADO Lagger Node  v" + version + "        ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  Interface WireGuard : %s\n", cfg.wgInterface)
	fmt.Printf("  Endpoint WireGuard  : %s  (UDP)\n", endpoint)
	fmt.Printf("  Chave pública WG    : %s\n", serverPubKey)
	fmt.Printf("  Relay HTTP API      : %s  (TCP)\n", relayAPIURL)
	fmt.Printf("  Subnet de clientes  : %s\n", cfg.subnet)
	fmt.Printf("  Região              : %s\n", cfg.region)
	fmt.Println()

	// Serve in background goroutine
	srvErr := make(chan error, 1)
	go func() {
		fmt.Printf("[relay] Escutando em %s...\n", apiListen)
		srvErr <- srv.Serve(apiListen)
	}()

	// Give the server a moment to bind
	time.Sleep(300 * time.Millisecond)

	// ── Register with Cloudflare Worker ───────────────────────────────────────
	dataDir := "/var/lib/laggado-lagger"
	os.MkdirAll(dataDir, 0755)

	disc := discovery.NewClient(cfg.workerURL, dataDir)
	disc.WgPublicKey = serverPubKey
	disc.Endpoint    = endpoint
	disc.RelayAPI    = relayAPIURL
	disc.Region      = cfg.region
	disc.City        = cfg.city
	disc.Country     = cfg.country
	disc.Version     = version

	fmt.Print("[discovery] Registrando no Lagger Network... ")
	ctx := context.Background()
	if err := disc.Register(ctx); err != nil {
		fmt.Printf("AVISO: %v\n", err)
		fmt.Println("           (continuando — tentará novamente em 2 min)")
	} else {
		fmt.Println("OK ✓")
		fmt.Printf("           ID: %s\n", disc.LaggerID())
	}

	fmt.Println()
	fmt.Println("Lagger ativo! Pressione Ctrl+C para parar.")
	fmt.Println()

	// ── Wait for signal ───────────────────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		fmt.Printf("\n[signal] Recebido %s — encerrando...\n", sig)
	case err := <-srvErr:
		fmt.Printf("[relay] Servidor parou com erro: %v\n", err)
	}

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	fmt.Print("[discovery] Saindo do Lagger Network... ")
	disc.Leave()
	fmt.Println("OK")
	fmt.Println("Encerrado.")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func next(args []string, i *int) string {
	*i++
	if *i < len(args) {
		return args[*i]
	}
	return ""
}

func fatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "[ERRO] "+format+"\n", a...)
	os.Exit(1)
}

func detectPublicIP() string {
	client := &http.Client{Timeout: 5 * time.Second}
	for _, u := range []string{
		"https://api.ipify.org",
		"https://ipv4.icanhazip.com",
		"https://checkip.amazonaws.com",
	} {
		resp, err := client.Get(u)
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if err == nil {
			ip := strings.TrimSpace(string(b))
			if ip != "" {
				return ip
			}
		}
	}
	return ""
}

func getWGPubKey(iface string) string {
	out, err := exec.Command("wg", "show", iface, "public-key").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func printHelp() {
	fmt.Println(`laggado-lagger — LAGGADO community relay node daemon

Uso:
  sudo ./laggado-lagger [opções]

Opções:
  --public-ip  <IP>    Seu IP público (detectado automaticamente se omitido)
  --region     <cod>   Região: SA (padrão), EU, NA, AS
  --city       <nome>  Cidade (ex: "Sao Paulo")
  --country    <cod>   País (ex: BR, PT, US)
  --wg-interface <if>  Interface WireGuard (padrão: wg0)
  --wg-port    <porta> Porta WireGuard UDP (padrão: 51820)
  --api-port   <porta> Porta relay HTTP (padrão: 7735)
  --subnet     <CIDR>  Pool de IPs para clientes (padrão: 10.100.1.0/24)
  --gateway    <IP>    Gateway do túnel (padrão: 10.100.0.1)
  --worker-url <URL>   URL do Worker de discovery (usa oficial se omitido)

Exemplo (VPS em São Paulo):
  sudo ./laggado-lagger --region SA --city "Sao Paulo" --country BR

Pré-requisitos:
  1. sudo apt install wireguard
  2. Configurar interface wg0 (veja /etc/wireguard/setup.sh gerado pelo LAGGADO)
  3. Abrir portas: UDP 51820 e TCP 7735 no firewall do VPS
  4. sudo sysctl -w net.ipv4.ip_forward=1
`)
}
