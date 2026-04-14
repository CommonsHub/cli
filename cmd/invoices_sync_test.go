package cmd

import "testing"

func TestExtractTxHash(t *testing.T) {
	hash := "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	cases := []string{
		hash,
		"gnosis:wallet:" + hash + ":0",
		"payment ref " + hash,
	}
	for _, input := range cases {
		if got := extractTxHash(input); got != hash {
			t.Fatalf("extractTxHash(%q) = %q, want %q", input, got, hash)
		}
	}
}

func TestInferTxProvider(t *testing.T) {
	if got := inferTxProvider("ch_123"); got != "stripe" {
		t.Fatalf("inferTxProvider(stripe ref) = %q", got)
	}
	if got := inferTxProvider("gnosis:wallet:0xabc:0"); got != "gnosis" {
		t.Fatalf("inferTxProvider(gnosis ref) = %q", got)
	}
}
