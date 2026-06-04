# Reasonix v2 Effort（推理深度）配置驱动化重构 — 已落地

## 背景

`effort`（推理深度）的支持列表原先硬编码在 `effort.go` 中，仅支持 DeepSeek 和 Anthropic。为了接入 MiMo 及未来任意 OpenAI 兼容模型，需将 effort 能力下放至 TOML 配置层，实现"零代码侵入"的模型接入。

**核心验证结论**：底层 `openai.go` 已通过 `omitempty` 完美支持 `auto`（不发送参数以保护 Prefix Cache），本次重构打通了**配置层 → 逻辑层 → UI 层**的链路。

---

## 架构设计

### 三层分离

| 层级 | 结构 | 职责 |
|------|------|------|
| **Provider 能力** | `ProviderEntry.SupportedEfforts` / `DefaultEffort` | 声明该 provider 支持哪些 effort 级别 |
| **会话状态** | `Session.Provider` / `Session.Effort` | 用户当前选中的 provider 和 effort |
| **解析层** | `ResolveEffort(caps, chosen) → ResolveResult` | 纯函数：校验 + 降级 + warning |

### 数据流

```
TOML config
  ├─ [session] provider / effort          ← 会话状态
  └─ [[providers]] supported_efforts / default_effort  ← 能力声明
        ↓
  EffortCapabilityForEntry(e) → EffortCapability
        ↓
  ResolveEffort(caps, chosen) → ResolveResult{Effort, Blocked, Degraded, Warning}
        ↓
  各调用方消费：
    CLI: /effort → 验证 + 降级 warning + 保存 Session.Effort
    CLI: /model  → AdaptToProvider(newCaps) + 降级 warning + 保存
    Desktop: Effort() → EffortInfo → 下拉
    Desktop: SetEffort() → 保存 Session.Effort → rebuild
    Desktop: SwitchProvider() → AdaptToProvider + 返回 warning
    boot: NewProviderWithProxy(entry, cfg.Session.Effort, proxy) → provider.Config.Extra["effort"]
```

### Cache-Safe 机制

- `auto` 模式映射为空字符串 `""`
- `chatRequest.ReasoningEffort` 标记 `json:"reasoning_effort,omitempty"`
- 空字符串不序列化 → API 请求中无此字段 → **Prefix Cache 键保持稳定**（前后请求的 prompt prefix 哈希一致，缓存命中保留）

---

## 变更清单

### 1. `internal/config/config.go` — Session 结构 + 迁移

```go
type Config struct {
    // ... 现有字段 ...
    Session       Session  `toml:"session"`
    MigrationHint string   `toml:"-"`  // 非序列化，加载时填充
}

type Session struct {
    Provider string `toml:"provider"`
    Effort   string `toml:"effort"`
}

func (s *Session) AdaptToProvider(caps EffortCapability) string  // 降级 + 返回 warning
func (c *Config) ValidateEffortConfig() error                    // 校验 DefaultEffort ∈ SupportedEfforts
```

ProviderEntry 保留 `Effort` 字段作为迁移兼容（读旧 TOML 用，写盘时不输出）：

```go
type ProviderEntry struct {
    // ... 现有字段 ...
    Thinking         string   `toml:"thinking"`
    Effort           string   `toml:"effort"`            // 旧字段：读旧 TOML 用，写盘不输出
    SupportedEfforts []string `toml:"supported_efforts"`  // 能力声明
    DefaultEffort    string   `toml:"default_effort"`     // 默认级别（通常 "auto"）
}
```

### 2. `internal/config/effort.go` — 核心逻辑

```go
type ResolveResult struct {
    Effort   string  // 规范化后值；"" 代表 auto（序列化为 omitempty）
    Blocked  bool    // provider 不支持 effort，UI 隐藏切换器
    Degraded bool    // 用户选择被降级
    Warning  string  // 人类可读
}

func ResolveEffort(caps EffortCapability, chosen string) ResolveResult  // 纯函数
func EffortCapabilityForEntry(e *ProviderEntry) EffortCapability       // 能力解析
```

内置启发式：
- **DeepSeek**：`["auto", "high", "max"]`，default `"auto"`
- **Anthropic**：`["auto", "low", "medium", "high", "xhigh", "max"]`，default `"auto"`
- **其他**：必须显式声明 `supported_efforts`，否则 `Supported: false`

### 3. `internal/config/render.go` — TOML 序列化

新增 `[session]` 段：

```toml
[session]
provider = "mimo-pro"
effort   = "high"
```

provider 段不再输出 `effort` 字段（已迁移到 `[session]`）。

### 4. `internal/boot/boot.go` — Provider 创建

```go
func NewProviderWithProxy(e *config.ProviderEntry, effort string, proxy netclient.ProxySpec) (provider.Provider, error)
```

`effort` 参数来自 `cfg.Session.Effort`，通过 `Extra["effort"]` 传给 provider。

### 5. `internal/cli/model.go` — 切换 provider 时自动适配

```go
// 在 buildController 之前
newCaps := config.EffortCapabilityForEntry(newEntry)
warning := cfg.Session.AdaptToProvider(newCaps)
if warning != "" {
    m.notice(fmt.Sprintf("effort: %s", warning))
}
```

