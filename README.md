# [vibecoded] sure-tui

A terminal UI for the [Sure](https://app.sure.am) personal-finance API: browse,
filter, create, edit, and delete transactions without leaving the keyboard.
Transactions for the chosen range are downloaded once and filtered client-side,
so navigation and filtering are instant.

## Install & run

```sh
make build      # -> bin/sure-tui
make run        # build + run
make test       # go test ./...
```

On first launch it prompts for the API URL, API key, and range, then saves them
(see [Configuration](#configuration)).

```sh
sure-tui                 # start the UI
sure-tui -range 365      # download the last 365 days on start
sure-tui relogin         # re-run the setup prompts (change URL/key/range)
```

`-range` accepts `90`, `180`, `365`, or `all`. Without the flag, the value from
config is used.

## Configuration

Config lives at `$XDG_CONFIG_HOME/sure-tui/config.toml` (falls back to
`~/.config/sure-tui/config.toml`) and is created from a template on first run.

```toml
api_key = "sk-..."
api_url = "https://app.sure.am"
range   = "180"            # 90, 180, 365, all
instant_update = true      # splice new/edited rows in instead of refetching
ui      = "classic"        # pane layout: "classic" (│ separator) or "bordered" (boxed)
max_transactions = 5000    # cap on rows downloaded per range

[theme]                    # hex colors; omit any key to keep the default
# header = "#7aa2f7"
# ...
```

Raise `max_transactions` for larger ledgers (rows download in pages of 100).
Toggle `ui` live with `u` in the app; the choice is written back to the file.

**Setup / relogin.** With no `api_key` (first run) or when started as
`sure-tui relogin`, an interactive prompt asks for URL, key, and range and writes
them back to the file (mode `0600`). Piped/non-interactive runs skip the prompt.

**Environment overrides** (take precedence over the file when set):

| Variable        | Purpose                                  |
| --------------- | ---------------------------------------- |
| `SURE_API_KEY`  | API key (`X-Api-Key` header)             |
| `SURE_API_URL`  | API base URL                             |
| `SURE_THEME`    | Path to a theme TOML overlaid on `[theme]` |

Press `c` in the app to see the resolved config (the key is masked).

## Layout

```
 filter bar                         ← active query + sidebar selections
┌──────────────┬──────────────────┐
│ totals       │ transactions      │
│ accounts     │  grouped by date  │
│ categories   │  with daily       │
│ tags         │  +income/-expense │
└──────────────┴──────────────────┘
 status                             ← last action / download progress
 help line
```

Left pane: per-currency income/expense totals for the current filter, a count,
and the accounts/categories/tags lists. Each account shows its current balance
(with currency) on its own line. Right pane: transactions newest-first, grouped
by date, with the day's totals on each date header.

## Keys

| Key                 | Action                                                      |
| ------------------- | ----------------------------------------------------------- |
| `tab`               | switch focus between the left and right pane                |
| `j` / `k`, `↑` / `↓`| move the cursor                                             |
| `[` / `]`, `h` / `l`| jump half a page                                            |
| `ctrl+u` / `ctrl+d` | half-page up / down                                         |
| `enter` (left pane) | **include** the item in the filter (`●`, green) — toggles   |
| `x` (left pane)     | **exclude** the item from the filter (`✕`, red) — toggles   |
| `enter` / `e` (right pane)| edit the selected transaction                         |
| `n` / `a` / `i`     | new transaction                                             |
| `d`                 | delete the selected transaction (asks to confirm)           |
| `u`                 | switch UI layout (classic ↔ bordered) and save it           |
| `/`                 | open the text filter (see [Filtering](#filtering))          |
| `esc`               | clear the text filter and all sidebar selections            |
| `r`                 | reload from the API                                         |
| `c`                 | config window                                               |
| `?`                 | help window                                                 |
| `q` / `ctrl+c`      | quit                                                        |

Any key dismisses the config/help window.

## Filtering

Two filters combine (ANDed together):

### Sidebar selections (ID-based, tri-state)

Each account/category/tag in the left pane has three states, cycled by key:

- **none** — no effect.
- **include** (`enter`, shown `●`) — transaction must match one of the included
  items *of that kind*. Multiple includes of the same kind are ORed.
- **exclude** (`x`, shown `✕`) — transaction must **not** match this item.

Different kinds are ANDed (e.g. include category *Grocery* **and** exclude
account *Visa*). Selections key on the item's ID, so names containing spaces,
commas, or parentheses never break the filter.

### Text query (`/`)

A comma-separated DSL matched client-side. Press `/`, type, then `enter` to
apply (`esc` to leave). Tokens:

| Token                     | Meaning                                                       |
| ------------------------- | ------------------------------------------------------------- |
| `income` / `expense`      | only that side; both together = every non-transfer            |
| `transfer` / `transfers`  | only transfers                                                |
| `from: 2026-05-01`        | on/after this date (inclusive)                                |
| `to: 2026-05-31`          | on/before this date (inclusive)                               |
| `30d`, `2w`, `3m`, `1y`   | relative lookback: N days / weeks / months / years            |
| `today`, `week`, `month`, `year` | aliases for `0d` / `1w` / `1m` / `1y`                  |
| `july`, `may`, …          | the most recent occurrence of that whole month                |
| `+Name`                   | category name contains `Name` (ORed across multiple)          |
| `@Name`                   | tag name contains `Name` (ORed)                               |
| `(Name)`                  | account name contains `Name` (ORed; parens allow commas)      |
| bare words                | transaction name contains every term (ANDed)                  |

Example: `/income, from: 2026-05-01, +Salary, paycheck`

## Adding & editing transactions

`n` (or `a`/`i`) opens an entry box; `enter` on a row edits it (pre-filled). The
single-line DSL:

```
2026-06-29 (Checking) +20.00 Coffee shop +Dining @reimbursable
```

- **`(Account)`** — required; matched by name (first `(...)` group). An exact
  name wins over a longer one it's a substring of (so `(Bank)` ≠ `Bank Card`).
- **`+amount`** — income; **`-amount`** or no sign — expense.
- **currency** — `-100USD` (suffix) or a standalone `USD`; defaults to the
  account's currency when omitted.
- **date** — `YYYY-MM-DD`; defaults to today if omitted.
- **leading words** — the transaction name.
- **`+Category`** / **`@Tag`** — matched by name; may contain spaces (a segment
  runs until the next marker). Multiple `@Tag`s allowed.

Date, amount, and currency are order-independent. `tab` completes the
account/category/tag token being typed.

**Validation.** The box shows live parse feedback — `✓ ready` with an
interpreted breakdown (date / account / amount+currency / name / category /
tags) so you can confirm before saving, or the specific error (`✗ amount
required`, `✗ no category matching "groery"`, …). `enter` saves only when parsing
succeeds, so a bad line is never half-created; fix it or press `esc` to cancel.

## Notes / limits

- Up to `max_transactions` rows (default 5000) are downloaded per range, in
  pages of 100 that stream in — rows appear as each page arrives.
- Transfers net to zero and are excluded from income/expense totals; the
  `transfer` filter shows them, with `→`/`←` indicating direction.
- All filtering is in-memory; `r` re-downloads.
- Account balances are recomputed server-side asynchronously, so under
  `instant_update` they're adjusted locally on save/delete; `r` reconciles them.
