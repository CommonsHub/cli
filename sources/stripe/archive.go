package stripe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func RelPath(elems ...string) string {
	parts := append([]string{"sources", Source}, elems...)
	return filepath.Join(parts...)
}

func Path(dataDir, year, month string, elems ...string) string {
	parts := []string{dataDir, year, month, RelPath(elems...)}
	return filepath.Join(parts...)
}

func TransactionCachePath(dataDir, year, month string) string {
	return Path(dataDir, year, month, BalanceTransactionsFile)
}

func TransactionCachePaths(dataDir, year, month string) []string {
	path := TransactionCachePath(dataDir, year, month)
	if fileExists(path) {
		return []string{path}
	}
	return nil
}

func LoadTransactionsSince(dataDir, accountID string, sinceUnix int64) ([]Transaction, error) {
	all, err := LoadTransactions(dataDir, accountID)
	if err != nil {
		return nil, err
	}
	var out []Transaction
	for _, tx := range all {
		if tx.Created > sinceUnix {
			out = append(out, tx)
		}
	}
	return out, nil
}

func LoadTransactions(dataDir, accountID string) ([]Transaction, error) {
	years, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	foundCache := false
	var out []Transaction
	for _, y := range years {
		if !y.IsDir() || len(y.Name()) != 4 {
			continue
		}
		months, err := os.ReadDir(filepath.Join(dataDir, y.Name()))
		if err != nil {
			continue
		}
		for _, m := range months {
			if !m.IsDir() || len(m.Name()) != 2 {
				continue
			}
			cache, ok := LoadCache(TransactionCachePath(dataDir, y.Name(), m.Name()))
			if !ok {
				continue
			}
			if accountID != "" && cache.AccountID != "" && !strings.EqualFold(accountID, cache.AccountID) {
				continue
			}
			foundCache = true
			for _, tx := range cache.Transactions {
				if tx.ID == "" || seen[tx.ID] {
					continue
				}
				seen[tx.ID] = true
				out = append(out, tx)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created == out[j].Created {
			return out[i].ID < out[j].ID
		}
		return out[i].Created < out[j].Created
	})
	if !foundCache {
		return nil, os.ErrNotExist
	}
	return out, nil
}

func LoadCache(path string) (CacheFile, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CacheFile{}, false
	}
	var cache CacheFile
	if json.Unmarshal(data, &cache) != nil {
		return CacheFile{}, false
	}
	return cache, len(cache.Transactions) > 0
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
