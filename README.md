<p align="center">
  <img src="docs/logo-ghost-wave-effect.svg" alt="Reasonix" width="360"/>
</p>

<p align="center">
  <strong>English</strong>
  &nbsp;·&nbsp;
  <a href="./README.zh-CN.md">简体中文</a>
  &nbsp;·&nbsp;
  <a href="./docs/GUIDE.md">Guide</a>
  &nbsp;·&nbsp;
  <a href="./docs/ACP.md">ACP</a>
  &nbsp;·&nbsp;
  <a href="./docs/EXTENSIONS.md">Extensions</a>
  &nbsp;·&nbsp;
  <a href="./docs/SPEC.md">Spec</a>
  &nbsp;·&nbsp;
  <a href="https://esengine.github.io/DeepSeek-Reasonix/">Website</a>
  &nbsp;·&nbsp;
  <strong><a href="https://discord.gg/XF78rEME2D">Discord</a></strong>
  &nbsp;·&nbsp;
  <strong><a href="https://qm.qq.com/q/i59b0z2R8s">QQ</a></strong>
</p>

<p align="center">
  <a href="https://github.com/esengine/DeepSeek-Reasonix/releases?q=studio-v&expanded=true"><img src="https://img.shields.io/github/v/release/esengine/DeepSeek-Reasonix.svg?filter=studio-v*&include_prereleases&style=flat-square&color=8250df&labelColor=161b22&label=studio%202.x" alt="Reasonix Studio 2.x"/></a>
  <a href="https://www.npmjs.com/package/reasonix"><img src="https://img.shields.io/npm/v/reasonix.svg?style=flat-square&color=cb3837&labelColor=161b22&logo=npm&logoColor=white" alt="npm version"/></a>
  <a href="https://github.com/esengine/DeepSeek-Reasonix/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/esengine/DeepSeek-Reasonix/ci.yml?style=flat-square&label=ci&labelColor=161b22&logo=githubactions&logoColor=white" alt="CI"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/npm/l/reasonix.svg?style=flat-square&color=8b949e&labelColor=161b22" alt="license"/></a>
  <a href="https://www.npmjs.com/package/reasonix"><img src="https://img.shields.io/npm/dm/reasonix.svg?style=flat-square&color=3fb950&labelColor=161b22&label=downloads" alt="downloads"/></a>
  <a href="https://github.com/esengine/DeepSeek-Reasonix/stargazers"><img src="https://img.shields.io/github/stars/esengine/DeepSeek-Reasonix.svg?style=flat-square&color=dbab09&labelColor=161b22&logo=github&logoColor=white" alt="GitHub stars"/></a>
  <a href="https://atomgit.com/esengine/DeepSeek-Reasonix"><img src="https://atomgit.com/esengine/DeepSeek-Reasonix/star/badge.svg" alt="AtomGit stars"/></a>
  <a href="https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors"><img src="https://img.shields.io/github/contributors/esengine/DeepSeek-Reasonix.svg?style=flat-square&color=bc8cff&labelColor=161b22&logo=github&logoColor=white" alt="contributors"/></a>
  <a href="https://github.com/esengine/DeepSeek-Reasonix/discussions"><img src="https://img.shields.io/github/discussions/esengine/DeepSeek-Reasonix.svg?style=flat-square&color=58a6ff&labelColor=161b22&logo=github&logoColor=white" alt="Discussions"/></a>
  <a href="https://discord.gg/XF78rEME2D"><img src="https://img.shields.io/badge/discord-join-5865F2.svg?style=flat-square&labelColor=161b22&logo=discord&logoColor=white" alt="Discord"/></a>
  <a href="https://qm.qq.com/q/i59b0z2R8s"><img src="https://img.shields.io/badge/QQ%20group-1093562660-12B7F5.svg?style=flat-square&labelColor=161b22&logo=tencentqq&logoColor=white" alt="QQ group 1093562660"/></a>
</p>

