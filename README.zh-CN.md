<p align="center">
  <img src="docs/logo.svg" alt="Reasonix" width="640"/>
</p>

<p align="center">
  <a href="./README.md">English</a>
  &nbsp;·&nbsp;
  <strong>简体中文</strong>
  &nbsp;·&nbsp;
  <a href="./docs/GUIDE.zh-CN.md">指南</a>
  &nbsp;·&nbsp;
  <a href="./docs/SPEC.md">规格</a>
  &nbsp;·&nbsp;
  <a href="https://esengine.github.io/DeepSeek-Reasonix/">官方网站</a>
  &nbsp;·&nbsp;
  <strong><a href="https://discord.gg/XF78rEME2D">Discord</a></strong>
</p>

> [!IMPORTANT]
> **Reasonix 1.0 是用 Go 从零重写的版本** —— 本分支(`main-v2`)已是新的默认分支,后续开发都在这里。
> 早期的 `0.x` TypeScript 版本转为 **legacy**,保留在 [`v1`](https://github.com/esengine/DeepSeek-Reasonix/tree/v1) 分支(仅维护)。
> 详见**[迁移指南](./docs/MIGRATING.md)**。`npm i -g reasonix` 仍是安装命令——`1.0.0`+ 装的是 Go 二进制,`0.x` 是 legacy TS 版。

<p align="center">
  <a href="https://www.npmjs.com/package/reasonix"><img src="https://img.shields.io/npm/v/reasonix.svg?style=flat-square&color=cb3837&labelColor=161b22&logo=npm&logoColor=white" alt="npm version"/></a>
  <a href="https://github.com/esengine/DeepSeek-Reasonix/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/esengine/DeepSeek-Reasonix/ci.yml?style=flat-square&label=ci&labelColor=161b22&logo=githubactions&logoColor=white" alt="CI"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/npm/l/reasonix.svg?style=flat-square&color=8b949e&labelColor=161b22" alt="license"/></a>
  <a href="https://www.npmjs.com/package/reasonix"><img src="https://img.shields.io/npm/dm/reasonix.svg?style=flat-square&color=3fb950&labelColor=161b22&label=downloads" alt="downloads"/></a>
  <a href="https://github.com/esengine/DeepSeek-Reasonix/stargazers"><img src="https://img.shields.io/github/stars/esengine/DeepSeek-Reasonix.svg?style=flat-square&color=dbab09&labelColor=161b22&logo=github&logoColor=white" alt="GitHub stars"/></a>
  <a href="https://atomgit.com/esengine/DeepSeek-Reasonix"><img src="https://atomgit.com/esengine/DeepSeek-Reasonix/star/badge.svg" alt="AtomGit stars"/></a>
  <a href="https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors"><img src="https://img.shields.io/github/contributors/esengine/DeepSeek-Reasonix.svg?style=flat-square&color=bc8cff&labelColor=161b22&logo=github&logoColor=white" alt="contributors"/></a>
  <a href="https://github.com/esengine/DeepSeek-Reasonix/discussions"><img src="https://img.shields.io/github/discussions/esengine/DeepSeek-Reasonix.svg?style=flat-square&color=58a6ff&labelColor=161b22&logo=github&logoColor=white" alt="Discussions"/></a>
  <a href="https://discord.gg/XF78rEME2D"><img src="https://img.shields.io/badge/discord-join-5865F2.svg?style=flat-square&labelColor=161b22&logo=discord&logoColor=white" alt="Discord"/></a>
</p>

<br/>

<h3 align="center">面向终端的 DeepSeek 原生 AI coding agent。</h3>
<p align="center">由配置与插件驱动的极薄 harness——单一静态 Go 二进制，围绕 DeepSeek 的前缀缓存调优，长会话也能把 token 成本压低。</p>

<br/>

> [!IMPORTANT]
> **加入社区 · Community** — 双语 Discord，提供安装答疑（`#help` / `#求助`）、工作流展示与功能想法。→ **<https://discord.gg/XF78rEME2D>**

## 特性

- **配置驱动**：provider、agent、启用的工具、插件全部在 `reasonix.toml` 中声明，
  内核无硬编码模型。
- **多模型 · 可组合**：DeepSeek 作为预设内置；任何 OpenAI 兼容
  端点都只是一条配置。可选让两个模型协同（执行器 + 规划器），各自独立、缓存稳定的 session。
- **插件驱动**：外部工具以子进程形式运行，通过 stdio JSON-RPC 通信（MCP 兼容）；
  内置工具在编译期自注册。
- **缓存友好的上下文维护**：启动时注入稳定的环境摘要；旧工具输出会先 snip/prune，
  再进入摘要 compaction；内置工具 schema 合约有文档和回归测试保护。
- **零摩擦分发**：`CGO_ENABLED=0` 单二进制；一条命令交叉编译到六个目标平台。
  唯一依赖是一个 TOML 解析库。

## 安装

```sh
npm i -g reasonix                  # 任意系统;自动拉取对应平台的原生二进制
brew install esengine/reasonix/reasonix   # macOS
```

预编译归档(`darwin|linux|windows × amd64|arm64`)和 `SHA256SUMS` 见每个
[GitHub release](https://github.com/esengine/DeepSeek-Reasonix/releases)。

### 代码签名

Windows 构建使用 [SignPath 基金会](https://signpath.org/) 提供的免费代码签名证书,
通过 [SignPath.io](https://signpath.io/) 完成签名。

### 从源码构建

```sh
make build      # -> bin/reasonix(.exe)
make cross      # -> dist/（darwin|linux|windows × amd64|arm64）
```

## 快速开始

```sh
reasonix setup                      # 配置向导 → ./reasonix.toml
export DEEPSEEK_API_KEY=sk-...      # 也可以让 setup 保存到 Reasonix 全局 .env
reasonix                            # 然后在会话里运行 /init 生成 AGENTS.md（项目记忆）
reasonix run "把 main.go 里的 TODO 实现掉"
reasonix run --model deepseek-pro "给这个函数补单元测试"
echo "解释这段代码" | reasonix run
```

## 配置

一个最小的 `reasonix.toml`——一个 provider 加一个默认模型——就够跑起来:

```toml
default_model = "deepseek-flash"

[[providers]]
name        = "deepseek-flash"
kind        = "openai"
base_url    = "https://api.deepseek.com"
model       = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
```

优先级为 **flag > `./reasonix.toml` > 用户配置文件 > 内置默认值**；从
**Reasonix v1.8.1** 开始，用户配置位于 macOS/Linux 的 `~/.reasonix/config.toml`，
Windows 为 `%AppData%\reasonix\config.toml`。迁移细节见
**[配置路径](./docs/CONFIG_PATHS.zh-CN.md)**，其中也说明了全局 `config.toml`
和 `.env` 的完整结构。Provider 通过 `api_key_env` 命名密钥，真实密钥值保存在
CLI 与桌面端共用的 Reasonix 全局 `<Reasonix home>/.env`；项目 `.env` 不再作为
provider key 的运行时 fallback，但仍会作为当前 workspace 范围内的 MCP/plugin 非 provider `${VAR}` 展开来源，不导入 Reasonix 控制变量。权限、沙盒、插件(MCP)、
斜杠命令、`@` 引用与双模型设置,全部在 **[指南](./docs/GUIDE.zh-CN.md)** 里。

## 文档

- **[指南](./docs/GUIDE.zh-CN.md)** —— 配置、权限与沙盒、插件(MCP)、斜杠命令、
  `@` 引用、双模型协同。
- **[机器人使用指南](./docs/BOT_GUIDE.zh-CN.md)** —— 桌面端连接飞书、Lark、微信
  Bot，以及 IM 里的审批、YOLO 和命令交互。
- **[规格](./docs/SPEC.md)** —— 工程契约:架构、registry、数据类型与路线图。
- **[任务合约与暂停策略](./docs/TASK_CONTRACT.zh-CN.md)** —— 用背景、输出边界、约束和暂停条件组织复杂请求。
- **[工具合约](./docs/TOOL_CONTRACT.zh-CN.md)** —— provider 可见的内置工具名、
  read-only 标记和 schema 快照保护。
- **[从 0.x 迁移](./docs/MIGRATING.md)** —— 从 legacy TypeScript 版本迁到 1.0 Go 重写版。
- **[Checkpoints 与 rewind](./docs/CHECKPOINTS.md)** —— 基于快照的编辑安全网
  (Esc-Esc / `/rewind`)。

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

## 支持本项目

如果 Reasonix 帮你省了时间或 token，欢迎请杯咖啡。捐助不会换来 feature 优先级，也不会影响 issue 的处理顺序——就是「谢谢」。

- **国内** — 微信支付（扫下方二维码）
- **海外** — PayPal: [paypal.me/yuhuahui](https://paypal.me/yuhuahui)

<p align="center">
  <img src=".github/sponsor/wechat-pay.jpg" alt="微信支付收款码" width="240"/>
</p>

<br/>

## 致谢

下面这些朋友的工作塑造了 Reasonix 今天的样子 —— 综合 commit 数和代码量两个维度。
**按字母顺序排列，排名不分先后。** 完整贡献者列表在
[GitHub](https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors)。

- [**ctharvey**](https://github.com/ctharvey)
- [**dimasd-angga**](https://github.com/dimasd-angga)（Dimas D. Angga）
- [**Evan-Pycraft**](https://github.com/Evan-Pycraft)
- [**ForeverYoungPp**](https://github.com/ForeverYoungPp)
- [**GTC2080**](https://github.com/GTC2080)（TaoMu）
- [**kabaka9527**](https://github.com/kabaka9527)
- [**lisniuse**](https://github.com/lisniuse)（Richie）
- [**wade19990814-hue**](https://github.com/wade19990814-hue)
- [**wviana**](https://github.com/wviana)（Wesley Viana）

另外特别感谢 [**Bernardxu123**](https://github.com/Bernardxu123) 设计的项目 logo，
以及 [AIGC Link](https://xhslink.com/m/80ngts127cA) 在小红书上的推广。

<p align="center">
  <a href="https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors">
    <img src="https://contrib.rocks/image?repo=esengine/DeepSeek-Reasonix&max=100&columns=12" alt="esengine/DeepSeek-Reasonix 贡献者" width="860"/>
  </a>
</p>

<br/>

---

<p align="center">
  <sub>MIT —— 见 <a href="./LICENSE">LICENSE</a></sub>
  <br/>
  <sub>由 <a href="https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors">esengine/DeepSeek-Reasonix</a> 社区共建</sub>
</p>
