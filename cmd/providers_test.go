package cmd

import (
	"strings"
	"testing"
)

func TestProvidersCommandListsProvidersAndCommands(t *testing.T) {
	out := captureStdout(t, func() {
		if err := ProvidersCommand(nil); err != nil {
			t.Fatalf("ProvidersCommand: %v", err)
		}
	})

	for _, want := range []string{
		"chb providers",
		"ics        sync|generate",
		"chb sync       Same as chb providers * sync",
		"chb generate   Same as chb providers * generate",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("providers output missing %q:\n%s", want, out)
		}
	}
}

func TestProvidersCommandShowsProviderHelp(t *testing.T) {
	out := captureStdout(t, func() {
		if err := ProvidersCommand([]string{"ics", "help"}); err != nil {
			t.Fatalf("ProvidersCommand: %v", err)
		}
	})

	for _, want := range []string{
		"chb providers ics",
		"chb providers ics sync",
		"chb providers ics generate",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("provider help missing %q:\n%s", want, out)
		}
	}
}
