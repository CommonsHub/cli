package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Provider owns one external source. A provider syncs source data into the
// monthly sources/<source>/ archive, then maps that archived data into standard
// generated objects. Cross-source enrichment belongs in DataPlugin.
type Provider interface {
	Source() string
	EnvVars() []ProviderEnvVar
	SyncSourceData(*ProviderSyncContext, ProviderSyncScope) error
	GenerateObjects(*ProviderGenerateContext, ProviderGenerateScope) (*ProviderGeneratedObjects, error)
}

type ProviderEnvVar struct {
	Name        string
	Description string
	Required    bool
}

type ProviderSyncContext struct {
	DataDir  string
	Settings *Settings
}

type ProviderGenerateContext struct {
	DataDir  string
	Settings *Settings
}

type ProviderSyncScope struct {
	Source     string
	Account    string
	StartMonth string
	EndMonth   string
	Force      bool
}

type ProviderGenerateScope struct {
	Year  string
	Month string
}

type ProviderGeneratedObjects struct {
	Transactions []TransactionEntry
	Events       []FullEvent
	Messages     []json.RawMessage
	Images       []ImageEntry
}

func providerSourceRelPath(source string, elems ...string) string {
	parts := append([]string{"sources", normalizeSourceName(source)}, elems...)
	return filepath.Join(parts...)
}

func providerSourcePath(dataDir, year, month, source string, elems ...string) string {
	parts := []string{dataDir, year, month, providerSourceRelPath(source, elems...)}
	return filepath.Join(parts...)
}

func writeProviderSourceJSON(dataDir, year, month, source string, v interface{}, elems ...string) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeMonthFile(dataDir, year, month, providerSourceRelPath(source, elems...), data)
}

func pluginDataRelPath(plugin string, elems ...string) string {
	parts := append([]string{"plugins", normalizeSourceName(plugin)}, elems...)
	return filepath.Join(parts...)
}

func pluginDataPath(dataDir, year, month, plugin string, elems ...string) string {
	parts := []string{dataDir, year, month, pluginDataRelPath(plugin, elems...)}
	return filepath.Join(parts...)
}

func writePluginDataJSON(dataDir, year, month, plugin string, v interface{}, elems ...string) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeMonthFile(dataDir, year, month, pluginDataRelPath(plugin, elems...), data)
}

func normalizeSourceName(source string) string {
	source = strings.TrimSpace(strings.ToLower(source))
	source = strings.ReplaceAll(source, "_", "-")
	source = strings.Join(strings.Fields(source), "-")
	return source
}

func registeredProviders() []Provider {
	return nil
}
