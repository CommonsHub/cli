package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const discordAPIBase = "https://discord.com/api/v10"

// DiscordMessage represents a Discord message
type DiscordMessage struct {
	ID          string              `json:"id"`
	ChannelID   string              `json:"channel_id,omitempty"`
	Author      DiscordAuthor       `json:"author"`
	Content     string              `json:"content"`
	Timestamp   string              `json:"timestamp"`
	Attachments []DiscordAttachment `json:"attachments"`
	Embeds      []json.RawMessage   `json:"embeds"`
	Mentions    []DiscordAuthor     `json:"mentions"`
	Reactions   []DiscordReaction   `json:"reactions,omitempty"`
}

// DiscordAuthor represents a Discord user
type DiscordAuthor struct {
	ID         string  `json:"id"`
	Username   string  `json:"username"`
	GlobalName *string `json:"global_name"`
	Avatar     *string `json:"avatar"`
}

// DiscordAttachment represents a message attachment
type DiscordAttachment struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	ProxyURL    string `json:"proxy_url"`
	ContentType string `json:"content_type,omitempty"`
}

// DiscordReaction represents a message reaction
type DiscordReaction struct {
	Emoji DiscordEmoji `json:"emoji"`
	Count int          `json:"count"`
	Me    bool         `json:"me"`
}

// DiscordEmoji represents a Discord emoji
type DiscordEmoji struct {
	ID   *string `json:"id"`
	Name string  `json:"name"`
}

// MessagesCacheFile is the structure saved to disk
type MessagesCacheFile struct {
	Messages  []DiscordMessage `json:"messages"`
	CachedAt  string           `json:"cachedAt"`
	ChannelID string           `json:"channelId"`
}

func MessagesSync(args []string) error {
	if HasFlag(args, "--help", "-h") {
		printMessagesSyncHelp()
		return nil
	}

	settings, err := LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN environment variable required")
	}

	monthFilter := GetOption(args, "--month")
	channelFilter := GetOption(args, "--channel")

	// Positional year/month arg (e.g. "2025" or "2025/03")
	posYear, posMonth, posFound := ParseYearMonthArg(args)

	fmt.Printf("\n%s💬 Syncing Discord messages%s\n", Fmt.Bold, Fmt.Reset)
	fmt.Printf("%sDATA_DIR: %s%s\n", Fmt.Dim, DataDir(), Fmt.Reset)
	fmt.Printf("%sGuild: %s%s\n\n", Fmt.Dim, settings.Discord.GuildID, Fmt.Reset)

	// Get all channel IDs from settings
	channels := GetDiscordChannelIDs(settings)
	if len(channels) == 0 {
		return fmt.Errorf("no Discord channels configured in settings.json")
	}

	totalMessages := 0
	for name, channelID := range channels {
		if channelFilter != "" && channelID != channelFilter && name != channelFilter {
			continue
		}

		fmt.Printf("  #%s (%s)\n", name, channelID)

		messages, err := fetchAllChannelMessages(channelID, token)
		if err != nil {
			fmt.Printf("    %s✗ Error: %v%s\n", Fmt.Red, err, Fmt.Reset)
			continue
		}

		fmt.Printf("    %sFetched %d messages%s\n", Fmt.Dim, len(messages), Fmt.Reset)

		// Group by month
		byMonth := groupMessagesByMonth(messages)

		saved := 0
		for ym, monthMsgs := range byMonth {
			if monthFilter != "" && ym != monthFilter {
				continue
			}
			// Positional year/month filter
			if posFound {
				if posMonth != "" {
					if ym != fmt.Sprintf("%s-%s", posYear, posMonth) {
						continue
					}
				} else {
					if !strings.HasPrefix(ym, posYear+"-") {
						continue
					}
				}
			}

			parts := strings.Split(ym, "-")
			if len(parts) != 2 {
				continue
			}
			year, month := parts[0], parts[1]

			// Save to data/YYYY/MM/channels/discord/{channelId}/messages.json
			dataDir := DataDir()
			relPath := filepath.Join("channels", "discord", channelID, "messages.json")

			cache := MessagesCacheFile{
				Messages:  monthMsgs,
				CachedAt:  time.Now().UTC().Format(time.RFC3339),
				ChannelID: channelID,
			}

			data, _ := json.MarshalIndent(cache, "", "  ")
			if err := writeMonthFile(dataDir, year, month, relPath, data); err != nil {
				fmt.Printf("    %s✗ Failed to write: %v%s\n", Fmt.Red, err, Fmt.Reset)
				continue
			}

			saved++
			totalMessages += len(monthMsgs)
		}

		if saved > 0 {
			fmt.Printf("    %s✓ Saved %d months%s\n", Fmt.Green, saved, Fmt.Reset)
		}

		// Rate limit between channels
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Printf("\n%s✓ Done!%s %d messages synced\n\n", Fmt.Green, Fmt.Reset, totalMessages)
	return nil
}

func fetchAllChannelMessages(channelID, token string) ([]DiscordMessage, error) {
	var allMessages []DiscordMessage
	var before string

	for {
		url := fmt.Sprintf("%s/channels/%s/messages?limit=100", discordAPIBase, channelID)
		if before != "" {
			url += "&before=" + before
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bot "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == 429 {
			// Rate limited
			var rateLimitResp struct {
				RetryAfter float64 `json:"retry_after"`
			}
			json.NewDecoder(resp.Body).Decode(&rateLimitResp)
			resp.Body.Close()

			wait := time.Duration(rateLimitResp.RetryAfter*1000+100) * time.Millisecond
			fmt.Printf("    %sRate limited, waiting %v%s\n", Fmt.Yellow, wait, Fmt.Reset)
			time.Sleep(wait)
			continue
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("Discord API error: %d", resp.StatusCode)
		}

		var messages []DiscordMessage
		if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		if len(messages) == 0 {
			break
		}

		allMessages = append(allMessages, messages...)
		before = messages[len(messages)-1].ID

		// Rate limit
		time.Sleep(300 * time.Millisecond)
	}

	return allMessages, nil
}

func groupMessagesByMonth(messages []DiscordMessage) map[string][]DiscordMessage {
	byMonth := make(map[string][]DiscordMessage)
	tz := BrusselsTZ()

	for _, msg := range messages {
		t, err := time.Parse(time.RFC3339Nano, msg.Timestamp)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05+00:00", msg.Timestamp)
			if err != nil {
				continue
			}
		}
		t = t.In(tz)
		ym := fmt.Sprintf("%d-%02d", t.Year(), t.Month())
		byMonth[ym] = append(byMonth[ym], msg)
	}

	// Sort messages within each month by timestamp
	for ym := range byMonth {
		sort.Slice(byMonth[ym], func(i, j int) bool {
			return byMonth[ym][i].Timestamp < byMonth[ym][j].Timestamp
		})
	}

	return byMonth
}

func printMessagesSyncHelp() {
	f := Fmt
	fmt.Printf(`
%schb messages sync%s — Fetch Discord messages

%sUSAGE%s
  %schb messages sync%s [year[/month]] [options]

%sOPTIONS%s
  %s<year>%s               Sync all months of the given year (e.g. 2025)
  %s<year/month>%s         Sync a specific month (e.g. 2025/03)
  %s--month%s <YYYY-MM>    Fetch specific month only
  %s--channel%s <id|name>  Fetch specific channel only
  %s--help, -h%s           Show this help

%sENVIRONMENT%s
  %sDISCORD_BOT_TOKEN%s    Discord bot token
`,
		f.Bold, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
	)
}
