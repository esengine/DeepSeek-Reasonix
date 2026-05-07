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
  <a href="./docs/ARCHITECTURE.md">架构文档</a>
  &nbsp;·&nbsp;
  <a href="./benchmarks/">基准测试</a>
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

要求 Node ≥ 22。已在 macOS · Linux · Windows（PowerShell · Git Bash · Windows Terminal）测过。[去拿 DeepSeek API Key →](https://platform.deepseek.com/api_keys) · 完整 flag 看 `reasonix code --help`。

**在其他目录工作：** Reasonix 把文件系统工具作用域绑定在启动目录。要在别的目录工作，传 `--dir`：

```bash
npx reasonix code --dir /path/to/project   # 也可以用相对路径
```

中途切换工作区是有意不支持的（消息日志和 memory 路径会和旧的根目录混在一起，状态错乱）。退出后用新的 `--dir` 重新启动来切换。`/status` 始终显示当前锁定的工作区。

<br/>

## Reasonix 的不同之处

整个循环围绕四根支柱组织。每一根解决的都是通用 agent 框架根本看不见的问题 —— 因为它们是为另一种缓存机制设计的。

<p align="center">
  <a href="./docs/ARCHITECTURE.md"><img src="docs/assets/pillars.zh-CN.svg" alt="Reasonix 四大支柱 — 缓存优先循环、R1 思维提取、工具调用修复、成本控制" width="880"/></a>
</p>

<sub align="center">

各支柱完整说明 → [Pillar 1](./docs/ARCHITECTURE.md#pillar-1--cache-first-loop) · [Pillar 2](./docs/ARCHITECTURE.md#pillar-2--r1-thought-harvesting-opt-in) · [Pillar 3](./docs/ARCHITECTURE.md#pillar-3--tool-call-repair) · [Pillar 4](./docs/ARCHITECTURE.md#pillar-4--cost-control-v06)

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
    <img src="https://contrib.rocks/image?repo=esengine/reasonix&max=100&columns=12" alt="esengine/reasonix 贡献者" width="860"/>
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
  <sub>MIT —— 见 <a href="./LICENSE">LICENSE</a></sub>
  <br/>
  <sub>由 <a href="https://github.com/esengine/reasonix/graphs/contributors">esengine/reasonix</a> 社区共建</sub>
</p>
