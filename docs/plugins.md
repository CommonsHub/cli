# Data Plugins

Data plugins enrich generated records without mixing provider-specific logic into
the core transaction or event builders. A plugin can warm a cache once per month,
augment every transaction and/or event, then flush public and private cache files.

Plugin-specific code should live under `plugins/<plugin>/` with generic file
names such as `cache.go`, `types.go`, or `client.go`. The CLI adapter still
implements the `DataPlugin` interface from `cmd/data_plugins.go`; keep only the
thin command wiring in `cmd/`.

## Lifecycle

For each generated month, CHB creates a `PluginContext` with:

- `DataDir`: root data directory.
- `Year`, `Month`: the month being generated.
- `HTTPClient`: shared HTTP client with a default timeout.

Then CHB calls:

1. `WarmUp(ctx)`: load caches or fetch provider data once.
2. `AugmentTransaction(ctx, tx)`: called for each generated transaction.
3. `AugmentEvent(ctx, event)`: called for each generated event.
4. `Flush(ctx)`: write updated caches or backup files.

If a plugin fails during warm-up, that plugin is skipped for the month. If it
fails for one record, CHB logs a warning and continues with the next record.

## Interface

```go
type DataPlugin interface {
	Name() string
	EnvVars() []PluginEnvVar
	WarmUp(*PluginContext) error
	AugmentTransaction(*PluginContext, *TransactionEntry) error
	AugmentEvent(*PluginContext, *FullEvent) error
	Flush(*PluginContext) error
}
```

`EnvVars()` declares configuration the plugin may use:

```go
type PluginEnvVar struct {
	Name        string
	Description string
	Required    bool
}
```

Environment declaration is documentation for now. The plugin should still check
required variables in `WarmUp`.

## Storage

Use the context helpers or the plugin package archive helpers instead of
hand-building paths:

```go
ctx.ReadPublicJSON(pluginName, "cache.json", &cache)
ctx.WritePublicJSON(pluginName, "cache.json", cache)
ctx.ReadPrivateJSON(pluginName, "raw.json", &raw)
ctx.WritePrivateJSON(pluginName, "raw.json", raw)
```

Public files are written to:

```text
DATA_DIR/YYYY/MM/plugins/<plugin>/<file>.json
DATA_DIR/latest/plugins/<plugin>/<file>.json
```

Private files are written to:

```text
DATA_DIR/YYYY/MM/plugins/<plugin>/private/<file>.json
DATA_DIR/latest/plugins/<plugin>/private/<file>.json
```

Public files must not contain PII. Private files may contain provider raw data,
counterparty names, emails, IBANs, billing details, or other sensitive fields.

## Transaction Tags

Plugins should prefer public Nostr-style transaction tags for filterable
enrichment:

```json
["event", "calendar-event-uid"]
["eventName", "Event name from generated/events.json"]
["lumaEvent", "evt-2gc6B12TEyRNRqN"]
["eventUrl", "https://luma.com/example"]
["i", "https://luma.com/example", "https://luma.com/example"]
["k", "web"]
["source", "monerium"]
["status", "needs-review"]
```

For event-related transactions, use `event` for the canonical event id from
`generated/events.json`. Keep upstream ids in source-specific tags such as
`lumaEvent`. This keeps `chb transactions --event <event-id>` aligned with
calendar events while preserving the provider reference for debugging.
Use `eventName` and `eventUrl` for human-readable/event-page values.

For external web resources, also add NIP-73 tags: `i` is the normalized
external id, `k` is its kind (`web`). Uppercase `I`/`K` are NIP-22 root-scope
tags for comments; CHB transaction records do not generate them.

Use the tag helpers:

```go
if !transactionHasTagKey(*tx, "eventUrl") {
	addTransactionTag(&tx.Tags, "eventUrl", eventURL)
	tx.Tags = normalizeTransactionTags(tx.Tags)
}
```

Do not put PII in tags. Avoid storing names, emails, IBANs, card fragments, or
free-form bank remittance text in public tags.

## Registering A Plugin

