package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestERC20BalanceOfCalldata(t *testing.T) {
	got, err := erc20BalanceOfCalldata("0x1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("erc20BalanceOfCalldata: %v", err)
	}
	want := "0x70a08231" + strings.Repeat("0", 24) + "1111111111111111111111111111111111111111"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestRawTokenBalanceToFloatHex(t *testing.T) {
	got, err := rawTokenBalanceToFloat("0xde0b6b3a7640000", 18)
	if err != nil {
		t.Fatalf("rawTokenBalanceToFloat: %v", err)
	}
	if got != 1 {
		t.Fatalf("got %v want 1", got)
	}
}

func TestFetchTokenBalanceFromRPC(t *testing.T) {
	var request struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0xde0b6b3a7640000"}`))
	}))
	defer server.Close()

	got, err := fetchTokenBalanceFromRPC(
		server.URL,
		"0x2222222222222222222222222222222222222222",
		"0x1111111111111111111111111111111111111111",
		18,
	)
	if err != nil {
		t.Fatalf("fetchTokenBalanceFromRPC: %v", err)
	}
	if got != 1 {
		t.Fatalf("got %v want 1", got)
	}
	if request.Method != "eth_call" {
		t.Fatalf("method %q", request.Method)
	}
}

func TestDefaultRPCForChainID(t *testing.T) {
	if got := defaultRPCForChainID(100); got != defaultGnosisRPC {
		t.Fatalf("gnosis rpc %q", got)
	}
	if got := defaultRPCForChainID(42220); got != defaultCeloRPC {
		t.Fatalf("celo rpc %q", got)
	}
	if got := defaultRPCForChainID(999999); got != "" {
		t.Fatalf("unknown chain rpc %q", got)
	}
}
