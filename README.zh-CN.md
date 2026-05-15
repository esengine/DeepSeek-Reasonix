<p align="center">
  <img src="docs/logo.svg" alt="Reasonix" width="640"/>
</p>

<p align="center">
  <a href="./README.md">English</a>
  &nbsp;·&nbsp;
  <strong>简体中文</strong>
  &nbsp;·&nbsp;
  <a href="https://esengine.github.io/DeepSeek-Reasonix/">官方网站</a>
  &nbsp;·&nbsp;
  <a href="https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh">配置指南</a>
  &nbsp;·&nbsp;
  <a href="./docs/ARCHITECTURE.md">架构文档</a>
  &nbsp;·&nbsp;
  <a href="./benchmarks/">基准测试</a>
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
  <a href="https://oosmetrics.com/repo/esengine/reasonix"><img src="https://api.oosmetrics.com/api/v1/badge/achievement/9e931d80-2050-4b10-902e-44970cc133ad.svg" alt="oosmetrics — Agents 速度榜 Top 2"/></a>
  <a href="https://oosmetrics.com/repo/esengine/reasonix"><img src="https://api.oosmetrics.com/api/v1/badge/achievement/556d94b3-61b7-486b-baf2-888b9327deab.svg" alt="oosmetrics — LLMs 速度榜 Top 3"/></a>
  <a href="https://oosmetrics.com/repo/esengine/reasonix"><img src="https://api.oosmetrics.com/api/v1/badge/achievement/0f457d4c-efca-4d15-ad2b-139691ff342c.svg" alt="oosmetrics — CLI 速度榜 Top 3"/></a>
</p>

<br/>

<h3 align="center">DeepSeek 原生的终端 AI 编程代理。</h3>
<p align="center">围绕前缀缓存稳定性设计 —— 长会话下 token 成本始终低位运行，可以一直开着。</p>

<br/>

<p align="center">
  <img src="docs/assets/hero-terminal.zh-CN.svg" alt="Reasonix code 模式预览 — 助手提出 SEARCH/REPLACE 编辑，未 /apply 不落盘" width="860"/>
</p>

<br/>

> [!TIP]
> **缓存稳定不是开关，而是循环要围绕设计的不变量。** 这就是 Reasonix 只支持 DeepSeek 的根本原因 —— 每一层都为 DeepSeek 字节稳定的前缀缓存机制调过。

<br/>

## 安装

```bash
cd my-project
npx reasonix code   # 首次运行粘贴 DeepSeek API Key，之后会记住
```

