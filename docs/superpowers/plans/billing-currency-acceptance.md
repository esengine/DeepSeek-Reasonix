# Billing currency refactor — acceptance checklist

## Automated (run before merge)

- [x] `go test ./...`
- [x] `cd desktop && go test ./...`
- [x] Key packages `-race` (billing, event, boot, cli, config, acp, serve, stats)
- [x] `cd desktop/frontend && pnpm typecheck && pnpm test:usage-stats && pnpm build`
- [x] `scripts/check-cache-impact.sh` → no provider-visible changes
- [x] `reasonix doctor billing` surfaces display/billing/FX-disabled/catalog

## Manual UI (Desktop + Serve)

1. **CNY ↔ USD hot switch** — Settings → Display currency; session estimate rebinds without zeroing; provider prices unchanged.
2. **Restart restore** — reload a session with `*.telemetry.json` v2 legacy cost; ledger migrates lazily; no invented totals for wiped zeros.
3. **Mixed models** — main + planner + subagent with different currencies; total uses shared valuations or shows incomplete.
4. **No runtime FX** — no ECB request, cache, or refresh goroutine; custom-currency totals stay in original buckets; official dual-region still values via `official_table`.
5. **Wallet** — multi-currency balance shows `≈` total + detail tooltip (originals, rate date).
6. **MiMo Token Plan** — cost label uses PAYG-equivalent wording when `billing_mode=subscription_equivalent`.
7. **Narrow / restore** — Serve and Desktop after session resume show same selected estimate.

## GitHub follow-up

- [ ] Open PR; link #4565, #3527, #4546; note supersedes #7790
- [ ] After merge, mark PR #7790 superseded
