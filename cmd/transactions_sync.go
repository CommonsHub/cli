package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// EtherscanResponse represents the Etherscan V2 API response
type EtherscanResponse struct {
	Status  string            `json:"status"`
	Message string            `json:"message"`
	Result  []json.RawMessage `json:"result"`
}

// TokenTransfer represents a single ERC20 token transfer
type TokenTransfer struct {
	BlockNumber  string `json:"blockNumber"`
	TimeStamp    string `json:"timeStamp"`
	Hash         string `json:"hash"`
	From         string `json:"from"`
	To           string `json:"to"`
	Value        string `json:"value"`
	TokenName    string `json:"tokenName"`
	TokenSymbol  string `json:"tokenSymbol"`
	TokenDecimal string `json:"tokenDecimal"`
}

// TransactionsCacheFile is the structure saved to disk
type TransactionsCacheFile struct {
	Transactions []TokenTransfer `json:"transactions"`
	CachedAt     string          `json:"cachedAt"`
	Account      string          `json:"account"`
	Chain        string          `json:"chain"`
	Token        string          `json:"token"`
}

func TransactionsSync(args []string) error {
	if HasFlag(args, "--help", "-h") {
		printTransactionsSyncHelp()
		return nil
	}

	settings, err := LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	force := HasFlag(args, "--force")
	monthFilter := GetOption(args, "--month")

	// Determine which months to process
	now := time.Now().In(BrusselsTZ())
	var startMonth, endMonth string

	if monthFilter != "" {
		startMonth = monthFilter
		endMonth = monthFilter
	} else {
		// Default: current month + previous month
		prev := now.AddDate(0, -1, 0)
		startMonth = fmt.Sprintf("%d-%02d", prev.Year(), prev.Month())
		endMonth = fmt.Sprintf("%d-%02d", now.Year(), now.Month())
	}

	fmt.Printf("\n%s⛓️  Syncing blockchain transactions%s\n", Fmt.Bold, Fmt.Reset)
	fmt.Printf("%sDATA_DIR: %s%s\n", Fmt.Dim, DataDir(), Fmt.Reset)
	fmt.Printf("%sMonth range: %s → %s%s\n\n", Fmt.Dim, startMonth, endMonth, Fmt.Reset)

	// Process each etherscan account
	etherscanAccounts := make([]FinanceAccount, 0)
	for _, acc := range settings.Finance.Accounts {
		if acc.Provider == "etherscan" && acc.Token != nil {
			etherscanAccounts = append(etherscanAccounts, acc)
		}
	}

	if len(etherscanAccounts) == 0 {
		fmt.Println("No etherscan accounts configured in settings.json")
		return nil
	}

	// Get API key
	apiKey := os.Getenv("ETHERSCAN_API_KEY")
	if apiKey == "" {
		// Try chain-specific keys
		apiKey = os.Getenv("GNOSISSCAN_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("ETHERSCAN_API_KEY or GNOSISSCAN_API_KEY environment variable required")
	}

	totalProcessed := 0
	for _, acc := range etherscanAccounts {
		fmt.Printf("  %s%s%s (%s/%s)\n", Fmt.Bold, acc.Name, Fmt.Reset, acc.Chain, acc.Token.Symbol)

		transfers, err := fetchTokenTransfers(acc, apiKey)
		if err != nil {
			fmt.Printf("    %s✗ Error: %v%s\n", Fmt.Red, err, Fmt.Reset)
			continue
		}

		fmt.Printf("    %sFetched %d total transfers%s\n", Fmt.Dim, len(transfers), Fmt.Reset)

		// Group by month
		byMonth := groupTransfersByMonth(transfers)

		saved := 0
		for ym, monthTxs := range byMonth {
			if ym < startMonth || ym > endMonth {
				continue
			}

			parts := strings.Split(ym, "-")
			if len(parts) != 2 {
				continue
			}
			year, month := parts[0], parts[1]

			// Save to data/YYYY/MM/finance/{chain}/{slug}.{token}.json
			dir := filepath.Join(DataDir(), year, month, "finance", acc.Chain)
			filename := fmt.Sprintf("%s.%s.json", acc.Slug, acc.Token.Symbol)
			filePath := filepath.Join(dir, filename)

			// Skip if exists and not force
			if !force && fileExists(filePath) {
				// But always update current month
				if ym != fmt.Sprintf("%d-%02d", now.Year(), now.Month()) {
					continue
				}
			}

			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Printf("    %s✗ Failed to create dir: %v%s\n", Fmt.Red, err, Fmt.Reset)
				continue
			}

			cache := TransactionsCacheFile{
				Transactions: monthTxs,
				CachedAt:     time.Now().UTC().Format(time.RFC3339),
				Account:      acc.Address,
				Chain:        acc.Chain,
				Token:        acc.Token.Symbol,
			}

			data, _ := json.MarshalIndent(cache, "", "  ")
			if err := os.WriteFile(filePath, data, 0644); err != nil {
				fmt.Printf("    %s✗ Failed to write: %v%s\n", Fmt.Red, err, Fmt.Reset)
				continue
			}

			saved++
			totalProcessed += len(monthTxs)
		}

		if saved > 0 {
			fmt.Printf("    %s✓ Saved %d months%s\n", Fmt.Green, saved, Fmt.Reset)
		}

		// Rate limit between accounts
		time.Sleep(400 * time.Millisecond)
	}

	fmt.Printf("\n%s✓ Done!%s %d transactions processed\n\n", Fmt.Green, Fmt.Reset, totalProcessed)
	return nil
}

