package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateMonthImagesGoUsesOriginalURLAndRelativeFilePath(t *testing.T) {
	tests := []struct {
		name         string
		year         string
		month        string
		wantFilePath string
	}{
		{
			name:         "monthly",
			year:         "2026",
			month:        "04",
			wantFilePath: "2026/04/messages/discord/images/att-1.png",
		},
		{
			name:         "latest",
			year:         "latest",
			month:        "",
			wantFilePath: "2026/04/messages/discord/images/att-1.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			discordDir := filepath.Join(dataDir, tt.year, tt.month, "messages", "discord", "chan-1")
			if err := os.MkdirAll(discordDir, 0755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			msgData := []byte(`{
			  "messages": [
			    {
			      "id": "msg-1",
			      "channel_id": "chan-1",
			      "author": {
			        "id": "user-1",
			        "username": "alice",
			        "global_name": "Alice <Admin>",
			        "avatar": "avatar-1"
			      },
			      "content": "<tag>& photo",
			      "timestamp": "2026-04-13T12:00:00.000000+00:00",
			      "attachments": [
			        {
			          "id": "att-1",
			          "url": "https://cdn.discordapp.com/attachments/file.png?ex=1&is=2",
			          "content_type": "image/png"
			        }
			      ],
			      "reactions": [
			        {
			          "emoji": {"name": "🔥"},
			          "count": 2
			        }
			      ]
			    }
			  ]
			}`)
			if err := os.WriteFile(filepath.Join(discordDir, "messages.json"), msgData, 0644); err != nil {
				t.Fatalf("write messages: %v", err)
			}

			if n := generateMonthImagesGo(dataDir, tt.year, tt.month); n != 1 {
				t.Fatalf("generateMonthImagesGo() = %d, want 1", n)
			}

			outPath := filepath.Join(dataDir, tt.year, tt.month, "generated", "images.json")
			outData, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf("read images.json: %v", err)
			}
			out := string(outData)

			if strings.Contains(out, `"proxyUrl"`) {
				t.Fatalf("images.json unexpectedly contains proxyUrl: %s", out)
			}
			if strings.Contains(out, `\u`) {
				t.Fatalf("images.json unexpectedly contains unicode escapes: %s", out)
			}
			if !strings.Contains(out, `"url": "https://cdn.discordapp.com/attachments/file.png?ex=1&is=2"`) {
				t.Fatalf("images.json missing original url: %s", out)
			}
			if !strings.Contains(out, `"filePath": "`+tt.wantFilePath+`"`) {
				t.Fatalf("images.json missing relative file path %q: %s", tt.wantFilePath, out)
			}
			if !strings.Contains(out, `"message": "<tag>& photo"`) {
				t.Fatalf("images.json escaped message content unexpectedly: %s", out)
			}
		})
	}
}