### 6. `internal/cli/effort.go` — 使用 ResolveEffort

```go
res := config.ResolveEffort(cap, args[1])
if res.Warning != "" {
    m.notice(fmt.Sprintf("effort: %s", res.Warning))
}
edit.Session.Effort = res.Effort
```

### 7. `desktop/settings_app.go` — 桥接层

```go
type SettingsView struct {
    // ... 现有字段 ...
    SessionEffort   string `json:"sessionEffort"`
    SessionProvider string `json:"sessionProvider"`
}

func (a *App) SwitchProvider(providerName string) string  // 返回 warning
```

SaveProvider 采用合并模式：保留 TOML 中的 `supported_efforts` / `default_effort`，不因 UI 未暴露而擦除。

### 8. 前端 UI

- `SettingsPanel.tsx`：ModelsSection 新增 Session Provider 下拉 + Session Effort 下拉（动态选项）
- `bridge.ts`：`SwitchProvider(providerName) → Promise<string>` 接口
- `en.ts` / `zh.ts`：`settings.sessionEffort` / `settings.sessionProvider` 等 i18n 键

---

## 测试覆盖

### 新增测试

| 文件 | 测试函数 | 验证内容 |
|------|---------|---------|
| `effort_test.go` | `TestResolveEffort_ExactMatch` | chosen ∈ levels → 透传 |
| | `TestResolveEffort_AutoAlwaysEmpty` | auto/AUTO/"" → "" |
| | `TestResolveEffort_DegradeToDefault` | max ∉ levels → Default |
| | `TestResolveEffort_DegradeToAuto` | Default=auto → "" |
| | `TestResolveEffort_DegradeToFirstLevel` | Default ∉ levels → Levels[0] |
| | `TestResolveEffort_Blocked` | Supported=false → Blocked |
| | `TestResolveEffort_CaseInsensitive` | HIGH → high |
| | `TestSession_AdaptToProvider` | 降级 + warning |
| `capability_test.go` | `TestCapability_DeepSeekBuiltin` | DeepSeek 启发式 |
| | `TestCapability_AnthropicBuiltin` | Anthropic 启发式 |
| | `TestCapability_CustomWithSupportedEfforts` | 配置优先 |
| | `TestCapability_CustomOverridesBuiltin` | 显式覆盖内置 |
| | `TestCapability_GenericOpenAINotSupported` | 无声明 = 不支持 |
| `migrate_test.go` | `TestMigrate_LegacyProviderEffort` | 旧字段 → Session.Effort |
| | `TestMigrate_SessionEffortAlreadySet` | 已设置不覆盖 |
| | `TestValidateEffortConfig` | DefaultEffort ∉ SupportedEfforts → 报错 |

### 已有测试更新

| 文件 | 更新 |
|------|------|
| `edit_test.go` | `TestSetProviderEffort` 改检查 `Session.Effort` |
| `edit_test.go` | `TestNormalizeLegacyEffortMigratesOff` 改检查迁移后行为 |
| `render_test.go` | `TestRenderTOMLRoundTrips` 改用 `Session.Effort` |
| `serve/effort_test.go` | 改检查 `Session.Effort` |
| `cli/chat_tui_test.go` | +`TestEffortCommandDegradesUnsupportedLevel` |
| `cli/cli_test.go` | +Anthropic 预设适配 |
| `desktop/app_test.go` | DeepSeek default `"high"` → `"auto"` |

---

## TOML 配置示例

```toml
[session]
provider = "mimo-pro"
effort   = "high"

[[providers]]
name        = "mimo-pro"
kind        = "openai"
base_url    = "https://token-plan-cn.xiaomimimo.com/v1"
model       = "mimo-v2.5-pro"
api_key_env = "MIMO_API_KEY"
supported_efforts = ["auto", "low", "medium", "high"]
default_effort    = "auto"
no_proxy    = true
```

---

## 迁移说明

### 旧格式（v1.0.0）

```toml
[[providers]]
name = "deepseek"
effort = "high"
```

### 新格式（v1.1.0+）

```toml
[session]
provider = "deepseek"
effort   = "high"

[[providers]]
name = "deepseek"
# effort 字段不再需要
```

**自动迁移**：首次加载旧格式时，`normalizeLegacyEffort` 将 `ProviderEntry.Effort` 迁移到 `Session.Effort`，并在 stderr 打一次 deprecation warning。保存后旧字段被清除。

---

## 设计亮点

1. **零代码侵入**：未来接入任何新模型只需修改 `reasonix.toml`，核心代码无需改动
2. **向后兼容**：旧 TOML 自动迁移，无需手动修改
3. **Cache-Safe**：`auto` 模式不发送参数，保护 Prefix Cache
4. **安全降级**：不支持的级别回退到 `DefaultEffort`（而非最高级），防止成本失控
5. **UI 自适应**：`supported_efforts` 为空时自动隐藏 effort 切换器
6. **纯函数设计**：`ResolveEffort` 无副作用，易于测试和推理
