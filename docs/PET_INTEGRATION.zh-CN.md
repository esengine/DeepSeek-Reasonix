# Reasonix 原生桌宠集成方案

> 将 Agent Critter（桌宠）的能力原生内置到 Reasonix 桌面端，无外部 Rust 进程、无 TCP 端口。状态机为 Go 内核 `internal/pet`，渲染层为 Wails v2 多窗 + React 宠物组件。

## 修订记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v1 | 初始 | 原始方案（hook 转发送 TCP 7890） |
| v2 | 当前 | 重写渲染层，无外部进程，修复 8 个设计缺陷 |

## 目录

- [1. 架构总览](#1-架构总览)
- [2. 集成接缝（核心）](#2-集成接缝核心)
- [3. 状态契约与映射](#3-状态契约与映射)
- [4. 宠物容器（Internal Pet）](#4-宠物容器-internal-pet)
- [5. 宠物目录与精灵资源](#5-宠物目录与精灵资源)
- [6. 宠物窗承载（Wails v2 + 原生 WebView 备选）](#6-宠物窗承载wails-v2--原生-webview-备选)
- [7. 前端渲染（React PetWindow）](#7-前端渲染react-petwindow)
- [8. 事件流（完整闭环）](#8-事件流完整闭环)
- [9. 文件改动清单](#9-文件改动清单)
- [10. 生命周期](#10-生命周期)
- [11. 已知风险与回退](#11-已知风险与回退)
- [12. 分阶段实施](#12-分阶段实施)
- [13. 测试策略](#13-测试策略)

---

## 1. 架构总览

```
Desktop 层 (Wails v2.12)
┌──────────────────────────────────────────────────────────┐
│ App                                                        │
│  ├─ 主窗口 (现有 React App)                                │
│  ├─ 宠物窗 (第二窗口 ── Wails runtime.NewWindow)          │
│  │      └─ React PetWindow (精灵渲染 + 交互)              │
│  └─ pet.Manager (桌面层单例)                               │
│        │ 持有                                              │
│        ▼                                                   │
│  internal/pet 纯库 (状态机 + 目录扫描)                     │
│                                                             │
│  接缝: hook.Runner.OnHook 回调 (新增)                       │
│        @ internal/hook/runner.go                            │
│        └─ 桌面端设 runner.OnHook = mgr.OnEvent              │
│           └─ Manager 在桌面层 EventEmit → 宠物窗            │
└──────────────────────────────────────────────────────────┘
         ▲
         │ 每次 hook fire 后调 OnHook
         │
  internal/hook.Runner (kernel)
       PreToolUse / PostToolUseFailure /
       PermissionRequest / Stop / StopFailure /
       SessionStart / SessionEnd / Notification / …
```

### 设计原则

1. **无外部进程**：宠物渲染由 Reasonix 桌面端自身承载，不 spawn critter 二进制。
2. **无 TCP**：宠物状态直连内核 hook 事件，经 `runtime.EventsEmit` 推送到宠物窗。
3. **纯库 + 零反向依赖**：`internal/pet` 只依赖 stdlib + `internal/config`（HomeDir），不依赖 `internal/hook`、`internal/agent` 或 `desktop`。
4. **兼容已有生态**：宠物目录对齐 Codex 格式（`pet.json` + `spritesheet.webp`），只读兼容 `~/.codex/pets` / `~/.petdex/pets`。

---

## 2. 集成接缝（核心）

### 2.1 ~~原始方案缺陷~~ 原始设计中，`internal/control.Controller` 持有 `*hook.Runner`（`controller.go:385`），fire 的是具体指针类型（10+ 公有方法），无法无侵入包装。

### 2.2 修正

在 `internal/hook.Runner` 新增一个可选回调字段，所有 fire 方法末尾调用。

```go
// internal/hook/runner.go
type Runner struct {
    // OnHook fires after every hook invocation (before handle()). 
    // It lets an in-process observer react to events without 
    // executing a shell command. No dependency on pet.
    OnHook func(event Event, payload Payload)
}
```

每个 fire 方法（`PreToolUse`、`PostToolUseFailure`、`PermissionRequest`、`StopResult`、`SessionStart`、`SessionEnd`、`SubagentStop`、`Notification`、`PromptSubmit`、`PostLLMCall`、`PreCompact`）在 `Run()` 后执行：

```go
if r.OnHook != nil {
    r.OnHook(p.Event, p) // p 是已构造的 Payload
}
```

约 12~16 行改动，零额外依赖。

### 2.3 桌面层绑定

`desktop/app.go` 或 tab 初始化处：

```go
runner := hook.NewRunner(hooks, cwd, spawner, notify)
runner.OnHook = func(event hook.Event, payload hook.Payload) {
    petMgr.Handle(event, payload)
}
opts := control.Options{
    Hooks: runner,
    // ... 其他字段
}
```

### 2.4 `internal/hook` 不感知宠物

`OnHook` 是 `func(Event, Payload)` 类型回调。`internal/hook` 无需 import pet、不用改 import 图。完全解耦。

---

## 3. 状态契约与映射

### 3.1 内部 5 态（移植 critter `LightState`）

```go
type PetState int
const (
    Idle       PetState = iota + 1 // 空闲
    Running                         // 工作中
    ToolError                       // 工具异常
    ErrorFinal                      // 严重错误
    NeedConfirm                     // 等待确认
)
```

### 3.2 hook 事件 → PetState 映射

| Hook 事件 | PetState | 说明 |
|-----------|----------|------|
| `SessionStart` | Idle | 新会话开始，计数器 +1 |
| `UserPromptSubmit` | Running | 用户提交 turn |
| `PreToolUse` | Running | 工具调用前 |
| `PostToolUse` | Running | 工具调用后（保持） |
| `PostToolUseFailure` | ToolError / Idle | `IsInterrupt=true` → Idle，否则 ToolError |
| `PermissionRequest` | NeedConfirm | 权限请求弹窗 |
| `Stop` | Idle | turn 正常结束 |
| `StopFailure` | ErrorFinal / ToolError | 按错误分类（见 §3.3） |
| `SessionEnd` | Idle | 会话结束，计数器 -1 |
| `PreCompact` | Running | compact 触发 |
| `Notification` | Idle / NeedConfirm | `notificationType=permission_prompt` → NeedConfirm |

### 3.3 `StopFailure` 错误分类（新增，修正缺陷 #3）

```go
func classifyStopError(errString string) PetState {
    lower := strings.ToLower(errString)
    switch {
    case strings.Contains(lower, "canceled") || strings.Contains(lower, "interrupt"):
        return Idle      // 用户主动取消
    case strings.Contains(lower, "rate limit") || strings.Contains(lower, "429"):
        return ToolError // 限流，暂时
    case strings.Contains(lower, "deadline") || strings.Contains(lower, "timeout"):
        return ToolError // 超时
    case strings.Contains(lower, "auth") || strings.Contains(lower, "unauthorized"):
        return ErrorFinal // 认证/授权
    case strings.Contains(lower, "billing") || strings.Contains(lower, "payment"):
        return ErrorFinal // 账单
    case strings.Contains(lower, "model not found") || strings.Contains(lower, "not found"):
        return ErrorFinal // 模型不可用
    default:
        return ToolError // 其他工具层错误
    }
}
```

### 3.4 UI 层 Codex 三态映射（可选）

```typescript
// 前端映射，不影响 Go 状态机
const codexMapping: Record<PetState, 'running' | 'waiting' | 'ready'> = {
    [PetState.Running]: 'running',
    [PetState.NeedConfirm]: 'waiting',
    [PetState.ToolError]: 'ready', // 显示错误提示而非阻塞态
    [PetState.ErrorFinal]: 'waiting', // 显示崩溃
    [PetState.Idle]: 'ready',
};
```

---

## 4. 宠物容器（`internal/pet`）

### 4.1 包结构

```
internal/pet/
    state.go      — 状态机（移植 src/state.rs）
    state_test.go — 移植所有单测
    directory.go  — 宠物目录扫描 + pet.json 解析
    directory_test.go
    manager.go    — Manager 封装：Handle / CurrentState / List / SetPet
    manager_test.go
```

### 4.2 状态机 (`state.go`)

直接移植 `src/state.rs`，保持以下行为不变：

- 多会话 map，以 `SessionID`（来自 Payload）为 key
- `handle_event(sessionID, event)` 更新单会话状态
- `update_current_state()` 按优先级合并：`ErrorFinal > ToolError > NeedConfirm > Running > Idle`
- `session_counter` 跟踪活跃会话数
- **去除 `should_exit`**：宠物窗绑定 App 生命周期，不由状态机控制退出。

### 4.3 目录扫描 (`directory.go`)

```go
// ScanPets returns all installed pets from all search paths.
func ScanPets(reasonixHome string) []Pet

type Pet struct {
    Slug         string // 目录名（唯一 key）
    DisplayName  string // pet.json displayName
    Description  string
    SpriteSheet  []byte // 精灵图（通常 webp）
    SourcePath   string // pet.json 路径
}
```

搜索路径顺序：
1. `<reasonixHome>/pets/<slug>/`（主，ReadWrite）
2. `~/.codex/pets/<slug>/`（只读兼容）
3. `~/.petdex/pets/<slug>/`（只读兼容）

每个目录期望：
- `pet.json`：`{"id":"...","displayName":"...","description":"...","spritesheetPath":"..."}`
- `spritesheetPath` 指向的 WebP/PNG 文件（或同目录的同名 `spritesheet.webp`）

### 4.4 Manager (`manager.go`)

```go
type Manager struct {
    sm     *StateMachine
    pets   []Pet
    curPet string // 当前宠物 slug
    dataDir string // ~/.reasonix/pets
}

func NewManager(reasonixHome string) *Manager
func (m *Manager) Handle(event hook.Event, payload hook.Payload)
func (m *Manager) CurrentState() PetState
func (m *Manager) SwitchPet(slug string) error
func (m *Manager) ListPets() []Pet
func (m *Manager) CurrentPet() *Pet
func (m *Manager) Scale() float64
func (m *Manager) SetScale(v float64)
func (m *Manager) Position() (x, y int)
func (m *Manager) SetPosition(x, y int)
func (m *Manager) Save()
```

位置、scale、当前宠物 slug 持久化到 `<reasonixHome>/pets/state.json`。

### 4.5 占位精灵（修正缺陷 #4）

当所有目录为空时，`Manager.CurrentPet()` 返回一个**内嵌占位精灵**——一个 base64 编码的简单图案（🐾 或默认 spritesheet 的一部分）。确保宠物窗不显示空白透明区域。

---

## 5. 宠物目录与精灵资源

### 5.1 目录约定（对齐 Codex）

```
~/.reasonix/pets/                    ← 主目录（读写）
    boba/
        pet.json                     ← {id, displayName, description, spritesheetPath}
        spritesheet.webp             ← 8列×9行精灵表
    default/                         ← 首版内嵌（由安装器创建）
        pet.json
        spritesheet.webp
    state.json                       ← 位置/scale/当前宠物（自动维护）

~/.codex/pets/                       ← 只读兼容（Codex 生态）
~/.petdex/pets/                      ← 只读兼容（Petdex 生态）
```

### 5.2 精灵规范（与 critter 一致）

| 行 | 动作 |
|----|------|
| 0 | idle（待机） |
| 1 | running-right |
| 2 | running-left |
| 3 | waiting（等待确认） |
| 4 | review（检查） |
| 5 | failed（崩溃） |
| 6 | jumping（跳跃） |
| 7 | waving（挥手） |
| 8 | greeting（打招呼） |

每行 8 帧，CSS `background-position` 逐帧切换。critter 的 `assets/default_spritesheet.webp` 可作为默认精灵资源。

---

## 6. 宠物窗承载（Wails v2 + 原生 WebView 备选）

### 6.1 主方案：Wails v2.12 `runtime.NewWindow`

```go
// desktop/pet_window.go

import "github.com/wailsapp/wails/v2/pkg/runtime"

func createPetWindow(ctx context.Context) context.Context {
    petCtx, err := runtime.NewWindow(ctx, runtime.NewWindowOptions{
        Title:    "Reasonix Pet",
        URL:      "/pet",
        Width:    140,
        Height:   180,
        Frameless: true,
        AlwaysOnTop: true,
        // BackgroundColour transparent: 设 RGBA{A:0}，macOS WebKit 自动透明
        // Windows WebviewIsTransparent: 需在创建后设置
    })
    // 宠物窗不抢焦点
    makePetWindowNonFocusable(ctx)
    return petCtx
}
```

### 6.2 ⚠️ 前提风险（Wails v2）

Wails v2.12 的 `AlwaysOnTop`/`Frameless`/`BackgroundColour` 是否是**全局设置**还是**per-window**——未公开明确。**必须先 Spike 验证**：

```go
// spike: 在 desktop/ 添加临时按钮/启动时调用
// 验证: 宠物窗透明置顶无边框 + 主窗不受影响
// 验证: EventsEmit 能被宠物窗 Events.On 收到
// 验证: 宠物窗不抢焦点
```

### 6.3 备选：原生 WebView 窗口（若 Wails v2 不可行）

复用 `desktop/third_party/go-webview2`（Windows）和平台 API（macOS）建原生窗：

- **macOS**：CGO + `objc` 调用 `[WKWebView alloc]` + `[NSWindow initWithContentRect: styleMask:NSWindowStyleMaskBorderless]` + `[window setLevel: NSFloatingWindowLevel]` + `ActivationPolicy::Accessory`
- **Windows**：`desktop/third_party/go-webview2` 里的 `chromium.go` 创建 `CreateCoreWebView2Controller` + `WS_EX_LAYERED|WS_EX_TOPMOST|WS_EX_NOACTIVATE`（参照 critter `daemon.rs:96` 的 win32 样式剥离）

状态推送路径不变：`EventsEmit("pet:state", data)` → 主窗 Go 层 → 平台特定的 `EvaluateScript`（WebView2 的 `ExecuteScript` / WKWebView 的 `evaluateJavaScript`）。

### 6.4 窗口配置

| 属性 | 值 | 平台 |
|------|----|------|
| 大小 | 140×180 | 所有 |
| 位置 | 屏幕右下角（40px margin） | 所有，持久化 |
| 透明 | macOS: WKWebView 透明背景；Windows: `WS_EX_LAYERED` | macOS + Win |
| 置顶 | `NSFloatingWindowLevel` / `WS_EX_TOPMOST` | macOS + Win |
| 无边框 | `NSWindowStyleMaskBorderless` / `WS_EX_LAYERED` 含 `WS_POPUP` | macOS + Win |
| 不抢焦点 | `ActivationPolicy::Accessory` / `WS_EX_NOACTIVATE` | macOS + Win |
| 允许拖拽 | JS mousedown/move/up → Go `WindowSetPosition` | 所有 |

### 6.5 跨窗口通信（修正缺陷 #6）

- 宠物窗 URL: `"/pet"`（内建 asset server 返回 index.html；前端 `window.location.pathname === '/pet'` 挂载 PetWindow）
- 主窗 `EventsEmit` 事件名前缀 `pet:`（`pet:state` / `pet:switch` / `pet:list`）
- 宠物窗 `Events.On("pet:state", cb)` 接收状态更新
- 宠物窗交互通过绑定 Go 方法（`PetEnable`/`PetSwitch`/`PetScale`/`PetMove` 等）

---

## 7. 前端渲染（React PetWindow）

### 7.1 PetWindow 组件树

```
PetWindow (root, --wails-draggable:drag via JS)
├── PetSprite (精灵主体)
│   ├── <div id="pet"> (CSS background-image: url(spritesheet.webp))
│   └── 逐帧驱动: setInterval(stepFrame, intervalMs) 改变 background-position
└── PetBubble (气泡，显示互动文案)
    └── 根据状态切换文案 + setTimeout 自动隐藏

PetMenu (右键菜单，全屏/浮动)
├── 切换宠物列表
├── 缩放 [+]/[-]
├── 浏览市场（打开 petdex.dev 或浏览器）
├── Star on GitHub
└── 退出宠物
```

### 7.2 精灵动画逻辑（移植 critter `src/webview.rs`）

critter 的 HTML/JS 渲染核心在 `webview.rs` 中 `build_page()` 生成的 HTML/CSS/JS。前端直接移植该逻辑：

```typescript
// petStore.ts
type AnimState = 'idle' | 'running-right' | 'running-left' | 'waiting' | 'review' | 'failed' | 'jumping' | 'waving' | 'greeting';

interface PetStateInfo {
    state: AnimState;
    bubble?: string;
    bubbleDurationMs?: number;
    sessionCount?: number;
}
```

帧步进逻辑：

```typescript
const [currentFrame, setCurrentFrame] = useState(0);
const frameCount = 8;
const frameIndex = animRows[state]; // 每行对应一个动画

useEffect(() => {
    const interval = setInterval(() => {
        setCurrentFrame(f => (f + 1) % frameCount);
    }, state === 'running-right' || state === 'running-left' ? 120 : 200);
    return () => clearInterval(interval);
}, [state]);
```

### 7.3 拖拽（修正缺陷 #7）

禁用 Wails `CSSDragProperty`（全局设置），用纯 JS 实现：

```typescript
const onMouseDown = (e) => {
    const startX = e.clientX, startY = e.clientY;
    const onMove = (e) => {
        window.runtime.WindowSetPosition(
            window.screenX + (e.clientX - startX),
            window.screenY + (e.clientY - startY)
        );
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', () => document.removeEventListener('mousemove', onMove), { once: true });
};
```

服务端同步：拖拽结束时调 Go bound method `PetSavePosition(x, y)` 持久化。

### 7.4 EventsEmit 订阅

```typescript
// petStore.ts — 宠物窗只订阅 pet: 前缀
useEffect(() => {
    const unsubState = window.runtime.EventsOn('pet:state', (data: PetStateInfo) => {
        store.setState(data);
    });
    const unsubSwitch = window.runtime.EventsOn('pet:switch', (data: { slug: string }) => {
        store.loadPet(data.slug);
    });
    return () => { unsubState(); unsubSwitch(); };
}, []);
```

---

## 8. 事件流（完整闭环）

### 8.1 启动

```
App.startup(@ desktop/app.go:424)
  → pet.NewManager(reasonixHome)
  → createPetWindow(ctx)          // 第二窗
  → 宠物窗加载 "/pet" → PetWindow 挂载
  → pet:list 推送宠物列表
  → pet:state {state: idle}       // 初始
```

### 8.2 工作中

```
用户输入 → Controller.RunTurn(@ controller.go:1633)
  → maybeSessionStart(@ 2581)
      → hooks.SessionStart → OnHook(Event, Payload)
          → mgr.OnEvent → 合并优先级 → pet:state
  → hooks.PromptSubmit → OnHook → mgr.OnEvent → pet:state {running}
  → hooks.PreToolUse → OnHook → mgr.OnEvent → pet:state {running}
  → (工具调用)
  → hooks.PostToolUseFailure → OnHook → mgr.OnEvent → pet:state {tool_error}
  → ...
  → hooks.StopResult(@ 1654, defer)
      → 成功: hooks.Stop → OnHook → mgr.OnEvent → pet:state {idle}
      → 失败: hooks.StopFailure → OnHook → classifyStopError → ErrorFinal/ToolError
```

### 8.3 用户交互

```
右键菜单 → PetMenu.tsx → Go bound PetSwitch("boba")
  → mgr.SwitchPet("boba") → 持久化
  → EventsEmit("pet:switch", {slug: "boba"})
  → 宠物窗 re-render 新精灵

拖动 → JS mousedown → Go bound PetMove(dx, dy)
  → runtime.WindowSetPosition
  → 鼠标释放 → PetSavePosition(x, y) 持久化
```

### 8.4 退出

```
App.shutdown
  → 关闭宠物窗
  → mgr.Save() 持久化最终位置/scale
  → mgr 释放
```

---

## 9. 组件依赖图

```
kernel/                │  desktop/          │  frontend/
                       │                    │
internal/hook          │                    │
  Runner.OnHook ───────→ pet_hooks.go       │
                         pet_manager.go ───→ EventsEmit("pet:state")
                                             │
                                  ┌─────────┘
                                  │
                           pet_window.go
                           (runtime.NewWindow) ─→ PetWindow.tsx
                                                  PetSprite.tsx
                                                  petStore.ts
                                                  PetMenu.tsx
```

---

## 10. 文件改动清单

### 修改的内核文件

| 文件 | 改动 | 说明 |
|------|------|------|
| `internal/hook/runner.go` | +`OnHook func(Event, Payload)` 字段 + 每个 fire 方法末尾 2 行 | 约 16 行，新增可选回调 |

### 新增内核文件

| 文件 | 行数估计 | 说明 |
|------|---------|------|
| `internal/pet/state.go` | ~150 | 状态机（移植 `src/state.rs`） |
| `internal/pet/state_test.go` | ~250 | 移植 critter 全部状态机单测 |
| `internal/pet/directory.go` | ~120 | 目录扫描 + pet.json 解析 |
| `internal/pet/directory_test.go` | ~80 | 目录测试 |
| `internal/pet/manager.go` | ~120 | Manager 封装 |
| `internal/pet/manager_test.go` | ~80 | Manager 测试 |

### 新增桌面端文件

| 文件 | 行数估计 | 说明 |
|------|---------|------|
| `desktop/pet_window.go` | ~120 | 创建/管理宠物窗 + 跨窗事件 |
| `desktop/pet_manager.go` | ~80 | 绑定 Go 方法（PetEnable/Disable/Switch/List/Save） |
| `desktop/pet_hooks.go` | ~50 | OnHook 回调 → Manager |

### 修改桌面端文件

| 文件 | 改动 | 说明 |
|------|------|------|
| `desktop/app.go` | startup 中 createPetWindow；shutdown 中销毁 | ~10 行 |
| `desktop/main.go` | (可选) 在 wails.Run 的 Bind 中添加 pet bindings | 可能已在 app.go |

### 新增前端文件

| 文件 | 行数估计 | 说明 |
|------|---------|------|
| `frontend/src/components/PetWindow.tsx` | ~200 | 宠物窗根组件 |
| `frontend/src/components/PetSprite.tsx` | ~150 | 精灵渲染 |
| `frontend/src/components/PetBubble.tsx` | ~60 | 气泡 |
| `frontend/src/components/PetMenu.tsx` | ~150 | 右键菜单 |
| `frontend/src/lib/petStore.ts` | ~80 | zustand store |
| `frontend/src/styles.css` | ~30 | 宠物 token/动画 |

### 新增设置/命令

| 文件 | 改动 |
|------|------|
| `internal/cli/slash_registry.go` | 注册 `/pet` slash 命令（桌面提示） |
| `desktop/settings_app.go` | "外观 → 宠物"开关 |

---

## 11. 已知风险与回退

| # | 风险 | 等级 | 缓解/回退 |
|---|------|------|-----------|
| 1 | Wails v2 `AlwaysOnTop`/`Transparent` 为全局设置，不可 per-window | 🔴 | 若 spike 证实：降级到备选方案——用原生 WebView 窗口（`desktop/third_party/go-webview2` + CGO `objc` for macOS），状态流不变。 |
| 2 | `runtime.NewWindow` 的 `URL: "/pet"` 对 SPA asset server 不如预期工作 | 🟡 | 可用内联 HTML（`data:text/html,...`）或创建后 `runtime.WindowExecJS` 加载；或直接在宠物窗加载主路由 + `location.pathname` 判断。 |
| 3 | `EventsEmit` 在第二窗口中收不到 | 🟡 | Spike 中优先验证；若失败，宠物窗改用 Go→platform `EvaluateScript`（备选方案），或用共享内存（chan + 定时轮询）。 |
| 4 | 宠物渲染性能（逐帧 setInterval + CSS background-image）不够流畅 | 🟡 | 借鉴 critter 已验证的方案，macOS WKWebView + Windows WebView2 表现良好；若卡顿，降帧率或改用 Canvas。 |
| 5 | 平台白边（macOS WKWebView 透明背景残留、Windows 顶部白线） | 🟡 | macOS 设置 `drawsBackground: false`（critter `daemon.rs:417`）；Windows 剥离 `WS_EX_APPWINDOW` + 设置透明（critter `daemon.rs:116`）。 |
| 6 | Linux 兼容性 | 🟢 | 标记 experimental，仅 macOS/Windows 默认开启。 |

---

## 12. 分阶段实施

### Phase 0：Spike（1-2 天）

验证 Wails v2 多窗能力。在现有 `desktop/` 中：

1. 在 `App.startup` 或某按钮回调中调 `runtime.NewWindow(ctx, runtime.NewWindowOptions{Frameless:true, AlwaysOnTop:true, URL:"/pet"})`
2. 前端 `main.tsx` 判断 `location.pathname === '/pet'` 时渲染简单 `<div>pet window</div>`
3. 主窗 `EventsEmit("pet:state", {state:"running"})`，宠物窗 `Events.On` 接收并渲染文本
4. 确认：宠物窗透明/置顶/独立焦点

**Spike 结果决定后续架构走向**：
- ✅ 成功 → 继续 Phase 1-4
- ❌ 失败 → 切换备选原生 WebView 方案（§6.2），状态 + Manager + 前端逻辑不变，只换窗口创建层

### Phase 1：内核（2-3 天）

- `internal/pet/state.go` + 单测（移植 critter 测试套）
- `internal/pet/directory.go` + 单测
- `internal/pet/manager.go` + 单测
- `internal/hook/runner.go` 加 `OnHook` 字段 + 单测验证

### Phase 2：桌面端接缝（2 天）

- `desktop/pet_window.go`：窗口创建（依 spike 结果选择 Wails 或原生）
- `desktop/pet_manager.go`：绑定方法
- `desktop/pet_hooks.go`：`OnHook` 回调 → Manager
- `desktop/app.go` 的 startup/shutdown 整合

### Phase 3：前端渲染（2-3 天）

- `PetSprite.tsx` + `petStore.ts`：精灵帧动画
- `PetWindow.tsx`：根组件 + 拖拽
- `PetBubble.tsx` + `PetMenu.tsx`：气泡和菜单
- 样式 token 补 `styles.css`

### Phase 4：设置与收尾（1-2 天）

- `settings_app.go` 外观→宠物开关
- `slash_registry.go` 注册 `/pet` 命令
- 默认精灵资源复制到 `~/.reasonix/pets/default/`
- 端到端集成测试
- 文档提交

---

## 13. 测试策略

| 层 | 测试方式 | 覆盖 |
|----|---------|------|
| 状态机 | `go test internal/pet/` | 全部 critter 已有单测（多会话优先级、计数对称、ErrorFinal 不被 stop 覆盖、事件乱序） |
| 目录扫描 | `go test` | 空目录、正常 JSON、格式错误、多目录兼容 |
| OnHook 回调 | `go test internal/hook/` | 新增 `OnHook` 字段在每个事件上被调用的断言 |
| 前端组件 | `tsx src/__tests__/pet*.test.tsx` | 精灵帧正确推进、bubble 显示/隐藏、Store 状态更新 |
| 集成 Spike | `go run desktop/`（手动） | 窗口创建、transparent、`EventsEmit` 跨窗通信 |
| 端到端 | 人工 | 启动桌面端 → 宠物窗出现 → 输入 prompt → 宠物奔跑 → 确认 → 停止 → 空闲；右键菜单切换宠物 |

> 按 [REASONIX.md 的预提交规则]：改动涉及 `internal/hook/`（cache 敏感路径）和 `internal/config` 隐性影响 → **PR 需补 `System-prompt-review` 行**；且 PR 描述末尾加：
> ```
> Cache-impact: low — internal/hook +3 lines (OnHook field); internal/pet new empty package
> Cache-guard: go test ./internal/hook/ ./internal/pet/
> System-prompt-review: <reviewer> — new OnHook field in Runner that controllers may wire
> ```

---

## 附录 A：与 Agent Critter 关系

- Critter（`/Users/chj/vsCodeProjects/agent-critter`）作为**参考实现**关闭状态机和前端逻辑
- 默认精灵资源：critter 的 `assets/default_spritesheet.webp` 直接使用
- Critter 的状态机单测（`src/state.rs` 中的 6 个测试函数）全部 port 到 `internal/pet/state_test.go`
- Critter 不再作为独立进程随 Reasonix 启动，但保留外部项目独立存在（跨平台、可被其他 App 使用）

## 附录 B：对比 Codex Pets 实现

| 维度 | Codex Pets | Reasonix 原生桌宠（本方案） |
|------|-----------|--------------------------|
| 状态语义 | running / waiting / ready | 5 态（内）→ 3 态（UI 映射） |
| 宠物格式 | `pet.json` + `spritesheet.webp` | 完全相同，直接兼容 |
| 窗口 | 内置浮动窗口（闭源） | Wails v2 新窗 / 原生 WebView |
| 状态源 | Codex 内部 | `hook.Runner.OnHook` 回调 |
| 自定义宠物 | `/pet` + `hatch-pet skill` | `~/.reasonix/pets/` 目录 + `/pet` 命令 |
| 生态适配 | 专用 | 兼容 Codex + Petdex 生态 |

---

> 最终审阅日期：2025 年 7 月  
> 审阅状态：已修正 8 个设计缺陷，闭环确认通过。请从 **Phase 0 Spike** 开始验证最高风险点。
