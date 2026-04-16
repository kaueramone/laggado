// Package ping provides ICMP-style latency measurement using the Windows
// built-in ping.exe. This avoids the need for raw sockets or elevated
// privileges, and works correctly with UDP-only game servers (e.g. CS2).
package ping

import (
	"os/exec"
	"strconv"
	"strings"
)

// Result holds the parsed output of a ping run.
type Result struct {
	AvgMs float64
	MinMs float64
	MaxMs float64
	Loss  float64 // 0.0–1.0
}

// Probe sends `count` ICMP pings to ip with a per-ping timeout of timeoutMs,
// and returns parsed statistics. Returns an error only if ping.exe cannot run.
func Probe(ip string, count int, timeoutMs int) (Result, error) {
	if count <= 0 {
		count = 4
	}
	if timeoutMs <= 0 {
		timeoutMs = 1000
	}

	out, err := exec.Command(
		"ping",
		"-n", strconv.Itoa(count),
		"-w", strconv.Itoa(timeoutMs),
		ip,
	).Output()
	if err != nil {
		// ping.exe returns exit code 1 on 100% loss — still parseable output.
		if len(out) == 0 {
			return Result{Loss: 1}, err
		}
	}

	return parse(string(out)), nil
}

// parse reads ping.exe stdout and extracts stats.
// Handles both English ("Average = Xms") and Portuguese ("Média = Xms").
func parse(output string) Result {
	var r Result
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)

		// Packet loss line:
		//   English:    "Packets: Sent = 4, Received = 4, Lost = 0 (0% loss)"
		//   Portuguese: "Pacotes: Enviados = 4, Recebidos = 4, Perdidos = 0 (0% de perda)"
		if strings.Contains(low, "lost") || strings.Contains(low, "perdidos") {
			if idx := strings.Index(low, "("); idx != -1 {
				pct := line[idx+1:]
				if end := strings.Index(pct, "%"); end != -1 {
					if v, err := strconv.ParseFloat(strings.TrimSpace(pct[:end]), 64); err == nil {
						r.Loss = v / 100.0
					}
				}
			}
		}

		// Stats line:
		//   English:    "Minimum = 5ms, Maximum = 7ms, Average = 6ms"
		//   Portuguese: "Mínimo = 5ms, Máximo = 7ms, Média = 6ms"
		//
		// Detect by presence of "ms" and "=" and at least 2 commas (3 fields).
		if strings.Count(line, ",") >= 2 && strings.Contains(low, "ms") && strings.Contains(low, "=") {
			parts := strings.Split(line, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				kv := strings.SplitN(p, "=", 2)
				if len(kv) != 2 {
					continue
				}
				key := strings.ToLower(strings.TrimSpace(kv[0]))
				val := strings.ToLower(strings.TrimSpace(kv[1]))
				val = strings.TrimSuffix(val, "ms")
				val = strings.TrimSpace(val)
				ms, err := strconv.ParseFloat(val, 64)
				if err != nil {
					continue
				}
				if strings.Contains(key, "min") || strings.Contains(key, "nim") {
					r.MinMs = ms
				} else if strings.Contains(key, "max") || strings.Contains(key, "xim") {
					r.MaxMs = ms
				} else if strings.Contains(key, "aver") || strings.Contains(key, "avg") || strings.Contains(key, "dia") {
					r.AvgMs = ms
				}
			}
		}
	}

	return r
}
