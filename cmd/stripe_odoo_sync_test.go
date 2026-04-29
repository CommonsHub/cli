package cmd

import "testing"

func TestStripeStatementLineAmountUsesGrossForCustomerTransactions(t *testing.T) {
	tests := []struct {
		name string
		bt   StripeTransaction
		want float64
	}{
		{
			name: "charge",
			bt:   StripeTransaction{Type: "charge", Amount: 2500, Fee: 100, Net: 2400},
			want: 25,
		},
		{
			name: "payment",
			bt:   StripeTransaction{Type: "payment", Amount: 4250, Fee: 150, Net: 4100},
			want: 42.5,
		},
		{
			name: "refund",
			bt:   StripeTransaction{Type: "refund", Amount: -1000, Fee: -40, Net: -960},
			want: -10,
		},
		{
			name: "payment refund",
			bt:   StripeTransaction{Type: "payment_refund", Amount: -1600, Fee: -60, Net: -1540},
			want: -16,
		},
		{
			name: "payout",
			bt:   StripeTransaction{Type: "payout", Amount: -5000, Fee: 0, Net: -5000},
			want: -50,
		},
		{
			name: "stripe fee",
			bt:   StripeTransaction{Type: "stripe_fee", Amount: -300, Fee: 0, Net: -300},
			want: -3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripeStatementLineAmount(tt.bt); got != tt.want {
				t.Fatalf("stripeStatementLineAmount() = %.2f, want %.2f", got, tt.want)
			}
		})
	}
}

func TestUpdateBTStatsUsesGrossCustomerAmounts(t *testing.T) {
	stats := &syncStats{}

	updateBTStats(stats, StripeTransaction{Type: "charge", Amount: 2500, Fee: 100, Net: 2400}, 25)
	updateBTStats(stats, StripeTransaction{Type: "refund", Amount: -1000, Fee: -40, Net: -960}, -10)

	if stats.Charges != 1 {
		t.Fatalf("Charges = %d, want 1", stats.Charges)
	}
	if stats.ChargesGross != 25 {
		t.Fatalf("ChargesGross = %.2f, want 25.00", stats.ChargesGross)
	}
	if stats.ChargeFees != 0.6 {
		t.Fatalf("ChargeFees = %.2f, want 0.60", stats.ChargeFees)
	}
	if stats.Refunds != 1 {
		t.Fatalf("Refunds = %d, want 1", stats.Refunds)
	}
	if stats.RefundsTotal != -10 {
		t.Fatalf("RefundsTotal = %.2f, want -10.00", stats.RefundsTotal)
	}
}

func TestStripeFeeAdjustmentCentsTracksCustomerTransactionFees(t *testing.T) {
	tests := []struct {
		name string
		bt   StripeTransaction
		want int64
		ok   bool
	}{
		{
			name: "charge fee",
			bt:   StripeTransaction{Type: "charge", Amount: 2500, Fee: 100, Net: 2400},
			want: 100,
			ok:   true,
		},
		{
			name: "refund returned fee",
			bt:   StripeTransaction{Type: "refund", Amount: -1000, Fee: -40, Net: -960},
			want: -40,
			ok:   true,
		},
		{
			name: "payout no fee line",
			bt:   StripeTransaction{Type: "payout", Amount: -2400, Fee: 0, Net: -2400},
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := stripeFeeAdjustmentCents(tt.bt)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("fee cents = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestStripeGrossCustomerRowsPlusAggregateFeeLineEqualNet(t *testing.T) {
	var feeCents int64
	var grossTotal float64
	var netTotal float64

	for _, bt := range []StripeTransaction{
		{Type: "charge", Amount: 2500, Fee: 100, Net: 2400},
		{Type: "refund", Amount: -1000, Fee: -40, Net: -960},
	} {
		grossTotal += stripeStatementLineAmount(bt)
		netTotal += centsToEuros(bt.Net)
		if cents, ok := stripeFeeAdjustmentCents(bt); ok {
			feeCents += cents
		}
	}

	total := grossTotal + stripeAggregateFeeLineAmount(feeCents)
	if total != netTotal {
		t.Fatalf("gross+aggregate fee = %.2f, want net %.2f", total, netTotal)
	}
	if got := stripeAggregateFeeLineAmount(feeCents); got != -0.6 {
		t.Fatalf("aggregate fee line = %.2f, want -0.60", got)
	}
}