Create a file such as `cmd/plugin_monerium.go`, implement `DataPlugin`, then add
it to `registeredDataPlugins()` in `cmd/data_plugins.go`:

```go
func registeredDataPlugins() []DataPlugin {
	return []DataPlugin{
		newLumaStripePlugin(),
		newMoneriumPlugin(),
	}
}
```

## Minimal Skeleton

```go
package cmd

import (
	"os"
	"strings"
)

type moneriumOrder struct {
	State string
}

type moneriumRawOrders struct{}

type moneriumPlugin struct {
	ordersByTxHash map[string]moneriumOrder
	rawOrders      moneriumRawOrders
	changed        bool
}

func newMoneriumPlugin() *moneriumPlugin {
	return &moneriumPlugin{}
}

func (p *moneriumPlugin) Name() string {
	return "monerium"
}

func (p *moneriumPlugin) EnvVars() []PluginEnvVar {
	return []PluginEnvVar{
		{Name: "MONERIUM_CLIENT_ID", Required: true},
		{Name: "MONERIUM_CLIENT_SECRET", Required: true},
	}
}

func (p *moneriumPlugin) WarmUp(ctx *PluginContext) error {
	if os.Getenv("MONERIUM_CLIENT_ID") == "" {
		return nil // or return an error if enrichment must be explicit
	}

	// 1. Load cached raw/private data with ctx.ReadPrivateJSON.
	// 2. Fetch fresh provider data if needed.
	// 3. Build indexes used by AugmentTransaction.
	p.ordersByTxHash = map[string]moneriumOrder{}
	return nil
}

func (p *moneriumPlugin) AugmentTransaction(ctx *PluginContext, tx *TransactionEntry) error {
	if tx.TxHash == "" {
		return nil
	}
	order, ok := p.ordersByTxHash[strings.ToLower(tx.TxHash)]
	if !ok {
		return nil
	}

	// Public, filterable enrichment.
	addTransactionTag(&tx.Tags, "source", "monerium")
	tx.Tags = normalizeTransactionTags(tx.Tags)

	// Non-PII public metadata is OK. PII belongs in private cache/output.
	if tx.Metadata == nil {
		tx.Metadata = map[string]interface{}{}
	}
	tx.Metadata["moneriumState"] = order.State
	return nil
}

func (p *moneriumPlugin) AugmentEvent(ctx *PluginContext, ev *FullEvent) error {
	return nil
}

func (p *moneriumPlugin) Flush(ctx *PluginContext) error {
	if !p.changed {
		return nil
	}
	return ctx.WritePrivateJSON(p.Name(), "orders.json", p.rawOrders)
}
```

## Example: Luma

`cmd/plugin_luma_stripe.go` is the reference implementation for the `luma-stripe`
plugin. It:

- Declares optional `LUMA_API_KEY`.
- Loads `plugins/luma-stripe/event-urls.json` and generated calendar events in
  `WarmUp`.
- For each transaction with a Luma event id (`evt-...`), preserves it as
  `["lumaEvent", "evt-..."]`.
- Resolves the public URL by calling `https://luma.com/event/:eventId` with
  redirects disabled and reading the `Location` header.
- Adds `["eventUrl", "<resolved URL>"]` plus NIP-73 `i`/`k` web tags.
- If the URL matches an event from `generated/events.json`, sets `tx.Event`
  and `["event", "<calendar-event-id>"]` to that canonical calendar event id,
  and adds `["eventName", "<calendar event name>"]`.
- Writes the updated public cache in `Flush`.

## Guidelines

- Keep provider raw data out of generated public files.
- Use `WarmUp` for provider APIs that return account-wide or monthly snapshots.
- Keep per-record hooks cheap; they should mostly do map lookups.
- Make plugins idempotent: rerunning generation should not duplicate tags or
  rewrite unchanged cache files.
- Prefer canonical tags for filterable values and `metadata` only for small,
  non-sensitive details.
- Return errors for broken configuration or failed provider calls; return `nil`
  for records the plugin does not handle.
