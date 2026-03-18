package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const repoAPI = "https://api.github.com/repos/CommonsHub/chb/commits/main"

// CheckLatestVersion checks GitHub for the latest commit and compares with current version tag
func CheckLatestVersion(currentVersion string) {
	fmt.Printf("chb v%s\n", currentVersion)

	fmt.Printf("%sChecking for updates...%s", Fmt.Dim, Fmt.Reset)

	latest, err := getLatestTag()
	if err != nil {
		fmt.Printf("\r%sCould not check for updates:%s %v\n", Fmt.Yellow, Fmt.Reset, err)
		return
	}

	// Clear the "checking" line
	fmt.Print("\r\033[K")

	if latest == "" {
		fmt.Printf("chb v%s %s(latest)%s\n", currentVersion, Fmt.Green, Fmt.Reset)
		return
	}

	latestClean := strings.TrimPrefix(latest, "v")
	if latestClean == currentVersion {
		fmt.Printf("chb v%s %s(latest)%s\n", currentVersion, Fmt.Green, Fmt.Reset)
	} else {
		fmt.Printf("%sUpdate available:%s v%s → %s\n", Fmt.Yellow, Fmt.Reset, currentVersion, latest)
		fmt.Printf("Run %schb update%s to update\n", Fmt.Bold, Fmt.Reset)
	}
}

// getLatestTag fetches the latest release tag from GitHub
func getLatestTag() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	// Try releases first
	resp, err := client.Get("https://api.github.com/repos/CommonsHub/chb/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err == nil && release.TagName != "" {
			return release.TagName, nil
		}
	}

	// Fall back to tags
	resp2, err := client.Get("https://api.github.com/repos/CommonsHub/chb/tags?per_page=1")
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()

	if resp2.StatusCode == 200 {
		var tags []struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(resp2.Body).Decode(&tags); err == nil && len(tags) > 0 {
			return tags[0].Name, nil
		}
	}

	// No tags/releases yet
	return "", nil
}