要求 Node ≥ 22。在 macOS · Linux · Windows（PowerShell · Git Bash · Windows Terminal）都跑得顺。[去拿 DeepSeek API Key →](https://platform.deepseek.com/api_keys) · 完整 flag 看 `reasonix code --help`。

`npx` 是推荐路径 —— 不用全局安装，每次都拿最新版。如果你天天用、想把 `reasonix` 装到 `PATH`，跑一次 `reasonix update`。

| 命令 | 何时用 |
|---|---|
| `reasonix code [dir]` | 编码 agent。**先用这个。** |
| `reasonix chat` | 纯聊天 —— 不挂文件系统 / shell 工具。 |
| `reasonix run "task"` | 一次性，结果流到 stdout。适合 shell 管道。 |
| `reasonix doctor` | 体检：Node 版本、API Key、MCP 接线。 |
| `reasonix update` | 升级 Reasonix 本身。 |

其他子命令（`replay` · `diff` · `events` · `stats` · `index` · `mcp` · `prune-sessions`）在 `reasonix --help` 和 [CLI 参考](https://esengine.github.io/DeepSeek-Reasonix/#cli)。

<details>
<summary><strong>切换工作区 · chat vs. code · 写第一个 Skill</strong></summary>

**切换工作区。** Reasonix 把文件系统工具作用域绑定在启动目录，传 `--dir` 可以指别处。中途切换是有意不支持的（消息日志和 memory 路径会和旧根目录混在一起）—— 退出再启动。

```bash
npx reasonix code --dir /path/to/project
```

**`chat` 还是 `code`？** `code` 是默认入口、唯一带文件系统 / shell 工具和 SEARCH/REPLACE 审阅的模式。`chat` 是更轻量的纯对话壳——想要一个挂着 MCP 但没有磁盘权限的“思路助手”时用它。

| 你拿到什么 | `code` | `chat` |
|---|---|---|
| 文件系统工具 + `edit_file` | ✓ | — |
| SEARCH/REPLACE → `/apply` 审阅 | ✓ | — |
| Shell 工具（带 gate） | ✓ | — |
| Plan 模式 · `/todo` · `/skill new` · `/mcp add` | ✓ | — |
| Memory（`remember` / `recall_memory`） | 项目 + 全局 | 仅全局 |
| 配置里的 MCP · web 搜索 · `ask_choice` | ✓ | ✓ |
| 编码导向系统提示词 | ✓ | 通用 |
| Session 作用域 | 按目录 | 共享默认 |

**写第一个 Skill。** 暂无在线市场——自己写。编辑文件（`description:` frontmatter + 正文），然后 `/skill list` 就能看到。frontmatter 加 `runAs: subagent` 会以隔离 subagent 跑，而不是把正文内联进父 prompt。

```bash
/skill new my-skill              # <project>/.reasonix/skills/my-skill.md
/skill new my-skill --global     # ~/.reasonix/skills，跨项目共用
```

</details>

<br/>

## 配置

一个全局 JSON 文件 `~/.reasonix/config.json`，加上项目级 `<project>/.reasonix/` 下的覆盖。完整的双语参考 —— 每一个 key、每一条斜杠命令、skills / memory / hooks 在磁盘上的形状 —— 都在这里：

> 📘 **[配置指南](https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh)** · [English](https://esengine.github.io/DeepSeek-Reasonix/configuration.html)

| 主题 | 速读 |
|---|---|
| [MCP 服务器](https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh#mcp) | stdio · SSE · Streamable HTTP。`config.json` 和 `--mcp` 共用同一种 spec 格式。 |
| [Skills](https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh#skills) | 模型可以调用的 markdown 剧本。`inline` 或 `subagent` 两种模式。 |
| [Memory](https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh#memory) | 用户私有的知识，钉进前缀。`user` / `feedback` / `project` / `reference` 四类。 |
| [Hooks](https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh#hooks) | 生命周期事件触发的 shell 命令。`PreToolUse`（拦截）· `PostToolUse` · `UserPromptSubmit` · `Stop`。 |
| [权限](https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh#permissions) | 按工作区的 shell 白名单，精确前缀匹配。 |
| [Web 搜索](https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh#search) | 默认 Mojeek；用 `/search-engine` 可切到自托管的 SearXNG 或 Metaso。 |
| [语义索引](https://esengine.github.io/DeepSeek-Reasonix/configuration.html?lang=zh#index) | `reasonix index` —— 本地 Ollama，或任何 OpenAI 兼容的 embedding 接口。 |

<br/>

## Reasonix 的不同之处

整个循环围绕三根支柱组织。每一根解决的都是通用 agent 框架根本看不见的问题 —— 因为它们是为另一种缓存机制设计的。

<sub align="center">

各支柱完整说明 → [Pillar 1 — 缓存优先循环](./docs/ARCHITECTURE.md#pillar-1--cache-first-loop) · [Pillar 2 — 工具调用修复](./docs/ARCHITECTURE.md#pillar-2--tool-call-repair) · [Pillar 3 — 成本控制](./docs/ARCHITECTURE.md#pillar-3--cost-control-v06)

</sub>

<br/>

## 能力一览

<p align="center">
  <img src="docs/assets/feature-grid.zh-CN.svg" alt="Reasonix 能力一览 — cell-diff 渲染器、MCP、计划模式、权限、仪表盘、持久化会话、Hooks/Skills/Memory、语义检索、自动 checkpoint、/effort 旋钮、transcript 重放、事件日志" width="880"/>
</p>

<br/>

## 横向对比

|                            | Reasonix          | Claude Code       | Cursor              | Aider              |
|----------------------------|-------------------|-------------------|---------------------|--------------------|
| 后端                       | DeepSeek          | Anthropic         | OpenAI / Anthropic  | 任意（OpenRouter） |
| 协议                       | **MIT**           | 闭源              | 闭源                | Apache 2           |
| 单任务成本                 | **低**            | 高                | 订阅 + 用量         | 不一               |
| DeepSeek 前缀缓存          | **专门工程化**    | 不适用            | 不适用              | 偶发命中           |
| 内嵌 web 仪表盘            | 支持              | —                 | 不适用 (IDE)        | —                  |
| 持久化的工作区会话         | 支持              | 部分              | 不适用              | —                  |
| 计划模式 · MCP · Hooks     | 支持              | 支持              | 支持                | 部分               |
| 开放社区共建               | 支持              | —                 | —                   | 支持               |

实测缓存命中率、成本、方法论看 [`benchmarks/`](./benchmarks/) —— 这些数会随模型定价变化，所以归在 harness 里，不进 README。

<br/>

## 文档

- [**架构**](./docs/ARCHITECTURE.md) —— 四大支柱、缓存优先循环、思维提取、脚手架
- [**CLI 参考**](./docs/CLI-REFERENCE.md) —— 每个 shell 子命令、每个 slash 命令、每个快捷键
- [**基准测试**](./benchmarks/) —— τ-bench-lite harness、transcript、成本方法论
- [**官方网站**](https://esengine.github.io/DeepSeek-Reasonix/) —— 入门、Dashboard 设计稿、TUI 设计稿
- [**贡献指南**](./CONTRIBUTING.md) —— 注释规则、错误处理、用现成库不手写
- [**行为准则**](./CODE_OF_CONDUCT.md) · [**安全策略**](./SECURITY.md)

<br/>

## 社区

> [!NOTE]
> Reasonix 是开源、社区共建的项目。下面贡献者墙不是装饰 —— 每一个头像都对应一次真实合并的 PR。

给新手准备的入门 issue —— 每个都带背景说明、代码定位、验收标准、提示 —— 全部挂在 [`good first issue`](https://github.com/esengine/reasonix/labels/good%20first%20issue) 标签下。挑任意一个还没人认领的就行。

**正在征集意见的 Discussions：**

- [#20 · CLI / TUI 设计](https://github.com/esengine/reasonix/discussions/20) —— 哪里坏了、哪里少东西、哪里你会怎么改？
- [#21 · Dashboard 设计](https://github.com/esengine/reasonix/discussions/21) —— 对着[设计稿](https://esengine.github.io/DeepSeek-Reasonix/design/agent-dashboard.html)拍砖
- [#22 · 未来功能愿望单](https://github.com/esengine/reasonix/discussions/22) —— 你希望 Reasonix 长出什么功能？

**正在使用 Reasonix，愿意让更多人了解它？** 欢迎将相关博客、文章、截图、演讲或视频发布到 [**Show and tell**](https://github.com/esengine/reasonix/discussions/categories/show-and-tell)。项目没有营销预算，新用户主要通过社区口碑找到这里。持续参与传播的用户将获得下方这枚徽章，颁发后会展示在贡献者墙旁：

<p align="center">
  <a href="https://github.com/esengine/reasonix/discussions/categories/show-and-tell">
    <img src="https://img.shields.io/badge/REASONIX-📣%20ADVOCATE-c4b5fd?style=for-the-badge&labelColor=0d1117" alt="Reasonix Advocate 徽章 —— 授予持续参与传播的用户"/>
  </a>
</p>

**第一次提 PR 之前**：先读 [`CONTRIBUTING.md`](./CONTRIBUTING.md) —— 短小、严格的项目规则（注释、错误处理、用现成库不手写）。`tests/comment-policy.test.ts` 静态强制执行注释那部分，`npm run verify` 是 push 前的闸。参与本项目即同意 [行为准则](./CODE_OF_CONDUCT.md)。安全相关问题请走 [SECURITY.md](./SECURITY.md)。

<p align="center">
  <a href="https://github.com/esengine/reasonix/graphs/contributors">
    <img src="https://contrib.rocks/image?repo=esengine/DeepSeek-Reasonix&max=100&columns=12" alt="esengine/DeepSeek-Reasonix 贡献者" width="860"/>
  </a>
</p>

<br/>

## 不做的事

> [!IMPORTANT]
> Reasonix 是有立场的。有些事它故意 *不做* —— 列在这里方便你为自己的工作挑对工具。

- **多供应商灵活性。** 故意只做 DeepSeek。绑死一个后端是 feature，不是限制。
- **IDE 集成。** 终端优先。diff 在 `git diff`，文件树在 `ls`。仪表盘是 TUI 的伴生，不是 Cursor 的替代。
- **追最难的 reasoning 榜单。** Claude Opus 在某些榜单上还是赢家。DeepSeek 在编程任务上有竞争力；如果你的工作是"解一个 PhD 级证明"而不是"修个 auth bug"，先用 Claude。
- **完全离线 / 永远免费。** Reasonix 需要付费的 DeepSeek API Key。要离线 / 零成本，看 Aider + Ollama 或 [Continue](https://continue.dev)。

<br/>

## Star 趋势

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
  <sub>MIT —— 见 <a href="./LICENSE">LICENSE</a></sub>
  <br/>
  <sub>由 <a href="https://github.com/esengine/reasonix/graphs/contributors">esengine/reasonix</a> 社区共建</sub>
</p>
