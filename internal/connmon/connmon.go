// Package connmon monitors active network connections per process using
// Windows GetExtendedTcpTable / GetExtendedUdpTable APIs.
// This avoids heavy packet sniffing — we poll the OS connection tables directly.
package connmon

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

// Protocol identifies TCP vs UDP.
type Protocol uint8

const (
	TCP Protocol = iota
	UDP
)

func (p Protocol) String() string {
	if p == TCP {
		return "TCP"
	}
	return "UDP"
}

// Connection represents a single active network connection.
type Connection struct {
	Protocol  Protocol
	LocalIP   net.IP
	LocalPort uint16
	RemoteIP  net.IP
	RemotePort uint16
	PID       uint32
	State     string // TCP only; UDP is stateless
}

func (c Connection) String() string {
	return fmt.Sprintf("[%s] %s:%d -> %s:%d (PID %d) %s",
		c.Protocol, c.LocalIP, c.LocalPort, c.RemoteIP, c.RemotePort, c.PID, c.State)
}

// IsPublicRemote returns true if the remote IP is a public (non-private, non-loopback) address.
func (c Connection) IsPublicRemote() bool {
	ip := c.RemoteIP
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	// Check RFC1918 private ranges
	private := []net.IPNet{
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
	}
	for _, pn := range private {
		if pn.Contains(ip) {
			return false
		}
	}
	return true
}

var (
	modIphlpapi          = syscall.NewLazyDLL("iphlpapi.dll")
	procGetExtendedTcpTable = modIphlpapi.NewProc("GetExtendedTcpTable")
	procGetExtendedUdpTable = modIphlpapi.NewProc("GetExtendedUdpTable")
)

const (
	afINET          = 2
	tcpTableOwnerPIDAll = 5
	udpTableOwnerPID    = 1
)

// TCP states from MIB_TCP_STATE
var tcpStates = map[uint32]string{
	1:  "CLOSED",
	2:  "LISTEN",
	3:  "SYN_SENT",
	4:  "SYN_RCVD",
	5:  "ESTABLISHED",
	6:  "FIN_WAIT1",
	7:  "FIN_WAIT2",
	8:  "CLOSE_WAIT",
	9:  "CLOSING",
	10: "LAST_ACK",
	11: "TIME_WAIT",
	12: "DELETE_TCB",
}

// GetTCPConnections returns all active TCP connections with owning PID.
func GetTCPConnections() ([]Connection, error) {
	var size uint32

	// First call to get required buffer size
	ret, _, _ := procGetExtendedTcpTable.Call(
		0,
		uintptr(unsafe.Pointer(&size)),
		0, // no sorting
		afINET,
		tcpTableOwnerPIDAll,
		0,
	)
	if ret != 122 { // ERROR_INSUFFICIENT_BUFFER
		return nil, fmt.Errorf("GetExtendedTcpTable sizing call failed: %d", ret)
	}

	buf := make([]byte, size)
	ret, _, _ = procGetExtendedTcpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
		afINET,
		tcpTableOwnerPIDAll,
		0,
	)
	if ret != 0 {
		return nil, fmt.Errorf("GetExtendedTcpTable failed: %d", ret)
	}

	numEntries := binary.LittleEndian.Uint32(buf[0:4])
	const entrySize = 24 // sizeof(MIB_TCPROW_OWNER_PID) = 6 * uint32 = 24
	conns := make([]Connection, 0, numEntries)

	for i := uint32(0); i < numEntries; i++ {
		offset := 4 + i*entrySize
		if int(offset+entrySize) > len(buf) {
			break
		}
		row := buf[offset : offset+entrySize]

		state := binary.LittleEndian.Uint32(row[0:4])
		localAddr := make(net.IP, 4)
		copy(localAddr, row[4:8])
		localPort := binary.BigEndian.Uint16(row[8:10])
		remoteAddr := make(net.IP, 4)
		copy(remoteAddr, row[12:16])
		remotePort := binary.BigEndian.Uint16(row[16:18])
		pid := binary.LittleEndian.Uint32(row[20:24])

		stateName := tcpStates[state]
		if stateName == "" {
			stateName = fmt.Sprintf("UNKNOWN(%d)", state)
		}

		conns = append(conns, Connection{
			Protocol:   TCP,
			LocalIP:    localAddr,
			LocalPort:  localPort,
			RemoteIP:   remoteAddr,
			RemotePort: remotePort,
			PID:        pid,
			State:      stateName,
		})
	}

	return conns, nil
}

// GetUDPConnections returns all active UDP endpoints with owning PID.
// Note: UDP is connectionless, so RemoteIP/RemotePort will be 0.0.0.0:0.
// We still record the local binding which helps identify game processes.
func GetUDPConnections() ([]Connection, error) {
	var size uint32

	ret, _, _ := procGetExtendedUdpTable.Call(
		0,
		uintptr(unsafe.Pointer(&size)),
		0,
		afINET,
		udpTableOwnerPID,
		0,
	)
	if ret != 122 {
		return nil, fmt.Errorf("GetExtendedUdpTable sizing call failed: %d", ret)
	}

	buf := make([]byte, size)
	ret, _, _ = procGetExtendedUdpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
		afINET,
		udpTableOwnerPID,
		0,
	)
	if ret != 0 {
		return nil, fmt.Errorf("GetExtendedUdpTable failed: %d", ret)
	}

	numEntries := binary.LittleEndian.Uint32(buf[0:4])
	const entrySize = 12 // sizeof(MIB_UDPROW_OWNER_PID) = 3 * uint32 = 12
	conns := make([]Connection, 0, numEntries)

	for i := uint32(0); i < numEntries; i++ {
		offset := 4 + i*entrySize
		if int(offset+entrySize) > len(buf) {
			break
		}
		row := buf[offset : offset+entrySize]

		localAddr := make(net.IP, 4)
		copy(localAddr, row[0:4])
		localPort := binary.BigEndian.Uint16(row[4:6])
		pid := binary.LittleEndian.Uint32(row[8:12])

		conns = append(conns, Connection{
			Protocol:   UDP,
			LocalIP:    localAddr,
			LocalPort:  localPort,
			RemoteIP:   net.IPv4zero,
			RemotePort: 0,
			PID:        pid,
			State:      "STATELESS",
		})
	}

	return conns, nil
}

// GetAllConnections returns both TCP and UDP connections combined.
func GetAllConnections() ([]Connection, error) {
	tcp, err := GetTCPConnections()
	if err != nil {
		return nil, fmt.Errorf("TCP: %w", err)
	}
	udp, err := GetUDPConnections()
	if err != nil {
		return nil, fmt.Errorf("UDP: %w", err)
	}
	return append(tcp, udp...), nil
}

// GetConnectionsByPID returns all connections for a specific process.
func GetConnectionsByPID(pid uint32) ([]Connection, error) {
	all, err := GetAllConnections()
	if err != nil {
		return nil, err
	}
	var result []Connection
	for _, c := range all {
		if c.PID == pid {
			result = append(result, c)
		}
	}
	return result, nil
}
