# Cookbook

Copy-pasteable recipes for common operations. Most assume you've run `chb setup` once and have credentials in `~/.chb/settings/config.env`.

## Daily refresh

```bash
chb pull                       # fetch everything from external sources
chb generate                   # transform into local outputs
chb odoo journals push         # publish ready Odoo journal lines
chb nostr push                 # publish ready Nostr annotations
```

`chb pull` is bounded by default to the recent window (current month + a few back). To force a wider window: `chb pull --since 2024-01` or `chb pull --history`.

## Fix a miscategorised transaction

Say a Proximus charge ended up under "utilities" instead of "internet".

1. **Update the rule** that picked the wrong category:

   ```bash
   chb rules edit             # opens $EDITOR on rules.json
   # or:
   chb rules add --description "proximus" --category internet --collective commonshub
   ```

2. **Regenerate** so the local files reflect the new rule:

   ```bash
   chb generate
   ```

3. **Push the change** — for transactions that were never pushed, the next push picks up the new resolution automatically. For lines already in Odoo, force a re-apply:

   ```bash
   chb odoo journals 28 categorize --dry-run   # preview
   chb odoo journals 28 categorize             # apply
   ```

   `categorize` rewrites the analytic_distribution + GL account_id on existing lines without creating new ones.

## Add a new merchant rule

Recognise a card payment merchant the description-parser doesn't yet know about:

```bash
chb rules add --description "vistaprint" --category marketing --collective commonshub
chb generate
```

If the new category also needs an Odoo destination, add the mapping:

```bash
chb odoo mapping add --category marketing --account 615000
```

## Add a new bank account

1. Edit `~/.chb/settings/accounts.json`. See [accounts.md](accounts.md) for the schema.
2. If linking to Odoo, find the journal id:

   ```bash
   chb odoo journals          # lists existing journals
   ```

   and set `odooJournalId` in the new entry.
3. Pull to populate the archive:

   ```bash
   chb pull
   chb accounts               # confirm the balance shows up
   ```

## Re-run categorize on a single journal

After fixing a rule that affects many existing Odoo lines:

```bash
chb odoo journals 28 categorize --dry-run         # preview (KBC journal)
chb odoo journals 28 categorize --verbose         # with full per-line trace
chb odoo journals 28 categorize                   # apply
```

The `--dry-run` table shows category/account drift per line. `--verbose` adds the matched rule + previous → new GL account.

## Push only one journal

```bash
chb odoo journals 48 push                          # journal 48 (Stripe)
chb odoo journals 48 push --dry-run                # preview
chb odoo journals 48 push --since 2026-04-01       # window down
```

You can also reference a journal by linked account slug:

```bash
chb odoo journals stripe push
chb odoo journals kbc push --dry-run
```

## Investigate why a partner was created when it shouldn't have

Odoo partner creation has its own audit log under `cmd/odoo_partners_sync.go`. The usual cause is a missing IBAN → partner_bank mapping. Diagnose:

```bash
chb odoo partners                  # list cached partners
chb odoo journals 28 push --dry-run --verbose
# look for "creating partner" lines in the preview
```

If the unwanted partner is already in Odoo, archive it manually there; the local cache will catch up on the next `chb odoo pull`.

## Migrate from legacy settings shapes

`chb` auto-migrates on first load when it can — see `LoadAccountConfigs` (strips legacy `odooJournalName`) and `LoadOdooMappings` (renames `odoo_rules.json` → `odoo_mapping.json`). If you have an older install, just run `chb pull` once; the migration warnings tell you what changed.

## Open every settings file in one shot

```bash
chb rules path                     # prints rules.json path
chb odoo mapping path              # prints odoo_mapping.json path
chb categories                     # opens categories.json TUI
```

Or just `$EDITOR ~/.chb/settings/`.

## Reset a month's archive

Useful when you suspect a partial sync corrupted an archive:

```bash
rm -rf ~/.chb/data/2026/04/providers/stripe
chb pull --month 2026/04                # re-fetch
chb generate --month 2026/04            # regenerate
```

`chb generate` is deterministic — given the same `providers/` input, it produces byte-identical output, so it's safe to re-run.
