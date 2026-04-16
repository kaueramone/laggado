// Package upnp implements minimal UPnP IGD (Internet Gateway Device) support.
// It can automatically map a port on the user's home router so their machine
// becomes reachable as a LAGGADO relay without any manual configuration.
//
// Protocol:
//   1. SSDP multicast discovery → find the router
//   2. Fetch router's device description XML → find control URL
//   3. SOAP HTTP POST → AddPortMapping / GetExternalIPAddress / DeletePortMapping
package upnp

import (
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	ssdpAddr    = "239.255.255.250:1900"
	ssdpSearch  = "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n"
	ssdpTimeout = 3 * time.Second
)

// Gateway represents a UPnP-capable router with a WANIPConnection service.
type Gateway struct {
	ControlURL string // SOAP endpoint for port mapping actions
	ServiceType string
}

// Discover finds the first UPnP Internet Gateway Device on the local network.
// Returns ErrNotFound if no gateway responds within the timeout.
func Discover() (*Gateway, error) {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("upnp listen: %w", err)
	}
	defer conn.Close()

	dst, _ := net.ResolveUDPAddr("udp4", ssdpAddr)
	conn.(*net.UDPConn).WriteToUDP([]byte(ssdpSearch), dst)
	conn.SetDeadline(time.Now().Add(ssdpTimeout))

	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			break
		}
		resp := string(buf[:n])
		loc := extractHeader(resp, "LOCATION")
		if loc == "" {
			continue
		}

		gw, err := fetchGateway(loc)
		if err == nil {
			return gw, nil
		}
	}
	return nil, fmt.Errorf("no UPnP gateway found (router may have UPnP disabled)")
}

// GetExternalIP returns the WAN IP address assigned to the gateway.
func (g *Gateway) GetExternalIP() (string, error) {
	resp, err := g.soapCall("GetExternalIPAddress", "")
	if err != nil {
		return "", err
	}
	ip := extractXML(resp, "NewExternalIPAddress")
	if ip == "" {
		return "", fmt.Errorf("empty external IP in response")
	}
	return ip, nil
}

// AddPortMapping creates a port forwarding rule on the gateway.
// protocol: "TCP" or "UDP"
// externalPort: port exposed on the WAN side
// internalIP: LAN IP of this machine
// internalPort: port on this machine
// description: human-readable label
// leaseDuration: seconds (0 = permanent until reboot)
func (g *Gateway) AddPortMapping(protocol string, externalPort int, internalIP string, internalPort int, description string, leaseDuration int) error {
	body := fmt.Sprintf(
		`<NewRemoteHost></NewRemoteHost>`+
			`<NewExternalPort>%d</NewExternalPort>`+
			`<NewProtocol>%s</NewProtocol>`+
			`<NewInternalPort>%d</NewInternalPort>`+
			`<NewInternalClient>%s</NewInternalClient>`+
			`<NewEnabled>1</NewEnabled>`+
			`<NewPortMappingDescription>%s</NewPortMappingDescription>`+
			`<NewLeaseDuration>%d</NewLeaseDuration>`,
		externalPort, protocol, internalPort, internalIP, description, leaseDuration,
	)
	_, err := g.soapCall("AddPortMapping", body)
	return err
}

// DeletePortMapping removes a port forwarding rule.
func (g *Gateway) DeletePortMapping(protocol string, externalPort int) error {
	body := fmt.Sprintf(
		`<NewRemoteHost></NewRemoteHost>`+
			`<NewExternalPort>%d</NewExternalPort>`+
			`<NewProtocol>%s</NewProtocol>`,
		externalPort, protocol,
	)
	_, err := g.soapCall("DeletePortMapping", body)
	return err
}

// ─── SOAP helpers ─────────────────────────────────────────────────────────────

func (g *Gateway) soapCall(action, bodyArgs string) (string, error) {
	soapBody := fmt.Sprintf(`<?xml version="1.0"?>`+
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" `+
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`+
		`<s:Body><u:%s xmlns:u="%s">%s</u:%s></s:Body></s:Envelope>`,
		action, g.ServiceType, bodyArgs, action)

	req, _ := http.NewRequest("POST", g.ControlURL, strings.NewReader(soapBody))
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", fmt.Sprintf(`"%s#%s"`, g.ServiceType, action))

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("SOAP %s: %w", action, err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	result := string(b)

	if resp.StatusCode != 200 {
		errCode := extractXML(result, "errorCode")
		errDesc := extractXML(result, "errorDescription")
		if errCode != "" {
			return "", fmt.Errorf("UPnP error %s: %s", errCode, errDesc)
		}
		return "", fmt.Errorf("SOAP %s: HTTP %d", action, resp.StatusCode)
	}
	return result, nil
}

// ─── Device description helpers ───────────────────────────────────────────────

type deviceDescription struct {
	XMLName  xml.Name  `xml:"root"`
	Device   xmlDevice `xml:"device"`
	BaseURL  string
}
type xmlDevice struct {
	DeviceType  string        `xml:"deviceType"`
	DeviceList  []xmlDevice   `xml:"deviceList>device"`
	ServiceList []xmlService  `xml:"serviceList>service"`
}
type xmlService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

func fetchGateway(descURL string) (*Gateway, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(descURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var desc deviceDescription
	if err := xml.NewDecoder(resp.Body).Decode(&desc); err != nil {
		return nil, fmt.Errorf("parse device description: %w", err)
	}

	// Extract base URL from descURL
	baseURL := descURL
	if idx := strings.LastIndex(descURL, "/"); idx > 8 {
		baseURL = descURL[:idx+1]
	}

	// Search recursively for WANIPConnection or WANPPPConnection
	gw := findWANService(desc.Device, baseURL)
	if gw == nil {
		return nil, fmt.Errorf("no WANIPConnection service found")
	}
	return gw, nil
}

func findWANService(d xmlDevice, baseURL string) *Gateway {
	wanTypes := []string{
		"urn:schemas-upnp-org:service:WANIPConnection:1",
		"urn:schemas-upnp-org:service:WANIPConnection:2",
		"urn:schemas-upnp-org:service:WANPPPConnection:1",
	}
	for _, svc := range d.ServiceList {
		for _, t := range wanTypes {
			if svc.ServiceType == t {
				controlURL := svc.ControlURL
				if !strings.HasPrefix(controlURL, "http") {
					controlURL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(controlURL, "/")
				}
				return &Gateway{ControlURL: controlURL, ServiceType: svc.ServiceType}
			}
		}
	}
	for _, child := range d.DeviceList {
		if gw := findWANService(child, baseURL); gw != nil {
			return gw
		}
	}
	return nil
}

// ─── String helpers ───────────────────────────────────────────────────────────

func extractHeader(resp, key string) string {
	key = strings.ToLower(key) + ":"
	for _, line := range strings.Split(resp, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), key) {
			return strings.TrimSpace(line[len(key):])
		}
	}
	return ""
}

func extractXML(s, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(s, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(s[start:], close)
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}

// LocalIP returns the local IPv4 address used to reach the outside world.
func LocalIP() (string, error) {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}
