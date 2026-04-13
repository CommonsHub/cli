package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDiscordChannelIDsIncludesRoomChannels(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	settingsJSON := []byte(`{
		"discord": {
			"guildId": "guild-1",
			"roles": {},
			"channels": {
				"general": "123",
				"nested": {
					"ops": "456"
				}
			}
		}
	}`)
	if err := writeTestFile(chbDir(), "settings.json", settingsJSON); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	roomsJSON := []byte(`{
		"rooms": [
			{
				"id": "ostrom",
				"name": "Ostrom Room",
				"slug": "ostrom",
				"discordChannelId": "1443322327159803945"
			},
			{
				"id": "no-channel",
				"name": "No Channel",
				"slug": "no-channel"
			}
		]
	}`)
	if err := writeTestFile(chbDir(), "rooms.json", roomsJSON); err != nil {
		t.Fatalf("write rooms.json: %v", err)
	}

	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}

	got := GetDiscordChannelIDs(settings)
	want := map[string]string{
		"general":     "123",
		"nested/ops":  "456",
		"room/ostrom": "1443322327159803945",
	}

	if len(got) != len(want) {
		t.Fatalf("unexpected channel count: got %d want %d (%v)", len(got), len(want), got)
	}
	for key, wantID := range want {
		if got[key] != wantID {
			t.Fatalf("channel %q: got %q want %q", key, got[key], wantID)
		}
	}
}

func writeTestFile(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0644)
}
