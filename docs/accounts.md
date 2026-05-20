# Accounts

The word *account* in `chb` means a bank or payment account — something that produces `TransactionEntry` records. Calendar feeds, even though they're external sources, are not accounts.

## `accounts.json`

Lives at `$APP_DATA_DIR/settings/accounts.json`. One entry per bank/payment account.

```json
[
  {
    "name": "💳 Stripe Account",
    "slug": "stripe",
    "provider": "stripe",
    "accountId": "acct_1Nn0FaFAhaWeDyow",
    "currency": "EUR",
    "odooJournalId": 48
  },
  {
    "name": "🏦 KBC",
    "slug": "kbc",
    "provider": "kbcbrussels",
    "iban": "BE46734072238636",
    "currency": "EUR",
    "odooJournalId": 28
  },
  {
    "name": "🏦 Monerium Savings",
    "slug": "savings",
    "provider": "etherscan",
    "chain": "gnosis",
    "chainId": 100,
    "address": "0x6fDF0AaE33E313d9C98D2Aa19Bcd8EF777912CBf",
    "walletType": "safe",
    "odooJournalId": 47,
    "token": {
      "address": "0x420CA0f9B9b604cE0fd9C18EF134C705e5Fa3430",
      "name": "EURe",
      "symbol": "EURe",
      "decimals": 18
    }
  }
]
```

### Fields

| Field | Required | Notes |
|---|---|---|
| `name` | yes | Human-friendly label used in the UI (emoji ok). |
| `slug` | yes | Stable identifier — used in URLs, filenames, and `--slug` filters. |
| `provider` | yes | `stripe`, `kbcbrussels`, `etherscan`, `monerium`. |
| `accountId` | stripe | Stripe connected-account id (`acct_…`). |
| `iban` | kbcbrussels | The bank IBAN, no spaces. |
| `chain` / `chainId` | etherscan | Lower-case chain slug (`gnosis`, `celo`, `ethereum`) and EIP-155 id. |
| `address` | etherscan | EVM address holding the balance. |
| `walletType` | optional | `safe` for Safe multisigs, `EOA` otherwise. Determines balance fetching. |
| `currency` | optional | Display currency; defaults to `EUR` or the token symbol. |
| `token` | optional | Token contract metadata (ERC-20). When set, balances reflect the token, not native. |
| `odooJournalId` | optional | Numeric Odoo `account.journal` id. Required to push to Odoo. |

### Not in `accounts.json`

- **`odooJournalName`** — the journal name often embeds the IBAN, which we don't want to leak when this file is checked into a public repo. The display name is cached separately under `$APP_DATA_DIR/cache/`. (Legacy entries with this field are stripped on next load.)
- **ICS calendars** — those live in `calendars.json`, see below.

## `calendars.json`

ICS calendar feeds, not accounts. Schema:

```json
{
  "google": {
    "url": "https://calendar.google.com/calendar/ical/.../public/basic.ics",
    "visibility": "auto"
  },
  "luma": {
    "url": "https://api2.luma.com/ics/get?entity=calendar&id=cal-…",
    "visibility": "public"
  }
}
```

`visibility` controls whether events from this feed land in public outputs (`events.json`) or stay in the internal booking view: `public`, `private`, or `auto` (decide per-event from ICS classification).

## Adding a new account

The interactive way:

```bash
chb setup        # walks through Stripe / Odoo / blockchain / KBC setup
```

The direct way: edit `~/.chb/settings/accounts.json`, add an entry, then:

```bash
chb pull                 # picks up the new account on the next pull
chb accounts             # confirm the account shows up with a balance
```

If you're linking it to an Odoo journal, also set `odooJournalId` and run `chb odoo journals` to see it listed.

## See also

- [data-model.md](data-model.md) — how `accountId` URIs flow into `TransactionEntry`.
- [cookbook.md § Add a new bank account](cookbook.md#add-a-new-bank-account) — recipe.
- [philosophy.md § "Account" means bank/payment account](philosophy.md#account-means-bankpayment-account-never-feed) — why we keep this distinction.
