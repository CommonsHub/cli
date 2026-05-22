package cmd

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
)

// odooJournalConvertInvoicePayments rewrites invoice-payment
// reconciliations from a source journal onto an equivalent target
// journal. Designed for the "old j30 had A/R-via-partial-reconcile,
// new j48 has direct-revenue-posting" migration case.
//
// Scope: only acts on source lines reconciled to specific
// out_invoice / in_invoice / out_refund / in_refund moves. Lines
// reconciled to aggregate accounting moves (move_type=entry, e.g.
// "2025_STRIPE_01") are skipped — those aggregates already account
// for themselves on the target side via direct posting and don't
// need per-line migration.
//
// Stripe fee lines (payment_ref starting with "Billing - Usage Fee"
// or "Automatic Taxes" or category=stripe_fee) are skipped — they
// don't pay invoices.
//
// Matching: source line → target line by (date, amount). When
// multiple targets match the same (date, amount), pairs are
// assigned chronologically by line id within that bucket — same
// rank in the source goes to the same rank in the target.
//
// Apply: for each pair, reconcileStatementLineWithMove handles the
// unreconcile-and-reattach dance (source's reconcile chain is
// dropped; target's revenue counterpart is rewritten to A/R; new
// partial.reconcile is created against the same invoice).
func odooJournalConvertInvoicePayments(creds *OdooCredentials, uid int, sourceJournalID int, targetArg string, dryRun, verbose, yes bool) error {
	if !dryRun {
		if err := RequireOdooWriteCapability(); err != nil {
			return err
		}
	}
	targetJournalID, err := resolveJournalIDArg(targetArg)
	if err != nil {
		return err
	}
	if sourceJournalID == targetJournalID {
		return fmt.Errorf("--convert-invoice-payments target must be a different journal")
	}

	plan, err := buildOdooJournalConvertPlan(creds, uid, sourceJournalID, targetJournalID, verbose)
	if err != nil {
		return err
	}
	printOdooJournalConvertPlan(plan, dryRun, verbose)
	if dryRun {
		return nil
	}
	if len(plan.Pairs) == 0 {
		return nil
	}

	if !yes {
		fmt.Printf("\n  %sApply %d invoice-payment conversion%s on Odoo? [Y/n] %s",
			Fmt.Bold, len(plan.Pairs), plural(len(plan.Pairs)), Fmt.Reset)
		reader := bufio.NewReader(os.Stdin)
		resp, _ := reader.ReadString('\n')
		resp = strings.TrimSpace(strings.ToLower(resp))
		if resp == "n" || resp == "no" {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	applied, failed := applyOdooJournalConvertPlan(creds, uid, plan)
	fmt.Printf("\n  %sConverted:%s %d  %sFailed:%s %d\n\n",
		Fmt.Dim, Fmt.Reset, applied, Fmt.Dim, Fmt.Reset, failed)
	if failed > 0 {
		return fmt.Errorf("invoice-payment conversion failed for %d line(s)", failed)
	}
	return nil
}

type odooJournalConvertPlan struct {
	SourceJournalID int
	TargetJournalID int
	Pairs           []odooJournalConvertPair
	Skipped         []odooJournalConvertSkip
}

type odooJournalConvertPair struct {
	Source       odooStatementLineForReconcile
	Target       odooStatementLineForReconcile
	InvoiceMove  odooMoveCandidate
	InvoiceLabel string // human-readable invoice ref (move.name)
}

type odooJournalConvertSkip struct {
	Source odooStatementLineForReconcile
	Reason string
}

var aggregateMoveNamePattern = regexp.MustCompile(`^[0-9]{4}[_-][A-Z][A-Z0-9_-]+$`)

// stripeFeePattern matches the payment_ref of Stripe fee lines that
// don't pay invoices. Conservative: only known prefixes.
var stripeFeePattern = regexp.MustCompile(`(?i)^(billing\s*-\s*usage\s*fee|automatic\s*taxes|stripe\s*fee)`)

func buildOdooJournalConvertPlan(creds *OdooCredentials, uid int, sourceJournalID, targetJournalID int, verbose bool) (*odooJournalConvertPlan, error) {
	sourceLines, err := fetchJournalReconciledStatementLines(creds, uid, sourceJournalID)
	if err != nil {
		return nil, err
	}
	enrichOdooReconciledTargets(creds, uid, sourceLines)

	plan := &odooJournalConvertPlan{
		SourceJournalID: sourceJournalID,
		TargetJournalID: targetJournalID,
	}

	// Resolve every source line's reconciled-to move(s) so we can
	// classify (invoice vs aggregate vs unknown) and filter.
	invoiceLookup, err := loadInvoiceMovesForLines(creds, uid, sourceLines)
	if err != nil {
		return nil, err
	}

	// Walk the source, classify, and pick the relevant lines.
	type pendingSource struct {
		line    odooStatementLineForReconcile
		invoice odooMoveCandidate
		label   string
	}
	var pending []pendingSource
	for _, src := range sourceLines {
		if stripeFeePattern.MatchString(src.PaymentRef) {
			plan.Skipped = append(plan.Skipped, odooJournalConvertSkip{Source: src, Reason: "Stripe fee"})
			continue
		}
		if len(src.ReconciledLineIDs) == 0 {
			plan.Skipped = append(plan.Skipped, odooJournalConvertSkip{Source: src, Reason: "no reconciliation chain resolved"})
			continue
		}
		invoice, label, ok := pickInvoiceFromReconciledIDs(src.ReconciledLineIDs, invoiceLookup)
		if !ok {
			plan.Skipped = append(plan.Skipped, odooJournalConvertSkip{Source: src, Reason: fmt.Sprintf("reconciled to %s (not a real invoice)", src.ReconciledTo)})
			continue
		}
		pending = append(pending, pendingSource{line: src, invoice: invoice, label: label})
	}

	if len(pending) == 0 {
		return plan, nil
	}

	// Date window for the target query — narrow to dates we actually
	// need so a giant target journal doesn't blow the read budget.
	minDate, maxDate := "", ""
	for _, p := range pending {
		if minDate == "" || p.line.Date < minDate {
			minDate = p.line.Date
		}
		if maxDate == "" || p.line.Date > maxDate {
			maxDate = p.line.Date
		}
	}
	targetLines, err := fetchStatementLinesByDateWindowInJournal(creds, uid, targetJournalID, minDate, maxDate)
	if err != nil {
		return nil, err
	}

	// Index target lines by (date, amount) and sort each bucket by id
	// — same stable order Odoo creates them in, which is the
	// chronological-creation-order the operator asked for.
	targetByKey := map[string][]odooStatementLineForReconcile{}
	for _, tl := range targetLines {
		key := odooReconcileDateAmountKey(tl)
		if key == "" {
			continue
		}
		targetByKey[key] = append(targetByKey[key], tl)
	}
	for k := range targetByKey {
		sort.SliceStable(targetByKey[k], func(i, j int) bool {
			return targetByKey[k][i].ID < targetByKey[k][j].ID
		})
	}

	// Group source lines by (date, amount) so we can pair entire
	// buckets at once (chronological order within each bucket).
	sourceByKey := map[string][]pendingSource{}
	keyOrder := []string{}
	for _, p := range pending {
		key := odooReconcileDateAmountKey(p.line)
		if key == "" {
			plan.Skipped = append(plan.Skipped, odooJournalConvertSkip{Source: p.line, Reason: "no date/amount"})
			continue
		}
		if _, seen := sourceByKey[key]; !seen {
			keyOrder = append(keyOrder, key)
		}
		sourceByKey[key] = append(sourceByKey[key], p)
	}
	for k := range sourceByKey {
		sort.SliceStable(sourceByKey[k], func(i, j int) bool {
			return sourceByKey[k][i].line.ID < sourceByKey[k][j].line.ID
		})
	}

	// Chronological 1:1 pairing within each (date, amount) bucket.
	// Surplus on either side is reported as a skip.
	for _, key := range keyOrder {
		srcs := sourceByKey[key]
		tgts := targetByKey[key]
		n := len(srcs)
		if len(tgts) < n {
			n = len(tgts)
		}
		for i := 0; i < n; i++ {
			plan.Pairs = append(plan.Pairs, odooJournalConvertPair{
				Source:       srcs[i].line,
				Target:       tgts[i],
				InvoiceMove:  srcs[i].invoice,
				InvoiceLabel: srcs[i].label,
			})
		}
		for i := n; i < len(srcs); i++ {
			plan.Skipped = append(plan.Skipped, odooJournalConvertSkip{
				Source: srcs[i].line,
				Reason: fmt.Sprintf("no target on %s amount %.2f (only %d target line(s), %d source line(s))", srcs[i].line.Date, math.Abs(srcs[i].line.Amount), len(tgts), len(srcs)),
			})
		}
	}

	return plan, nil
}

// pickInvoiceFromReconciledIDs walks the line ids the source was
// reconciled to, finds the one that lives on an out_invoice /
// in_invoice / out_refund / in_refund move, and returns it as the
// invoice to migrate the payment to. Aggregate moves (move_type=
// "entry") are not considered invoices and return ok=false.
func pickInvoiceFromReconciledIDs(reconciledLineIDs []int, lookup map[int]odooMoveCandidate) (odooMoveCandidate, string, bool) {
	for _, lineID := range reconciledLineIDs {
		mv, ok := lookup[lineID]
		if !ok {
			continue
		}
		switch mv.MoveType {
		case "out_invoice", "in_invoice", "out_refund", "in_refund":
			return mv, mv.Name, true
		}
	}
	return odooMoveCandidate{}, "", false
}

// loadInvoiceMovesForLines reads every move that owns any of the
// reconciledLineIDs on the source lines, plus the line→move mapping
// so callers can resolve a counterpart-line back to its move
// quickly. Used to classify what each source line was reconciled to.
func loadInvoiceMovesForLines(creds *OdooCredentials, uid int, lines []odooStatementLineForReconcile) (map[int]odooMoveCandidate, error) {
	idSet := map[int]bool{}
	for _, ln := range lines {
		for _, id := range ln.ReconciledLineIDs {
			if id > 0 {
				idSet[id] = true
			}
		}
	}
	if len(idSet) == 0 {
		return map[int]odooMoveCandidate{}, nil
	}
	ids := make([]interface{}, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	rows, err := odooSearchReadAllMaps(creds, uid, "account.move.line",
		[]interface{}{[]interface{}{"id", "in", ids}},
		[]string{"id", "move_id", "amount_residual", "balance"}, "")
	if err != nil {
		return nil, err
	}
	moveIDs := map[int]bool{}
	lineToMoveID := map[int]int{}
	for _, row := range rows {
		moveID := odooFieldID(row["move_id"])
		if moveID > 0 {
			moveIDs[moveID] = true
		}
		lineToMoveID[odooInt(row["id"])] = moveID
	}
	if len(moveIDs) == 0 {
		return map[int]odooMoveCandidate{}, nil
	}
	moveIDList := make([]interface{}, 0, len(moveIDs))
	for id := range moveIDs {
		moveIDList = append(moveIDList, id)
	}
	moveRows, err := odooSearchReadAllMaps(creds, uid, "account.move",
		[]interface{}{[]interface{}{"id", "in", moveIDList}},
		[]string{"id", "name", "move_type", "partner_id", "amount_residual"}, "")
	if err != nil {
		return nil, err
	}
	moves := map[int]odooMoveCandidate{}
	for _, row := range moveRows {
		mv := odooMoveCandidate{
			ID:             odooInt(row["id"]),
			Name:           odooString(row["name"]),
			MoveType:       odooString(row["move_type"]),
			PartnerID:      odooFieldID(row["partner_id"]),
			PartnerName:    odooMoveFieldDisplay(row["partner_id"]),
			AmountResidual: odooFloat(row["amount_residual"]),
		}
		moves[mv.ID] = mv
	}
	out := map[int]odooMoveCandidate{}
	for lineID, moveID := range lineToMoveID {
		if mv, ok := moves[moveID]; ok {
			out[lineID] = mv
		}
	}
	return out, nil
}

func printOdooJournalConvertPlan(plan *odooJournalConvertPlan, dryRun, verbose bool) {
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Printf("\n  %sInvoice-payment conversion%s  %s%s%s\n", Fmt.Bold, Fmt.Reset, Fmt.Dim, mode, Fmt.Reset)
	fmt.Printf("  %sFrom journal #%d → journal #%d%s\n\n", Fmt.Dim, plan.SourceJournalID, plan.TargetJournalID, Fmt.Reset)
	fmt.Printf("  %sScope:%s only source lines reconciled to specific invoices/bills/refunds. Aggregate accounting moves and Stripe fees are skipped.\n", Fmt.Dim, Fmt.Reset)
	fmt.Printf("  %sAmbiguity:%s same date+amount on both sides → paired in chronological line-id order.\n\n", Fmt.Dim, Fmt.Reset)

	if len(plan.Pairs) > 0 {
		fmt.Printf("  %sPlanned conversions: %d%s\n", Fmt.Bold, len(plan.Pairs), Fmt.Reset)
		headers := []string{"Date", "Amount", "Source #", "Target #", "Invoice", "Description"}
		rows := make([][]string, 0, len(plan.Pairs))
		for i, pair := range plan.Pairs {
			if !verbose && i >= 25 {
				break
			}
			rows = append(rows, []string{
				pair.Source.Date,
				formatBalancePlain(pair.Source.Amount, "EUR"),
				fmt.Sprintf("#%d", pair.Source.ID),
				fmt.Sprintf("#%d", pair.Target.ID),
				Truncate(pair.InvoiceLabel, 20),
				Truncate(pair.Source.PaymentRef, 40),
			})
		}
		renderTicketsTable(headers, rows, nil, map[int]bool{1: true})
		if !verbose && len(plan.Pairs) > 25 {
			fmt.Printf("  %s… and %d more (-v to see all)%s\n", Fmt.Dim, len(plan.Pairs)-25, Fmt.Reset)
		}
	} else {
		fmt.Printf("  %sNo lines qualify for invoice-payment conversion.%s\n", Fmt.Dim, Fmt.Reset)
	}

	if len(plan.Skipped) > 0 {
		bucket := map[string]int{}
		for _, s := range plan.Skipped {
			bucket[s.Reason]++
		}
		reasons := make([]string, 0, len(bucket))
		for r := range bucket {
			reasons = append(reasons, r)
		}
		sort.Slice(reasons, func(i, j int) bool { return bucket[reasons[i]] > bucket[reasons[j]] })
		fmt.Printf("\n  %sSkipped: %d source line(s)%s\n", Fmt.Dim, len(plan.Skipped), Fmt.Reset)
		for _, r := range reasons {
			fmt.Printf("    %s· %s%s   %d\n", Fmt.Dim, r, Fmt.Reset, bucket[r])
		}
	}

	if dryRun {
		fmt.Printf("\n  %s(dry-run — no writes; re-run without --dry-run to apply.)%s\n\n", Fmt.Dim, Fmt.Reset)
	}
}

func applyOdooJournalConvertPlan(creds *OdooCredentials, uid int, plan *odooJournalConvertPlan) (applied, failed int) {
	for i, pair := range plan.Pairs {
		fmt.Printf("  %d/%d %sconverting%s line #%d → invoice %s\n", i+1, len(plan.Pairs), Fmt.Dim, Fmt.Reset, pair.Target.ID, pair.InvoiceLabel)
		targetForReconcile := odooStatementLineForReconcile{
			ID:     pair.Target.ID,
			MoveID: pair.Target.MoveID,
			Amount: pair.Target.Amount,
		}
		if err := reconcileStatementLineWithMove(creds, uid, targetForReconcile, pair.InvoiceMove); err != nil {
			failed++
			Warnf("  %s⚠ pair %d: %v%s", Fmt.Yellow, i+1, err, Fmt.Reset)
			continue
		}
		applied++
	}
	return applied, failed
}
