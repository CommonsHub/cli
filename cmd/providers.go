package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Provider owns one external upstream. A provider syncs data into the monthly
// providers/<provider>/ archive, then maps that archived data into standard
// generated objects. Cross-provider enrichment belongs in DataProcessor.
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
	parts := append([]string{"providers", normalizeSourceName(source)}, elems...)
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

func processorDataRelPath(processor string, elems ...string) string {
	parts := append([]string{"processors", normalizeSourceName(processor)}, elems...)
	return filepath.Join(parts...)
}

func processorDataPath(dataDir, year, month, processor string, elems ...string) string {
	parts := []string{dataDir, year, month, processorDataRelPath(processor, elems...)}
	return filepath.Join(parts...)
}

func writeProcessorDataJSON(dataDir, year, month, processor string, v interface{}, elems ...string) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeMonthFile(dataDir, year, month, processorDataRelPath(processor, elems...), data)
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

type providerCommandSpec struct {
	Name        string
	Description string
	Commands    []string
	Sync        func([]string) error
	Generate    func([]string) error
}

func providerCommandSpecs() []providerCommandSpec {
	return []providerCommandSpec{
		{
			Name:        "ics",
			Description: "Calendar ICS feeds for room bookings and public events.",
			Commands:    []string{"sync", "generate"},
			Sync: func(args []string) error {
				_, _, err := CalendarsSync(args)
				return err
			},
			Generate: GenerateEvents,
		},
		{
			Name:        "etherscan",
			Description: "Etherscan-compatible chain transaction archives.",
			Commands:    []string{"sync", "generate"},
			Sync:        syncTransactionsProvider("etherscan"),
			Generate:    GenerateTransactions,
		},
		{
			Name:        "stripe",
			Description: "Stripe balance, charge, payout, and membership archives.",
			Commands:    []string{"sync", "generate"},
			Sync:        syncTransactionsProvider("stripe"),
			Generate:    GenerateTransactions,
		},
		{
			Name:        "monerium",
			Description: "Monerium order and EUR transaction archives.",
			Commands:    []string{"sync", "generate"},
			Sync:        syncTransactionsProvider("monerium"),
			Generate:    GenerateTransactions,
		},
		{
			Name:        "discord",
			Description: "Discord message and image attachment archives.",
			Commands:    []string{"sync", "generate"},
			Sync: func(args []string) error {
				if _, err := MessagesSync(args); err != nil {
					return err
				}
				_, err := ImagesSync(args)
				return err
			},
			Generate: GenerateMessages,
		},
		{
			Name:        "odoo",
			Description: "Odoo invoices, bills, attachments, and accounting metadata.",
			Commands:    []string{"sync", "generate"},
			Sync:        OdooProviderSync,
			Generate:    GenerateMembers,
		},
		{
			Name:        "nostr",
			Description: "Nostr annotations and metadata archives.",
			Commands:    []string{"pull", "generate"},
			// Pull-only here: chb pull (= run every provider's Sync) must
			// never trigger Nostr writes. The push side is reached via
			// `chb nostr push` / `chb push`.
			Sync:     NostrPull,
			Generate: GenerateTransactions,
		},
	}
}

func syncTransactionsProvider(source string) func([]string) error {
	return func(args []string) error {
		if GetOption(args, "--source") == "" {
			args = append([]string{"--source", source}, args...)
		}
		_, err := TransactionsSync(args)
		return err
	}
}

