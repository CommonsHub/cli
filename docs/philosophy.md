# Architecture Philosophy

CHB has two complementary phases. Keep them separate.

## `sync` — download raw provider data

`sync` fetches data from external providers (Luma, Stripe, Etherscan, Discord,
Odoo, Google Calendar, …) and archives it **unchanged** under
`DATA_DIR/YYYY/MM/sources/<source>/`.

Rules:

- No transformation, no enrichment, no normalisation.
- No timezone conversion. Keep timestamps in whatever form the provider sent.
- No re-serialisation that loses information (e.g. don't decode and re-encode
  JSON if it loses ordering or strips fields).
- Idempotent: running `sync` twice on the same period should produce identical
  files. Re-running `sync` must never change generated outputs — only re-fetched
  raw source data can change them.
- Failures here only block fetching; they never corrupt generated state.

If a sync command currently emits derived artifacts (e.g. `generated/events.json`
or `latest/generated/events.md`), that is a layering violation to be migrated
into `generate`, not extended.

## `generate` — transform sources into standardised outputs

`generate` reads `sources/` archives and produces every file under `generated/`
and `latest/generated/`. This is where all normalisation happens.

Rules:

- **All times are normalised to `Europe/Brussels`** in human-facing outputs
  (Markdown, CSV, summary JSON). RFC3339 timestamps in machine-facing JSON may
  carry an explicit `+01:00` / `+02:00` offset; never write naïve local times
  and never write UTC when the user-facing semantics are Brussels.
- All-day events (ICS `VALUE=DATE`) are flagged and rendered without a clock
  time — never as "00:00" or "02:00".
- All currency, locale, and date formatting is consistent across outputs.
- Deterministic: given the same `sources/` input, `generate` produces
  byte-identical output. No timestamps-of-now in payload bodies, only in
  explicit "last updated" fields.
- Safe to re-run. Re-running `generate` after a `sync` must not require
  manual cleanup.

## File layout: one sync file, one generate file per source

For every source under `cmd/`, keep sync and generate code in **separate
files**. The naming convention is:

- `<source>_sync.go` — fetch raw data, archive under `sources/<source>/`.
- `<source>_generate.go` — read archives, produce everything under
  `generated/`.

Example: `cmd/events_sync.go` only fetches ICS feeds and writes per-month
bookings to `sources/ics/`. `cmd/events_generate.go` owns every derived
artifact (`events.json`, `public.ics`, yearly aggregates, `events.csv`,
`events.md`, `rooms.md`).

When the same orchestrator command needs both phases (e.g. `chb calendars
sync` fetches then immediately generates), the sync file's orchestrator calls
into the generate file via a single hand-off function (e.g.
`generateCalendarsForMonths(...)`). Generate code never imports HTTP clients
or provider SDKs. Sync code never writes to `generated/`.

This file split:

- Makes the philosophy reviewable at a glance (a sync file with a
  `writeDataFile("…/generated/…")` is an obvious smell).
- Keeps the OG/enrichment/markdown code reachable when we later expose a
  `chb generate` entry point that runs without re-fetching.
- Lets per-source contributors learn one source at a time.

## Where to put new logic

| You want to… | Put it in |
|---|---|
| Call a provider API and store the response | `sources/<source>/` and `cmd/<source>_sync.go` |
| Parse a raw provider format (ICS, CSV, …) | parser package (e.g. `ical/`) — called from `<source>_generate.go`, not the sync file |
| Normalise a time, currency, or label | `cmd/<source>_generate.go`, near where the output is written |
| Render Markdown / CSV / summary JSON | `cmd/<source>_generate.go` |
| Enrich an existing generated record with cross-source data | a processor (see [processors.md](processors.md)) |

## Why this matters

When a user reports "the times look wrong in `events.md`", the fix must be in
`generate` (or in the parser used by `generate`). Touching the `sync` layer to
fix presentation bugs makes the archives stop matching the upstream source and
breaks the "re-sync is a no-op for generated outputs" invariant.
