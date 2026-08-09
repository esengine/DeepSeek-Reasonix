# AGENTS.md

本项目面向 AI 编码代理的项目指令文件。加载优先级高于构建/Git 等通用约定，低于用户当前明确指令。

## 自动推送与 PR 规则（必须遵守）

开发完成后，**构建与验证（测试等）全部通过**，无需等待额外用户确认，应**主动**将改动推送到 GitHub 并创建或更新 Pull Request（PR）。

- **触发时机**：一个功能分支的开发完成、`go build`/前端构建/测试等本地验证全部通过后，自动进入推送流程。
- **推送目标**：当前所在 feature 分支（如 `feat/*`）推送至 `origin`；若改动直接发生在 `main-v2`，则推送到 `origin/main-v2`。
- **PR 目标**：PR 统一指向 fork 仓库的默认分支 **`main-v2`**（除非用户明确指定其他 base）。
- **PR 内容**：包含变更摘要、验证命令与结果、可能的 Cache-impact/System-prompt-review 标注（如涉及缓存敏感路径）。
- **已存在 PR**：若同名 feature 分支已有 PR，则推送新提交后**更新**该 PR（amend 或追加 commit 皆可，保持 PR 单一）。

### 触发方式（AI/用户均可）

| 方式 | 命令 |
| --- | --- |
| 自动（构建验证通过） | AI 完成验证后直接执行下方推送+PR 命令 |
| 手动（用户触发） | 用户说"推送/建 PR/同步"或提及 P2/T2 规则即触发 |
| 斜杠命令 | 在 REASONIX 内运行 `/agents` 或提及"PR 规则" |

### 推送 + 创建/更新 PR 命令

```bash
# 1) 提交（若有未提交改动）
git add -A
git commit -m "feat: <描述>"    # 参照仓库提交风格

# 2) 推送当前分支
git push -u origin HEAD

# 3) 创建/更新 PR（需 gh CLI 或等效工具；没有则用 web 端）
gh pr create --base main-v2 --head <当前分支> --title "<PR 标题>" --body "<摘要+验证结果>"
# 若 PR 已存在：
gh pr view <当前分支> --json url   # 查已有 PR
```

## 本地与共享配置分层

| 文件 | 是否提交到 GitHub | 用途 |
| --- | --- | --- |
| `AGENTS.md`（本文件） | ✅ 提交 | 共享给所有协作者/AI 的项目规则 |
| `AGENTS.local.md` | ❌ 仅本地（被 `.gitignore` 过滤） | 个人本地 AI 指令，不随仓库分发 |
| `REASONIX.md` | ✅ 已由仓库维护 | Reasonix 引擎专用指令 |

## 文档引用约定

项目正式文档（`docs/`）如需引用本文件，统一用相对链接（见 `docs/AGENTS_REFERENCE.md` 的约定与示例）。

## 验证

- 推送前运行：`gofmt -w . && go vet ./... && go test ./internal/tool/builtin/ ./internal/boot/`（仓库既有 CI 前置规则）。
- PR 创建后确认：`gh pr view --json url` 或 GitHub 网页可见。

## 发布与签名（v1.21.5 起）

桌面端 release 发布流程与本仓库签名约定：

- **版本号**：语义化版本，tag 命名 `desktop-v<semver>`（如 `desktop-v1.21.5`）。当前基线版本 v1.21.5。
- **更新源可注入**：`desktop/updater.go` 的 `githubManifestFallback` 是 `var`（非 const），构建时用
  `-ldflags "-X main.githubManifestFallback=https://github.com/<owner>/<repo>/releases/latest/download/latest.json"` 覆盖为自己的仓库。
- **签名密钥**（minisign）：
  - 私钥：`desktop/build/signkey/reasonix.key`（加密，密码 `reasonix-release-2026`，**务必备份，丢失无法再签名**）
  - 公钥：`RWTh+5VH/HnH1Ieqfn2AH4rY0N87a8ae8QD3JfS038PfN4pHsWFnQ+ru`（key ID `D4C779FC4795FBE1`）
  - 公钥已嵌入 `desktop/internal/update/verify.go` 的 `publicKey` 常量——更换密钥时必须同步更新此处与 CI secret。
- **签名命令**：`MINISIGN_PASSWORD=... MINISIGN_PRIVATE_KEY="$(cat build/signkey/reasonix.key)" go run ./cmd/sign sign <exe>`
- **NSIS 安装器**：需先安装 NSIS（makensis，位于 `C:\Program Files (x86)\NSIS`），
  并构建辅助二进制到 `build/windows/installer/`（reasonix-guard/launcher/update-helper/cli，见 `scripts/desktop-build.sh`）。
  `project.nsi` 含中文，makensis 需 UTF-8 BOM 或 `-DARG_WAILS_AMD64_BINARY=...` 参数。
- **manifest**：`GITHUB_REPOSITORY=<owner>/<repo> go run ./cmd/sign manifest dist <version> <tag>` 生成 `latest.json`，
  产物命名须匹配平台 key（如 `Reasonix-windows-amd64-installer.exe`）。
- **发布**：推 tag `desktop-v<semver>` + `gh release create` 上传 exe/安装器/`.minisig`/`latest.json`。

## 语音输入（STT）关键约定

- **首次启用授权**：Edge 以 `--use-fake-ui-for-media-stream` 启动自动接受麦克风授权（仍用真实设备）。
  权限 prompt 时页面上报 `state:"need-permission"`，Go 端显示识别页窗口让用户点允许，授权后自动隐藏。
- **启动确认**：`StartListening` 发 `cmd:start` 后不提前置 `listening=true`（否则 handleWS 的 `changed`
  判断会跳过 emit，前端按钮一直转圈）。页面回传状态（listening/error/idle/need-permission）时无条件
  emit `{listening, starting:false}`；确认窗口 8s，超时自动重试最多 3 轮。
- **静默超时默认值**：未配置（0）时使用默认 6s（与设置 UI fallback 一致），不要钳到 3s；显式配置 1-2s 钳到 3s。
- **热键注销**：`sttHotkeyManager.stop()` 后 Go 复用 OS 线程不终止，热键仍被旧线程持有；
  WM_QUIT 分支必须显式 `UnregisterHotKey`，否则重新注册返回"组合键被占用"。

## 思考级别（effort）约定

- opencode.ai 网关（`openai.IsOpencode`）走 binary thinking knob（auto/enabled/disabled），
  `EffortCapabilityForEntry`/`NormalizeEffort` 已支持，big-pickle 等模型可显示思考模式下拉框。
- 窄容器 CSS 断点（`@container` 760px/560px）已移除对 `.composer-meta__control--effort` 的 `display:none`，
  思考模式下拉框始终显示。