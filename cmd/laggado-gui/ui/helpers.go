package ui

import (
	"image/color"
	"net"
	"time"
)

// dialTCP is a simple TCP connect helper used by graph and game panel.
func dialTCP(ip string, port int) (net.Conn, error) {
	addr := net.JoinHostPort(ip, itoa(port))
	return net.DialTimeout("tcp", addr, 2*time.Second)
}

// colorWithAlpha blends a color with reduced alpha for backgrounds.
func colorWithAlpha(c color.NRGBA, alpha uint8) color.NRGBA {
	c.A = alpha
	return c
}

// accentFill returns the accent color with low alpha for row highlights.
func accentFill() color.NRGBA {
	return colorWithAlpha(ColorAccent, 30)
}

// bestRowFill returns the green tint for the best route row.
func bestRowFill() color.NRGBA {
	return colorWithAlpha(ColorGreen, 20)
}

// itoa converts int to string without fmt.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := [20]byte{}
	pos := 19
	for n > 0 {
		digits[pos] = byte('0' + n%10)
		n /= 10
		pos--
	}
	return string(digits[pos+1:])
}

// containsStr reports whether s contains sub.
func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

