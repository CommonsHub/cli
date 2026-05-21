package cmd

import (
	"fmt"
	"os"
	"strings"
)

// StepMark returns the terminal mark for a finished step. Error wins
// over warnings, warnings over a clean run. Centralised so every
// step-runner (pull / push / per-journal / generate) picks the same
// icon for the same condition.
func StepMark(err error, diag *StepDiagnostics) string {
	if err != nil || (diag != nil && len(diag.Errors) > 0) {
		return Fmt.Red + "✗" + Fmt.Reset
	}
	if diag != nil && len(diag.Warnings) > 0 {
		return Fmt.Yellow + "⚠" + Fmt.Reset
	}
	return Fmt.Green + "✓" + Fmt.Reset
}

// PrintCapturedDiagnostics renders the "Issues" footer for a sync run.
// Groups captured warnings/errors by step and prints each message
// once with the step it came from. Returns silently when nothing was
// captured so a clean run doesn't get an empty section.
//
// Called at the end of SyncAll / PullAll / PushAllTargets after the
// final status line has been drawn, so the footer never interleaves
// with the per-row stream.
func PrintCapturedDiagnostics() {
	steps := CapturedStepDiagnostics()
	if len(steps) == 0 {
		return
	}

	var totalWarn, totalErr int
	for _, s := range steps {
		totalWarn += len(s.Warnings)
		totalErr += len(s.Errors)
	}

	var parts []string
	if totalErr > 0 {
		parts = append(parts, pluralCount(totalErr, "error"))
	}
	if totalWarn > 0 {
		parts = append(parts, pluralCount(totalWarn, "warning"))
	}
	heading := "Issues"
	if len(parts) > 0 {
		heading = fmt.Sprintf("Issues (%s)", strings.Join(parts, ", "))
	}

	fmt.Fprintf(os.Stderr, "\n  %s%s%s\n", Fmt.Bold, heading, Fmt.Reset)
	for _, s := range steps {
		// Use the step's own mark so the footer entry visually matches
		// the row that surfaced it earlier.
		mark := Fmt.Yellow + "⚠" + Fmt.Reset
		if len(s.Errors) > 0 {
			mark = Fmt.Red + "✗" + Fmt.Reset
		}
		fmt.Fprintf(os.Stderr, "    %s %s%s%s\n", mark, Fmt.Bold, s.Label, Fmt.Reset)
		for _, msg := range s.Errors {
			fmt.Fprintf(os.Stderr, "        %s%s%s\n", Fmt.Red, formatFooterDetail(msg), Fmt.Reset)
		}
		for _, msg := range s.Warnings {
			fmt.Fprintf(os.Stderr, "        %s%s%s\n", Fmt.Dim, formatFooterDetail(msg), Fmt.Reset)
		}
	}
	if path := DiagnosticsLogPath(); path != "" {
		fmt.Fprintf(os.Stderr, "  %sFull log: %s%s\n", Fmt.Dim, path, Fmt.Reset)
	}
}

// formatFooterDetail strips the inline ANSI / leading "⚠" / "✗" /
// "Warning:" / "Error:" marks from a captured message so the footer's
// own colouring is the only signal of severity. Long single-line
// messages are kept on one line — the operator already has the full
// text in the log file.
func formatFooterDetail(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"⚠", "✗", "Warning:", "Error:", "WARN:", "ERROR:"} {
		s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
	}
	return s
}
