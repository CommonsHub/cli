package stripe

import (
	"encoding/json"
	"testing"
)

func TestMergeTransactionsDedupesIncomingAndSortsNewestFirst(t *testing.T) {
	existing := []Transaction{
		{ID: "txn_old", Created: 100, Net: 100},
		{ID: "txn_dup", Created: 200, Net: 200},
	}
	incoming := []Transaction{
		{ID: "txn_new", Created: 300, Net: 300},
		{ID: "txn_dup", Created: 250, Net: 250},
	}

	got := MergeTransactions(existing, incoming)
	if len(got) != 3 {
		t.Fatalf("len(MergeTransactions()) = %d, want 3", len(got))
	}
	wantIDs := []string{"txn_new", "txn_dup", "txn_old"}
	wantNets := []int64{300, 250, 100}
	for i := range wantIDs {
		if got[i].ID != wantIDs[i] || got[i].Net != wantNets[i] {
			t.Fatalf("merged[%d] = (%s, %d), want (%s, %d)", i, got[i].ID, got[i].Net, wantIDs[i], wantNets[i])
		}
	}
}

func TestEnrichTransactionFromExpandedPaymentSource(t *testing.T) {
	tx := Transaction{
		ID:     "txn_123",
		Source: json.RawMessage(`{"object":"payment","id":"py_123","description":"Luma ticket","application":"ca_HB0JKrk4R6zGWt4fAD9M6iutRhuBdFqd","customer":{"id":"cus_123","name":"Jane Donor","email":"jane@example.com"},"billing_details":{"name":"Jane Billing","email":"billing@example.com"},"metadata":{"event_api_id":"evt_123","payment_type":"registration"}}`),
	}

	EnrichTransaction(&tx)

	if tx.ChargeID != "" {
		t.Fatalf("ChargeID = %q, want empty for py_ payment source", tx.ChargeID)
	}
	if tx.CustomerName != "Jane Donor" || tx.CustomerEmail != "jane@example.com" {
		t.Fatalf("customer = %q <%s>", tx.CustomerName, tx.CustomerEmail)
	}
	if got := tx.Metadata["application"]; got != "luma" {
		t.Fatalf("metadata.application = %#v, want luma", got)
	}
	if got := tx.Metadata["event_api_id"]; got != "evt_123" {
		t.Fatalf("metadata.event_api_id = %#v, want evt_123", got)
	}
}

func TestEnrichTransactionDoesNotUsePaymentIDAsChargeID(t *testing.T) {
	tx := Transaction{
		ID:     "txn_123",
		Source: json.RawMessage(`{"object":"charge","id":"py_123","customer":{"name":"Jane Donor","email":"jane@example.com"}}`),
	}

	EnrichTransaction(&tx)

	if tx.ChargeID != "" {
		t.Fatalf("ChargeID = %q, want empty for non-ch_ source ID", tx.ChargeID)
	}
}
