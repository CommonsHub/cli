package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// discordGuildNameCachePath holds the cached guildID → name lookups so the
// sync header can render "CommonsHub" instead of a numeric guild ID without
// round-tripping the Discord API on every run.
//
// File: APP_DATA_DIR/cache/discord-guild-names.json
type discordGuildNameCache map[string]string

func discordGuildNameCachePath() string {
	return filepath.Join(AppDataDir(), "cache", "discord-guild-names.json")
}

func loadDiscordGuildNameCache() discordGuildNameCache {
	out := discordGuildNameCache{}
	data, err := os.ReadFile(discordGuildNameCachePath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

func saveDiscordGuildNameCache(cache discordGuildNameCache) error {
	if cache == nil {
		return nil
	}
	path := discordGuildNameCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// DiscordGuildName returns the cached display name for the given guild ID,
// or "" if the cache has no entry yet. Callers fall back to the guild ID
// in their own rendering.
func DiscordGuildName(guildID string) string {
	if guildID == "" {
		return ""
	}
	return loadDiscordGuildNameCache()[guildID]
}

// CacheDiscordGuildName records a guildID → name mapping. No-ops on empty
// arguments or write failure (the cache is best-effort).
func CacheDiscordGuildName(guildID, name string) {
	if guildID == "" || name == "" {
		return
	}
	cache := loadDiscordGuildNameCache()
	if cache[guildID] == name {
		return
	}
	cache[guildID] = name
	_ = saveDiscordGuildNameCache(cache)
}

// FetchAndCacheDiscordGuildName looks up the guild name via the Discord API
// and caches it. Returns the cached name immediately when present so callers
// can invoke this on every sync without re-hitting the API. Silently returns
// "" if no token is set or the call fails — the header falls back to the
// guild ID.
func FetchAndCacheDiscordGuildName(guildID string) string {
	if guildID == "" {
		return ""
	}
	if cached := DiscordGuildName(guildID); cached != "" {
		return cached
	}
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		return ""
	}
	req, err := http.NewRequest("GET", "https://discord.com/api/v10/guilds/"+guildID, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bot "+token)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	var guild struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&guild); err != nil {
		return ""
	}
	CacheDiscordGuildName(guildID, guild.Name)
	return guild.Name
}
