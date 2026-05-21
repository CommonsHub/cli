package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	diagnosticsMu       sync.Mutex
	diagnosticsFile     *os.File
	diagnosticsPath     string
	diagnosticsWarnings int
	diagnosticsErrors   int
	deferWarningEcho    bool
	deferredWarnings    []string
	ansiPattern         = regexp.MustCompile(`\x1b\[[0-9;]*m`)

	// Step diagnostic capture: while a step is active, Warnf/Errorf
	// are buffered onto stepStack[top] instead of echoing to stderr,
	// so the row's ⚠/✗ mark reflects whether the step had issues
	// without interrupting the spinner mid-frame. Detail messages
	// land in the daily log file only — the consolidated tail line
	// from PrintDiagnosticsSummary reports the count at process exit.
	stepStack     []*StepDiagnostics
	capturedSteps []StepDiagnostics
)

// StepDiagnostics captures warnings and errors emitted during one
// labelled step (e.g. "Discord", "#47 savings", "Generate"). Steps
// without any captures are not appended to capturedSteps, so the
// footer only mentions steps that had something to report.
type StepDiagnostics struct {
	Label    string
	Warnings []string
	Errors   []string
}

// HasIssues reports whether the step accumulated any warnings/errors.
func (d *StepDiagnostics) HasIssues() bool {
	return d != nil && (len(d.Warnings) > 0 || len(d.Errors) > 0)
}

// BeginStepDiagnostics pushes a new step bucket. Warnings and errors
// emitted via Warnf/Errorf until the matching EndStepDiagnostics are
// captured onto this bucket and *not* echoed to stderr. The caller is
// expected to render its own status line and use the returned bucket
// (or call EndStepDiagnostics) to decide on the final mark.
//
// Calls nest: an inner step's diagnostics attach to the innermost
// bucket, so a step that itself calls into sub-steps does not lose
// messages emitted at the outer level.
func BeginStepDiagnostics(label string) *StepDiagnostics {
	d := &StepDiagnostics{Label: label}
	diagnosticsMu.Lock()
	stepStack = append(stepStack, d)
	diagnosticsMu.Unlock()
	return d
}

// EndStepDiagnostics pops the top step bucket, appends it to
// capturedSteps if it had any issues, and returns it. Safe to call
// without a matching Begin — returns nil in that case.
func EndStepDiagnostics() *StepDiagnostics {
	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()
	if len(stepStack) == 0 {
		return nil
	}
	d := stepStack[len(stepStack)-1]
	stepStack = stepStack[:len(stepStack)-1]
	if d != nil && (len(d.Warnings) > 0 || len(d.Errors) > 0) {
		capturedSteps = append(capturedSteps, *d)
	}
	return d
}

// CapturedStepDiagnostics returns a snapshot of every finalized step
// that had warnings or errors. Used by the footer renderer.
func CapturedStepDiagnostics() []StepDiagnostics {
	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()
	out := make([]StepDiagnostics, len(capturedSteps))
	copy(out, capturedSteps)
	return out
}

// ResetCapturedStepDiagnostics clears the captured-steps slice so the
// next sync run starts fresh. Called at the start of SyncAll /
// PullAll / PushAll entry points.
func ResetCapturedStepDiagnostics() {
	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()
	capturedSteps = nil
	stepStack = nil
}

func Warnf(format string, args ...interface{}) {
	writeDiagnostic("warning", true, format, args...)
}

func LogWarningf(format string, args ...interface{}) {
	writeDiagnostic("warning", false, format, args...)
}

func Errorf(format string, args ...interface{}) {
	writeDiagnostic("error", true, format, args...)
}

func LogErrorf(format string, args ...interface{}) {
	writeDiagnostic("error", false, format, args...)
}

func DiagnosticsLogPath() string {
	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()
	return diagnosticsPath
}

