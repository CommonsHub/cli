package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	odoosource "github.com/CommonsHub/chb/providers/odoo"
)

// suggestTestSetup wires up an isolated APP_DATA_DIR + DATA_DIR pair,
// stages an accounts.json with a single Odoo-linked account so
// allLinkedOdooJournalIDs sees the test journal, and returns the data
// dir for the caller to drop fixture files into.
func suggestTestSetup(t *testing.T, journalID int) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("APP_DATA_DIR", filepath.Join(tmp, "app"))
	t.Setenv("DATA_DIR", filepath.Join(tmp, "data"))
	t.Setenv("NO_COLOR", "1")
	if err := SaveAccountConfigs([]AccountConfig{
		{
			Name:          "Test",
			Slug:          "test",
			Provider:      "kbcbrussels",
			OdooJournalID: journalID,
			IBAN:          "BE00000000000000",
			Currency:      "EUR",
		},
	}); err != nil {
		t.Fatalf("SaveAccountConfigs: %v", err)
	}
	return DataDir()
}

// writeJournalLinesFixture drops a synthetic OdooJournalLinesFile into
// the latest/ archive so loadLatestOdooJournalLinesCache can read it
// back. Mirrors writeOdooJournalLinesCacheFile's path layout but skips
// the dated archive write — the suggester only reads from latest/.
func writeJournalLinesFixture(t *testing.T, dataDir string, journalID int, lines []OdooCacheLine) {
	t.Helper()
	file := OdooJournalLinesFile{
		SchemaVersion: odooJournalLinesSchemaVersion,
		Provider:      odoosource.Source,
		JournalID:     journalID,
		Count:         len(lines),
		Lines:         lines,
	}
	if err := odoosource.WriteJSON(dataDir, "latest", "", file,
		"journals", journalLinesCacheName(journalID)); err != nil {
		t.Fatalf("write journal lines fixture: %v", err)
	}
}

