package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// OdooGet inspects one Odoo bank statement line by its global unique_import_id.
func OdooGet(args []string) error {
	if len(args) == 0 || HasFlag(args, "--help", "-h") {
		fmt.Printf("\n%schb odoo get <ref>%s — Inspect an Odoo statement line by unique_import_id\n\n", Fmt.Bold, Fmt.Reset)
		return nil
	}
	ref := strings.TrimSpace(args[0])
	if ref == "" {
		return fmt.Errorf("missing ref")
	}

	creds, err := ResolveOdooCredentials()
	if err != nil {
		return err
	}
	uid, err := odooAuth(creds.URL, creds.DB, creds.Login, creds.Password)
	if err != nil || uid == 0 {
		return fmt.Errorf("Odoo authentication failed: %v", err)
	}

	rows, err := odooSearchReadAllMaps(creds, uid, "account.bank.statement.line",
		[]interface{}{[]interface{}{"unique_import_id", "=", ref}},
		[]string{
			"id", "date", "payment_ref", "narration", "amount", "unique_import_id",
			"journal_id", "statement_id", "partner_id", "partner_bank_id", "move_id",
			"is_reconciled", "create_date", "write_date",
		},
		"date desc, id desc",
	)
	if err != nil {
		return fmt.Errorf("fetch Odoo line: %v", err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("no Odoo statement line found for ref %q", ref)
	}

	fmt.Printf("\n%sOdoo ref%s  %s\n\n", Fmt.Bold, Fmt.Reset, ref)
	for i, row := range rows {
		if i > 0 {
			fmt.Println()
		}
		printOdooGetLine(creds, uid, row)
	}
	fmt.Println()
	return nil
}

func printOdooGetLine(creds *OdooCredentials, uid int, row map[string]interface{}) {
	currency := "EUR"
	if jid := odooFieldID(row["journal_id"]); jid > 0 {
		if acc := linkedAccountForJournal(jid); acc != nil {
			currency = accCurrency(acc)
		}
	}

	w := 14
	pad := func(label string) string { return padRight(label+":", w) }
	fmt.Printf("  %s%s%s #%d\n", Fmt.Dim, pad("Line"), Fmt.Reset, odooInt(row["id"]))
	fmt.Printf("  %s%s%s %s\n", Fmt.Dim, pad("Date"), Fmt.Reset, odooString(row["date"]))
	fmt.Printf("  %s%s%s %s\n", Fmt.Dim, pad("Description"), Fmt.Reset, odooString(row["payment_ref"]))
	fmt.Printf("  %s%s%s %s\n", Fmt.Dim, pad("Amount"), Fmt.Reset, formatBalancePlain(odooFloat(row["amount"]), currency))
	fmt.Printf("  %s%s%s %s (#%d)\n", Fmt.Dim, pad("Journal"), Fmt.Reset, odooFieldName(row["journal_id"]), odooFieldID(row["journal_id"]))
	fmt.Printf("  %s%s%s %s (#%d)\n", Fmt.Dim, pad("Statement"), Fmt.Reset, odooFieldName(row["statement_id"]), odooFieldID(row["statement_id"]))
	fmt.Printf("  %s%s%s %s (#%d)\n", Fmt.Dim, pad("Partner"), Fmt.Reset, odooFieldName(row["partner_id"]), odooFieldID(row["partner_id"]))
	if bank := odooFieldName(row["partner_bank_id"]); bank != "" {
		fmt.Printf("  %s%s%s %s (#%d)\n", Fmt.Dim, pad("Partner bank"), Fmt.Reset, bank, odooFieldID(row["partner_bank_id"]))
	}
	fmt.Printf("  %s%s%s #%d\n", Fmt.Dim, pad("Move"), Fmt.Reset, odooFieldID(row["move_id"]))
	fmt.Printf("  %s%s%s %t\n", Fmt.Dim, pad("Reconciled"), Fmt.Reset, odooBool(row["is_reconciled"]))
	fmt.Printf("  %s%s%s %s\n", Fmt.Dim, pad("Created"), Fmt.Reset, odooString(row["create_date"]))
	fmt.Printf("  %s%s%s %s\n", Fmt.Dim, pad("Updated"), Fmt.Reset, odooString(row["write_date"]))

	if moveID := odooFieldID(row["move_id"]); moveID > 0 {
		printOdooGetMoveLines(creds, uid, moveID, currency)
	}
	printOdooGetNarration(row)
}

func printOdooGetMoveLines(creds *OdooCredentials, uid int, moveID int, currency string) {
	rows, err := odooSearchReadAllMaps(creds, uid, "account.move.line",
		[]interface{}{[]interface{}{"move_id", "=", moveID}},
		[]string{"id", "account_id", "partner_id", "name", "debit", "credit", "balance", "account_type"},
		"id asc",
	)
	if err != nil || len(rows) == 0 {
		return
	}
	fmt.Printf("\n  %sMove lines%s\n", Fmt.Bold, Fmt.Reset)
	table := make([][]string, 0, len(rows))
	for _, ml := range rows {
		table = append(table, []string{
			fmt.Sprintf("#%d", odooInt(ml["id"])),
			Truncate(odooFieldName(ml["account_id"]), 28),
			Truncate(odooFieldName(ml["partner_id"]), 20),
			Truncate(odooString(ml["name"]), 28),
			formatBalancePlain(odooFloat(ml["debit"]), currency),
			formatBalancePlain(odooFloat(ml["credit"]), currency),
			formatBalancePlain(odooFloat(ml["balance"]), currency),
		})
	}
	renderTicketsTable([]string{"ID", "Account", "Partner", "Label", "Debit", "Credit", "Balance"}, table, []string{"", Pluralize(len(rows), "move line", ""), "", "", "", "", ""}, map[int]bool{4: true, 5: true, 6: true})
}

func printOdooGetNarration(row map[string]interface{}) {
	narr := strings.TrimSpace(odooString(row["narration"]))
	if narr == "" {
		return
	}
	fmt.Printf("\n  %sNarration metadata%s\n", Fmt.Bold, Fmt.Reset)
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(narr), &meta); err == nil && len(meta) > 0 {
		keys := make([]string, 0, len(meta))
		for k := range meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %s%s:%s %v\n", Fmt.Dim, k, Fmt.Reset, meta[k])
		}
		return
	}
	fmt.Printf("  %s\n", narr)
}
