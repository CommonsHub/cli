package cmd

import (
	"fmt"
	"os"
	"os/exec"
)

// Update runs go install to update chb to the latest version
func Update() error {
	// Show what we're updating from
	bi := getBuildInfo()
	if bi.SHA != "" {
		short := bi.SHA
		if len(short) > 7 {
			short = short[:7]
		}
		fmt.Printf("Current: %s (%s)\n", short, bi.Date)
	}

	// Check latest before updating
	latest, err := getLatestCommit()
	if err == nil && latest != nil {
		ts := formatCommitDate(latest.Commit.Author.Date)
		msg := firstLine(latest.Commit.Message)
		fmt.Printf("Latest:  %s (%s) %s%s%s\n", latest.SHA[:7], ts, Fmt.Dim, msg, Fmt.Reset)
	}

	fmt.Printf("\n%sUpdating...%s\n", Fmt.Dim, Fmt.Reset)

	goCmd := exec.Command("go", "install", "github.com/CommonsHub/chb@latest")
	goCmd.Env = append(os.Environ(), "GOPROXY=direct")
	goCmd.Stdout = os.Stdout
	goCmd.Stderr = os.Stderr

	if err := goCmd.Run(); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Printf("%s✓ Updated successfully%s\n", Fmt.Green, Fmt.Reset)
	return nil
}
