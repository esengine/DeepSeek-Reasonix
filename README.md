<p align="center">
  <img src="docs/logo.svg" alt="Reasonix" width="640"/>
</p>

<p align="center">
  <strong>English</strong>
  &nbsp;·&nbsp;
  <a href="./README.zh-CN.md">简体中文</a>
  &nbsp;·&nbsp;
  <a href="https://esengine.github.io/DeepSeek-Reasonix/">Website</a>
  &nbsp;·&nbsp;
  <a href="https://esengine.github.io/DeepSeek-Reasonix/configuration.html">Guide</a>
  &nbsp;·&nbsp;
  <a href="./docs/ARCHITECTURE.md">Architecture</a>
  &nbsp;·&nbsp;
  <a href="./benchmarks/">Benchmarks</a>
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/reasonix"><img src="https://img.shields.io/npm/v/reasonix.svg?style=flat-square&color=cb3837&labelColor=161b22&logo=npm&logoColor=white" alt="npm version"/></a>
  <a href="https://github.com/esengine/reasonix/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/esengine/reasonix/ci.yml?style=flat-square&label=ci&labelColor=161b22&logo=githubactions&logoColor=white" alt="CI"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/npm/l/reasonix.svg?style=flat-square&color=8b949e&labelColor=161b22" alt="license"/></a>
  <a href="https://www.npmjs.com/package/reasonix"><img src="https://img.shields.io/npm/dm/reasonix.svg?style=flat-square&color=3fb950&labelColor=161b22&label=downloads" alt="downloads"/></a>
  <a href="./package.json"><img src="https://img.shields.io/node/v/reasonix.svg?style=flat-square&color=5fa04e&labelColor=161b22&logo=nodedotjs&logoColor=white" alt="node"/></a>
  <a href="https://github.com/esengine/reasonix/stargazers"><img src="https://img.shields.io/github/stars/esengine/reasonix.svg?style=flat-square&color=dbab09&labelColor=161b22&logo=github&logoColor=white" alt="GitHub stars"/></a>
  <a href="https://github.com/esengine/reasonix/graphs/contributors"><img src="https://img.shields.io/github/contributors/esengine/reasonix.svg?style=flat-square&color=bc8cff&labelColor=161b22&logo=github&logoColor=white" alt="contributors"/></a>
  <a href="https://github.com/esengine/reasonix/discussions"><img src="https://img.shields.io/github/discussions/esengine/reasonix.svg?style=flat-square&color=58a6ff&labelColor=161b22&logo=github&logoColor=white" alt="Discussions"/></a>
</p>

<p align="center">
  <a href="https://oosmetrics.com/repo/esengine/reasonix"><img src="https://api.oosmetrics.com/api/v1/badge/achievement/9e931d80-2050-4b10-902e-44970cc133ad.svg" alt="oosmetrics — Top 2 in Agents by velocity"/></a>
  <a href="https://oosmetrics.com/repo/esengine/reasonix"><img src="https://api.oosmetrics.com/api/v1/badge/achievement/556d94b3-61b7-486b-baf2-888b9327deab.svg" alt="oosmetrics — Top 3 in LLMs by velocity"/></a>
  <a href="https://oosmetrics.com/repo/esengine/reasonix"><img src="https://api.oosmetrics.com/api/v1/badge/achievement/0f457d4c-efca-4d15-ad2b-139691ff342c.svg" alt="oosmetrics — Top 3 in CLI by velocity"/></a>
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

## Install

```bash
cd my-project
npx reasonix code   # paste a DeepSeek API key on first run; persists after
```

