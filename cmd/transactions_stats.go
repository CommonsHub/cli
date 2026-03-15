package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type TransactionsStatsMonth struct {
	Month    string   `json:"month"`
	Count    int      `json:"count"`
	Accounts []string `json:"accounts"`
}

type TransactionsStatsResult struct {
	Total  int                      `json:"total"`
	Months []TransactionsStatsMonth `json:"months"`
}

type transactionsFile struct {
	Transactions []json.RawMessage `json:"transactions"`
}

func TransactionsStats(args []string) {
	if HasFlag(args, "--help", "-h") {
		PrintTransactionsStatsHelp()
		return
	}

	jsonOut := GetOption(args, "--format") == "json"
	dataDir := DataDir()

	type monthInfo struct {
		count    int
		accounts map[string]bool
	}
	monthData := map[string]*monthInfo{}
	total := 0

	yearDirs, _ := os.ReadDir(dataDir)
	for _, yd := range yearDirs {
		if !yd.IsDir() || len(yd.Name()) != 4 {
			continue
		}
		monthDirs, _ := os.ReadDir(filepath.Join(dataDir, yd.Name()))
		for _, md := range monthDirs {
			if !md.IsDir() || len(md.Name()) != 2 {
				continue
			}
			ym := yd.Name() + "-" + md.Name()
			financeDir := filepath.Join(dataDir, yd.Name(), md.Name(), "finance")
			if _, err := os.Stat(financeDir); os.IsNotExist(err) {
				continue
			}

			chains, _ := os.ReadDir(financeDir)
			for _, chain := range chains {
				if !chain.IsDir() {
					continue
				}
				chainName := chain.Name()
				files, _ := os.ReadDir(filepath.Join(financeDir, chainName))
				for _, f := range files {
					if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
						continue
					}
					data, err := os.ReadFile(filepath.Join(financeDir, chainName, f.Name()))
					if err != nil {
						continue
					}
					var tf transactionsFile
					if err := json.Unmarshal(data, &tf); err != nil {
						continue
					}
					n := len(tf.Transactions)
					if n == 0 {
						continue
					}
					total += n
					if monthData[ym] == nil {
						monthData[ym] = &monthInfo{accounts: map[string]bool{}}
					}
					monthData[ym].count += n
					monthData[ym].accounts[chainName] = true
				}
			}
		}
	}

	var months []TransactionsStatsMonth
	for ym, info := range monthData {
		var accts []string
		for a := range info.accounts {
			accts = append(accts, a)
		}
		sort.Strings(accts)
		months = append(months, TransactionsStatsMonth{
			Month:    ym,
			Count:    info.count,
			Accounts: accts,
		})
	}
	sort.Slice(months, func(i, j int) bool {
		return months[i].Month > months[j].Month
	})

	if months == nil {
		months = []TransactionsStatsMonth{}
	}
	result := TransactionsStatsResult{Total: total, Months: months}

	if jsonOut {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Printf("📊 Transactions: %d total\n", result.Total)
	for _, m := range result.Months {
		fmt.Printf("  %s: %d transactions [%s]\n", m.Month, m.Count, joinStrings(m.Accounts))
	}
}

func joinStrings(ss []string) string {
	r := ""
	for i, s := range ss {
		if i > 0 {
			r += ", "
		}
		r += s
	}
	return r
}
