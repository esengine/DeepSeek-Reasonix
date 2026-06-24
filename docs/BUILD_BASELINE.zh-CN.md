# Windows 编译基线记录

本文记录在 Windows / PowerShell 环境下跑通本项目编译与测试的过程，便于后续改造为“桥通智能助手”时先确认基线干净。

## 环境

- 工作目录：`C:\codes-internal\DeepSeek-QiaotongAgent`
- Go：`go1.26.4 windows/amd64`
- Node：`v24.10.0`
- npm：`11.6.1`
- pnpm：`10.30.3`
- Wails CLI：`v2.12.0`

`go.mod` 要求 `toolchain go1.26.4`，桌面端 `desktop/go.mod` 使用 `github.com/wailsapp/wails/v2 v2.12.0`，因此 Wails CLI 建议安装同版本：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
```

安装后如果当前 shell 的 `PATH` 没刷新，可以用绝对路径调用：

```powershell
& "$((go env GOPATH).Trim())\bin\wails.exe" version
```

## 根目录 Go 编译

Windows 下不要直接依赖 `Makefile` 里的 Unix 风格环境变量写法，可以用等价 PowerShell 命令：

```powershell
$version = (git describe --tags --always 2>$null)
if (-not $version) { $version = 'dev' }
New-Item -ItemType Directory -Force -Path bin | Out-Null
go build -ldflags "-s -w -X main.version=$version" -o bin\qiaotongagent.exe .\cmd\qiaotongagent
go build -ldflags "-s -w -X main.version=$version" -o bin\qiaotongagent-plugin-example.exe .\cmd\qiaotongagent-plugin-example
```

成功产物：

- `bin\qiaotongagent.exe`
- `bin\qiaotongagent-plugin-example.exe`

## 桌面前端编译

桌面前端依赖 Wails 生成的绑定代码。如果直接运行 `npm run build` 或 `pnpm build`，但 `desktop/frontend/wailsjs` 不存在，会看到类似错误：

```text
Cannot find module '../../wailsjs/go/main/App'
Cannot find module '../../wailsjs/runtime/runtime'
```

先生成绑定：

```powershell
cd C:\codes-internal\DeepSeek-QiaotongAgent\desktop
& "$((go env GOPATH).Trim())\bin\wails.exe" generate module
```

再编译前端：

```powershell
cd C:\codes-internal\DeepSeek-QiaotongAgent\desktop\frontend
pnpm install
pnpm build
```

本次验证中前端编译通过，Vite 只给出 chunk 体积和动态导入分块警告，不影响构建结果。

## Wails 桌面端编译

桌面端完整构建命令：

```powershell
cd C:\codes-internal\DeepSeek-QiaotongAgent\desktop
& "$((go env GOPATH).Trim())\bin\wails.exe" build
```

成功产物：

- `desktop\build\bin\qiaotongagent-desktop.exe`

## 站点构建

```powershell
cd C:\codes-internal\DeepSeek-QiaotongAgent\site
npm ci
npm run build
```

注意：`site` 的 `prebuild` 会运行 `scripts/fetch-community.mjs` 联网刷新 `site/src/data/community.json`，因此构建后该文件可能出现数据差异。这是构建脚本行为，不是手工业务改动。

## Worker Typecheck

```powershell
cd C:\codes-internal\DeepSeek-QiaotongAgent\workers\crash-report
npm ci
npm run typecheck
```

本次验证通过。

## 测试基线

根模块：

```powershell
cd C:\codes-internal\DeepSeek-QiaotongAgent
go test ./...
```

桌面嵌套模块：

```powershell
cd C:\codes-internal\DeepSeek-QiaotongAgent\desktop
go test ./...
```

本次为了让 Windows 测试基线通过，修正了几个测试环境差异：

- `internal/cli/statusline_test.go`：Windows 下用 `more` 替代 `cat` 读取 stdin。
- `internal/installsource/install_source_test.go`：symlink 测试在 Windows 下提前 skip，避免未授权创建符号链接。
- `desktop/app_test.go`：隔离 `XDG_CACHE_HOME` 和 `LOCALAPPDATA`，避免内置 MCP 更新 stamp 读到真实用户缓存。
- `desktop/app_test.go`：相关 MCP 测试使用 `robustTempDir`，降低 Windows 临时目录被后台进程短暂占用导致的清理失败。
- `desktop/skills_app_test.go`：模拟 home 时补齐 `USERPROFILE`，保证 `~` 路径解析在 Windows 下进入测试隔离目录。

## 本次通过的命令汇总

```powershell
# 根目录 Go 编译
$version = (git describe --tags --always 2>$null)
if (-not $version) { $version = 'dev' }
New-Item -ItemType Directory -Force -Path bin | Out-Null
go build -ldflags "-s -w -X main.version=$version" -o bin\qiaotongagent.exe .\cmd\qiaotongagent
go build -ldflags "-s -w -X main.version=$version" -o bin\qiaotongagent-plugin-example.exe .\cmd\qiaotongagent-plugin-example

# 桌面前端与 Wails 桌面程序
cd C:\codes-internal\DeepSeek-QiaotongAgent\desktop
& "$((go env GOPATH).Trim())\bin\wails.exe" generate module
& "$((go env GOPATH).Trim())\bin\wails.exe" build

# 根模块测试
cd C:\codes-internal\DeepSeek-QiaotongAgent
go test ./...

# 桌面模块测试
cd C:\codes-internal\DeepSeek-QiaotongAgent\desktop
go test ./...

# 站点
cd C:\codes-internal\DeepSeek-QiaotongAgent\site
npm ci
npm run build

# Worker
cd C:\codes-internal\DeepSeek-QiaotongAgent\workers\crash-report
npm ci
npm run typecheck
```