Requires Node ≥ 22. Works on macOS · Linux · Windows (PowerShell · Git Bash · Windows Terminal). Grab a [DeepSeek API key →](https://platform.deepseek.com/api_keys) · `reasonix code --help` for flags.

`npx` is the recommended path — no global install, always latest. If you use Reasonix daily and want it on `PATH`, run `reasonix update` once.

| Command | When |
|---|---|
| `reasonix code [dir]` | The coding agent. **Start here.** |
| `reasonix chat` | Plain chat — no filesystem or shell tools. |
| `reasonix run "task"` | One-shot, streams to stdout. Good for pipes. |
| `reasonix doctor` | Health check: Node, API key, MCP wiring. |
| `reasonix update` | Upgrade Reasonix itself. |

Other subcommands (`replay` · `diff` · `events` · `stats` · `index` · `mcp` · `prune-sessions`) are in `reasonix --help` and the [CLI reference](https://esengine.github.io/DeepSeek-Reasonix/#cli).

<details>
<summary><strong>Working in another folder · chat vs. code · author a skill</strong></summary>

**Working in a different folder.** Reasonix scopes filesystem tools to the launch directory; pass `--dir` to retarget. Mid-session switching isn't supported by design (memory paths would tangle with stale roots) — quit and relaunch.

```bash
npx reasonix code --dir /path/to/project
```

**Picking `chat` vs `code`.** `code` is the default and the only mode with filesystem / shell tools and SEARCH/REPLACE review. `chat` is the lighter, tools-off shell — reach for it when you want a thinking partner with MCP attached but no disk access.

| What you get | `code` | `chat` |
|---|---|---|
| Filesystem tools + `edit_file` | ✓ | — |
| SEARCH/REPLACE → `/apply` review | ✓ | — |
| Shell tool (gated) | ✓ | — |
| Plan mode · `/todo` · `/skill new` · `/mcp add` | ✓ | — |
| Memory (`remember` / `recall_memory`) | project + global | global only |
| MCP servers from config · web search · `ask_choice` | ✓ | ✓ |
| Coding system prompt | ✓ | generic |
| Session scope | per-directory | shared default |

**Author your first skill.** No remote registry — write them directly. Edit the file (`description:` frontmatter + body), then `/skill list`. Add `runAs: subagent` to spawn an isolated subagent loop instead of inlining the body.

```bash
/skill new my-skill              # <project>/.reasonix/skills/my-skill.md
/skill new my-skill --global     # ~/.reasonix/skills for cross-project use
```

</details>

<br/>

## Configuration

One JSON file at `~/.reasonix/config.json` plus per-project overrides under `<project>/.reasonix/`. The full bilingual reference — every key, every slash command, the on-disk shape of skills/memory/hooks — lives at:

> 📘 **[Configuration Guide](https://esengine.github.io/DeepSeek-Reasonix/configuration.html)** · [中文](https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh)

| Topic | Quick read |
|---|---|
| [MCP servers](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#mcp) | stdio · SSE · Streamable HTTP. One spec format works for both `config.json` and `--mcp`. |
| [Skills](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#skills) | Markdown playbooks the model can invoke. `inline` or `subagent` mode. |
| [Memory](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#memory) | User-private knowledge pinned into the prefix. `user` / `feedback` / `project` / `reference` types. |
| [Hooks](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#hooks) | Shell commands on lifecycle events. `PreToolUse` (gating) · `PostToolUse` · `UserPromptSubmit` · `Stop`. |
| [Permissions](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#permissions) | Per-workspace shell allowlist. Exact-prefix match. |
| [Web search](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#search) | Mojeek by default; switch to self-hosted SearXNG with `/search-engine`. |
| [Semantic index](https://esengine.github.io/DeepSeek-Reasonix/configuration.html#index) | `reasonix index` — local Ollama or any OpenAI-compatible embedding endpoint. |

<br/>

## What makes Reasonix different

The loop is organized around three pillars. Each one solves a problem generic agent frameworks don't even see — because they were designed for a different cache mechanic.

<sub align="center">

Click through to the full architecture writeup → [Pillar 1 — Cache-first loop](./docs/ARCHITECTURE.md#pillar-1--cache-first-loop) · [Pillar 2 — Tool-call repair](./docs/ARCHITECTURE.md#pillar-2--tool-call-repair) · [Pillar 3 — Cost control](./docs/ARCHITECTURE.md#pillar-3--cost-control-v06)

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

- [**Architecture**](./docs/ARCHITECTURE.md) — three pillars: cache-first loop, tool-call repair, cost control
- [**CLI Reference**](./docs/CLI-REFERENCE.md) — every shell subcommand, every slash command, every keybinding
- [**Benchmarks**](./benchmarks/) — τ-bench-lite harness, transcripts, cost methodology
- [**Website**](https://esengine.github.io/DeepSeek-Reasonix/) — getting started, dashboard mockup, TUI mockup
- [**Contributing**](./CONTRIBUTING.md) — comment policy, error-handling rules, library-over-hand-rolled
- [**Code of Conduct**](./CODE_OF_CONDUCT.md) · [**Security policy**](./SECURITY.md)

<br/>

## Community

> [!NOTE]
> Reasonix is open source and community-developed. The contributors wall below isn't decoration — every avatar is a real PR that shipped.

Scoped starter tickets — each with background, code pointers, acceptance criteria, and hints — live under the [`good first issue`](https://github.com/esengine/reasonix/labels/good%20first%20issue) label. Pick anything open.

**Open Discussions — opinions wanted:**

- [#20 · CLI / TUI design](https://github.com/esengine/reasonix/discussions/20) — what's broken, what's missing, what would you change?
- [#21 · Dashboard design](https://github.com/esengine/reasonix/discussions/21) — react against the [proposed mockup](https://esengine.github.io/DeepSeek-Reasonix/design/agent-dashboard.html)
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
    <img src="https://contrib.rocks/image?repo=esengine/DeepSeek-Reasonix&max=100&columns=12" alt="Contributors to esengine/DeepSeek-Reasonix" width="860"/>
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

<a href="https://www.star-history.com/?repos=esengine%2FDeepSeek-Reasonix&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=esengine/DeepSeek-Reasonix&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=esengine/DeepSeek-Reasonix&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=esengine/DeepSeek-Reasonix&type=date&legend=top-left" />
 </picture>
</a>

<br/>

---

<p align="center">
  <sub>MIT — see <a href="./LICENSE">LICENSE</a></sub>
  <br/>
  <sub>Built by the community at <a href="https://github.com/esengine/reasonix/graphs/contributors">esengine/reasonix</a></sub>
  <br/>
  <sub>Special thanks to <a href="https://xhslink.com/m/80ngts127cA">AIGC Link</a> for the promotions on XiaoHongShu.</sub>
</p>
