// Package gamelib provides the LAGGADO game database — 2389 games sourced
// from ExitLag's local cache. Icons are read from ExitLag's temp directory
// when available, otherwise a placeholder is returned.
package gamelib

import (
	_ "embed"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed games_map.csv
var gamesMapCSV string

// Game holds metadata for a game entry.
type Game struct {
	ID       int    // ExitLag app_id
	Slug     string // icon filename slug (without .png)
	Name     string // human-readable name derived from slug
	IconPath string // local path to cached icon, or "" if not available
}

// ExitLagIconDir is the standard location where ExitLag caches icons.
const ExitLagIconDir = `C:\Users\Administrator\AppData\Local\Temp\exitlag_client_base_icons`

var (
	allGames  []Game
	byID      map[int]*Game
	bySlugKey map[string]*Game // slug keyword → game
)

func init() {
	byID = make(map[int]*Game)
	bySlugKey = make(map[string]*Game)

	iconDir := ExitLagIconDir
	// Try per-user path too
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, "AppData", "Local", "Temp", "exitlag_client_base_icons")
		if _, err := os.Stat(candidate); err == nil {
			iconDir = candidate
		}
	}

	seenID   := make(map[int]bool)
	seenName := make(map[string]bool) // dedup por nome normalizado
	for _, line := range strings.Split(gamesMapCSV, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		if seenID[id] {
			continue // toma apenas a primeira ocorrência por ID
		}
		seenID[id] = true

		slug := strings.TrimSpace(parts[1])
		name := slugToName(slug)

		// Filtra jogos sem nome legível (só código, só número, vazio)
		if !isValidGameName(name) {
			continue
		}

		// Deduplica por nome normalizado (ex: dois "Tibia" com IDs diferentes)
		normalName := strings.ToLower(strings.TrimSpace(name))
		if seenName[normalName] {
			continue
		}
		seenName[normalName] = true

		iconPath := filepath.Join(iconDir, "app"+strconv.Itoa(id)+".png")
		if _, err := os.Stat(iconPath); err != nil {
			iconPath = "" // ícone não disponível
		}

		g := Game{
			ID:       id,
			Slug:     slug,
			Name:     name,
			IconPath: iconPath,
		}
		allGames = append(allGames, g)
		byID[id] = &allGames[len(allGames)-1]

		// Index by first meaningful keyword in the slug
		kw := firstKeyword(slug)
		if kw != "" && bySlugKey[kw] == nil {
			bySlugKey[kw] = &allGames[len(allGames)-1]
		}
	}
}

// All returns all games.
func All() []Game { return allGames }

// ByID returns a game by its ExitLag app_id, or nil.
func ByID(id int) *Game { return byID[id] }

// Search returns games whose name contains the query (case-insensitive).
func Search(query string) []Game {
	if query == "" {
		return allGames
	}
	q := strings.ToLower(query)
	var out []Game
	for _, g := range allGames {
		if strings.Contains(strings.ToLower(g.Name), q) {
			out = append(out, g)
		}
	}
	return out
}

// FindByExeName tries to find a game matching a Windows exe name.
func FindByExeName(exe string) *Game {
	lower := strings.ToLower(strings.TrimSuffix(exe, ".exe"))
	// Exact known mappings
	if id, ok := knownExeToID[lower]; ok {
		return byID[id]
	}
	// Fuzzy: check if the exe stem matches a slug keyword
	if g, ok := bySlugKey[lower]; ok {
		return g
	}
	return nil
}

// slugToName converts an ExitLag slug to a human-readable name.
// e.g. "counter-strike_2_1209_1761157748968" → "Counter-Strike 2"
func slugToName(slug string) string {
	// Strip trailing numeric timestamp suffixes like _1209_1761157748968
	parts := strings.Split(slug, "_")
	// Drop trailing pure-numeric parts (timestamps and IDs)
	end := len(parts)
	for end > 0 {
		p := parts[end-1]
		if isNumeric(p) && len(p) > 4 {
			end--
		} else {
			break
		}
	}
	// Also drop trailing short numeric suffixes that are just the app_id
	for end > 1 {
		p := parts[end-1]
		if isNumeric(p) {
			end--
		} else {
			break
		}
	}
	parts = parts[:end]

	// Replace hyphens with spaces, title-case each word
	words := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ReplaceAll(p, "-", " ")
		for _, w := range strings.Fields(p) {
			if len(w) == 0 {
				continue
			}
			// Preserve known acronyms
			up := strings.ToUpper(w)
			if knownAcronyms[up] {
				words = append(words, up)
			} else {
				words = append(words, strings.ToUpper(w[:1])+w[1:])
			}
		}
	}
	return strings.Join(words, " ")
}

// isValidGameName retorna true se o nome é legível por humanos.
// Rejeita: vazio, puramente numérico, sem letras, muito curto.
func isValidGameName(name string) bool {
	name = strings.TrimSpace(name)
	if len(name) < 2 {
		return false
	}
	hasLetter := false
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasLetter = true
			break
		}
	}
	return hasLetter
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func firstKeyword(slug string) string {
	// Return first non-numeric, non-hash segment of the slug
	parts := strings.FieldsFunc(slug, func(r rune) bool {
		return r == '_' || r == '-'
	})
	for _, p := range parts {
		if len(p) < 3 || isNumeric(p) || isHash(p) {
			continue
		}
		return strings.ToLower(p)
	}
	return ""
}

func isHash(s string) bool {
	if len(s) < 16 {
		return false
	}
	hexChars := 0
	for _, c := range s {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			hexChars++
		}
	}
	return hexChars == len(s)
}

var knownAcronyms = map[string]bool{
	"GTA": true, "CS": true, "WOW": true, "FFXIV": true, "LOL": true,
	"ESO": true, "EVE": true, "PUBG": true, "BF": true, "TF2": true,
	"HoN": true, "FPS": true, "RPG": true, "MMO": true, "PVP": true,
}

// knownExeToID maps lowercase exe stems to ExitLag app IDs.
var knownExeToID = map[string]int{
	"cs2":                            1209,
	"csgo":                           730,  // maps to cs2 icon anyway
	"counter-strike_2":               1209,
	"dota2":                          17,
	"leagueclient":                   16,
	"league of legends":              16,
	"rocketleague":                   40,
	"fortniteclient-win64-shipping":  97,
	"fortnite":                       97,
	"valorant-win64-shipping":        300,
	"valorant":                       300,
	"r5apex":                         217,
	"bf2042":                         809,
	"battlefield2042":                809,
	"bfv":                            47,
	"bf1":                            1504,
	"bf4":                            1507,
	"bf3":                            1506,
	"battlefield6":                   3607,
	"bf":                             3607,
	"pubg":                           385,
	"tslgame":                        385,
	"rainbowsix":                     93,
	"rainbow6":                       93,
	"r6siege":                        93,
	"overwatchlauncher":              790,
	"overwatch":                      790,
	"escape from tarkov":             131,
	"escapefromtarkov":               131,
	"wow":                            5,
	"worldofwarcraft":                5,
	"gta5":                           38,
	"gtav":                           38,
	"rust":                           92,
	"minecraft":                      62,
	"minecraft_launcher":             62,
	"warframe":                       108,
	"hearthstone":                    175,
	"starcraftii":                    34,
	"diablo":                         31,
	"diabloiii":                      31,
}
