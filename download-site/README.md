# reasonix-web

Static landing + download site for Reasonix. No build step — React 18 UMD + Babel
standalone (in-browser JSX compile). Hosted on Cloudflare Pages.

## Layout

```
index.html       — landing (/) — hero, install, three pillars, features, …
download.html    — download page (/download) — smart-mirror probe + per-OS picks
src/
  *.jsx          — React components, loaded via <script type="text/babel">
  styles.css     — full sheet, port of the design mockup
  fonts.css      — system-font stack (no external font CDN; matters for CN)
_headers         — Cloudflare Pages response headers
_redirects       — pretty URL → static file mappings
```

## Run locally

```
cd download-site
python3 -m http.server 4173
# open http://127.0.0.1:4173
```

Babel standalone compiles the JSX on every load — fine for dev, fine for our
traffic volume, but if the bundle ever needs to be fast on cold loads, this
should be converted to a real Vite build emitting plain JS.

## Updating the version

Currently hardcoded in three places — keep them in sync per release:

- `src/mirrors.jsx` → `const VERSION = "..."`
- `src/download-page.jsx` → release notes + hero version label + AppImage chmod example
- `src/nav.jsx` + `src/hero.jsx` + `src/footer.jsx` → display strings

`grep -rn "0.42.0-3" src/` will find every site.

## Cloudflare Pages deploy

Two options:

### Dashboard (one-time setup)

1. Cloudflare dashboard → **Pages** → **Connect to Git**
2. Pick this repo, branch `main`
3. **Root directory**: `download-site`
4. **Build command**: *(leave blank — static)*
5. **Build output directory**: `download-site`
6. Save & deploy. Auto-redeploys on push to `main`.

### Wrangler (CI-driven, optional)

A GitHub Action can deploy on every push using
`cloudflare/pages-action@v1` + a `CLOUDFLARE_API_TOKEN` secret. Skipped here —
add it once we want preview deploys per PR.

### Custom domain

In Pages → **Custom domains**, add e.g. `reasonix.dev` or
`download.reasonix.dev`. Update `src/mirrors.jsx`'s `R2_BASE` if R2 also moves
to a custom domain later.