<p align="center">
  <a href="https://trendshift.io/repositories/27020?utm_source=trendshift-badge&amp;utm_medium=badge&amp;utm_campaign=badge-trendshift-27020" target="_blank" rel="noopener noreferrer"><img src="https://trendshift.io/api/badge/trendshift/repositories/27020/monthly?language=Go" alt="esengine/DeepSeek-Reasonix | Trendshift" width="250" height="55"/></a>
  <a href="https://trendshift.io/repositories/27020?utm_source=repository-badge&amp;utm_medium=badge&amp;utm_campaign=badge-repository-27020" target="_blank" rel="noopener noreferrer"><img src="https://trendshift.io/api/badge/repositories/27020" alt="esengine/DeepSeek-Reasonix | Trendshift" width="250" height="55"/></a>
</p>

<br/>

<p align="center"><strong>Open source · MIT · a single Go binary</strong></p>
<h3 align="center">A reliable coding agent for complex software engineering tasks.</h3>
<p align="center">One local engine, four ways in — terminal, desktop app, browser, or your editor over ACP. Plan mode, permissions, a workspace sandbox and per-turn checkpoints keep a long autonomous run something you can still read and undo.</p>
<p align="center">Maintained by <strong>Huahui Yu</strong>.</p>

> [!IMPORTANT]
> **Community · 加入社区** — bilingual Discord for setup help (`#help` / `#求助`), workflow showcases, and feature ideas. → **<https://discord.gg/XF78rEME2D>**
>
> QQ group **DeepSeek-Reasonix官方群** (`1093562660`) → **<https://qm.qq.com/q/i59b0z2R8s>** ([QR code](./docs/assets/qq-group.svg))

> Douyin: **做游戏的小鱼** (Douyin ID `22703872788`) · [QR code](./docs/assets/douyin.png)

<br/>

## Versions

