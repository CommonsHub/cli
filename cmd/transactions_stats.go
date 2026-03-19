package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func TransactionsStats(args []string) {
	if HasFlag(args, "--help", "-h", "help") {
		PrintTransactionsStatsHelp()
		return
	}

	jsonOut := GetOption(args, "--format") == "json"
	dataDir := DataDir()

	// Parse optional year/month filter
	posYear, posMonth, posFound := ParseYearMonthArg(args)

	type sourceStats struct {
		Count   int     `json:"count"`
		In      float64 `json:"in"`
		Out     float64 `json:"out"`
		Net     float64 `json:"net"`
	}

	type monthStats struct {
		Month   string                 `json:"month"`
		Count   int                    `json:"count"`
		In      float64                `json:"in"`
		Out     float64                `json:"out"`
		Net     float64                `json:"net"`
		Sources map[string]*sourceStats `json:"sources"`
	}

	monthData := map[string]*monthStats{}
	totalCount := 0
	totalIn := 0.0
	totalOut := 0.0

	// Scan year/month directories for generated transactions.json
	yearDirs, _ := os.ReadDir(dataDir)
	for _, yd := range yearDirs {
		if !yd.IsDir() || len(yd.Name()) != 4 {
			continue
		}
		year := yd.Name()

		// Year filter
		if posFound && posMonth == "" && year != posYear {
			continue
		}

		monthDirs, _ := os.ReadDir(filepath.Join(dataDir, year))
		for _, md := range monthDirs {
			if !md.IsDir() || len(md.Name()) != 2 {
				continue
			}
			month := md.Name()
			ym := year + "-" + month

			// Month filter
			if posFound && posMonth != "" && (year != posYear || month != posMonth) {
				continue
			}

			txPath := filepath.Join(dataDir, year, month, "transactions.json")
			data, err := os.ReadFile(txPath)
			if err != nil {
				continue
			}

			var txFile TransactionsFile
			if json.Unmarshal(data, &txFile) != nil {
				continue
			}

			ms := &monthStats{
				Month:   ym,
				Sources: map[string]*sourceStats{},
			}

			for _, tx := range txFile.Transactions {
				amount := tx.NormalizedAmount
				if amount == 0 {
					amount = tx.Amount
				}

				// Determine source label
				source := tx.AccountSlug
				if source == "" {
					source = tx.AccountName
				}
				if source == "" && tx.Chain != nil {
					source = *tx.Chain
				}
				if source == "" {
					source = "unknown"
				}

				ss, ok := ms.Sources[source]
				if !ok {
					ss = &sourceStats{}
					ms.Sources[source] = ss
				}

				ss.Count++
				ms.Count++
				totalCount++

				absAmount := math.Abs(amount)
				if tx.Type == "CREDIT" || amount > 0 {
					ss.In += absAmount
					ms.In += absAmount
					totalIn += absAmount
				} else {
					ss.Out += absAmount
					ms.Out += absAmount
					totalOut += absAmount
				}
			}

			ss := ms.Sources
			for k := range ss {
				ss[k].Net = ss[k].In - ss[k].Out
			}
			ms.Net = ms.In - ms.Out

			monthData[ym] = ms
		}
	}

	// Sort months descending
	var months []*monthStats
	for _, ms := range monthData {
		months = append(months, ms)
	}
	sort.Slice(months, func(i, j int) bool {
		return months[i].Month > months[j].Month
	})

	if jsonOut {
		out := struct {
			Total  int            `json:"total"`
			In     float64        `json:"in"`
			Out    float64        `json:"out"`
			Net    float64        `json:"net"`
			Months []*monthStats  `json:"months"`
		}{
			Total:  totalCount,
			In:     totalIn,
			Out:    totalOut,
			Net:    totalIn - totalOut,
			Months: months,
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return
	}

	// Pretty print
	f := Fmt
	net := totalIn - totalOut
	sign := "+"
	if net < 0 {
		sign = ""
	}

	fmt.Printf("\n%s💰 Transactions: %d total%s\n", f.Bold, totalCount, f.Reset)
	fmt.Printf("   %s↑ In:%s  €%.2f\n", f.Green, f.Reset, totalIn)
	fmt.Printf("   %s↓ Out:%s €%.2f\n", f.Red, f.Reset, totalOut)
	fmt.Printf("   %sNet:%s  %s€%.2f\n\n", f.Bold, f.Reset, sign, net)

	for _, ms := range months {
		mNet := ms.In - ms.Out
		mSign := "+"
		if mNet < 0 {
			mSign = ""
		}
		fmt.Printf("  %s%s%s  %d tx  %s↑%s€%.0f  %s↓%s€%.0f  %snet %s€%.0f%s\n",
			f.Bold, ms.Month, f.Reset,
			ms.Count,
			f.Green, f.Reset, ms.In,
			f.Red, f.Reset, ms.Out,
			f.Dim, mSign, mNet, f.Reset,
		)

		// Sources sorted by volume
		var sources []struct {
			name string
			ss   *sourceStats
		}
		for name, ss := range ms.Sources {
			sources = append(sources, struct {
				name string
				ss   *sourceStats
			}{name, ss})
		}
		sort.Slice(sources, func(i, j int) bool {
			return (sources[i].ss.In + sources[i].ss.Out) > (sources[j].ss.In + sources[j].ss.Out)
		})

		for _, s := range sources {
			parts := []string{}
			if s.ss.In > 0 {
				parts = append(parts, fmt.Sprintf("%s↑%s€%.0f", f.Green, f.Reset, s.ss.In))
			}
			if s.ss.Out > 0 {
				parts = append(parts, fmt.Sprintf("%s↓%s€%.0f", f.Red, f.Reset, s.ss.Out))
			}
			fmt.Printf("    %s%-14s%s %d tx  %s\n",
				f.Dim, s.name, f.Reset,
				s.ss.Count,
				strings.Join(parts, "  "),
			)
		}
	}
	fmt.Println()
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

func PrintTransactionsStatsHelp() {
	f := Fmt
	fmt.Printf(`
%sUSAGE%s
  %schb transactions%s [year[/month]] [options]

%sOPTIONS%s
  %s<year>%s              Show stats for a specific year (e.g. 2025)
  %s<year/month>%s        Show stats for a specific month (e.g. 2025/03)
  %s--format json%s       Output as JSON
  %s--help, -h%s          Show this help

%sEXAMPLES%s
  %schb transactions%s              All-time breakdown
  %schb transactions 2025%s         2025 only
  %schb transactions 2025/03%s      March 2025 only
  %schb transactions --format json%s  JSON output

%sNOTE%s
  Reads from generated transactions.json files. Run %schb sync%s first.
`,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
	)
}