func fetchTokenTransfers(acc FinanceAccount, apiKey string) ([]TokenTransfer, error) {
	baseURL := fmt.Sprintf("https://api.etherscan.io/v2/api?chainid=%d", acc.ChainID)
	url := fmt.Sprintf("%s&module=account&action=tokentx&contractaddress=%s&address=%s&startblock=0&endblock=99999999&sort=desc&apikey=%s",
		baseURL, acc.Token.Address, acc.Address, apiKey)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		resp, err := http.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		var result struct {
			Status  string          `json:"status"`
			Message string          `json:"message"`
			Result  json.RawMessage `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			lastErr = err
			continue
		}

		if result.Status == "0" && result.Message != "No transactions found" {
			if strings.Contains(strings.ToLower(result.Message), "rate limit") {
				lastErr = fmt.Errorf("rate limited: %s", result.Message)
				time.Sleep(2 * time.Second)
				continue
			}
			return nil, fmt.Errorf("API error: %s", result.Message)
		}

		var transfers []TokenTransfer
		if err := json.Unmarshal(result.Result, &transfers); err != nil {
			// Could be "No transactions found" which returns a string
			return []TokenTransfer{}, nil
		}

		return transfers, nil
	}

	return nil, fmt.Errorf("failed after 3 attempts: %v", lastErr)
}

func groupTransfersByMonth(transfers []TokenTransfer) map[string][]TokenTransfer {
	byMonth := make(map[string][]TokenTransfer)
	tz := BrusselsTZ()

	for _, tx := range transfers {
		ts, err := strconv.ParseInt(tx.TimeStamp, 10, 64)
		if err != nil {
			continue
		}
		t := time.Unix(ts, 0).In(tz)
		ym := fmt.Sprintf("%d-%02d", t.Year(), t.Month())
		byMonth[ym] = append(byMonth[ym], tx)
	}

	return byMonth
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// parseTokenValue converts raw token value string to float using decimals
func parseTokenValue(rawValue string, decimals int) float64 {
	val := new(big.Float)
	val.SetString(rawValue)
	divisor := new(big.Float).SetFloat64(math.Pow10(decimals))
	result := new(big.Float).Quo(val, divisor)
	f, _ := result.Float64()
	return f
}

func printTransactionsSyncHelp() {
	f := Fmt
	fmt.Printf(`
%schb transactions sync%s — Fetch blockchain transactions

%sUSAGE%s
  %schb transactions sync%s [options]

%sOPTIONS%s
  %s--month%s <YYYY-MM>    Fetch specific month only
  %s--force%s              Re-fetch even if cached data exists
  %s--help, -h%s           Show this help

%sENVIRONMENT%s
  %sETHERSCAN_API_KEY%s    Etherscan/Gnosisscan API key
  %sGNOSISSCAN_API_KEY%s   Fallback API key for Gnosis chain
`,
		f.Bold, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
	)
}