Reasonix ships on two lines. See the [version roadmap announcement](https://github.com/esengine/DeepSeek-Reasonix/discussions/10748) for the reasoning behind the split.

| Line | Branch | Status | Get it |
| --- | --- | --- | --- |
| **Reasonix 2.x** | `studio` (this branch) | Active development, pre-release | [Studio releases](https://github.com/esengine/DeepSeek-Reasonix/releases?q=studio-v&expanded=true) |
| **Reasonix 1.x** | [`main-v2`](https://github.com/esengine/DeepSeek-Reasonix/tree/main-v2) | Maintenance / stable | `npm i -g reasonix` · [desktop download](https://reasonix.io/?download=desktop#start) |

- **Want something stable?** Stay on 1.x. It keeps receiving bug fixes,
  provider/API compatibility, updater and security fixes; its core architecture
  is no longer being extended.
- **Want the new architecture?** Use 2.x Reasonix Studio. It is still moving
  quickly; reports are welcome.

Valuable fixes, tests and behaviour from 1.x are reviewed one by one and ported
or reimplemented on 2.x where they fit the new architecture.

- [Moving from 1.x to 2.x](./docs/MIGRATING.md): what the two lines share, and
  how to run both on one machine without losing sessions.
- [2.x roadmap](./docs/ROADMAP.md): what 2.x still has to deliver, how it ships,
  and the decisions still open.

## Features

- **Config-driven.** Providers, the agent, enabled tools, and plugins are all
  declared in `reasonix.toml`. No hardcoded models.
- **Multi-model & composable.** DeepSeek ships as a preset; any
  OpenAI-compatible endpoint is a config entry, not new code. Optionally run
  two models together (executor + planner) in separate, cache-stable sessions.
- **Plugin-driven.** MCP servers contribute tools, prompts, and resources;
  Extension Protocol v1 sidecars can also intercept runtime events, contribute
  Providers and structured UI, and ship versioned plugin packages.
- **Cache-aware context maintenance.** Startup injects a small stable environment
  summary, stale tool output is snipped/pruned before summary compaction, and the
  built-in tool schema contract is documented for regression review.
- **Zero-friction distribution.** `CGO_ENABLED=0` single binary; cross-compile
  to six targets with one command. The result is a fully self-contained static
  binary — nothing to install on the target machine beyond the binary itself.

## Install

This branch is **Reasonix 2.x**. Its desktop app is Reasonix Studio; the CLI,
Studio, and editor integrations all run the same local Reasonix engine.

### Reasonix Studio (2.x)

Download the package for your platform from the latest
[Studio release](https://github.com/esengine/DeepSeek-Reasonix/releases?q=studio-v&expanded=true)
(tagged `studio-v2.*`, published as pre-releases while 2.x is in active
development):

| Platform | Package | Architecture |
| --- | --- | --- |
| macOS | `.dmg` or `.zip` | Apple Silicon (`arm64`) / Intel (`amd64`) |
| Windows | Installer `-installer.exe` or portable `.zip` | x64 |
| Linux | `.deb` | x64 |

Every package ships with a `.minisig` signature, and the release carries
`SHA256SUMS`. Once installed, Studio updates itself in place.

The same release also carries the 2.x `reasonix` CLI as archives for
`darwin|linux|windows × amd64|arm64`.

### Reasonix 1.x (stable)

The 1.x CLI installs through npm on any supported platform, or Homebrew on
macOS:

```sh
npm i -g reasonix                  # any OS; pulls the prebuilt native binary
brew install esengine/reasonix/reasonix   # macOS
```

The 1.x desktop app is on the
[official download page](https://reasonix.io/?download=desktop#start). Windows
installers are code-signed with a Certum code-signing certificate.

### VS Code extension

The extension does not bundle the CLI; it starts your local `reasonix acp`
backend and adds native chat, editor context, tool-call approvals, model
selection, and workspace sessions. Install the 1.x CLI first.

- **VS Code:** [install from Visual Studio Marketplace](https://marketplace.visualstudio.com/items?itemName=SivanLiu.reasonix-agent)
- **VSCodium / Eclipse Theia:** [install from Open VSX Registry](https://open-vsx.org/extension/SivanLiu/reasonix-agent)
- **Extension ID:** `SivanLiu.reasonix-agent` · [source and usage guide](https://github.com/SivanCola/reasonix-vscode)

### Build from source

Clone the repository; `studio` builds 2.x and `main-v2` builds 1.x:

```sh
git clone https://github.com/esengine/DeepSeek-Reasonix.git
cd DeepSeek-Reasonix
git switch studio
```

#### CLI

The CLI build requires **Go 1.25+**. The module pins a `toolchain` directive;
keep `GOTOOLCHAIN=auto` so Go downloads the pinned toolchain, or install it.

```sh
make build      # -> bin/reasonix(.exe)
make cross      # -> dist/ (darwin|linux|windows × amd64|arm64)
```

#### Studio

Studio additionally requires **Node 24+ and pnpm 10**
(`npm install -g pnpm@10`) for the frontend.

```sh
make studio
```

See the [Studio build guide](desktop/README.md#prerequisites) for platform
webview dependencies and Linux build tags.

## Quick start

### Reasonix Studio

Install and launch Studio, then connect a provider and model in the app. No CLI
setup is needed.

### CLI / TUI

```sh
reasonix setup                      # configure a provider and model
reasonix                            # start an interactive session
reasonix run "implement the TODOs in main.go"
```

In an interactive session, run `/init` when you want Reasonix to create project
instructions.

For advanced CLI usage and configuration, see the **[CLI reference](./docs/CLI.md)**,
**[Guide](./docs/GUIDE.md)**, and
**[configuration paths](./docs/CONFIG_PATHS.md)**.

## Documentation

- **Getting started:** [Guide](./docs/GUIDE.md) · [CLI reference](./docs/CLI.md) ·
  [Configuration paths](./docs/CONFIG_PATHS.md) · [ACP editor integration](./docs/ACP.md)
- **Features & troubleshooting:** [Subagent profiles](./docs/SUBAGENT_PROFILES.md) ·
  [Context Engine v2](./docs/SESSION_MEMORY_RETRIEVAL.md) ·
  [Capability diagnostics](./docs/CAPABILITY_DIAGNOSTICS.md) ·
  [Recovery and updates](./docs/RECOVERY.md) ·
  [Checkpoints & rewind](./docs/CHECKPOINTS.md)
- **Engineering & migration:** [Spec](./docs/SPEC.md) ·
  [Task contracts & pause policy](./docs/TASK_CONTRACT.md) ·
  [Tool contract](./docs/TOOL_CONTRACT.md) · [Moving from 1.x to 2.x](./docs/MIGRATING.md) ·
  [2.x roadmap](./docs/ROADMAP.md)
- **Extension development:** [Skills](./docs/SKILLS.md) · [Extensions](./docs/EXTENSIONS.md) ·
  [Plugin packages and Manifest v1](./docs/PLUGIN_PACKAGES.md) ·
  [Extension Protocol](./docs/EXTENSION_PROTOCOL.md) ·
  [Go SDK and starter](./sdk/go/README.md)

## Star History

<a href="https://www.star-history.com/?repos=esengine%2FDeepSeek-Reasonix&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/esengine/DeepSeek-Reasonix/star-history/assets/star-history/star-history-dark.svg" />
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/esengine/DeepSeek-Reasonix/star-history/assets/star-history/star-history-light.svg" />
   <img alt="Star History Chart" src="https://raw.githubusercontent.com/esengine/DeepSeek-Reasonix/star-history/assets/star-history/star-history-light.svg" />
 </picture>
</a>

<br/>

## Acknowledgments

A small list of folks whose work has shaped Reasonix the most — the current top
20 contributors by commit count. The full contributor graph is on
[GitHub](https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors?all=1).

<!-- reasonix-top-contributors:start -->
| Contributor | Contributor | Contributor | Contributor |
| --- | --- | --- | --- |
| [**SivanCola**](https://github.com/SivanCola) | [**esengine**](https://github.com/esengine) | [**ttmouse**](https://github.com/ttmouse) | [**lifu963**](https://github.com/lifu963) |
| **reasonix** | [**HUQIANTAO**](https://github.com/HUQIANTAO) | [**GTC2080**](https://github.com/GTC2080) | [**mchenziyi**](https://github.com/mchenziyi) |
| [**Li-Charles-One**](https://github.com/Li-Charles-One) | **merge-order-check** | [**light-front-theory**](https://github.com/light-front-theory) | **Yan Li** |
| [**eghrhegpe**](https://github.com/eghrhegpe) | **wufengfan** | [**Bernardxu123**](https://github.com/Bernardxu123) | [**HaoyueQin**](https://github.com/HaoyueQin) |
| [**CVEngineer66**](https://github.com/CVEngineer66) | [**KHG420**](https://github.com/KHG420) | [**JesonChou**](https://github.com/JesonChou) | [**SuMuxi66**](https://github.com/SuMuxi66) |
<!-- reasonix-top-contributors:end -->

Special thanks to [**Bernardxu123**](https://github.com/Bernardxu123) for designing the project logo and intro video.

<p align="center">
  <a href="https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors">
    <img src="https://contrib.rocks/image?repo=esengine/DeepSeek-Reasonix&max=100&columns=12" alt="Contributors to esengine/DeepSeek-Reasonix" width="860"/>
  </a>
</p>

<br/>

---

<p align="center">
  <sub>MIT — see <a href="./LICENSE">LICENSE</a></sub>
  <br/>
  <sub>Built by the community at <a href="https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors">esengine/DeepSeek-Reasonix</a></sub>
</p>

---

<p align="center"><sub><strong>Support this project</strong></sub></p>

If Reasonix has been useful and you'd like to say thanks, you can. It stays a
coffee, not a contract — donations don't buy feature priority or change how
issues get triaged.

- **International** — PayPal: [paypal.me/yuhuahui](https://paypal.me/yuhuahui)
- **国内** — 微信支付 / 支付宝（扫码）

<p align="center">
  <img src=".github/sponsor/wechat-pay.jpg" alt="WeChat Pay QR code" width="180"/>
  <img src=".github/sponsor/alipay.jpg" alt="Alipay QR code" width="180"/>
</p>
