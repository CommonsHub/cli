package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

type commitInfo struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Date string `json:"date"`
		} `json:"author"`
		Message string `json:"message"`
	} `json:"commit"`
}

type buildInfo struct {
	SHA  string
	Date string
}

func getBuildInfo() buildInfo {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return buildInfo{}
	}
	var b buildInfo
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			b.SHA = s.Value
		case "vcs.time":
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				b.Date = t.Format("2006-01-02 15:04")
			} else {
				b.Date = s.Value
			}
		}
	}
	return b
}

// CheckLatestVersion checks GitHub for the latest commit and compares
func CheckLatestVersion(currentVersion string) {
	bi := getBuildInfo()

	fmt.Printf("chb v%s", currentVersion)
	if bi.SHA != "" {
		short := bi.SHA
		if len(short) > 7 {
			short = short[:7]
		}
		fmt.Printf(" (%s, %s)", short, bi.Date)
	}
	fmt.Println()

	fmt.Printf("%sChecking for updates...%s", Fmt.Dim, Fmt.Reset)

	latest, err := getLatestCommit()
	if err != nil {
		fmt.Printf("\r\033[K%sCould not check for updates:%s %v\n", Fmt.Yellow, Fmt.Reset, err)
		return
	}

	fmt.Print("\r\033[K")

	if latest == nil {
		return
	}

	latestShort := latest.SHA[:7]
	ts := formatCommitDate(latest.Commit.Author.Date)
	msg := firstLine(latest.Commit.Message)

	isUpToDate := false
	if bi.SHA != "" {
		isUpToDate = strings.HasPrefix(latest.SHA, bi.SHA[:7]) || strings.HasPrefix(bi.SHA, latestShort)
	}

	if isUpToDate {
		fmt.Printf("%s✓ Up to date%s\n", Fmt.Green, Fmt.Reset)
	} else {
		fmt.Printf("%sLatest:%s %s (%s) %s%s%s\n", Fmt.Yellow, Fmt.Reset, latestShort, ts, Fmt.Dim, msg, Fmt.Reset)
		fmt.Printf("%sUpdate available!%s Run %schb update%s to update\n", Fmt.Yellow, Fmt.Reset, Fmt.Bold, Fmt.Reset)
	}
}

func getLatestCommit() (*commitInfo, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/CommonsHub/chb/commits/main")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var info commitInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func formatCommitDate(isoDate string) string {
	t, err := time.Parse(time.RFC3339, isoDate)
	if err != nil {
		return isoDate
	}
	return t.Format("2006-01-02 15:04")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
