# Sources

Sources own provider data. A source is responsible for downloading provider data,
splitting it into the monthly `DATA_DIR/YYYY/MM/sources/<source>/` layout, and
reading that archived data back without calling the provider again for past
months.

Each source should have a directory under `sources/<source>/` with:

- `source.go`: source identity, declared output files, and source-level storage
  helpers.
- One file per provider data family where practical, for example
  `transactions.go`, `charges.go`, `customers.go`, `subscriptions.go`,
  `payouts.go`, or `balance.go`.
- `types.go` only for shared provider data structures used by multiple files.

The shared source descriptor lives in `sources/source.go`:

```go
type Source interface {
	Name() string
	Files() []File
}
```

`cmd/` should orchestrate commands and wire source data into existing CLI flows.
Provider API calls, provider file paths, provider archive readers, and provider
object types should live in the source package.
