# Testing

## Running the suite

```bash
go test ./... -count 1 -short
```

`-count 1` defeats Go's test cache (useful when iterating). `-short` skips integration tests that need live external systems.

## Test layout

The tests under `cmd/` divide roughly into three groups:

1. **Pure unit tests** — exercise schema, parsing, rule matching, URI canonicalisation, formatters. Fast, no I/O. Examples: `rules_test.go`, `import_id_canonical_test.go`, `import_id_roundtrip_test.go`, `categories_test.go`.

2. **Filesystem integration tests** — set up a temp `APP_DATA_DIR` + `DATA_DIR`, write fixtures, exercise generate / settings load. Examples: `events_generate_test.go`, `events_sync_test.go`, `settings_test.go`, `generate_sources_test.go`.

3. **External-service tests** — talk to a real Odoo / Stripe test environment. Skipped without credentials. Examples: `accounts_balance_test.go`, `accounts_fetch_test.go`, `accounts_odoo_test.go`, `odoo_create_failures_test.go`, `stripe_odoo_sync_test.go` (the Odoo-touching subset).

The third group fails loudly when the test instance is unreachable — that's expected; it tells you the test was attempted. Don't suppress them by deleting the test; gate via env vars.

## Targeted runs

```bash
go test ./cmd/ -count 1 -short -run TestImportID         # one suite
go test ./cmd/ -count 1 -short -run "TestStripe|TestKBC" # multiple
go test ./cmd/ -count 1 -short -v -run TestSpecificCase  # verbose, single case
```

## Smoke testing the CLI

After a non-trivial change, run the visible commands and watch the output:

```bash
./chb --help                                # top-level help, instant
./chb pull --help                           # short-circuits before any I/O
./chb pull                                  # live progress per provider
./chb generate                              # deterministic, no network
./chb accounts                              # account list + balances
./chb events                                # upcoming events
./chb report 2026/05                        # monthly report
./chb odoo journals 28 push --dry-run       # preview, don't apply
./chb nostr pending                         # outbox listing
```

`--dry-run` is your friend. Every push path should accept it and produce a preview without writing remotely.

## CI / pre-commit hooks

`gofmt`, `go vet`, and the full short test suite all run before merge. Don't bypass with `--no-verify`; if a hook fails, fix the underlying issue.

## Known pre-existing failures

Some tests in `transactions_browser_test.go` and `settings_test.go` carry pre-existing failures unrelated to current work (the Pluralize migration, embedded-default updates). They surface in `go test ./...` output but aren't regressions from any single change. If you see them, check `git log` against the file to confirm.
