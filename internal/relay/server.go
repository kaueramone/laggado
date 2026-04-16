// relay/server.go — LAGGADO community relay server.
// Run with: laggado relay serve ...
//
// Manages WireGuard peers dynamically via the wg CLI.
// No per-client pre-configuration needed: clients call POST /join
// and receive their WireGuard config in the response.
package relay

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

const peerExpiry = 24 * time.Hour

// PeerEntry tracks a registered client peer.
type PeerEntry struct {
	PublicKey    string
	ClientIP     string
	RegisteredAt time.Time
	LastSeen     time.Time
}

// Server manages WireGuard peers for community relay operation.
type Server struct {
	WgInterface    string // e.g., "wg0"
	WgExe          string // path to wg/wg.exe
	PublicKey      string // server's WireGuard public key
	ServerEndpoint string // publicly reachable IP:WGPort
	GatewayIP      string // tunnel gateway, e.g., "10.100.0.1"
	SubnetCIDR     string // client IP pool, e.g., "10.100.1.0/24"

	mu     sync.Mutex
	peers  map[string]*PeerEntry // keyed by public key
	nextIP net.IP
	ipNet  *net.IPNet
}

// NewServer creates a relay server.
func NewServer(wgInterface, wgExe, serverPublicKey, serverEndpoint, gatewayIP, subnetCIDR string) (*Server, error) {
	_, ipNet, err := net.ParseCIDR(subnetCIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet %q: %w", subnetCIDR, err)
	}

	// Start assigning from .10 to leave room for gateway and manual entries
	startIP := cloneIP(ipNet.IP)
	startIP[len(startIP)-1] = 10

	return &Server{
		WgInterface:    wgInterface,
		WgExe:          wgExe,
		PublicKey:      serverPublicKey,
		ServerEndpoint: serverEndpoint,
		GatewayIP:      gatewayIP,
		SubnetCIDR:     subnetCIDR,
		peers:          make(map[string]*PeerEntry),
		nextIP:         startIP,
		ipNet:          ipNet,
	}, nil
}

// Serve starts the HTTP management API and blocks until it returns an error.
func (s *Server) Serve(listenAddr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", s.handleInfo)
	mux.HandleFunc("/join", s.handleJoin)
	mux.HandleFunc("/leave", s.handleLeave)
	mux.HandleFunc("/peers", s.handlePeers)

	go s.cleanupLoop()

	return http.ListenAndServe(listenAddr, mux)
}

// ─── HTTP handlers ────────────────────────────────────────────────────────────

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	count := len(s.peers)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"serverPublicKey": s.PublicKey,
		"serverEndpoint":  s.ServerEndpoint,
		"gatewayIP":       s.GatewayIP,
		"subnet":          s.SubnetCIDR,
		"activePeers":     count,
	})
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req JoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClientPublicKey == "" {
		http.Error(w, "invalid request: missing clientPublicKey", http.StatusBadRequest)
		return
	}
	// WireGuard public keys are always 44 base64 chars
	if len(req.ClientPublicKey) < 40 || len(req.ClientPublicKey) > 50 {
		http.Error(w, "invalid public key format", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-join for already registered peers: refresh LastSeen and return existing config
	if existing, ok := s.peers[req.ClientPublicKey]; ok {
		existing.LastSeen = time.Now()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.buildJoinResponse(existing.ClientIP))
		return
	}

	clientIP := s.assignNextIP()
	if clientIP == "" {
		http.Error(w, "relay is full — no IPs available", http.StatusServiceUnavailable)
		return
	}

	if err := s.addWGPeer(req.ClientPublicKey, clientIP+"/32"); err != nil {
		http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
		return
	}

	s.peers[req.ClientPublicKey] = &PeerEntry{
		PublicKey:    req.ClientPublicKey,
		ClientIP:     clientIP,
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.buildJoinResponse(clientIP))
}

func (s *Server) handleLeave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "DELETE required", http.StatusMethodNotAllowed)
		return
	}

	var req JoinRequest
	json.NewDecoder(r.Body).Decode(&req)

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.peers[req.ClientPublicKey]; ok {
		s.removeWGPeer(req.ClientPublicKey)
		delete(s.peers, req.ClientPublicKey)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	count := len(s.peers)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"activePeers": count})
}

// ─── WireGuard peer management ────────────────────────────────────────────────

func (s *Server) addWGPeer(pubKey, allowedIPs string) error {
	out, err := exec.Command(s.WgExe, "set", s.WgInterface,
		"peer", pubKey,
		"allowed-ips", allowedIPs,
		"persistent-keepalive", "25",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wg set peer: %s — %s", err, out)
	}
	return nil
}

func (s *Server) removeWGPeer(pubKey string) {
	exec.Command(s.WgExe, "set", s.WgInterface, "peer", pubKey, "remove").Run()
}

// ─── IP assignment ────────────────────────────────────────────────────────────

func (s *Server) assignNextIP() string {
	for i := 0; i < 240; i++ {
		ip := s.nextIP.String()
		incrementIP(s.nextIP)

		// Skip if out of subnet or gateway conflict
		if !s.ipNet.Contains(net.ParseIP(ip)) {
			return "" // exhausted
		}
		if ip == s.GatewayIP {
			continue
		}

		// Check not already assigned
		inUse := false
		for _, p := range s.peers {
			if p.ClientIP == ip {
				inUse = true
				break
			}
		}
		if !inUse {
			return ip
		}
	}
	return ""
}

// ─── Peer expiry cleanup ──────────────────────────────────────────────────────

func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for key, peer := range s.peers {
			if time.Since(peer.LastSeen) > peerExpiry {
				s.removeWGPeer(peer.PublicKey)
				delete(s.peers, key)
			}
		}
		s.mu.Unlock()
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func (s *Server) buildJoinResponse(clientIP string) JoinResponse {
	return JoinResponse{
		ClientIP:        clientIP,
		ClientIPCIDR:    clientIP + "/32",
		ServerPublicKey: s.PublicKey,
		ServerEndpoint:  s.ServerEndpoint,
		GatewayIP:       s.GatewayIP,
	}
}

func cloneIP(ip net.IP) net.IP {
	clone := make(net.IP, len(ip))
	copy(clone, ip)
	return clone
}

func incrementIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}
