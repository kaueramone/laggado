// Package tunnel integrates with WireGuard for optional traffic routing.
// It shells out to wireguard.exe / wg.exe CLI tools — does NOT reimplement the protocol.
// Supports split tunneling via Windows route table manipulation.
package tunnel

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// Config represents a WireGuard tunnel configuration.
type Config struct {
	Name       string `json:"name"`       // Tunnel interface name
	ConfigPath string `json:"configPath"` // Path to .conf file
	Endpoint   string `json:"endpoint"`   // VPS endpoint address:port
	PublicKey  string `json:"publicKey"`
}

// WireGuard manages WireGuard tunnel operations.
type WireGuard struct {
	WgExe        string // path to wg.exe
	WireGuardExe string // path to wireguard.exe
	ConfigDir    string // directory for generated configs
}

// NewWireGuard creates a WireGuard manager, auto-detecting executable paths.
func NewWireGuard(configDir string) *WireGuard {
	wg := &WireGuard{
		ConfigDir: configDir,
	}

	// Try common install locations
	paths := []string{
		`C:\Program Files\WireGuard\wg.exe`,
		`C:\Program Files\WireGuard\wireguard.exe`,
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			if strings.HasSuffix(p, "wg.exe") {
				wg.WgExe = p
			} else {
				wg.WireGuardExe = p
			}
		}
	}

	// Fallback to PATH
	if wg.WgExe == "" {
		if p, err := exec.LookPath("wg.exe"); err == nil {
			wg.WgExe = p
		}
	}
	if wg.WireGuardExe == "" {
		if p, err := exec.LookPath("wireguard.exe"); err == nil {
			wg.WireGuardExe = p
		}
	}

	os.MkdirAll(configDir, 0755)
	return wg
}

// IsAvailable checks if WireGuard tools are installed.
func (w *WireGuard) IsAvailable() bool {
	return w.WireGuardExe != "" || w.WgExe != ""
}

// Status returns the current status of a WireGuard tunnel.
func (w *WireGuard) Status(tunnelName string) (string, error) {
	if w.WgExe == "" {
		return "", fmt.Errorf("wg.exe not found")
	}

	out, err := exec.Command(w.WgExe, "show", tunnelName).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("wg show %s: %s", tunnelName, string(out))
	}
	return string(out), nil
}

// InstallTunnel installs and activates a WireGuard tunnel from a config file.
func (w *WireGuard) InstallTunnel(configPath string) error {
	if w.WireGuardExe == "" {
		return fmt.Errorf("wireguard.exe not found — install WireGuard first")
	}

	out, err := exec.Command(w.WireGuardExe, "/installtunnelservice", configPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("install tunnel: %s: %s", err, string(out))
	}
	return nil
}

// UninstallTunnel stops and removes a WireGuard tunnel.
func (w *WireGuard) UninstallTunnel(tunnelName string) error {
	if w.WireGuardExe == "" {
		return fmt.Errorf("wireguard.exe not found")
	}

	out, err := exec.Command(w.WireGuardExe, "/uninstalltunnelservice", tunnelName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("uninstall tunnel: %s: %s", err, string(out))
	}
	return nil
}

// SplitRoute represents a route to add for split tunneling.
type SplitRoute struct {
	DestIP    string // e.g., "185.25.182.0"
	Mask      string // e.g., "255.255.255.0" or CIDR like "/24"
	Gateway   string // tunnel gateway IP
	Interface string // tunnel interface name
}

// AddSplitRoute adds a Windows route to direct specific traffic through the tunnel.
// Only the game server IP gets routed through the tunnel — everything else stays direct.
func AddSplitRoute(destIP, mask, gateway string) error {
	// route ADD destination MASK mask gateway METRIC 1
	out, err := exec.Command("route", "ADD", destIP, "MASK", mask, gateway, "METRIC", "1").CombinedOutput()
	if err != nil {
		return fmt.Errorf("route add: %s: %s", err, string(out))
	}
	return nil
}

// RemoveSplitRoute removes a previously added split route.
func RemoveSplitRoute(destIP, mask, gateway string) error {
	out, err := exec.Command("route", "DELETE", destIP, "MASK", mask, gateway).CombinedOutput()
	if err != nil {
		return fmt.Errorf("route delete: %s: %s", err, string(out))
	}
	return nil
}

