<p align="center">
  <img src="docs/logo.svg" alt="Reasonix" width="640"/>
</p>

<p align="center">
  <strong>English</strong>
  &nbsp;·&nbsp;
  <a href="./README.zh-CN.md">简体中文</a>
  &nbsp;·&nbsp;
  <a href="https://esengine.github.io/reasonix/">Website</a>
  &nbsp;·&nbsp;
  <a href="./docs/ARCHITECTURE.md">Architecture</a>
  &nbsp;·&nbsp;
  <a href="./benchmarks/">Benchmarks</a>
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/reasonix"><img src="https://img.shields.io/npm/v/reasonix.svg?style=flat-square&color=0d1117&labelColor=161b22" alt="npm version"/></a>
  <a href="https://github.com/esengine/reasonix/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/esengine/reasonix/ci.yml?style=flat-square&label=ci&color=0d1117&labelColor=161b22" alt="CI"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/npm/l/reasonix.svg?style=flat-square&color=0d1117&labelColor=161b22" alt="license"/></a>
  <a href="https://www.npmjs.com/package/reasonix"><img src="https://img.shields.io/npm/dm/reasonix.svg?style=flat-square&color=0d1117&labelColor=161b22" alt="downloads"/></a>
  <a href="./package.json"><img src="https://img.shields.io/node/v/reasonix.svg?style=flat-square&color=0d1117&labelColor=161b22" alt="node"/></a>
  <a href="https://github.com/esengine/reasonix/stargazers"><img src="https://img.shields.io/github/stars/esengine/reasonix.svg?style=flat-square&color=0d1117&labelColor=161b22&logo=github" alt="GitHub stars"/></a>
  <a href="https://github.com/esengine/reasonix/graphs/contributors"><img src="https://img.shields.io/github/contributors/esengine/reasonix.svg?style=flat-square&color=0d1117&labelColor=161b22&logo=github" alt="contributors"/></a>
  <a href="https://github.com/esengine/reasonix/discussions"><img src="https://img.shields.io/github/discussions/esengine/reasonix.svg?style=flat-square&color=0d1117&labelColor=161b22&logo=github" alt="Discussions"/></a>
</p>

<br/>

<h3 align="center">A DeepSeek-native AI coding agent for your terminal.</h3>
<p align="center">Engineered around prefix-cache stability — so token costs stay low across long sessions, and you can leave it running.</p>

<br/>

<p align="center">
  <img src="docs/assets/hero-terminal.svg" alt="Reasonix code mode — assistant proposes a SEARCH/REPLACE edit; nothing on disk until /apply" width="860"/>
</p>

<br/>

> [!TIP]
> **Cache stability isn't a feature you turn on; it's an invariant the loop is designed around.** That's the whole reason Reasonix is DeepSeek-only — every layer is tuned to the byte-stable prefix-cache mechanic.

