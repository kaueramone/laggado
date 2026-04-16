// Package gamedet detects running game processes and maps them to their connections.
// Uses Windows CreateToolhelp32Snapshot to enumerate processes efficiently.
package gamedet

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modKernel32              = syscall.NewLazyDLL("kernel32.dll")
	procCreateToolhelp32Snapshot = modKernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32First       = modKernel32.NewProc("Process32FirstW")
	procProcess32Next        = modKernel32.NewProc("Process32NextW")
)

const (
	th32csSnapProcess = 0x00000002
	maxPath           = 260
)

// processEntry32W matches the Windows PROCESSENTRY32W structure.
type processEntry32W struct {
	Size              uint32
	Usage             uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	Threads           uint32
	ParentProcessID   uint32
	PriorityClassBase int32
	Flags             uint32
	ExeFile           [maxPath]uint16
}

// ProcessInfo holds minimal info about a running process.
type ProcessInfo struct {
	PID  uint32
	Name string // e.g. "cs2.exe"
}

// KnownGames maps game executable names (lowercase) to friendly names.
// Derived from ExitLag's game database (reverse engineered) + community knowledge.
// ExitLag IDs noted in comments for cross-reference.
var KnownGames = map[string]string{
	// ── Valve / Steam ──────────────────────────────────────────────
	"cs2.exe":                            "Counter-Strike 2",           // ExitLag #1209
	"csgo.exe":                           "CS:GO",
	"dota2.exe":                          "Dota 2",                     // ExitLag #17
	"hl2.exe":                            "Half-Life 2",
	"deadlock.exe":                        "Deadlock",
	"tf2.exe":                            "Team Fortress 2",

	// ── Riot Games ────────────────────────────────────────────────
	"valorant.exe":                       "Valorant",
	"valorant-win64-shipping.exe":        "Valorant",
	"riotclient.exe":                     "Riot Client",
	"leagueclient.exe":                   "League of Legends",          // ExitLag #16
	"league of legends.exe":              "League of Legends",

	// ── Epic Games ────────────────────────────────────────────────
	"rocketleague.exe":                   "Rocket League",              // ExitLag #40 (Epic)
	"fortniteclient-win64-shipping.exe":  "Fortnite",
	"fortnite.exe":                       "Fortnite",

	// ── EA / Battlefield series ───────────────────────────────────
	"bf2042.exe":                         "Battlefield 2042",           // ExitLag #809/849
	"bfv.exe":                            "Battlefield V",              // ExitLag #47
	"bf1.exe":                            "Battlefield 1",              // ExitLag #1504 (Steam)
	"bf4.exe":                            "Battlefield 4",              // ExitLag #1507
	"bf3.exe":                            "Battlefield 3",              // ExitLag #1506
	"bf.exe":                             "Battlefield 6",              // ExitLag #3607 (placeholder)
	"battlefield6.exe":                   "Battlefield 6",              // ExitLag #3607
	"battlefield2042.exe":                "Battlefield 2042",
	// EA App launcher may launch under these process names
	"eadesktop.exe":                      "EA App",
	"origin.exe":                         "EA Origin",

	// ── Activision / Blizzard ─────────────────────────────────────
	"modernwarfare.exe":                  "Call of Duty: MW",
	"cod.exe":                            "Call of Duty",
	"overwatch.exe":                      "Overwatch 2",
	"diablo4.exe":                        "Diablo IV",
	"wowclassic.exe":                     "WoW Classic",
	"wow.exe":                            "World of Warcraft",           // ExitLag #5

	// ── Other FPS / Battle Royale ─────────────────────────────────
	"r5apex.exe":                         "Apex Legends",
	"pubg.exe":                           "PUBG: Battlegrounds",
	"tslgame.exe":                        "PUBG: Battlegrounds",
	"destiny2.exe":                       "Destiny 2",
	"squadgame-win64-shipping.exe":       "Squad",
	"escape from tarkov.exe":             "Escape from Tarkov",
	"eft.exe":                            "Escape from Tarkov",
	"rust.exe":                           "Rust",
	"dayz_x64.exe":                       "DayZ",

	// ── MOBA / Strategy ───────────────────────────────────────────
	"heroesofthestorm.exe":               "Heroes of the Storm",
	"smite.exe":                          "SMITE",

	// ── Racing / Sports ───────────────────────────────────────────
	"ams2avx.exe":                        "Automobilista 2",
	"efootball.exe":                      "eFootball",
	"fifa.exe":                           "EA FC",
	"fc24.exe":                           "EA FC 24",
	"fc25.exe":                           "EA FC 25",

	// ── MMORPGs ────────────────────────────────────────────────────
	"ffxiv_dx11.exe":                     "Final Fantasy XIV",          // ExitLag #9
	"lineage2.exe":                       "Lineage 2",                  // ExitLag #10
	"tibia.exe":                          "Tibia",                      // ExitLag #1

	// ── Other popular ─────────────────────────────────────────────
	"gta5.exe":                           "GTA V",
	"gtav.exe":                           "GTA V",
	"marvelrivals-win64-shipping.exe":    "Marvel Rivals",
	"marvelrivals_launcher.exe":          "Marvel Rivals",
}

// ListProcesses enumerates all running processes on the system.
func ListProcesses() ([]ProcessInfo, error) {
	handle, _, err := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if handle == uintptr(^uintptr(0)) { // INVALID_HANDLE_VALUE
		return nil, fmt.Errorf("CreateToolhelp32Snapshot failed: %v", err)
	}
	defer syscall.CloseHandle(syscall.Handle(handle))

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))

	ret, _, err := procProcess32First.Call(handle, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		return nil, fmt.Errorf("Process32First failed: %v", err)
	}

	var procs []ProcessInfo
	for {
		name := syscall.UTF16ToString(entry.ExeFile[:])
		procs = append(procs, ProcessInfo{
			PID:  entry.ProcessID,
			Name: name,
		})

		entry.Size = uint32(unsafe.Sizeof(entry))
		ret, _, _ = procProcess32Next.Call(handle, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			break
		}
	}

	return procs, nil
}

// DetectGames scans running processes and returns those matching known game executables.
func DetectGames() ([]ProcessInfo, error) {
	procs, err := ListProcesses()
	if err != nil {
		return nil, err
	}

	var games []ProcessInfo
	for _, p := range procs {
		lower := strings.ToLower(filepath.Base(p.Name))
		if _, ok := KnownGames[lower]; ok {
			games = append(games, p)
		}
	}
	return games, nil
}

// FindProcessByName returns the PID of a process matching the given name (case-insensitive).
func FindProcessByName(name string) (uint32, error) {
	procs, err := ListProcesses()
	if err != nil {
		return 0, err
	}

	target := strings.ToLower(name)
	for _, p := range procs {
		if strings.ToLower(p.Name) == target {
			return p.PID, nil
		}
	}
	return 0, fmt.Errorf("process %q not found", name)
}

// FriendlyName returns the human-readable name for a game exe, or the exe name itself.
func FriendlyName(exeName string) string {
	lower := strings.ToLower(filepath.Base(exeName))
	if name, ok := KnownGames[lower]; ok {
		return name
	}
	return exeName
}
