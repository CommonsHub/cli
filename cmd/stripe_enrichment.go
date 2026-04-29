package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	stripesource "github.com/CommonsHub/chb/sources/stripe"
)

type StripeChargeEnrichment = stripesource.ChargeEnrichment
type StripeCharge = stripesource.Charge

var knownStripeApps = stripesource.KnownApps

func fetchStripeCharges(apiKey, accountID string, chargeIDs []string) (map[string]*StripeCharge, error) {
	return stripesource.FetchChargesWithProgress(apiKey, accountID, chargeIDs, func(current, total int) {
		fmt.Printf(" %s%d/%d%s", Fmt.Dim, current, total, Fmt.Reset)
	})
}

func LoadStripeChargeEnrichment(dataDir, year, month string) (map[string]*StripeCharge, map[string]string) {
	return stripesource.LoadChargeEnrichment(dataDir, year, month)
}

func SaveStripeChargeEnrichment(dataDir, year, month string, charges map[string]*StripeCharge, refundToCharge map[string]string) {
	enrichment := StripeChargeEnrichment{
		FetchedAt:      time.Now().UTC().Format(time.RFC3339),
		Charges:        charges,
		RefundToCharge: refundToCharge,
	}
	_ = writeProviderSourceJSON(dataDir, year, month, stripesource.Source, enrichment, stripesource.ChargesFile)
}

func loadStripeCustomerData(dataDir, year, month string) map[string]*StripeCustomerPII {
	return stripesource.LoadCustomerData(dataDir, year, month)
}

func extractSourceID(source json.RawMessage) string {
	return stripesource.ExtractSourceID(source)
}

func extractChargeID(source json.RawMessage) string {
	return stripesource.ExtractChargeID(source)
}

func fetchRefundChargeID(apiKey, accountID, refundID string) string {
	return stripesource.FetchRefundChargeID(apiKey, accountID, refundID)
}
