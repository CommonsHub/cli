package cmd

import (
	"encoding/json"
	"sync"
)

// mirrorOdooWriteToLocalCache applies the effect of a successful
// mutating Odoo RPC to the matching local cache file so subsequent
// reads (dry-run, reconcile, transactions table) see the change without
// requiring an explicit `chb pull`.
//
// Wired into odooExec so every call site benefits automatically. Best
// effort: silently no-ops when the model/method isn't recognised or
// when the args don't match the expected shape — the local cache
// stays consistent with Odoo by either being updated here or being
// refetched on the next pull.
//
// When the change can't be reflected by mirroring (e.g. reconcile side
// effects that flip is_reconciled and rewrite counterpart account_id
// on related lines), the affected journal is added to a refetch queue
// that FlushOdooCacheRefetches drains at the end of the operation.
func mirrorOdooWriteToLocalCache(model, method string, args []interface{}, result json.RawMessage) {
	if !isMutatingOdooMethod(method) {
		return
	}
	switch model {
	case "account.bank.statement.line":
		switch method {
		case "write":
			mirrorBankStatementLineWrite(args)
		case "create":
			// New lines: invalidate the journal cache so the next read
			// refetches. We don't know the journal id from the response
			// alone without an extra read — handled by queueing.
			queueJournalsForRefetchFromCreateArgs(args)
		case "unlink":
			mirrorBankStatementLineUnlink(args)
		}
	case "account.move.line":
		switch method {
		case "write":
			// Reconcile and account-rewrite paths edit move-line fields
			// (account_id, partner_id) that are mirrored into the journal
			// lines cache as counterpartId / partnerId. Queue refetch
			// rather than try to map move-line ids → bank lines here.
			queueAllLinkedJournalsForRefetch()
		}
	case "account.move":
		switch method {
		case "button_draft", "action_post":
			// State toggles around a write — handled by the surrounding
			// statement-line/move-line writes.
		case "unlink":
			queueAllLinkedJournalsForRefetch()
		}
	}
}

