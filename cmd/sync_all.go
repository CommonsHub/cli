package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// formatCountSummary returns a short "12 new transactions" / "1 new
// transaction" / "" (empty when n == 0) phrase for use inside a status line.
func formatCountSummary(n int, singular, plural string) string {
	if n <= 0 {
		return ""
	}
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// formatNewSummary builds a summary for steps that produce two counts (e.g.
// CalendarsSync returns new bookings + new events). Empty when both are zero.
func formatNewSummary(a int, aSing, aPlu string, b int, bSing, bPlu string) string {
	s := formatCountSummary(a, aSing, aPlu)
	t := formatCountSummary(b, bSing, bPlu)
	switch {
	case s != "" && t != "":
		return s + ", " + t
	case s != "":
		return s
	case t != "":
		return t
	default:
		return ""
	}
}

// truncErr returns a single-line summary of err suitable for embedding in a
// status line. Long multi-line errors get clipped — the full text already
// went to stderr via Warnf.
func truncErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > 60 {
		msg = msg[:57] + "…"
	}
	return msg
}

// silenceStdout redirects os.Stdout to /dev/null and returns a closure
// that restores it. Used by the compact pull output to swallow each
// sub-sync's chatter and keep the per-step layout to one line.
// Stderr is intentionally left alone so Warnf / deferred warnings
// still reach the operator.
func silenceStdout() func() {
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return func() {}
	}
	origOut := os.Stdout
	os.Stdout = devnull
	_ = io.Discard
	return func() {
		os.Stdout = origOut
		_ = devnull.Close()
	}
}

// SyncSummary is the JSON shape returned by `chb sync --json`.
type SyncSummary struct {
	ElapsedMS       int64             `json:"elapsedMs"`
	NewBookings     int               `json:"newBookings"`
	NewEvents       int               `json:"newEvents"`
	NewTransactions int               `json:"newTransactions"`
	NewInvoices     int               `json:"newInvoices"`
	NewBills        int               `json:"newBills"`
	NewAttachments  int               `json:"newAttachments"`
	NewMessages     int               `json:"newMessages"`
	NewImages       int               `json:"newImages"`
	Errors          map[string]string `json:"errors,omitempty"`
}