> [!NOTE]
> **Real user, single day (2026-05-01):** 435M input tokens, **99.82% cache hit**, ~$12 instead of the ~$61 the same workload would cost with no cache on `v4-flash` — see the [case study](./benchmarks/real-world-cache/README.md). DeepSeek provides the cacheable bytes; the four mechanisms in [Pillar 1](./docs/ARCHITECTURE.md#pillar-1--cache-first-loop) are how Reasonix keeps them cacheable across long sessions.

<br/>

## Web search

Reasonix includes `web_search` and `web_fetch` tools. By default it uses **Mojeek** (no setup required). You can switch to a **self-hosted SearXNG** instance — a metasearch engine that aggregates whatever upstream engines your instance is configured for.

### Switching engines (persists to disk)

The `/search-engine` slash command (alias `/se`) writes your choice to `~/.reasonix/config.json` immediately — it survives restarts:

```
/search-engine mojeek              # default, no external deps
/search-engine searxng             # SearXNG at http://localhost:8080
/search-engine searxng http://192.168.1.100:8888  # custom endpoint
```

Equivalent `~/.reasonix/config.json`:

```json
{
  "webSearchEngine": "searxng",
  "webSearchEndpoint": "http://localhost:8080"
}
```

The tool picks up the change on the next call — no restart needed.

### Starting SearXNG

```sh
podman run -d --replace --name searxng -p 8080:8080 docker.io/searxng/searxng
# or: docker run -d -p 8080:8080 searxng/searxng
```

Verify it's running:

```sh
curl http://localhost:8080/search?q=test
# → HTML search results page
```

> **Note:** The endpoint must include the protocol (`http://`). `localhost:8080` alone will fail — the tool will show a clear error telling you to install SearXNG if the server is unreachable.

<br/>

## Install

```bash
cd my-project
npx reasonix code   # paste a DeepSeek API key on first run; persists after
```

Requires Node ≥ 22. Tested on macOS · Linux · Windows (PowerShell · Git Bash · Windows Terminal). Get a [DeepSeek API key →](https://platform.deepseek.com/api_keys) · `reasonix code --help` for flags.

<br/>

## What makes Reasonix different

The loop is organized around four pillars. Each one solves a problem generic agent frameworks don't even see — because they were designed for a different cache mechanic.

<p align="center">
  <a href="./docs/ARCHITECTURE.md"><img src="docs/assets/pillars.svg" alt="Reasonix four pillars — Cache-first loop, R1 thought harvesting, Tool-call repair, Cost control" width="880"/></a>
</p>

<sub align="center">

Click any card to read the full architecture writeup → [Pillar 1](./docs/ARCHITECTURE.md#pillar-1--cache-first-loop) · [Pillar 2](./docs/ARCHITECTURE.md#pillar-2--r1-thought-harvesting-opt-in) · [Pillar 3](./docs/ARCHITECTURE.md#pillar-3--tool-call-repair) · [Pillar 4](./docs/ARCHITECTURE.md#pillar-4--cost-control-v06)

</sub>

<br/>

## Capabilities

<p align="center">
  <img src="docs/assets/feature-grid.svg" alt="Reasonix capabilities — cell-diff renderer, MCP, plan mode, permissions, dashboard, persistent sessions, hooks/skills/memory, semantic search, auto-checkpoints, /effort knob, transcript replay, event log" width="880"/>
</p>

<br/>

## How it compares

|                                   | Reasonix         | Claude Code       | Cursor              | Aider              |
|-----------------------------------|------------------|-------------------|---------------------|--------------------|
| Backend                           | DeepSeek         | Anthropic         | OpenAI / Anthropic  | any (OpenRouter)   |
| License                           | **MIT**          | closed            | closed              | Apache 2           |
| Cost profile                      | **low per task** | premium           | subscription + use  | varies             |
| DeepSeek prefix-cache             | **engineered**   | not applicable    | not applicable      | incidental         |
| Embedded web dashboard            | yes              | —                 | n/a (IDE)           | —                  |
| Configurable web search engine    | `/search-engine` | —             | —                   | —                  |
| Persistent per-workspace sessions | yes              | partial           | n/a                 | —                  |
| Plan mode · MCP · hooks · skills  | yes              | yes               | yes                 | partial            |
| Web search (Mojeek + SearXNG)      | yes              | yes               | yes                 | yes                |
| Open community development        | yes              | —                 | —                   | yes                |

For live cache-hit rates, costs, and methodology, see [`benchmarks/`](./benchmarks/) — the numbers move with model pricing, so they live with the harness, not in the README.

<br/>

## Documentation

- [**Architecture**](./docs/ARCHITECTURE.md) — the four pillars, cache-first loop, harvesting, scaffolds
- [**Benchmarks**](./benchmarks/) — τ-bench-lite harness, transcripts, cost methodology
- [**Website**](https://esengine.github.io/reasonix/) — getting started, dashboard mockup, TUI mockup
- [**Contributing**](./CONTRIBUTING.md) — comment policy, error-handling rules, library-over-hand-rolled
- [**Code of Conduct**](./CODE_OF_CONDUCT.md) · [**Security policy**](./SECURITY.md)

<br/>

## Community

> [!NOTE]
> Reasonix is open source and community-developed. The contributors wall below isn't decoration — every avatar is a real PR that shipped.

Scoped starter tickets — each with background, code pointers, acceptance criteria, and hints — live under the [`good first issue`](https://github.com/esengine/reasonix/labels/good%20first%20issue) label. Pick anything open.

**Open Discussions — opinions wanted:**

- [#20 · CLI / TUI design](https://github.com/esengine/reasonix/discussions/20) — what's broken, what's missing, what would you change?
- [#21 · Dashboard design](https://github.com/esengine/reasonix/discussions/21) — react against the [proposed mockup](https://esengine.github.io/reasonix/design/agent-dashboard.html)
- [#22 · Future feature wishlist](https://github.com/esengine/reasonix/discussions/22) — what would you build into Reasonix next?

**Already using Reasonix and willing to help others discover it?** Publish blog posts, articles, screenshots, talks, or videos to [**Show and tell**](https://github.com/esengine/reasonix/discussions/categories/show-and-tell). The project has no marketing budget — community word of mouth is how new users find it. Sustained advocates earn the badge below, displayed next to the contributors wall once awarded:

<p align="center">
  <a href="https://github.com/esengine/reasonix/discussions/categories/show-and-tell">
    <img src="https://img.shields.io/badge/REASONIX-📣%20ADVOCATE-c4b5fd?style=for-the-badge&labelColor=0d1117" alt="Reasonix Advocate badge — earned by sustained advocates"/>
  </a>
</p>

**Before your first PR**: read [`CONTRIBUTING.md`](./CONTRIBUTING.md) — short, strict rules (comments, errors, libraries-over-hand-rolled). `tests/comment-policy.test.ts` enforces the comment ones; `npm run verify` is the pre-push gate. By participating you agree to the [Code of Conduct](./CODE_OF_CONDUCT.md). Security issues → [SECURITY.md](./SECURITY.md).

<p align="center">
  <a href="https://github.com/esengine/reasonix/graphs/contributors">
    <img src="https://contrib.rocks/image?repo=esengine/reasonix&max=100&columns=12" alt="Contributors to esengine/reasonix" width="860"/>
  </a>
</p>

<br/>

## Non-goals

> [!IMPORTANT]
> Reasonix is opinionated. Some things it deliberately *doesn't* do — listed here so you can pick the right tool for your work.

- **Multi-provider flexibility.** DeepSeek-only on purpose. Coupling to one backend is the feature, not a limitation.
- **IDE integration.** Terminal-first. The diff lives in `git diff`, the file tree in `ls`. The dashboard is a companion, not a Cursor replacement.
- **Hardest-leaderboard reasoning.** Claude Opus still wins some benchmarks. DeepSeek is competitive on coding; if your work is "solve this PhD proof" rather than "fix this auth bug," start with Claude.
- **Air-gapped / fully-free.** Reasonix needs a paid DeepSeek API key. For air-gapped or zero-cost runs see Aider + Ollama or [Continue](https://continue.dev).

<br/>

## Star History

<a href="https://www.star-history.com/?repos=esengine%2Freasonix&type=timeline&logscale=&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=esengine/reasonix&type=timeline&theme=dark&logscale&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=esengine/reasonix&type=timeline&logscale&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=esengine/reasonix&type=timeline&logscale&legend=top-left" />
 </picture>
</a>

<br/>

---

<p align="center">
  <sub>MIT — see <a href="./LICENSE">LICENSE</a></sub>
  <br/>
  <sub>Built by the community at <a href="https://github.com/esengine/reasonix/graphs/contributors">esengine/reasonix</a></sub>
</p>
