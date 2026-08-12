# Reasonix 桌面端开发笔记（2026-08-09）

> 记录本次分支 `feat/voice-to-text` 的开发内容：思考级别下拉框悬停提示、
> STT 热键独立重置、即说即输防重复、静默超时默认值调整，以及排障过程。

## 一、功能改动

### 1. 思考模式（推理级别）下拉框悬停提示

**文件**：`desktop/frontend/src/components/EffortSwitcher.tsx`

- 触发按钮（Gauge 图标 + 当前级别 + 下拉箭头）用 `<Tooltip label={tooltipLabel} fill disabled={open || closing}>` 包裹，与旁边模型下拉框（`ModelSwitcher`）交互一致。
- 提示文案复用现有 i18n 键（三语言包 en/zh/zh-TW 均已存在）：
  - `auto` 时：`status.effortAutoTitle` → 「推理力度：auto（模型默认：{def}）」
  - 非 auto 时：`status.effortTitle` + 当前级别 → 「推理力度：高」
- 下拉菜单打开/关闭动画期间禁用 Tooltip，避免提示与菜单重叠。

### 2. STT 全局热键独立重置按钮

**文件**：`desktop/frontend/src/components/SettingsPanel.tsx`、`styles.css`

- 第一版（提交 `8bbeb21`）：在「语音输入」设置区为 `HotkeyCaptureInput`（alt+s / alt+w）
  输入框右侧新增独立「重置」按钮，`disabled={disabled || !value}`，点击清空并保存。
- 后续整合：STT 热键行整体迁移到「快捷键」设置页（`ShortcutsSection`），与内置快捷键
  行同构——录制式 key 按钮（`ShortcutComboDisplay` 展示）+ 独立「重置」按钮，
  **重置恢复默认值 `alt+s` / `alt+w`**（`isCustom = value !== defaultValue` 时才可点）。
- 新增 `comboToHotkeyString` / `parseHotkeyCombo` 完成组合键与后端存储格式
  （小写 `alt+s`）互转。

### 3. STT 即说即输 + 防重复

**文件**：`desktop/frontend/src/components/Composer.tsx`

- 原逻辑：interim 只做按钮上方预览，final 才插入输入框 → 停止说话后要等静音+网络
  往返才上屏。
- 新逻辑：interim 实时上屏「删旧插新」；停顿 1.2s 无新 interim 时把当前句固定为已提交；
  final 到达时用已提交句去重后原子替换，避免 interim/final 交替重复输入。
- 解决「停止说话几秒才进输入框」的体验问题。

### 4. 静默自动停止默认值 10s → 6s

**文件**：`desktop/frontend/src/components/SettingsPanel.tsx`、`lib/bridge.ts`、`internal/config/render.go`

- 未设置时显示/注释默认值从 `10` 改为 `6`，与更灵敏的自动停止体验一致。

## 二、排障记录（重要经验）

### 1. 「改了代码但界面没生效」——dev 跑在旧 worktree 上

- 现象：运行 `dev.bat` 后新功能不出现。
- 排查：用 `Get-CimInstance Win32_Process` 查 `node.exe`/`reasonix-desktop-dev.exe`
  命令行，发现 vite 与 dev exe 实际来自
  `%LOCALAPPDATA%\reasonix\worktrees\<指纹>\<id>\DeepSeek-Reasonix`
  —— 这是 **Reasonix Delivery 隔离工作区**，不是当前项目目录。
- 根因：Delivery worktree 基于**已提交 HEAD** 创建（`internal/worktree/worktree.go`），
  分支 `reasonix/delivery-<时间戳>-<id>` 锁在旧快照（如 `24614f0`），不含之后提交的改动。
- 教训：**先确认 dev 进程的启动路径再排查代码**；worktree 是隔离的，不会自动同步。

### 2. 思考级别下拉框「消失」

- 现象：大号欢迎输入框（hero 模式）右下角没有思考级别下拉框。
- 根因：`Composer.tsx` 中 `{!heroMode && hasEffort && <EffortSwitcher/>}` ——
  hero 模式（creation 布局 + 空会话）**刻意隐藏**任务/审批/思考级别等控件，
  仅保留模型下拉框。属上游设计，非回归。
- 验证：输入文字（会话有内容）离开 hero 模式后即恢复显示。

### 3. 开发环境报错 `Failed to fetch dynamically imported module`

- 现象：`MarkdownRenderer.tsx` / `themeExperience.ts` 动态导入失败。
- 结论：文件存在、生产构建通过，纯属 vite dev server HMR/模块缓存失效，
  **刷新页面或重启 dev server** 即可，非代码问题。

## 三、验证

- `npx tsc --noEmit` ✅
- `npx eslint src/components/EffortSwitcher.tsx src/components/SettingsPanel.tsx` ✅
- `pnpm build`（lint + CSS 检查 + vite build + bundle 预算）✅
- `node scripts/check-css-syntax.mjs src/styles.css` ✅

## 四、提交与 PR

- 提交 `8bbeb21`：思考级别下拉框悬停提示 + STT 热键行独立重置按钮
- PR #2：https://github.com/dxdw2021/DeepSeek-Reasonix/pull/2（base: main-v2）
- 推送注意：本机 git 代理 `127.0.0.1:7897` 不可用时，临时 `git -c http.proxy= -c https.proxy= push`
  直连推送成功。
