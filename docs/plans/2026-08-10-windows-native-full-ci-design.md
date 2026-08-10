# Windows 原生隔离全量 CI 设计

## 背景与目标

宿主机 360 HIPS 对 `go test` 生成的临时 `ablation.test.exe` 触发启发式告警。后续扫描未发现威胁，但在安全厂商完成复核前，不应通过关闭杀毒、扩大白名单或继续在宿主机生成临时测试二进制来完成验收。

仓库现有 CI 已包含 Windows job，但 PR 场景的 Root module 只运行 smoke 包集合，SDK 可能因路径过滤跳过；只有 Desktop Windows job 执行完整 `go test ./...`。因此需要一个独立的、不会改变既有 required checks 的 Windows 原生全量工作流。

## 选择方案

新增 `.github/workflows/windows-native-full.yml`，在面向 `main-v2` 的 pull request、fork 的 `feature/**` 分支推送和手动触发时运行单一 `windows-latest` job。fork 推送入口用于工作流尚未进入上游默认分支时的首次隔离验证；它不使用 WSL、Docker 或 Linux 容器，权限限定为 `contents: read`。

工作流先通过 `actions/setup-go` 使用根 `go.mod` 的 Go 1.26.5 工具链，并显式设置 `GOTOOLCHAIN=local`，禁止运行时自动下载其他工具链。它验证当前 `bash` 来自 Git for Windows，而不是 `System32` 的 WSL 启动器。真实供应商缓存守卫保持关闭，测试不会读取 `apikey.txt` 或调用 DeepSeek/MinerU。

## 执行流程

1. Root：`go test -p 4 -count=1 -timeout=8m ./...`；
2. SDK：在 `sdk/go` 执行 `go test -count=1 -timeout=8m ./...`；
3. Desktop 前置：安装固定版本 Wails CLI，安装锁定的前端依赖并生成 production frontend；
4. Desktop：在 `desktop` 执行 `go test -p 1 -count=1 -timeout=12m ./...`。

三个测试步骤使用 `continue-on-error` 与 `if: always()`，保证 Root 失败时 SDK 和 Desktop 仍会运行并留下独立日志。最终 gate 汇总各 step outcome，任何 module 或 Desktop 前置失败都会让 job 失败。日志无论成功失败都由 `actions/upload-artifact` 上传。

## 安全边界

- Workflow token 只有仓库内容只读权限；不使用任何 repository secret。
- 不修改 runner 防火墙，不创建杀毒白名单，不上传宿主机告警样本。
- 依赖安装沿用仓库已使用的 GitHub Actions 与锁文件；Wails CLI 固定为 `v2.12.0`。
- 测试日志不得输出 API Key；真实 provider guard 显式为空。
- 发布到 GitHub 属于外部写入，只有在确认用户拥有可写远端后执行。

## 验收标准

- Workflow 语法可由本地 YAML 解析器读取。
- `windows-latest` 上 Root、SDK、Desktop 均实际执行禁缓存全量测试。
- Git Bash 路径预检通过且未调用 WSL。
- 任一模块失败不会阻止其余模块执行，但最终 job 正确失败。
- 日志 artifact 包含 `root.log`、`sdk-go.log`、`desktop-prep.log` 和 `desktop.log`（对应步骤执行时）。