// GenerateConfig creates a WireGuard config file for split tunneling to a specific game server.
var confTemplate = template.Must(template.New("wg").Parse(`[Interface]
PrivateKey = {{ .PrivateKey }}
Address = {{ .TunnelAddress }}
DNS = {{ .DNS }}

[Peer]
PublicKey = {{ .PeerPublicKey }}
Endpoint = {{ .PeerEndpoint }}
AllowedIPs = {{ .AllowedIPs }}
PersistentKeepalive = 25
`))

// WgConfigParams holds parameters for generating a WireGuard config.
type WgConfigParams struct {
	PrivateKey    string
	TunnelAddress string // e.g., "10.0.0.2/32"
	DNS           string // e.g., "1.1.1.1"
	PeerPublicKey string
	PeerEndpoint  string // e.g., "vps.example.com:51820"
	AllowedIPs    string // For split tunnel: just the game server IP, e.g., "185.25.182.0/24"
}

// GenerateConfigFile writes a WireGuard .conf file for the given parameters.
func (w *WireGuard) GenerateConfigFile(name string, params WgConfigParams) (string, error) {
	confPath := filepath.Join(w.ConfigDir, name+".conf")
	f, err := os.Create(confPath)
	if err != nil {
		return "", fmt.Errorf("create config: %w", err)
	}
	defer f.Close()

	if err := confTemplate.Execute(f, params); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}

	return confPath, nil
}

// GenerateKeys generates a WireGuard key pair using the installed wg.exe CLI.
// Returns (privateKey, publicKey, error). Keys are base64-encoded strings.
func (w *WireGuard) GenerateKeys() (privateKey, publicKey string, err error) {
	if w.WgExe == "" {
		return "", "", fmt.Errorf("wg.exe not found — install WireGuard from https://www.wireguard.com/install/")
	}

	privOut, err := exec.Command(w.WgExe, "genkey").Output()
	if err != nil {
		return "", "", fmt.Errorf("wg genkey: %w", err)
	}
	privKey := strings.TrimSpace(string(privOut))

	cmd := exec.Command(w.WgExe, "pubkey")
	cmd.Stdin = strings.NewReader(privKey + "\n")
	pubOut, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("wg pubkey: %w", err)
	}
	pubKey := strings.TrimSpace(string(pubOut))

	return privKey, pubKey, nil
}

// TunnelSession represents an active split-tunnel session managed by LAGGADO.
type TunnelSession struct {
	TunnelName string
	GatewayIP  string
	// CIDRRoutes lists all CIDR ranges routed through the tunnel (for cleanup).
	// Each entry is [network, mask] e.g. ["155.133.248.0", "255.255.248.0"].
	CIDRRoutes [][2]string
	// Legacy single-IP support (kept for backward compatibility).
	ServerIP string
	Mask     string
}

// ActivateSplitTunnel generates a WireGuard config, installs the tunnel service,
// and adds a host route so only the game server IP is routed through the tunnel.
//
// privateKey   — client's WireGuard private key (from GenerateKeys)
// peerPubKey   — VPS's WireGuard public key
// peerEndpoint — VPS address:port (e.g. "1.2.3.4:51820")
// tunnelAddr   — client tunnel IP in CIDR notation (e.g. "10.66.66.2/32")
// serverIP     — game server IP to route through the tunnel
// gatewayIP    — VPS tunnel gateway IP (e.g. "10.66.66.1")
func (w *WireGuard) ActivateSplitTunnel(privateKey, peerPubKey, peerEndpoint, tunnelAddr, serverIP, gatewayIP string) (*TunnelSession, error) {
	if !w.IsAvailable() {
		return nil, fmt.Errorf("WireGuard not installed — get it from https://www.wireguard.com/install/")
	}

	const tunnelName = "laggado"

	params := WgConfigParams{
		PrivateKey:    privateKey,
		TunnelAddress: tunnelAddr,
		DNS:           "1.1.1.1",
		PeerPublicKey: peerPubKey,
		PeerEndpoint:  peerEndpoint,
		AllowedIPs:    serverIP + "/32", // split tunnel: only game server IP
	}

	confPath, err := w.GenerateConfigFile(tunnelName, params)
	if err != nil {
		return nil, fmt.Errorf("generate config: %w", err)
	}

	// Remove any previous tunnel with the same name (ignore error if not present).
	_ = w.UninstallTunnel(tunnelName)
	time.Sleep(500 * time.Millisecond)

	if err := w.InstallTunnel(confPath); err != nil {
		return nil, fmt.Errorf("install tunnel: %w", err)
	}

	// Wait for the WireGuard interface to come up before adding the route.
	time.Sleep(2 * time.Second)

	if err := AddSplitRoute(serverIP, "255.255.255.255", gatewayIP); err != nil {
		_ = w.UninstallTunnel(tunnelName)
		return nil, fmt.Errorf("add split route: %w", err)
	}

	return &TunnelSession{
		TunnelName: tunnelName,
		ServerIP:   serverIP,
		GatewayIP:  gatewayIP,
		Mask:       "255.255.255.255",
	}, nil
}

