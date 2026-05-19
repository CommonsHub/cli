package cmd

import "testing"

func TestFindOdooReconcileMigrationTargetFallsBackToDateAmount(t *testing.T) {
	source := odooStatementLineForReconcile{
		Date:       "2026-05-13",
		Amount:     12.10,
		PaymentRef: "An Economy of Better for Europe",
	}
	target := odooStatementLineForReconcile{
		ID:         42,
		Date:       "2026-05-13",
		Amount:     12.10,
		PaymentRef: "ticket An Economy of Better for Europe",
	}

	got, reason, ok := findOdooReconcileMigrationTarget(
		source,
		nil,
		map[string][]odooStatementLineForReconcile{},
		map[string][]odooStatementLineForReconcile{odooReconcileDateAmountKey(source): {target}},
	)
	if !ok {
		t.Fatalf("expected fallback match, got reason %q", reason)
	}
	if got.ID != target.ID {
		t.Fatalf("matched target ID = %d, want %d", got.ID, target.ID)
	}
	if reason != "date+amount" {
		t.Fatalf("reason = %q, want date+amount", reason)
	}
}

func TestFindOdooReconcileMigrationTargetSkipsAmbiguousDateAmount(t *testing.T) {
	source := odooStatementLineForReconcile{Date: "2026-05-13", Amount: 12.10, PaymentRef: "old"}
	targets := []odooStatementLineForReconcile{
		{ID: 1, Date: source.Date, Amount: source.Amount, PaymentRef: "new a"},
		{ID: 2, Date: source.Date, Amount: source.Amount, PaymentRef: "new b"},
	}

	_, reason, ok := findOdooReconcileMigrationTarget(
		source,
		nil,
		map[string][]odooStatementLineForReconcile{},
		map[string][]odooStatementLineForReconcile{odooReconcileDateAmountKey(source): targets},
	)
	if ok {
		t.Fatalf("expected ambiguous date+amount match to be skipped")
	}
	if reason != "ambiguous date+amount match" {
		t.Fatalf("reason = %q, want ambiguous date+amount match", reason)
	}
}
