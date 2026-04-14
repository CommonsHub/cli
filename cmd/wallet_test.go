package cmd

import (
	"os"
	"testing"
)

func TestContributionWalletResolverScopeCacheKey(t *testing.T) {
	settingsA := &Settings{
		ContributionToken: &ContributionTokenSettings{
			Chain:   "celo",
			ChainID: 42220,
			Address: "0x65dd32834927de9e57e72a3e2130a19f81c6371d",
			Symbol:  "CHT",
		},
	}
	settingsB := &Settings{
		ContributionToken: &ContributionTokenSettings{
			Chain:   "gnosis",
			ChainID: 100,
			Address: "0x5815E61eF72c9E6107b5c5A05FD121F334f7a7f1",
			Symbol:  "EURb",
		},
	}

	keyA := contributionWalletResolverScope(settingsA).cacheKey("discord-user-1")
	keyB := contributionWalletResolverScope(settingsB).cacheKey("discord-user-1")

	if keyA == keyB {
		t.Fatalf("expected different cache keys for different tokens, got %q", keyA)
	}
}

func TestContributionTokenWalletManager(t *testing.T) {
	cw := &Settings{
		ContributionToken: &ContributionTokenSettings{
			Chain:   "celo",
			Symbol:  "CHT",
			Address: "0x65dd32834927de9e57e72a3e2130a19f81c6371d",
		},
	}
	if got := contributionTokenWalletManager(cw); got != "citizenwallet" {
		t.Fatalf("expected citizenwallet default for celo token, got %q", got)
	}

	oc := &Settings{
		ContributionToken: &ContributionTokenSettings{
			Chain:         "gnosis",
			Symbol:        "EURb",
			Address:       "0x5815E61eF72c9E6107b5c5A05FD121F334f7a7f1",
			WalletManager: "opencollective",
		},
	}
	if got := contributionTokenWalletManager(oc); got != "opencollective" {
		t.Fatalf("expected explicit opencollective manager, got %q", got)
	}
}

func TestResolveOpenCollectiveAddressDeterministic(t *testing.T) {
	originalPrimary := os.Getenv("SAFE_OWNER_ADDRESS")
	originalBackup := os.Getenv("SAFE_BACKUP_OWNER_ADDRESS")
	originalPK := os.Getenv("PRIVATE_KEY")
	originalBackupPK := os.Getenv("BACKUP_PRIVATE_KEY")
	t.Cleanup(func() {
		if originalPrimary == "" {
			os.Unsetenv("SAFE_OWNER_ADDRESS")
		} else {
			os.Setenv("SAFE_OWNER_ADDRESS", originalPrimary)
		}
		if originalBackup == "" {
			os.Unsetenv("SAFE_BACKUP_OWNER_ADDRESS")
		} else {
			os.Setenv("SAFE_BACKUP_OWNER_ADDRESS", originalBackup)
		}
		if originalPK == "" {
			os.Unsetenv("PRIVATE_KEY")
		} else {
			os.Setenv("PRIVATE_KEY", originalPK)
		}
		if originalBackupPK == "" {
			os.Unsetenv("BACKUP_PRIVATE_KEY")
		} else {
			os.Setenv("BACKUP_PRIVATE_KEY", originalBackupPK)
		}
	})

	os.Setenv("SAFE_OWNER_ADDRESS", "0x1111111111111111111111111111111111111111")
	os.Setenv("SAFE_BACKUP_OWNER_ADDRESS", "0x2222222222222222222222222222222222222222")
	os.Unsetenv("PRIVATE_KEY")
	os.Unsetenv("BACKUP_PRIVATE_KEY")

	addr1, err := resolveOpenCollectiveAddress("1234567890")
	if err != nil {
		t.Fatalf("unexpected error resolving open collective address: %v", err)
	}
	addr2, err := resolveOpenCollectiveAddress("1234567890")
	if err != nil {
		t.Fatalf("unexpected error resolving open collective address: %v", err)
	}
	addr3, err := resolveOpenCollectiveAddress("9876543210")
	if err != nil {
		t.Fatalf("unexpected error resolving open collective address: %v", err)
	}

	if addr1 != addr2 {
		t.Fatalf("expected deterministic address, got %s and %s", addr1, addr2)
	}
	if addr1 == addr3 {
		t.Fatalf("expected different discord user IDs to yield different addresses, got %s", addr1)
	}
	if !isHexAddress(addr1) || !isHexAddress(addr3) {
		t.Fatalf("expected valid hex addresses, got %s and %s", addr1, addr3)
	}
}

func TestResolveWalletAddress(t *testing.T) {
	// Xavier's Discord ID → should resolve to a known wallet address
	xavierDiscordID := "689614876515237925"

	addr, err := resolveCardManagerAddress(
		xavierDiscordID,
		defaultCardManagerAddress,
		defaultInstanceID,
		defaultCeloRPC,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr == "" {
		t.Fatal("expected non-empty address")
	}
	t.Logf("Xavier's wallet: %s", addr)

	// Verify it matches the CardManager-derived address (used in on-chain CHT transactions)
	expected := "0xa6f29e8afdd08d518df119e08c1d1afb3730871d"
	if addr != expected {
		t.Errorf("address mismatch: got %s, want %s", addr, expected)
	}
}