// DeactivateSplitTunnel removes the split route and stops the WireGuard tunnel.
func (w *WireGuard) DeactivateSplitTunnel(session *TunnelSession) error {
	var errs []string

	// Remove CIDR routes (multi-range split tunnel)
	for _, route := range session.CIDRRoutes {
		if err := RemoveSplitRoute(route[0], route[1], session.GatewayIP); err != nil {
			errs = append(errs, fmt.Sprintf("remove route %s: %v", route[0], err))
		}
	}

	// Legacy single-IP route support
	if session.ServerIP != "" && session.Mask != "" && len(session.CIDRRoutes) == 0 {
		if err := RemoveSplitRoute(session.ServerIP, session.Mask, session.GatewayIP); err != nil {
			errs = append(errs, fmt.Sprintf("remove route: %v", err))
		}
	}

	if err := w.UninstallTunnel(session.TunnelName); err != nil {
		errs = append(errs, fmt.Sprintf("uninstall tunnel: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// ActivateSplitTunnelCIDRs is like ActivateSplitTunnel but accepts multiple game server
// CIDR ranges (e.g. from gameservers.GetRoutes). Each range gets its own Windows route entry.
//
// allowedCIDRs  — game server CIDR blocks, e.g. ["155.133.248.0/21", "185.25.182.0/24"]
// Other parameters: same as ActivateSplitTunnel.
func (w *WireGuard) ActivateSplitTunnelCIDRs(privateKey, peerPubKey, peerEndpoint, tunnelAddr string, allowedCIDRs []string, gatewayIP string) (*TunnelSession, error) {
	if !w.IsAvailable() {
		return nil, fmt.Errorf("WireGuard não instalado — baixe em https://www.wireguard.com/install/")
	}
	if len(allowedCIDRs) == 0 {
		return nil, fmt.Errorf("nenhum CIDR fornecido para split tunnel")
	}

	const tunnelName = "laggado"

	params := WgConfigParams{
		PrivateKey:    privateKey,
		TunnelAddress: tunnelAddr,
		DNS:           "1.1.1.1",
		PeerPublicKey: peerPubKey,
		PeerEndpoint:  peerEndpoint,
		AllowedIPs:    strings.Join(allowedCIDRs, ", "),
	}

	confPath, err := w.GenerateConfigFile(tunnelName, params)
	if err != nil {
		return nil, fmt.Errorf("gerar config WireGuard: %w", err)
	}

	// Remove any previous tunnel with the same name (ignore error if not present).
	_ = w.UninstallTunnel(tunnelName)
	time.Sleep(500 * time.Millisecond)

	if err := w.InstallTunnel(confPath); err != nil {
		return nil, fmt.Errorf("instalar túnel: %w", err)
	}

	// Wait for the WireGuard interface to come up before adding routes.
	time.Sleep(2 * time.Second)

	// Parse each CIDR and add a split route.
	var cidrRoutes [][2]string
	for _, cidr := range allowedCIDRs {
		network, mask, err := parseCIDR(cidr)
		if err != nil {
			_ = w.UninstallTunnel(tunnelName)
			return nil, fmt.Errorf("CIDR inválido %q: %w", cidr, err)
		}
		if err := AddSplitRoute(network, mask, gatewayIP); err != nil {
			// Best-effort cleanup
			for _, prev := range cidrRoutes {
				_ = RemoveSplitRoute(prev[0], prev[1], gatewayIP)
			}
			_ = w.UninstallTunnel(tunnelName)
			return nil, fmt.Errorf("adicionar rota %s: %w", cidr, err)
		}
		cidrRoutes = append(cidrRoutes, [2]string{network, mask})
	}

	return &TunnelSession{
		TunnelName: tunnelName,
		GatewayIP:  gatewayIP,
		CIDRRoutes: cidrRoutes,
	}, nil
}

// parseCIDR converts a CIDR string to (network address, dotted-decimal mask).
// e.g. "155.133.248.0/21" → ("155.133.248.0", "255.255.248.0")
func parseCIDR(cidr string) (network, mask string, err error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", err
	}
	return ipNet.IP.String(), net.IP(ipNet.Mask).String(), nil
}