// writePrivateInvoicesFixture drops a monthly invoices.json under the
// private projection so loadLocalCandidates / loadLocalCandidatePartitions
// can read it back.
func writePrivateInvoicesFixture(t *testing.T, dataDir, year, month string, invoices []OdooOutgoingInvoicePrivate) {
	t.Helper()
	path := filepath.Join(dataDir, year, month, odoosource.PrivateRelPath(odoosource.InvoicesFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := OdooOutgoingInvoicesPrivateFile{
		SchemaVersion: 1,
		Year:          year,
		Month:         month,
		Source:        odoosource.Source,
		Count:         len(invoices),
		Invoices:      invoices,
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func writePrivateBillsFixture(t *testing.T, dataDir, year, month string, bills []OdooOutgoingInvoicePrivate) {
	t.Helper()
	path := filepath.Join(dataDir, year, month, odoosource.PrivateRelPath(odoosource.BillsFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := OdooVendorBillsPrivateFile{
		SchemaVersion: 1,
		Year:          year,
		Month:         month,
		Source:        odoosource.Source,
		Count:         len(bills),
		Bills:         bills,
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

// invoiceMoveRowFixture builds a moveRow shaped like the loader would
// produce, ready to be fed straight into SuggestForMove.
func invoiceMoveRowFixture(id int, partner, date string, total float64) moveRow {
	return moveRow{
		Year:    date[:4],
		Month:   date[5:7],
		Partner: partner,
		Move: OdooOutgoingInvoicePublic{
			ID:          id,
			Date:        date,
			TotalAmount: total,
			Currency:    "EUR",
			State:       "posted",
		},
	}
}

func TestSuggestForMoveReturnsUnreconciledOnly_WhenAnyExist(t *testing.T) {
	dataDir := suggestTestSetup(t, 42)
	writeJournalLinesFixture(t, dataDir, 42, []OdooCacheLine{
		{ID: 1, Date: "2026-04-02", Amount: 100.0, PaymentRef: "open match", UniqueImportID: "imp-1"},
		{ID: 2, Date: "2026-04-05", Amount: 100.0, PaymentRef: "paid earlier", UniqueImportID: "imp-2", IsReconciled: true},
	})

	row := invoiceMoveRowFixture(1001, "Acme NV", "2026-04-01", 100.0)
	got := SuggestForMove(row, moveKindInvoice)
	if len(got) != 1 {
		t.Fatalf("expected 1 suggestion, got %d: %+v", len(got), got)
	}
	if got[0].Line.ID != 1 {
		t.Errorf("expected line ID 1 (unreconciled), got %d", got[0].Line.ID)
	}
	if got[0].AlreadyAttached {
		t.Errorf("expected AlreadyAttached=false, got true")
	}
}

func TestSuggestForMoveFallsBackToReconciled_WhenAllExhausted(t *testing.T) {
	dataDir := suggestTestSetup(t, 42)
	writeJournalLinesFixture(t, dataDir, 42, []OdooCacheLine{
		{ID: 7, Date: "2026-04-02", Amount: 100.0, PaymentRef: "paid wrong", UniqueImportID: "imp-7", IsReconciled: true},
		{ID: 8, Date: "2026-04-09", Amount: 100.0, PaymentRef: "paid other", UniqueImportID: "imp-8", IsReconciled: true},
		// Decoy: amount mismatch — must not surface.
		{ID: 9, Date: "2026-04-03", Amount: 50.0, PaymentRef: "wrong amount", UniqueImportID: "imp-9"},
	})

	row := invoiceMoveRowFixture(1001, "Acme NV", "2026-04-01", 100.0)
	got := SuggestForMove(row, moveKindInvoice)
	if len(got) != 2 {
		t.Fatalf("expected 2 fallback suggestions, got %d: %+v", len(got), got)
	}
	for _, s := range got {
		if !s.AlreadyAttached {
			t.Errorf("expected every fallback suggestion AlreadyAttached, got %+v", s)
		}
	}
	// Closest-by-date wins the ordering: line 7 (delta 1d) before 8 (delta 8d).
	if got[0].Line.ID != 7 {
		t.Errorf("expected closest-by-date line 7 first, got %d", got[0].Line.ID)
	}
}

func TestSuggestForMovePartnerMatchedFirst(t *testing.T) {
	dataDir := suggestTestSetup(t, 42)
	writeJournalLinesFixture(t, dataDir, 42, []OdooCacheLine{
		// Closer date, no partner match -> should sort second.
		{ID: 1, Date: "2026-04-01", Amount: 100.0, PaymentRef: "ref XYZ", UniqueImportID: "imp-1"},
		// Further date, partner token "innerpreneurs" present -> should sort first.
		{ID: 2, Date: "2026-04-08", Amount: 100.0, PaymentRef: "innerpreneurs invoice", UniqueImportID: "imp-2"},
	})

	row := invoiceMoveRowFixture(1001, "Innerpreneurs VZW", "2026-04-02", 100.0)
	got := SuggestForMove(row, moveKindInvoice)
	if len(got) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(got))
	}
	if !got[0].PartnerMatch {
		t.Fatalf("expected partner-matched candidate first, got %+v", got[0])
	}
	if got[0].Line.ID != 2 {
		t.Errorf("expected line 2 first (partner match wins over closer date), got %d", got[0].Line.ID)
	}
}

func TestSuggestForMoveBillDirectionGate(t *testing.T) {
	dataDir := suggestTestSetup(t, 42)
	writeJournalLinesFixture(t, dataDir, 42, []OdooCacheLine{
		// Positive amount = incoming; for a bill we want outgoing (negative). Skip.
		{ID: 1, Date: "2026-04-02", Amount: 100.0, PaymentRef: "incoming", UniqueImportID: "imp-1"},
		// Negative amount = outgoing; this is the only valid candidate.
		{ID: 2, Date: "2026-04-04", Amount: -100.0, PaymentRef: "outgoing", UniqueImportID: "imp-2"},
	})

	row := invoiceMoveRowFixture(1001, "Vendor BV", "2026-04-01", 100.0)
	got := SuggestForMove(row, moveKindBill)
	if len(got) != 1 {
		t.Fatalf("expected 1 (outgoing-only) suggestion, got %d: %+v", len(got), got)
	}
	if got[0].Line.ID != 2 {
		t.Errorf("expected outgoing line 2, got %d", got[0].Line.ID)
	}
}

func TestSuggestForTxReturnsOpenOnly_WhenAnyExist(t *testing.T) {
	dataDir := suggestTestSetup(t, 42)
	writePrivateInvoicesFixture(t, dataDir, "2026", "04", []OdooOutgoingInvoicePrivate{
		{
			ID: 11, Number: "INV/2026/0011", InvoiceDate: "2026-04-01", State: "posted", PaymentState: "not_paid",
			ResidualAmount: 100.0, TotalSignedAmount: 100.0,
			Partner: OdooInvoicePartner{ID: 1, Name: "Acme NV", DisplayName: "Acme NV"},
		},
		{
			ID: 12, Number: "INV/2026/0012", InvoiceDate: "2026-04-04", State: "posted", PaymentState: "paid",
			ResidualAmount: 0, TotalSignedAmount: 100.0,
			Partner: OdooInvoicePartner{ID: 2, Name: "Other LLC", DisplayName: "Other LLC"},
		},
	})

	tx := incomeExpenseTx{
		Date: "2026-04-03", Amount: 100.0, SignedAmount: 100.0,
		Counterparty: "Acme NV", Currency: "EUR",
	}
	got := SuggestForTx(tx)
	if len(got) != 1 {
		t.Fatalf("expected 1 open suggestion, got %d: %+v", len(got), got)
	}
	if got[0].Move.ID != 11 {
		t.Errorf("expected open invoice 11, got %d", got[0].Move.ID)
	}
	if got[0].AlreadyAttached {
		t.Errorf("expected AlreadyAttached=false for the open invoice")
	}
}

func TestSuggestForTxFallsBackToReconciled_WhenAllExhausted(t *testing.T) {
	dataDir := suggestTestSetup(t, 42)
	writePrivateInvoicesFixture(t, dataDir, "2026", "04", []OdooOutgoingInvoicePrivate{
		{
			ID: 21, Number: "INV/2026/0021", InvoiceDate: "2026-04-01", State: "posted", PaymentState: "paid",
			ResidualAmount: 0, TotalSignedAmount: 100.0,
			Partner: OdooInvoicePartner{ID: 1, Name: "Acme NV", DisplayName: "Acme NV"},
		},
	})

	tx := incomeExpenseTx{
		Date: "2026-04-03", Amount: 100.0, SignedAmount: 100.0,
		Counterparty: "Acme NV", Currency: "EUR",
	}
	got := SuggestForTx(tx)
	if len(got) != 1 {
		t.Fatalf("expected 1 fallback suggestion, got %d: %+v", len(got), got)
	}
	if !got[0].AlreadyAttached {
		t.Errorf("expected AlreadyAttached=true on paid fallback, got %+v", got[0])
	}
}

func TestSuggestForTxDirectionGate(t *testing.T) {
	dataDir := suggestTestSetup(t, 42)
	writePrivateInvoicesFixture(t, dataDir, "2026", "04", []OdooOutgoingInvoicePrivate{
		{
			ID: 31, Number: "INV/2026/0031", InvoiceDate: "2026-04-01", State: "posted", PaymentState: "not_paid",
			ResidualAmount: 100.0, TotalSignedAmount: 100.0,
			Partner: OdooInvoicePartner{ID: 1, Name: "Acme NV", DisplayName: "Acme NV"},
		},
	})
	writePrivateBillsFixture(t, dataDir, "2026", "04", []OdooOutgoingInvoicePrivate{
		{
			ID: 41, Number: "BILL/2026/0041", InvoiceDate: "2026-04-02", State: "posted", PaymentState: "not_paid",
			ResidualAmount: 100.0, TotalSignedAmount: -100.0,
			Partner: OdooInvoicePartner{ID: 2, Name: "Vendor BV", DisplayName: "Vendor BV"},
		},
	})

	// Incoming tx (positive SignedAmount) -> only the invoice candidate.
	incoming := SuggestForTx(incomeExpenseTx{
		Date: "2026-04-03", Amount: 100.0, SignedAmount: 100.0, Counterparty: "Acme NV", Currency: "EUR",
	})
	if len(incoming) != 1 || incoming[0].Move.ID != 31 {
		t.Errorf("incoming tx should match only the invoice, got %+v", incoming)
	}
	// Outgoing tx (negative SignedAmount) -> only the bill candidate.
	outgoing := SuggestForTx(incomeExpenseTx{
		Date: "2026-04-03", Amount: 100.0, SignedAmount: -100.0, Counterparty: "Vendor BV", Currency: "EUR",
	})
	if len(outgoing) != 1 || outgoing[0].Move.ID != 41 {
		t.Errorf("outgoing tx should match only the bill, got %+v", outgoing)
	}
}

func TestFirstUnattachedIndex(t *testing.T) {
	cases := []struct {
		name string
		in   []Suggestion
		want int
	}{
		{"all open", []Suggestion{{}, {}}, 0},
		{"all attached", []Suggestion{{AlreadyAttached: true}, {AlreadyAttached: true}}, 0},
		{"first open", []Suggestion{{}, {AlreadyAttached: true}}, 0},
		{"open in middle", []Suggestion{{AlreadyAttached: true}, {}, {AlreadyAttached: true}}, 1},
		{"empty", nil, 0},
	}
	for _, tc := range cases {
		if got := FirstUnattachedIndex(tc.in); got != tc.want {
			t.Errorf("%s: FirstUnattachedIndex = %d, want %d", tc.name, got, tc.want)
		}
	}
}
