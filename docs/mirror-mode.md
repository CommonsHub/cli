# Mirror Mode (`CHB_SYNC_SOURCE`)

Most providers chb pulls from need credentials: Stripe secret keys,
Etherscan API keys, the Odoo password, Discord bot token, KBC export
credentials. Putting these on every operator's laptop is both impractical
(rotation churn) and risky (one lost laptop, many revoked tokens).

Mirror mode keeps the credentials on a single trusted host. Every other
teammate runs the same `chb` binary in **thin-client mode**: instead of
calling every provider themselves, they rsync the trusted host's already-
generated state.

## Enabling

Add one line to `$APP_DATA_DIR/settings/config.env` on the thin client:

```
CHB_SYNC_SOURCE=ops@accounting.commonshub.brussels:/srv/chb
```

The value format is the same syntax SSH and rsync accept:
`user@host:/abs/path/to/.chb`. Plain local paths work too
(`CHB_SYNC_SOURCE=/mnt/share/chb`), which is useful for tests.

SSH auth is delegated to the user's ssh-agent / keys. chb never manages
credentials itself — set up `ssh ops@accounting.commonshub.brussels`
working from the command line first, then mirror mode follows.

## Behaviour matrix

When `CHB_SYNC_SOURCE` is set:

| Command | Behaviour |
|---|---|
| `chb pull` | rsync data/ + settings + outbox from the trusted host. No provider calls. |
| `chb generate` | no-op; the trusted host already generated. |
| `chb push` | normal push path. Each target gates on its own credentials, so Odoo refuses on hosts without `ODOO_PASSWORD`; Nostr flushes if local keys exist. |
| `chb sync` | pull → (skip generate) → push (Odoo skipped if no creds, Nostr flushes if keys present). |
| read-only commands | unchanged (`chb invoices`, `chb income`, `chb stats`, `chb report`, …). |

**Credentials, not mirror mode, gate writes.** A thin-client without
`ODOO_PASSWORD` refuses Odoo writes regardless of `CHB_SYNC_SOURCE`;
a host with `ODOO_PASSWORD` writes regardless of mirror mode. The
simpler rule keeps the mental model small — there's no `--no-mirror`
escape hatch to remember.

## What gets synced

`$APP_DATA_DIR` is split into four buckets, each with its own sync policy:

```
$APP_DATA_DIR/
  data/
    latest/                # bucket 1: authoritative pull (--delete)
    <YYYY>/<MM>/...        # bucket 2: append-only (no --delete; past months are immutable history)
  settings/                # bucket 3: authoritative pull (--delete)
    accounts.json
    rules.json
    categories.json
    collectives.json
    rooms.json
    odoo_mapping.json
    tokens.json
    settings.json
    config.env             # EXCLUDED — secrets, per-machine
    .installed_defaults    # EXCLUDED — embedded-defaults tracker, per-machine
  keys/                    # NEVER synced (kept entirely out of the rsync scope)
    nostr.json             # local Nostr identity, SSH-style key dir
  nostr/
    outbox/                # bucket 4: bidirectional (--update)
    sent/                  # bucket 4: bidirectional (--update)
  cache/                   # never synced — per-machine derived caches
```

**Bucket 1 — `data/latest/`**: rsync `--delete --safe-links`. The latest
snapshot is authoritative. Files removed on the trusted host disappear
locally.

**Bucket 2 — `data/<YYYY>/<MM>/`**: rsync WITHOUT `--delete`. Past
months are immutable history. If the trusted host's backup is partial
or a teammate has months we don't, we keep both. New files arrive,
existing files update — nothing is ever deleted from this leg.

**Bucket 3 — `settings/`**: rsync `--delete`, master is source of
truth. The exclusion list keeps per-machine state in place:

  - `config.env` — secrets + `CHB_SYNC_SOURCE` itself.
  - `keys/` — Nostr identity (and any future key material). Lives
    outside `settings/`, but excluded here as belt-and-suspenders.
  - `.installed_defaults` — the embedded-defaults bootstrap tracker.

Earlier prototypes did a "merge-with-pending-updates" dance for
settings. We dropped it: the trusted host is the source of truth.
Edit `rules.json` / `accounts.json` on the master, not on a thin
client — a thin-client edit gets clobbered on the next pull.

**Bucket 4 — `nostr/outbox` and `nostr/sent`**: bidirectional rsync
`--update`. The order is: up first (preserve queued local
annotations), then down (learn about teammates' queued events). Both
legs are best-effort — a read-only mount or a missing remote dir
warns but doesn't abort the pull. The Nostr relay deduplicates by
event ID, so the worst case is "we re-push something already
published".

## Nostr keys

Keys live at `$APP_DATA_DIR/keys/nostr.json` (SSH convention:
private material in its own dedicated tree, 0600 + 0700 mode). Mirror
mode never rsyncs the `keys/` tree, so a thin-client setup keeps its
own Nostr identity even when the rest of the data dir is a read-only
mirror of the trusted host.

Legacy locations (`settings/nostr.json`, `.nostr-keys.json` at the
top level) are still read on a one-shot fallback path that
auto-migrates to the canonical location. After migration, remove the
legacy file manually.

## Lock file

Concurrent mirror operations are serialised via a flock on
`$APP_DATA_DIR/.sync.lock`. A second `chb pull` or `chb sync` waits
for the first to finish, so two cron jobs colliding can't corrupt the
local copy mid-rsync.

## Hard rule from CLAUDE.md is preserved

The "sync vs generate" invariant — sync downloads raw, generate
transforms — is unchanged. Mirror mode SKIPS both phases locally: the
trusted host runs the real pipeline; thin clients only rsync the
output. The invariant still holds on the trusted host, which is the
only place where data/ is authored.
