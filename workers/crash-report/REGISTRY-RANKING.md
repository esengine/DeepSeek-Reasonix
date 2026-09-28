# Registry ranking, votes and install counts

What the community registry (`/v1/packages`) counts and how it orders the
listing. The constants live in `src/registry/lib/ranking.ts`; this page and
that file must say the same thing.

## Votes

- One vote per account per package: `+1`, `-1`, or none. It can be changed or
  withdrawn at any time (`POST /v1/packages/:handle/:name/vote` with
  `{"value": 1 | -1 | 0}`; `GET` the same path returns the caller's vote).
- Only a signed-in account with a verified email may vote. A publisher cannot
  vote on their own packages (`403 own_package`), admins included.
- `packages.up_count` / `down_count` are recounted from the `votes` table in
  the same D1 batch as the vote itself, so they cannot drift.
- The legacy `POST .../star` toggles between a `+1` vote and no vote under the
  same rules. `starCount` in responses is an alias of `upCount`.
- Every legacy star was migrated to a `+1` vote, except a publisher's star on
  their own package. The `stars` table is left untouched.
- Only an account's first up-vote on a package appears in the public activity
  feed; down-votes and re-votes never do.

## Install counts

- `POST /v1/packages/:handle/:name/installed` with `{"installId": "<32 hex>"}`
  counts at most once per (install id, package, UTC day). `install_count` and
  the daily rollup move only on that first count.
- The install id is anonymous and chosen by the client. Reasonix Studio sends a
  random id it keeps only for the market, unrelated to its usage-statistics id,
  and sends nothing when anonymous usage statistics are switched off. The registry
  stores no IP address; dedupe rows are kept for the 30-day heat window and
  then deleted by the cron.
- A ping without a well-formed `installId` is acknowledged and not counted.

## Recommended order (the default sort)

```
reputation = Wilson score lower bound (z = 1.96, 95%) of up / (up + down); 0 with no votes
decayed    = Σ over distinct install ids seen in the last 30 UTC days of 0.5^(age_days / 14),
             age_days measured to that id's latest install of the package
heat       = min(1, ln(1 + decayed) / ln(1 + 5000))
score      = 0.75 × reputation + 0.25 × heat
```

- `heat` is normalized against a fixed cap (5000 decayed distinct ids), not the
  largest value in the current page, so a package's score does not change with
  the filter or search it was listed under.
- Install ids are anonymous and anyone can mint new ones, bounded only by the
  per-IP write limit (30 a minute), so heat carries the smaller weight: however
  many ids are minted, heat adds at most 0.25, which is below the reputation
  alone of a package with a solid vote record (50 up / 2 down gives 0.65). An
  id returning on several days counts once.
- The score is materialized in `packages.rec_score`: recomputed for a package
  when it receives a vote or a counted install, and for every package by the
  Worker's cron (decay changes scores with no write). It is computed in the
  Worker rather than relying on D1 exposing SQLite's optional math functions
  (`sqrt`, `ln`, `pow`), and the listing sorts on the stored column, which
  keeps pagination and the index in SQL. `POST /v1/admin/rescore` recomputes everything on demand.
- Ties fall back to install count, then newest, then package id.
- Responses carry `upCount`, `downCount`, `approvalRate` (`up / (up + down)`,
  `null` with no votes) and `score`.

## Review flag

A live package with at least 10 down-votes and approval under 30% is listed
under **Flagged** in the moderation console (`/community?status=flagged`,
`GET /v1/admin/packages?status=flagged`). Nothing is hidden automatically; an
admin decides.

## Rollout

1. `npm run migrate:registry-votes-columns` (exactly once)
2. `npm run migrate:registry-votes`
3. deploy the Worker
4. `POST /v1/admin/rescore` as an admin (or wait for the next cron run)