// DiagnosticsSummary returns a one-line tail like
//
//	"1 new error, 8 new warnings — see /home/.../20260521.log"
//
// The counts reflect Warnf/Errorf events emitted during this process
// only, which is also exactly how many lines were appended to the
// daily log file — so the operator can verify the count by diffing
// the log before and after the run (`wc -l 20260521.log`).
func DiagnosticsSummary() string {
	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()
	if diagnosticsWarnings == 0 && diagnosticsErrors == 0 {
		return ""
	}
	var parts []string
	if diagnosticsErrors > 0 {
		parts = append(parts, pluralCount(diagnosticsErrors, "new error"))
	}
	if diagnosticsWarnings > 0 {
		parts = append(parts, pluralCount(diagnosticsWarnings, "new warning"))
	}
	return fmt.Sprintf("%s — see %s", strings.Join(parts, ", "), diagnosticsPath)
}

func PrintDiagnosticsSummary() {
	summary := DiagnosticsSummary()
	if summary == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "%s%s%s\n", Fmt.Dim, summary, Fmt.Reset)
}

func BeginDeferredWarnings() {
	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()
	deferWarningEcho = true
	deferredWarnings = nil
}

func EndDeferredWarnings() []string {
	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()
	deferWarningEcho = false
	out := append([]string(nil), deferredWarnings...)
	deferredWarnings = nil
	return out
}

func Fatalf(format string, args ...interface{}) {
	Errorf(format, args...)
	ExitWithDiagnostics(1)
}

func ExitWithDiagnostics(code int) {
	PrintDiagnosticsSummary()
	CloseDiagnosticsLog()
	os.Exit(code)
}

func pluralCount(n int, singular string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %ss", n, singular)
}

func CloseDiagnosticsLog() {
	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()
	if diagnosticsFile != nil {
		_ = diagnosticsFile.Close()
		diagnosticsFile = nil
	}
}

func writeDiagnostic(level string, echo bool, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	message = strings.TrimRight(message, "\n")

	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()

	switch level {
	case "warning":
		diagnosticsWarnings++
	case "error":
		diagnosticsErrors++
	}

	// Capture into the innermost active step bucket regardless of the
	// `echo` flag — even log-only warnings (LogWarningf) belong in the
	// consolidated footer so the operator notices things like processor
	// failures from generate sub-steps. Echo to stderr is suppressed
	// while a step is active so the per-row layout isn't interrupted
	// mid-spinner; the footer surfaces details at the end.
	if len(stepStack) > 0 {
		bucket := stepStack[len(stepStack)-1]
		clean := ansiPattern.ReplaceAllString(message, "")
		switch level {
		case "warning":
			bucket.Warnings = append(bucket.Warnings, clean)
		case "error":
			bucket.Errors = append(bucket.Errors, clean)
		}
	} else if echo && level == "warning" && deferWarningEcho {
		deferredWarnings = append(deferredWarnings, message)
	} else if echo {
		fmt.Fprintf(os.Stderr, "%s\n", message)
	}

	if diagnosticsFile == nil {
		logDir := defaultDiagnosticsDir()
		_ = os.MkdirAll(logDir, 0755)
		diagnosticsPath = diagnosticsLogPath(logDir, time.Now())
		diagnosticsFile, _ = os.OpenFile(diagnosticsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	}
	if diagnosticsFile == nil {
		return
	}

	clean := ansiPattern.ReplaceAllString(message, "")
	_, _ = fmt.Fprintf(diagnosticsFile, "%s [%s] %s\n", time.Now().Format(time.RFC3339), strings.ToUpper(level), clean)
}

func diagnosticsLogPath(dir string, t time.Time) string {
	return filepath.Join(dir, t.Format("20060102")+".log")
}

// defaultDiagnosticsDir returns the directory where daily log files
// land — kept under $APP_DATA_DIR/data/logs so logs don't accidentally
// get committed to the repo when chb is invoked from a working tree.
// Falls back to the current working directory if DataDir can't be
// computed for any reason.
func defaultDiagnosticsDir() string {
	dir := DataDir()
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "."
		}
		return cwd
	}
	return filepath.Join(dir, "logs")
}
