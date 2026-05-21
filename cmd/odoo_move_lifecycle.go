package cmd

import (
	"fmt"
)

// withOdooMoveTemporarilyDraft is the shared "draft → mutate → post"
// helper used by every code path that has to write to a non-bank field
// on a posted account.move (counterpart account_id rewrites,
// payment_ref / narration refresh, internal-transfer flips). Odoo
// rejects writes to a posted move with "vous ne pouvez pas supprimer
// une écriture comptable validée" — moves have to drop to draft first.
//
// The helper makes the lifecycle safe in two ways:
//
//  1. It reads the current state up front. If the move is already in
//     `draft`, we skip button_draft (which would fail with "Seules les
//     pièces comptabilisées/annulées peuvent être remises en
//     brouillon"). We also skip the trailing action_post so we don't
//     promote a draft the operator intentionally left unposted.
//  2. The post step runs in a deferred-style block so a write failure
//     in fn still re-posts a previously-posted move — no half-drafted
//     entries stranded in the journal.
//
// Returns the first error encountered. Cancelled / unknown states are
// surfaced as errors so callers don't silently mis-handle them.
func withOdooMoveTemporarilyDraft(creds *OdooCredentials, uid, moveID int, fn func() error) error {
	if moveID == 0 {
		return fmt.Errorf("missing move_id")
	}
	state, err := readOdooMoveState(creds, uid, moveID)
	if err != nil {
		return fmt.Errorf("read move #%d state: %v", moveID, err)
	}
	switch state {
	case "draft":
		// Already in draft — write directly, don't try to draft again
		// and don't auto-post afterwards (preserve operator intent).
		return fn()
	case "posted":
		if _, derr := odooExec(creds.URL, creds.DB, uid, creds.Password,
			"account.move", "button_draft",
			[]interface{}{[]interface{}{moveID}}, nil); derr != nil {
			return fmt.Errorf("draft move #%d: %v", moveID, derr)
		}
		fnErr := fn()
		// Always re-post — even if fn failed, we don't want to leave
		// the move in draft state. The fn error takes precedence over
		// the post error in the return.
		_, postErr := odooExec(creds.URL, creds.DB, uid, creds.Password,
			"account.move", "action_post",
			[]interface{}{[]interface{}{moveID}}, nil)
		if fnErr != nil {
			return fnErr
		}
		if postErr != nil {
			return fmt.Errorf("repost move #%d: %v", moveID, postErr)
		}
		return nil
	case "cancel":
		return fmt.Errorf("move #%d is cancelled; refusing to mutate", moveID)
	default:
		return fmt.Errorf("move #%d has unexpected state %q", moveID, state)
	}
}

// readOdooMoveState returns the `state` field of an account.move.
// Returns "" without error when the move id resolves to no row.
func readOdooMoveState(creds *OdooCredentials, uid, moveID int) (string, error) {
	rows, err := odooSearchReadAllMaps(creds, uid, "account.move",
		[]interface{}{[]interface{}{"id", "=", moveID}},
		[]string{"id", "state"}, "")
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return odooString(rows[0]["state"]), nil
}

// assertNotBankCashLine returns an actionable error when the given
// account.move.line lives on the journal's default account
// (i.e. the bank-side line of a bank-statement move). Rewriting that
// line's account_id removes the only journal-default_account_id entry
// on the move and Odoo refuses with the "exactly one entry involving
// the bank or cash account" constraint. Odoo enforces the rule by
// matching the exact default_account_id, not the account_type — so we
// resolve the journal's default account and compare ids directly.
func assertNotBankCashLine(creds *OdooCredentials, uid, lineID int) error {
	if lineID <= 0 {
		return nil
	}
	lineRows, err := odooSearchReadAllMaps(creds, uid, "account.move.line",
		[]interface{}{[]interface{}{"id", "=", lineID}},
		[]string{"id", "account_id", "move_id"}, "")
	if err != nil {
		return fmt.Errorf("read line: %v", err)
	}
	if len(lineRows) == 0 {
		return nil
	}
	accID := odooFieldID(lineRows[0]["account_id"])
	moveID := odooFieldID(lineRows[0]["move_id"])
	if accID == 0 || moveID == 0 {
		return nil
	}
	defaultAccountID, err := fetchJournalDefaultAccount(creds, uid, moveID)
	if err != nil {
		return fmt.Errorf("resolve journal default account: %v", err)
	}
	if defaultAccountID > 0 && accID == defaultAccountID {
		accRows, _ := odooSearchReadAllMaps(creds, uid, "account.account",
			[]interface{}{[]interface{}{"id", "=", accID}},
			[]string{"id", "code", "name", "account_type"}, "")
		code, name, t := "", "", ""
		if len(accRows) > 0 {
			code = odooString(accRows[0]["code"])
			name = odooString(accRows[0]["name"])
			t = odooString(accRows[0]["account_type"])
		}
		return fmt.Errorf("refusing to rewrite account_id of the bank line "+
			"(line #%d on account #%d %s %q, type=%s, which is the "+
			"journal's default account) — that would violate Odoo's "+
			"one-bank-line-per-move rule. The counterpart lookup picked the "+
			"wrong line; check the move's structure in Odoo.",
			lineID, accID, code, name, t)
	}
	return nil
}
