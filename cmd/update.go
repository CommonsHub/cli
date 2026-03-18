package cmd

import (
	"fmt"
	"os"
	"os/exec"
)

// Update runs go install to update chb to the latest version
func Update() error {
	fmt.Printf("%sUpdating chb...%s\n", Fmt.Dim, Fmt.Reset)

	goCmd := exec.Command("go", "install", "github.com/CommonsHub/chb@latest")
	goCmd.Env = append(os.Environ(), "GOPROXY=direct")
	goCmd.Stdout = os.Stdout
	goCmd.Stderr = os.Stderr

	if err := goCmd.Run(); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Printf("%s✓ Updated successfully%s\n", Fmt.Green, Fmt.Reset)
	fmt.Println("Run `chb version` to verify")
	return nil
}
