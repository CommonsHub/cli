package cmd

import "testing"

func TestRuleMatchesAmountRoundedToCents(t *testing.T) {
	amount := 10.0
	rule := Rule{
		Match: RuleMatch{
			Provider:  "stripe",
			Currency:  "EUR",
			Amount:    &amount,
			Direction: "in",
		},
	}
	tx := TransactionEntry{
		Provider:         "stripe",
		Currency:         "EUR",
		Type:             "CREDIT",
		NormalizedAmount: 10.004,
	}
	if !rule.MatchesTransaction(tx) {
		t.Fatalf("rule should match amount rounded to cents")
	}

	tx.NormalizedAmount = 9.99
	if rule.MatchesTransaction(tx) {
		t.Fatalf("rule should not match a different amount")
	}
}

func TestRuleMatchesPaymentLink(t *testing.T) {
	rule := Rule{
		Match: RuleMatch{
			Provider:    "stripe",
			PaymentLink: "plink_openletter",
		},
	}
	tx := TransactionEntry{
		Provider: "stripe",
		Metadata: map[string]interface{}{
			"paymentLink": "plink_openletter",
		},
	}

	if !rule.MatchesTransaction(tx) {
		t.Fatalf("rule should match paymentLink metadata")
	}

	tx.Metadata["paymentLink"] = "plink_other"
	if rule.MatchesTransaction(tx) {
		t.Fatalf("rule should not match a different paymentLink")
	}
}

// TestRuleDescriptionDoesNotFallBackToCounterparty pins the new behavior:
// before this change, a `description` rule would also match against
// tx.Counterparty when metadata.description and metadata.memo were empty.
// Stripe-fee txs (where counterparty used to be the descriptive text) made
// the conflation obvious. After the fix, description matches against
// metadata.description / metadata.memo only; use the explicit
// `counterparty` field to match against the counterparty.
func TestRuleDescriptionDoesNotFallBackToCounterparty(t *testing.T) {
	descRule := Rule{
		Match: RuleMatch{Description: "*partena*"},
	}
	cpRule := Rule{
		Match: RuleMatch{Counterparty: "*partena*"},
	}
	tx := TransactionEntry{
		Counterparty: "PARTENA PROFESSIONAL",
		Type:         "DEBIT",
		Metadata:     map[string]interface{}{},
	}
	if descRule.MatchesTransaction(tx) {
		t.Fatalf("description rule should NOT match a tx where the keyword only lives in counterparty")
	}
	if !cpRule.MatchesTransaction(tx) {
		t.Fatalf("counterparty rule should match a tx with the keyword in counterparty")
	}

	// And the inverse: a description-only keyword shouldn't match a
	// counterparty rule.
	tx2 := TransactionEntry{
		Counterparty: "Stripe",
		Type:         "DEBIT",
		Metadata: map[string]interface{}{
			"description": "Automatic Taxes (2026-05-17): Automatic tax",
		},
	}
	descTaxRule := Rule{Match: RuleMatch{Description: "*Automatic Tax*"}}
	cpTaxRule := Rule{Match: RuleMatch{Counterparty: "*Automatic Tax*"}}
	if !descTaxRule.MatchesTransaction(tx2) {
		t.Fatalf("description rule should match metadata.description")
	}
	if cpTaxRule.MatchesTransaction(tx2) {
		t.Fatalf("counterparty rule should NOT match a tx where the keyword is only in metadata.description")
	}
}