// ProvidersCommand routes provider-scoped commands.
//
// The shape is intentionally regular:
//
//	chb providers <provider|*> <sync|generate> [args]
//
// Top-level chb sync and chb generate are shorthands for the wildcard forms.
func ProvidersCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		PrintProvidersHelp()
		return nil
	}

	provider := normalizeSourceName(args[0])
	if provider == "all" {
		provider = "*"
	}
	if provider != "*" {
		if _, ok := providerSpec(provider); !ok {
			if actionIdx := providerActionIndex(args); actionIdx > 1 {
				return ProvidersCommand(append([]string{"*", args[actionIdx]}, args[actionIdx+1:]...))
			}
		}
	}
	if provider == "" {
		PrintProvidersHelp()
		return nil
	}

	if len(args) < 2 {
		if provider == "*" {
			PrintProvidersHelp()
			return nil
		}
		if spec, ok := providerSpec(provider); ok {
			PrintProviderHelp(spec)
			return nil
		}
		return fmt.Errorf("unknown provider %q", args[0])
	}

	action := strings.ToLower(strings.TrimSpace(args[1]))
	rest := args[2:]
	switch provider {
	case "*":
		switch action {
		case "pull", "sync":
			if action == "sync" {
				Warnf("%s'sync' is deprecated — use 'pull' instead%s", Fmt.Dim, Fmt.Reset)
			}
			if HasFlag(rest, "--help", "-h", "help") {
				PrintSyncAllHelp()
				return nil
			}
			return runAllProviderSync(rest)
		case "generate":
			if len(rest) > 0 {
				switch strings.ToLower(rest[0]) {
				case "transactions", "tx":
					return GenerateTransactions(rest[1:])
				case "events":
					return GenerateEvents(rest[1:])
				case "messages":
					return GenerateMessages(rest[1:])
				case "members":
					return GenerateMembers(rest[1:])
				}
			}
			return Generate(rest)
		default:
			return fmt.Errorf("unknown providers action %q; expected sync or generate", action)
		}
	default:
		spec, ok := providerSpec(provider)
		if !ok {
			return fmt.Errorf("unknown provider %q", args[0])
		}
		switch action {
		case "pull", "sync":
			if action == "sync" {
				Warnf("%s'sync' is deprecated — use 'pull' instead%s", Fmt.Dim, Fmt.Reset)
			}
			if spec.Sync == nil {
				return fmt.Errorf("provider %q does not support pull", provider)
			}
			return spec.Sync(rest)
		case "generate":
			if spec.Generate == nil {
				return fmt.Errorf("provider %q does not support generate", provider)
			}
			return spec.Generate(rest)
		case "help", "--help", "-h":
			PrintProviderHelp(spec)
			return nil
		default:
			return fmt.Errorf("unknown provider action %q; expected pull or generate", action)
		}
	}
}

func providerActionIndex(args []string) int {
	for i, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "pull", "sync", "generate":
			return i
		}
	}
	return -1
}

func providerSpec(name string) (providerCommandSpec, bool) {
	for _, spec := range providerCommandSpecs() {
		if spec.Name == name {
			return spec, true
		}
	}
	return providerCommandSpec{}, false
}

func runAllProviderSync(args []string) error {
	verbose := HasFlag(args, "--verbose", "-v") || HasFlag(args, "--debug")
	for _, spec := range providerCommandSpecs() {
		if spec.Sync == nil {
			continue
		}
		display := providerDisplayName(spec.Name)
		if verbose {
			fmt.Printf("\n%s━━━ %s ━━━%s\n", Fmt.Bold, display, Fmt.Reset)
			if err := spec.Sync(args); err != nil {
				return err
			}
			continue
		}
		// Compact: live status line on stderr, swallow stdout chatter.
		sl := NewStatusLine(display)
		SetActiveStatusLine(sl)
		restore := silenceStdout()
		err := spec.Sync(args)
		restore()
		SetActiveStatusLine(nil)
		mark := Fmt.Green + "✓" + Fmt.Reset
		summary := ""
		if err != nil {
			mark = Fmt.Red + "✗" + Fmt.Reset
			summary = "error: " + truncErr(err)
		}
		sl.Final(mark, summary)
		if err != nil {
			return err
		}
	}
	return nil
}

func providerDisplayName(name string) string {
	switch name {
	case "ics":
		return "ICS"
	case "odoo":
		return "Odoo"
	default:
		if name == "" {
			return ""
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

func sortedProviderCommandSpecs() []providerCommandSpec {
	specs := providerCommandSpecs()
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}