// SyncAll runs all sync commands sequentially.
// Each sync function fetches all data in one API call (or paginated),
// then distributes to year/month folders.
func SyncAll(args []string) error {
	if HasFlag(args, "--help", "-h", "help") {
		PrintSyncAllHelp()
		return nil
	}

	// Verbose mode keeps the current per-step header banners and the
	// full sub-sync output. The default is compact: redirect each sub-
	// sync's stdout into a buffer, then print one summary line per step
	// with the returned counts. Errors are always surfaced via
	// recordErr regardless of verbosity.
	verbose := HasFlag(args, "--verbose", "-v") || HasFlag(args, "--debug")
	jsonMode := JSONMode(args)
	compact := !verbose && !jsonMode

	// In --json mode, silence every sub-sync's progress output by redirecting
	// stdout to /dev/null. Errors are captured into the summary and also
	// echoed to stderr by recordErr so they're visible without parsing JSON.
	var origStdout *os.File
	if jsonMode {
		origStdout = os.Stdout
		if devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
			os.Stdout = devnull
			defer func() {
				os.Stdout = origStdout
				_ = devnull.Close()
			}()
		}
	}

	startedAt := time.Now()

	if HasFlag(args, "--history") || GetOption(args, "--since") != "" {
		fmt.Printf("\n%s🔄 Pulling history...%s\n", Fmt.Bold, Fmt.Reset)
	} else {
		fmt.Printf("\n%s🔄 Pulling…%s\n", Fmt.Bold, Fmt.Reset)
	}

	var newBookings, newEvents, newTx, newInvoices, newBills, newAttachments, newMessages, newImages int
	errs := map[string]string{}
	recordErr := func(source string, err error) {
		if err == nil {
			return
		}
		errs[source] = err.Error()
		if jsonMode {
			// Stdout is silenced; surface the error directly on stderr.
			Warnf("⚠ %s: %v", source, err)
			return
		}
		Warnf("%s⚠ %s: %v%s", Fmt.Yellow, source, err, Fmt.Reset)
	}

	// step runs fn with stdout swallowed (compact mode) or untouched
	// (verbose), times it, and prints a live status line via StatusLine
	// (compact) or a per-step banner (verbose). The fn returns a short
	// summary string to embed in the final line (e.g. "12 new, 3 updated").
	step := func(label string, fn func() (string, error)) {
		key := strings.ToLower(label)
		if !compact {
			fmt.Printf("\n%s━━━ %s ━━━%s\n", Fmt.Bold, label, Fmt.Reset)
			_, err := fn()
			recordErr(key, err)
			return
		}
		// Compact mode: live status line on stderr, sub-sync stdout
		// silenced so its chatter doesn't break the layout.
		sl := NewStatusLine(label)
		SetActiveStatusLine(sl)
		restore := silenceStdout()
		summary, err := fn()
		restore()
		SetActiveStatusLine(nil)
		recordErr(key, err)
		mark := Fmt.Green + "✓" + Fmt.Reset
		if err != nil {
			mark = Fmt.Red + "✗" + Fmt.Reset
			if summary == "" {
				summary = "error: " + truncErr(err)
			}
		}
		sl.Final(mark, summary)
	}

	step("Calendars", func() (string, error) {
		b, e, err := CalendarsSync(args)
		newBookings = b
		newEvents = e
		return formatNewSummary(b, "booking", "bookings", e, "event", "events"), err
	})
	step("Transactions", func() (string, error) {
		n, err := TransactionsSync(args)
		newTx = n
		return formatCountSummary(n, "new transaction", "new transactions"), err
	})
	step("Messages", func() (string, error) {
		n, err := MessagesSync(args)
		newMessages = n
		return formatCountSummary(n, "new message", "new messages"), err
	})

	if os.Getenv("ODOO_URL") != "" {
		step("Odoo categories", func() (string, error) {
			n, err := OdooAnalyticSync(args)
			return formatCountSummary(n, "new category", "new categories"), err
		})
		step("Invoices", func() (string, error) {
			n, err := InvoicesSync(args)
			newInvoices = n
			return formatCountSummary(n, "invoice", "invoices"), err
		})
		step("Bills", func() (string, error) {
			n, err := BillsSync(args)
			newBills = n
			return formatCountSummary(n, "bill", "bills"), err
		})
	}

	if os.Getenv("STRIPE_SECRET_KEY") != "" || os.Getenv("ODOO_URL") != "" {
		step("Members", func() (string, error) { return "", MembersSync(args) })
	}

	if os.Getenv("ODOO_URL") != "" {
		step("Attachments", func() (string, error) {
			n, err := AttachmentsSync(args)
			newAttachments = n
			return formatCountSummary(n, "attachment", "attachments"), err
		})
	}

	step("Images", func() (string, error) {
		n, err := ImagesSync(args)
		newImages = n
		return formatCountSummary(n, "new image", "new images"), err
	})
	step("Generate", func() (string, error) { return "", Generate(args) })

	// Note: pushing local data to Odoo is intentionally NOT part of
	// `chb sync`. Sync is read-only (fetch from providers + local
	// transform). To push to Odoo journals, run
	// `chb odoo journals sync` explicitly after this finishes.
	if os.Getenv("ODOO_URL") != "" {
		fmt.Printf("\n  %sTo push to Odoo: chb odoo journals sync%s\n", Fmt.Dim, Fmt.Reset)
	}

	// Print summary
	hasAny := newBookings > 0 || newTx > 0 || newInvoices > 0 || newBills > 0 || newAttachments > 0 || newMessages > 0 || newImages > 0
	elapsed := time.Since(startedAt).Round(100 * time.Millisecond)

	if jsonMode {
		os.Stdout = origStdout
		summary := SyncSummary{
			ElapsedMS:       elapsed.Milliseconds(),
			NewBookings:     newBookings,
			NewEvents:       newEvents,
			NewTransactions: newTx,
			NewInvoices:     newInvoices,
			NewBills:        newBills,
			NewAttachments:  newAttachments,
			NewMessages:     newMessages,
			NewImages:       newImages,
			Errors:          errs,
		}
		_ = EmitJSON(summary)
		return nil
	}
	if hasAny {
		fmt.Printf("\n%s✓ Sync complete in %s%s\n", Fmt.Green, elapsed, Fmt.Reset)
		if newBookings > 0 {
			if newEvents > 0 {
				fmt.Printf("  📅 %d new bookings, including %d events\n", newBookings, newEvents)
			} else {
				fmt.Printf("  📅 %d new bookings\n", newBookings)
			}
		}
		if newTx > 0 {
			fmt.Printf("  💰 %d new transactions\n", newTx)
		}
		if newInvoices > 0 {
			fmt.Printf("  🧾 %d invoices\n", newInvoices)
		}
		if newBills > 0 {
			fmt.Printf("  🧾 %d bills\n", newBills)
		}
		if newAttachments > 0 {
			fmt.Printf("  📎 %d attachments\n", newAttachments)
		}
		if newMessages > 0 {
			fmt.Printf("  💬 %d new messages\n", newMessages)
		}
		if newImages > 0 {
			fmt.Printf("  📸 %d new images\n", newImages)
		}
	} else {
		fmt.Printf("\n%s✓ Everything up to date in %s%s\n", Fmt.Green, elapsed, Fmt.Reset)
	}
	fmt.Println()

	return nil
}
