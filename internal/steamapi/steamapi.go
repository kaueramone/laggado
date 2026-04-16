// Package steamapi fetches game metadata and cover art from public Steam/Epic CDNs.
// No API key required — uses public Steam store CDN URLs.
package steamapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GameInfo holds metadata for a detected game.
type GameInfo struct {
	Name      string `json:"name"`
	SteamID   int    `json:"steamId"`
	CoverPath string `json:"coverPath"` // local cached cover image path
	Platform  string `json:"platform"`  // "steam", "epic", "ea", "blizzard"
	ExeName   string `json:"exeName"`
}

// Steam App IDs for supported games (ExitLag IDs cross-referenced in comments).
var SteamAppIDs = map[string]int{
	// Valve
	"cs2.exe":      730,    // Counter-Strike 2 (ExitLag #1209)
	"csgo.exe":     730,
	"dota2.exe":    570,    // Dota 2 (ExitLag #17)
	"tf2.exe":      440,
	"deadlock.exe": 1422450,

	// EA / Battlefield (Steam versions)
	"bf1.exe":    1238840, // Battlefield 1 (ExitLag #1504)
	"bfv.exe":    1238840, // Battlefield V (ExitLag #47) — same Steam app
	"bf4.exe":    1238840, // Battlefield 4 (ExitLag #1507)
	"bf3.exe":    1238840, // Battlefield 3
	"bf2042.exe": 1517290, // Battlefield 2042 (ExitLag #809)

	// Riot
	"valorant.exe":                      0, // Not on Steam
	"valorant-win64-shipping.exe":       0,
	"leagueclient.exe":                  0,

	// Other
	"r5apex.exe":                        1172470, // Apex Legends
	"pubg.exe":                          578080,  // PUBG
	"tslgame.exe":                       578080,
	"destiny2.exe":                      1085660,
	"gta5.exe":                          271590,
	"gtav.exe":                          271590,
	"rust.exe":                          252490,
	"ffxiv_dx11.exe":                    39210,   // FFXIV (ExitLag #9)
	"wow.exe":                           0,       // Battle.net only
	"overwatch.exe":                     0,       // Battle.net only
	"fortnite.exe":                      0,       // Epic only
	"fortniteclient-win64-shipping.exe": 0,

	// Rocket League — moved to Epic, no Steam cover anymore
	"rocketleague.exe": 252950, // Old Steam ID still works for cover art
}

// EpicCovers maps Epic-only games to CDN image URLs.
var EpicCovers = map[string]string{
	"rocketleague.exe":                   "https://cdn1.epicgames.com/offer/9773aa1aa54f4f7b80e44bef04986cea/EGS_RocketLeague_PsyonixLLC_S2_1200x1600-5f7d5c73e36e9b3fc46f9a4c3eea25d3",
	"fortniteclient-win64-shipping.exe":  "https://cdn2.epicgames.com/offer/fn/[NEW]_EGS_Fortnite_Epic_G1A_00-2953_1200x1600-2953d7ae36e9b3fc46f9a4c3eea25d3",
	"fortnite.exe":                       "https://cdn2.epicgames.com/offer/fn/[NEW]_EGS_Fortnite_Epic_G1A_00-2953_1200x1600-2953d7ae36e9b3fc46f9a4c3eea25d3",
}

// Fetcher downloads and caches game cover images.
type Fetcher struct {
	cacheDir string
	client   *http.Client
}

// NewFetcher creates a cover art fetcher with local caching.
func NewFetcher(cacheDir string) *Fetcher {
	os.MkdirAll(cacheDir, 0755)
	return &Fetcher{
		cacheDir: cacheDir,
		client:   &http.Client{Timeout: 8 * time.Second},
	}
}

// GetGameInfo returns metadata and downloads cover art for a game exe.
func (f *Fetcher) GetGameInfo(exeName string, gameDisplayName string) (*GameInfo, error) {
	lower := strings.ToLower(exeName)
	info := &GameInfo{
		Name:    gameDisplayName,
		ExeName: exeName,
	}

	steamID, ok := SteamAppIDs[lower]
	if ok && steamID > 0 {
		info.SteamID = steamID
		info.Platform = "steam"
		coverPath, err := f.fetchSteamCover(steamID)
		if err == nil {
			info.CoverPath = coverPath
		}
	} else if epicURL, ok := EpicCovers[lower]; ok {
		info.Platform = "epic"
		coverPath, err := f.fetchURL(epicURL, fmt.Sprintf("epic_%s.jpg", lower))
		if err == nil {
			info.CoverPath = coverPath
		}
	} else {
		info.Platform = "other"
	}

	return info, nil
}

// fetchSteamCover downloads the Steam header image for an app ID.
// Steam CDN is free and doesn't require authentication.
func (f *Fetcher) fetchSteamCover(appID int) (string, error) {
	filename := fmt.Sprintf("steam_%d.jpg", appID)
	localPath := filepath.Join(f.cacheDir, filename)

	// Return cached version if exists
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	// Steam header image: 460x215
	url := fmt.Sprintf("https://cdn.akamai.steamstatic.com/steam/apps/%d/header.jpg", appID)
	return f.fetchURL(url, filename)
}

func (f *Fetcher) fetchURL(url, filename string) (string, error) {
	localPath := filepath.Join(f.cacheDir, filename)
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	resp, err := f.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch cover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("cover fetch status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return "", err
	}
	return localPath, nil
}

// GameMeta is a lightweight struct for display in the game list.
type GameMeta struct {
	ExeName string
	Name    string
	SteamID int
	Platform string
}

// GetGameMeta returns basic metadata without downloading images.
func GetGameMeta(exeName string, displayName string) GameMeta {
	lower := strings.ToLower(exeName)
	steamID := SteamAppIDs[lower]
	platform := "other"
	if steamID > 0 {
		platform = "steam"
	} else if _, ok := EpicCovers[lower]; ok {
		platform = "epic"
	}
	return GameMeta{
		ExeName:  exeName,
		Name:     displayName,
		SteamID:  steamID,
		Platform: platform,
	}
}

// SteamCoverURL returns the public Steam header image URL.
func SteamCoverURL(appID int) string {
	return fmt.Sprintf("https://cdn.akamai.steamstatic.com/steam/apps/%d/header.jpg", appID)
}

// CachedGames stores previously seen game info to disk.
type CachedGames struct {
	Games map[string]*GameInfo `json:"games"`
	path  string
}

func LoadCachedGames(dataDir string) *CachedGames {
	cg := &CachedGames{
		Games: make(map[string]*GameInfo),
		path:  filepath.Join(dataDir, "games.json"),
	}
	if data, err := os.ReadFile(cg.path); err == nil {
		json.Unmarshal(data, &cg.Games)
	}
	return cg
}

func (cg *CachedGames) Save() {
	data, _ := json.MarshalIndent(cg.Games, "", "  ")
	os.WriteFile(cg.path, data, 0644)
}

func (cg *CachedGames) Set(exeName string, info *GameInfo) {
	cg.Games[strings.ToLower(exeName)] = info
	cg.Save()
}

func (cg *CachedGames) Get(exeName string) *GameInfo {
	return cg.Games[strings.ToLower(exeName)]
}
