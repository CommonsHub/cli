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
