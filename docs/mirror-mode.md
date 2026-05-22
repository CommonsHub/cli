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
| `chb pull` | rsync data/ + outbox + settings from the trusted host. No provider calls. |
| `chb generate` | no-op; the trusted host already generated. |
| `chb push` | Odoo writes refuse with "no credentials, run on the trusted host". Nostr outbox is flushed if local keys are configured. |
| `chb sync` | pull → (skip generate) → push-nostr-only. |
| read-only commands | unchanged (`chb invoices`, `chb income`, `chb stats`, `chb report`, …). |

Per-invocation override: passing `--no-mirror` on `pull` / `sync` / `push`
bypasses mirror mode. The trusted host runs the same binary, so this
escape hatch is how it does its own `chb sync` without trying to rsync
from itself.

## What gets synced

`$APP_DATA_DIR` is split into three buckets, each with its own sync policy:

```
$APP_DATA_DIR/
  data/             # bucket 1: authoritative pull (--delete)
    <YYYY>/<MM>/providers/...
    <YYYY>/<MM>/generated/
    latest/
    logs/
  settings/         # bucket 2: merge-with-pending-updates
    accounts.json   #   ↑ auto-update if unedited, surface pending if edited
    rules.json
    categories.json
    collectives.json
    rooms.json
    settings.json
    config.env              # EXCLUDED — secrets, per-machine
    .installed-defaults.json # EXCLUDED — tracker, per-machine
    nostr.json              # EXCLUDED — local Nostr identity
  nostr/
    outbox/         # bucket 3: bidirectional (--update)
    sent/           # bucket 3: bidirectional (--update)
  cache/            # never synced — per-machine derived caches
```

Bucket 1 — **data/**: rsync `--delete --safe-links`. The trusted host's
state wins. Local edits to anything under `data/` are erased on the next
pull. This is fine: data/ is provider output + generated artefacts, never
operator state.

Bucket 2 — **settings/**: the per-file decision tree mirrors the
embedded-defaults reconciler in `cmd/settings.go`. Unedited files
auto-update; edited files are preserved and surfaced via
`PendingSettingsUpdates()` so `chb settings` shows the diff. The
`.installed-defaults.json` tracker records the last content we installed
on this machine, so a freshly-cloned settings file is recognised as
"unedited" until the operator changes it.

Bucket 3 — **nostr/outbox** and **nostr/sent**: bidirectional rsync
`--update`. The order is: up first (preserve queued local annotations),
then down (learn about teammates' queued events). Both legs are
best-effort — a read-only mount or a missing remote dir warns but doesn't
abort the pull. The Nostr relay deduplicates by event ID, so the worst
case is "we re-push something already published".

## Lock file

Concurrent mirror operations are serialised via a flock on
`$APP_DATA_DIR/.sync.lock`. A second `chb pull` or `chb sync` waits for
the first to finish, so two cron jobs colliding can't corrupt the local
copy mid-rsync.

## Hard rule from CLAUDE.md is preserved

The "sync vs generate" invariant — sync downloads raw, generate
transforms — is unchanged. Mirror mode SKIPS both phases locally: the
trusted host runs the real pipeline; thin clients only rsync the output.
The invariant still holds on the trusted host, which is the only place
where data/ is authored.
