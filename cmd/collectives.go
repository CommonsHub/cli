package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Collective represents a collective/project.
type Collective struct {
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
	// Aliases lists alternate slugs that should map to this canonical one.
	// Drives CanonicalCollectiveSlug — useful when an upstream source uses
	// a different spelling (e.g. Open Collective uses "commonshub-brussels"
	// while our chart of accounts says "commonshub").
	Aliases []string `json:"aliases,omitempty"`
}

func collectivesPath() string {
	return settingsFilePath("collectives.json")
}

// LoadCollectives reads collectives from APP_DATA_DIR/settings/collectives.json.
// The file is seeded from the embedded defaults on first run and kept in
// sync by EnsureSettingsBootstrapped when the user hasn't edited it locally.
func LoadCollectives() map[string]Collective {
	data, err := os.ReadFile(collectivesPath())
	if err != nil {
		return map[string]Collective{}
	}
	var collectives map[string]Collective
	if json.Unmarshal(data, &collectives) != nil {
		return map[string]Collective{}
	}
	return collectives
}

// SaveCollectives writes collectives to APP_DATA_DIR/settings/collectives.json.
func SaveCollectives(collectives map[string]Collective) error {
	data, err := json.MarshalIndent(collectives, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(collectivesPath()), 0755); err != nil {
		return err
	}
	return os.WriteFile(collectivesPath(), data, 0644)
}

// AddCollective adds a new collective and saves.
func AddCollective(slug string) {
	collectives := LoadCollectives()
	if _, ok := collectives[slug]; !ok {
		collectives[slug] = Collective{Name: slug}
		SaveCollectives(collectives)
	}
}

// CollectiveSlugs returns a sorted list of collective slugs.
func CollectiveSlugs() []string {
	collectives := LoadCollectives()
	var slugs []string
	for slug := range collectives {
		slugs = append(slugs, slug)
	}
	return slugs
}

// collectiveAliasCache caches the alias→canonical map. Cleared by
// resetCollectiveAliasCache (test hook); production code reloads when
// collectives.json changes via the normal settings reload paths.
var (
	collectiveAliasMu    sync.Mutex
	collectiveAliasCache map[string]string
)

func collectiveAliasMap() map[string]string {
	collectiveAliasMu.Lock()
	defer collectiveAliasMu.Unlock()
	if collectiveAliasCache != nil {
		return collectiveAliasCache
	}
	m := map[string]string{}
	for slug, c := range LoadCollectives() {
		canonical := strings.ToLower(strings.TrimSpace(slug))
		m[canonical] = canonical
		for _, a := range c.Aliases {
			alias := strings.ToLower(strings.TrimSpace(a))
			if alias != "" {
				m[alias] = canonical
			}
		}
	}
	collectiveAliasCache = m
	return m
}

// CanonicalCollectiveSlug resolves an arbitrary collective slug to the
// canonical one declared in collectives.json. Unknown slugs pass through
// (lower-cased, trimmed) so the user can still see the raw value rather
// than having it silently dropped.
func CanonicalCollectiveSlug(slug string) string {
	s := strings.ToLower(strings.TrimSpace(slug))
	if s == "" {
		return ""
	}
	if canonical, ok := collectiveAliasMap()[s]; ok {
		return canonical
	}
	return s
}

// resetCollectiveAliasCache is a test hook that forces the next call to
// CanonicalCollectiveSlug to rebuild from disk.
func resetCollectiveAliasCache() {
	collectiveAliasMu.Lock()
	collectiveAliasCache = nil
	collectiveAliasMu.Unlock()
}