// mirrorBankStatementLineWrite applies args[1] (the vals map) to every
// cached line with id in args[0]. Args shape:
//
//	[[ids...], {field: val, ...}]
func mirrorBankStatementLineWrite(args []interface{}) {
	if len(args) < 2 {
		return
	}
	ids := extractIDList(args[0])
	if len(ids) == 0 {
		return
	}
	vals, ok := args[1].(map[string]interface{})
	if !ok || len(vals) == 0 {
		return
	}
	wanted := make(map[int]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	forEachCachedJournal(func(journalID int, lines []OdooCacheLine) (bool, []OdooCacheLine) {
		changed := false
		for i := range lines {
			if !wanted[lines[i].ID] {
				continue
			}
			if applyOdooLineFieldsToCache(&lines[i], vals) {
				changed = true
			}
		}
		return changed, lines
	})
}

// mirrorBankStatementLineUnlink removes deleted lines from every
// matching journal cache. Args shape: [[ids...]].
func mirrorBankStatementLineUnlink(args []interface{}) {
	if len(args) < 1 {
		return
	}
	ids := extractIDList(args[0])
	if len(ids) == 0 {
		return
	}
	wanted := make(map[int]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	forEachCachedJournal(func(journalID int, lines []OdooCacheLine) (bool, []OdooCacheLine) {
		out := lines[:0]
		changed := false
		for _, ln := range lines {
			if wanted[ln.ID] {
				changed = true
				continue
			}
			out = append(out, ln)
		}
		return changed, out
	})
}

// applyOdooLineFieldsToCache copies the relevant fields from an Odoo
// write payload into a cached line. Returns true if anything changed.
// Only fields we track in OdooCacheLine are applied.
func applyOdooLineFieldsToCache(line *OdooCacheLine, vals map[string]interface{}) bool {
	changed := false
	for k, v := range vals {
		switch k {
		case "payment_ref":
			if s, ok := v.(string); ok && line.PaymentRef != s {
				line.PaymentRef = s
				changed = true
			}
		case "narration":
			if s, ok := v.(string); ok && line.Narration != s {
				line.Narration = s
				changed = true
			}
		case "partner_id":
			if id := odooFieldID(v); id > 0 && line.PartnerID != id {
				line.PartnerID = id
				changed = true
			}
		case "amount":
			if amt := toFloat(v); amt != 0 && line.Amount != amt {
				line.Amount = amt
				changed = true
			}
		case "unique_import_id":
			if s, ok := v.(string); ok && line.UniqueImportID != s {
				line.UniqueImportID = s
				changed = true
			}
		}
	}
	return changed
}

// forEachCachedJournal iterates every linked Odoo journal's local
// cache, invokes fn on each, and persists the result when fn reports a
// change. Order is irrelevant; fn is called serially.
func forEachCachedJournal(fn func(journalID int, lines []OdooCacheLine) (changed bool, updated []OdooCacheLine)) {
	for _, jid := range allLinkedOdooJournalIDs() {
		lines, ok := loadLatestOdooJournalLinesCache(jid)
		if !ok {
			continue
		}
		changed, updated := fn(jid, lines)
		if !changed {
			continue
		}
		_, _ = writeOdooJournalLinesCacheFile(jid, updated)
	}
}

// allLinkedOdooJournalIDs returns every linked Odoo journal id from
// accounts.json. Cached per process; rebuilt on demand from a fresh
// LoadAccountConfigs read.
func allLinkedOdooJournalIDs() []int {
	configs := LoadAccountConfigs()
	seen := map[int]bool{}
	out := make([]int, 0, len(configs))
	for _, acc := range configs {
		if acc.OdooJournalID > 0 && !seen[acc.OdooJournalID] {
			seen[acc.OdooJournalID] = true
			out = append(out, acc.OdooJournalID)
		}
	}
	return out
}

// extractIDList coerces an Odoo args[0] (either []int / []interface{} /
// []int64) into a flat []int. Handles the common shapes; returns an
// empty slice on anything unexpected.
func extractIDList(v interface{}) []int {
	switch ids := v.(type) {
	case []int:
		return ids
	case []int64:
		out := make([]int, len(ids))
		for i, id := range ids {
			out[i] = int(id)
		}
		return out
	case []interface{}:
		out := make([]int, 0, len(ids))
		for _, x := range ids {
			if id := odooFieldID(x); id > 0 {
				out = append(out, id)
			} else if i, ok := x.(int); ok {
				out = append(out, i)
			}
		}
		return out
	}
	if id := odooFieldID(v); id > 0 {
		return []int{id}
	}
	return nil
}

// toFloat coerces JSON numerics to float64. Stripped-down version of
// odoo helpers — only used here to avoid pulling in the wider odoo
// conversion surface.
func toFloat(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}

// ─── Refetch queue ───
//
// Some writes (reconcile, create, unlink) have effects we can't easily
// mirror in-process — they change related rows on the server side
// (counterpart accounts, statement totals, balance fields). The
// affected journals are queued here and the caller drains the queue at
// a natural boundary via FlushOdooCacheRefetches so we refetch each
// journal at most once per operation instead of after every RPC.

var (
	odooRefetchMu       sync.Mutex
	odooRefetchJournals = map[int]bool{}
)

// QueueOdooJournalRefetch marks a journal as needing a cache refresh
// before the next read. Exported so non-RPC code paths (e.g. KBC merge
// finishing a batch of writes outside odooExec) can opt in too.
func QueueOdooJournalRefetch(journalID int) {
	if journalID <= 0 {
		return
	}
	odooRefetchMu.Lock()
	odooRefetchJournals[journalID] = true
	odooRefetchMu.Unlock()
}

func queueAllLinkedJournalsForRefetch() {
	for _, jid := range allLinkedOdooJournalIDs() {
		QueueOdooJournalRefetch(jid)
	}
}

// queueJournalsForRefetchFromCreateArgs inspects an Odoo create
// payload and queues any journals referenced via the "journal_id"
// field. Args shape: [[{...vals...}, ...]] (Odoo's batch-create form).
func queueJournalsForRefetchFromCreateArgs(args []interface{}) {
	if len(args) < 1 {
		return
	}
	rows, ok := args[0].([]interface{})
	if !ok {
		queueAllLinkedJournalsForRefetch()
		return
	}
	for _, r := range rows {
		row, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if jid := odooFieldID(row["journal_id"]); jid > 0 {
			QueueOdooJournalRefetch(jid)
		}
	}
}

// FlushOdooCacheRefetches refetches every queued journal's line cache
// from Odoo and clears the queue. Called by higher-level operations
// (post-reconcile, post-batch-push) once the writes are done so the
// next read sees a fresh server state.
func FlushOdooCacheRefetches(creds *OdooCredentials, uid int) {
	odooRefetchMu.Lock()
	pending := odooRefetchJournals
	odooRefetchJournals = map[int]bool{}
	odooRefetchMu.Unlock()
	if creds == nil || len(pending) == 0 {
		return
	}
	for jid := range pending {
		// Best-effort — log but don't fail the operation if a refresh
		// stumbles. The next `chb pull` will recover the cache anyway.
		if _, err := writeOdooJournalLinesCache(creds, uid, jid); err != nil {
			LogWarningf("post-write cache refresh for journal #%d: %v", jid, err)
		}
	}
}

