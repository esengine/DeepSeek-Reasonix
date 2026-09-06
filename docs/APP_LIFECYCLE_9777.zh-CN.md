# PR #9777 App 生命周期实施检查点

[English](APP_LIFECYCLE_9777.md)

## 状态与范围

本文是实施检查点，不是合并验收结论。保留现有 PR 历史，包括主线合并
`5cb605a4d` 与资源/导航实施提交 `2b9aba0fb`。分层迁移已全部完成并满足
repolint 单模块 ≤800 行硬门禁；与当前 origin/main-v2 的合并（exact prompt
protocol、session-runtime ordering fence、AskCard session-draft wiring）已解决
并转绿。原生平台验收仍未完成。

## 已实现的共同契约

- 同步、异步命令使用同一个 layout-committed slot。render 不发布权威；
  cleanup 立即释放输入并撤销调用权。普通更新不改变 epoch；StrictMode
  和 Suspense 隐藏/恢复会使旧操作失效，但保留稳定入口身份。
- 异步 capture 同步执行，与独立 executor 分离。executor 仅接收最小输入
  和 checkpoint 凭据，每个 await 后的副作用前必须检查。仅有结果包装器
  并不能保证现有 App 操作安全；这些流程仍需迁移。
- DOM ref 接线不是业务命令。Transcript 独立组合 kernel 和入口动画 ref，
  保持 mutation 阶段接线，业务命令则等待 layout commit。
- 同一导航 intent 下替换会话也会获得唯一 paint 回执。消费成功返回原目标，
  App 不再代入当前活动 tab；回执只能消费一次，卸载释放回执及源 surface 引用。
- 订阅 scope 先撤销全部排队通知，再注销来源；即使一个 cleanup 抛错，仍清理
  其余订阅。终端 bridge 使用共享引用计数租约，旧 cleanup 不能关闭新 bridge。
- 操作 owner 的诊断通过注入接入；共同命令和 domain 不再依赖 App 浏览器探针。

AST 门禁解析 import、re-export、别名和动态 import，区分 type-only 边，并
遍历 domain/共同原语的运行时依赖。门禁只证明已迁移模块的依赖方向，
**不代表剩余 App 已成为纯组合层**。新分层模块启用完整 hooks 依赖检查。

## 验证证据

| 契约 | 证据 |
| --- | --- |
| 卸载立即撤销调用权 | 旧实现执行2次而非1次；新确定性测试通过 |
| layout 阶段发起操作 | 旧 passive setup 错误返回 disposed；修复后正常完成 |
| 同 intent 替换会话 | 旧实现复用 `navigation-1`；真实 hook 测试现在拒绝旧回执 |
| 探针识别保留对象 | 人为保留4096个对象，旧探针只报告2048；现在保留全部存活身份 |
| committed 输入和中间副作用 | 512次更新、transition 中断、Suspense 隐藏/恢复、源输入、替代和卸载通过 |
| Footer 遮罩和决策卡片 | 真实 DecisionFooterRegion 验证草稿/身份、inert、Todo/rewind 挂载 |
| 订阅生命周期 | 排队通知、cleanup 抛错、注册期间同步卸载及重叠终端租约通过 |

4项导航源码字符串断言已由真实 Footer 测试替代；2项懒加载断言改为检查实际
所属区域的 AST。这不代表此前删除的全部 App 源码断言已经审计完毕。

本地已通过：前端 build、App 生命周期测试、现有三布局浏览器回放、Transcript
单测、Chromium 选择/滚动/Composer 回放、Chromium 和 Playwright WebKit
reader 回放、前端及测试类型检查、hooks、含负例的 AST 门禁、single-writer
和 diff 空白检查。

最近已检查生产 bundle（KiB，合并 main-v2 后实测）：初始 JS 441.0 / 441.1；
外壳 CSS 116.4 / 116.5；简中 60.9 / 61.0；繁中 61.7 / 61.9；初始原始资源
2380.9 / 2381.0。预算按政策做了最小 ratchet：合并前精简图因工具链漂移已超
旧门禁 0.5-1.4；分层拆分的 bag 接口增加约 13 KiB 不可压缩属性名；main-v2
feature 链在 kernel 精简启动图上只增加 0.7 KiB；deferred CSS 与 locale 上限
覆盖的是先存 base 漂移。逐项 before/after 归因见
desktop/frontend/scripts/check-bundle-budget.mjs。复杂度、挂载块数和漂移预算
均未提高。仅设置页使用的图片控件 CSS 移入既有懒加载样式表；旧配置字段、
setter、事件和镜像的一版兼容政策不变。

## 9月5日系统性修复与诊断回放

DOM/监听器线性增长的保留路径已经定位：ExternalOpener 与
TopicbarSessionActions 是不同类型的兄弟组件，却共用 tab key；React 协调后
遗留仍附着的 ExternalOpener。使用角色隔离的 Fragment 保留原有资源重挂载语义，
不增加 DOM 包装。恢复旧结构时，真实128次替换测试在第一次切换即失败：
出现2个 opener，而非1个。

单独诊断进程对 full/windowed/safety/mixed 各完成32次 A→B→A 往返。
修复前每阶段增加832个节点、256个监听器；修复后基线及四阶段采样均保持
5980个节点、501个监听器。该增长不是 detached DOM。修复后脏工作区构建指纹为
`f3720e364bcafaf85eed6a7bf1a797ea17b13e8a6d51fb44152240374f76aed5`，
不是最终 head 验收。render token 基线12个、各阶段结束13个，不能据此设置新上限。
堆边仍显示项目导航、工作区回调引用退役 App 上下文，完整分层仍需继续。

共同操作 owner 已支持按源资源/类别隔离的请求通道。Composer 模式和审批
每个 await 后检查凭据；审批额外检查原提示 ID。远端 topic 重命名使用捕获的
session path，不使用异步列表返回时的 current 标记；旧实现已复现“A 发起却改名 B”。
合法源数据完成与当前 UI 权限分开，A→B→A 不恢复旧 UI 权限。

设置、引导、项目/topic、终端和远端 Composer 已有窄 committed 输入。
共同展示投影为 Composer、Context、StatusBar 选择同一运行时；远端字段缺失
不会回退到本地遥测、附件、固定文件或 inbox。以上是部分纵向迁移，不代表剩余 App 合格。

导航复用既有合并队列，由独立 executor 处理本地 topic、空白会话、历史、IM、
工作树与远端工作区。每个请求拥有自身结果、失败与 finally 清理；远端 opened
事件只登记资源，不能获取导航权。本地和远端复用同一 surface ticket 与 Kernel
paint 确认。真实 App 回放曾出现远端已 hydrate、Composer 仍禁用：只有本地
Transcript 确认导航。现在共同投影从实际展示的运行时取得 readiness，并将
Serve generation 纳入绘制身份；确定性测试拒绝旧回执，远端失败正常终结，
不依赖超时解锁。

完整 App 浏览器回放已覆盖远端项目选择、远端全局“新建会话”、返回本地历史，
Composer 持续挂载。验收前先修复 fixture：远端 open 与 ListTabs 共用目录，
snapshot/status 返回同一完整 profile。旧目录在真实 bridge 测试的第一次刷新
即失败；没有放宽生产状态校验。

Workspace 命令统一拥有项目偏好恢复、远端 explorer 请求及模式/最大化操作。
运行时轮询采用注入时钟的 single-flight owner：刷新合并、卸载撤销排队结果、
旧源 finally 不能释放新请求。这些是共同生命周期变更，不是新增轮询延时或平台补偿。

9月6日主线集成保留全窗口设置、回收站和自动化页面，接入既有区域 host；
实际工作区保持挂载并 inert。自动化链接复用共同导航队列和 committed 页面回执：
目标被接受后保留返回入口；页面替换、旧 finally 和卸载均不能恢复旧请求。
Node 组件发现器统一加载 CSS 资源桩，不再维护易遗漏传递依赖的测试文件白名单。
这不替代真实 CSS 与浏览器验证。

### 旧断言到行为证据的替代清单

| 旧源码断言 | 实际生产行为验证 |
| --- | --- |
| WorktreeBadge 必须写在 App 的 JSX | `topicbar-region.test.tsx` 挂载隔离/普通会话，验证标识可达性、条件显隐和合并按钮的源 tab |
| 后台 profile 恢复及 await 模型回调文本 | `controller-profile-lifecycle.test.tsx` 执行真实 effect/模型命令，覆盖单次失败呈现及直接调用的 reject 契约 |
| App Goal 激活、源发送和 undo 回调位置 | `session-submission-lifecycle.test.tsx` 执行真实 owner/adapter；既有 Goal 路由测试继续覆盖 Controller 与原子 bridge |
| App pending revision map 与 render ref 文本 | `pending-plan-revision-lifecycle.test.tsx` 验证源队列、资源替换、相同文本、旧 finally、失败保留及卸载 |
| Goal 清理/模式 JSX | `goal-action-errors.test.tsx` 挂载真实命令 hook，失败只提示一次 |
| 启动 snapshot、警告、失败默认值、IM 刷新 | `desktop-preferences-lifecycle.test.tsx` 验证 bridge 调用、revision、后端权威及卸载 |
| 引导回调文本 | `onboarding-commands.test.tsx` 验证 overlay 与 dismissal 状态 |
| General standard/deep JSX | 既有 `settings-refresh-snapshot.test.tsx` 挂载 SettingsPanel；新增控件测试覆盖 busy、键盘、同值失败回读 |
| Theme 回调位置 | `theme-pack.test.ts` 的真实 appearance 测试，加设置 hook 挂载测试 |
| 工作树合并变量拼写 | 既有 `worktree-merge-lifecycle.test.ts` 验证源/intent 与终态 |
| 远端模式/回滚字符串 | `composer-source-operations.test.tsx` 检查完整原子参数并禁止调用本地模式/goal 接口 |
| 远端发送、slash、Goal 操作字符串 | `remote-composer-commands.test.tsx` 验证生产 hook、源 continuation 和发送字节不变 |
| 远端附件/指导消息 JSX | `remote-composer-presentation.test.tsx` 挂载真实 Composer，验证本地文件/inbox 零写入及远端指导发送 |
| 远端遥测/布局 JSX | `conversation-projection.test.ts` 验证生产投影和真实 dock 隐藏呈现 |
| 远端终端 JSX/快捷键字符串 | `terminal-panel-commands.test.tsx` 驱动原生 key 事件、实际按钮及 warm host 挂载排除 |
| 工作树创建队列、脏目录提示字符串 | `project-topic-lifecycle.test.tsx` 与 `desktop-navigation-lifecycle.test.tsx` 驱动实际队列、源操作和提示端口 |
| Automation/dock 可见性回调字符串 | 共同展示投影与终端命令测试验证真实呈现、存储偏好及原生快捷键效果 |
| 远端向导 canonical 路径字符串 | `remote-connect-wizard.test.tsx` 经真实导航 owner 完成合并后的 canonical 项目 |
| 远端全局新建 helper 字符串 | `test:app-browser` 点击远端项目后的全局按钮，验证 hydrate、Composer 身份和返回导航 |
| 主线内联自动化导航字符串 | 自动化导航生命周期测试检查页面 ABA、目标接受和精确清理；导航队列测试确认仅胜出的目标发布接受回执 |

这不代表此前所有断言退役都已经审计；其余要求随所属领域迁移继续核对。

## 仍阻塞完整验收

当前状态更正：分层迁移已完成。`App.tsx` 是 10 行无 bridge 的组合入口（入口
契约门禁强制）；`AppRuntime.tsx` 收敛为 161 行组合根——adapter 解构、session
identity/fence/operations、navigation surface、store 状态、三次组合调用与一行
view 渲染。命令域位于 `app-runtime/useAppSessionComposition.ts`（755 行）与
`app-runtime/useAppNavigationComposition.ts`（221 行），返回树位于
`app-shell/AppRuntimeView.tsx`（516 行），另有 ChatPaneRegion、
TopicbarActionsStack、DockToggleButton 及 footer/chrome/dock/overlay 属性
builder。repolint 全局 ≤800 行单模块硬门禁对每个 App 模块通过，无 App 专属
例外。以下历史迁移清单中的旧行数仅用于追溯，不能作为当前实现数据。

Topicbar 标题、重命名输入与来源控件已迁入展示区域，展示数据和命令分开。
真实挂载测试验证原 DOM、同步焦点、键盘取消及操作子树身份；生产 App 回放
继续验证 Composer 和实际工作区文件预览节点。

模型切换、启动/runtime epoch 恢复和发送前 readiness 现已共用源会话 profile
应用路径。模型重建后读取源资源最新 layout-committed profile，不回写旧 render
或当前另一会话；资源替换和卸载撤销 continuation，A-B-A 不恢复旧 UI 权限。
模型和 readiness 共用 profile 请求通道，Controller 合并请求失败时，不论交错
顺序都只有一个失败呈现 owner。真实 hook 测试还覆盖直接 reject、boolean
失败、远端路径及相同 profile 下的 runtime 替换。生产 App 回放通过真实模型
选择器切换模型，验证已 hydrate 的 block key/内容 revision、草稿与可写状态；
临时原始 Markdown fallback 文本不能作为历史内容权威。

本地普通/直接提交与 Goal 激活现已接入共同源会话 submission owner，删除
render 阶段发布的 `commitThenSendRef` 和 rewind-state ref。等待 profile/Goal
返回后，在清理 undo、发送或 patch 源 profile 之前检查资源身份。结构化首轮
Goal 保留原子 Controller 接口及既有 trim、前缀和 invocation 字节语义。生产
App 回放实际发送 mock turn、点击停止，并验证 Composer 身份和可写恢复。

Plan 修订由独立的源会话队列 owner 管理，仅原请求可释放其槽位；旧传输等待中
不阻塞替换资源。失败修订保留原有源会话重新激活/运行结束时的重试语义，但无关
重渲染不会循环重试。提交需等待源 surface 已提交、runtime 可用且没有待处理提示。
队列以自身 authority 复用 submission executor，不嵌套 UI 命令，确保旧错误不弹到
当前 UI 时仍保留源资源的失败修订。卸载同步清空队列。原生 slash、clear/steer/stop 协调已在
`app-runtime/useComposerRouter.ts` 与 `app-runtime/useSessionControlCommands.ts`；
其余 App 领域均已随 runbook 完成迁移。

- App 分层已完成（最终结构见本节首段与 runbook 完成态）；已迁出：模块级代码（`lib/sessionTitles.ts`、`lib/mockScenarios.ts`、`lib/todoDismissalStorage.ts`、`app-shell/NoticePreviewPanel.tsx`、`app-shell/HotkeyRegistrations.tsx`）；窗口 chrome（`lib/desktopPlatform.ts`、`store/windowChrome.ts`、`app-runtime/WindowChromeLifecycle.tsx`，NativeWindowChrome 删除）；shell 几何（sidebar/right-dock/terminal 三个 pointer+键盘 resize、toggleSidebar/pulse/anchor 与全部宽度投影迁入 `app-runtime/useShellGeometry.ts`；拖拽瞬态状态 sidebarResizing/live 宽度/liveTerminalHeight/togglePressed 进 `store/layout.ts`；原 #5 flush、#7 timer 清理 effect 删除）。banner 栈（session 状态区：RemoteReclaim/lease/startupErr 横幅、takeover 对话框、config 警告、provider 提示、UpdateBanner）JSX 迁入 `app-shell/SessionStatusBanners.tsx`，reclaim/takeover/config/release-notes 命令入 `app-runtime/useSessionBannerCommands.ts`，takeoverDialogTab/reclaimBusyTab/providerSetupNeeded 进 `store/overlays.ts`，启动 onboarding 探测迁 `app-runtime/StartupGateLifecycle.tsx`；会话导出纵向迁 `app-runtime/useSessionExportCommands.ts`（exportSession/getSessionMarkdown/Json、导出弹层外点关闭 effect、主题 scene effect）；undo/rewind 核心迁 `app-runtime/useSessionUndo.ts`（rewindStates/committing/信号三态、handleMessageAction/handleEditPrompt/handleSessionRevertCommitted，undo 横幅仍读 hook 返回值）；extension form 纵向迁 `app-runtime/useExtensionSurface.ts`（busy/submit/cancel + 通知 toast drain）；tab-bar 命令迁 `app-runtime/useTabBarCommands.ts`（切换单飞队列、单/批量关闭带 ActiveWork 门、后台 runtime reveal 三件套与 delivery worktree 续跑）；runtime 事件面迁 `app-runtime/useRuntimeEventHandlers.ts`（tab-meta 注册表含 single-flight coordinator、runtime event/ready/rebuilt 监听、远端 status/forwards/server 监听、workspace focus 协调 effect #23，原 effect #23 从 App 移除）；命令面板迁 `app-runtime/usePaletteCommands.tsx`（openPalette、六个全局快捷键与 paletteItems 项构建，会话/扩展/远端项来自各自 store）；Composer 路由迁 `app-runtime/useComposerRouter.ts`（!/model/memory/clear/new、决策 mock 种子、goal-draft、theme 命令与远端 steer）；回收站/历史命令迁 `app-runtime/useHistoryCommands.ts`（openTrash/closeHistory/refresh、按运行态门控的删除与重命名，删除成功后本地过滤视图）；远端工作区连接迁 `app-runtime/useRemoteWorkspaceCommands.ts`（launch gate、从状态条打开、带 stop-and-retry 的连接超时与 host 级失败）。footer ResizeObserver（`app-runtime/useFooterHeightLifecycle.ts`）、
activeTabIdRef（`app-runtime/activeTabMirror.ts` 模块单例，layout-committed 语义不变）、
maximised 同步（`store/windowChrome.ts` 状态 + `useWindowsMaximisedSync` 生命周期）已迁。
最终切片另迁：clear-context 链（`app-runtime/useSessionClearCommands.ts`）、turn
验证 reveal（`useTurnVerificationCommands.ts`）、delivery 续跑（
`useDeliveryContinueCommands.ts`）、todo 面板链（`useTodoPanelCommands.ts`）、
全部 composer 插入通道（`useComposerInsertCommands.ts`）、session 控制与导航命令
（`useSessionControlCommands.ts`、`useSessionNavigationCommands.ts`）、窗口 chrome
命令（`useAppChromeCommands.ts`）、composer profile 投影与 patch 命令（
`useComposerProfileProjection.ts`）、transcript surface 投影（
`useTranscriptSurfaceProjection.ts`）、store 订阅面（`useAppShellStores.ts`）、
invocation metadata（`useInvocationMetadata.ts`）、decision surface/tab 条/
workspace scope 投影（`decisionSurfaceProjection.ts`、`controllerProfileOwner.ts`
的 `projectVisibleTabs`、`conversationProjection.ts`）。全部浏览器回放与生命周期
测试通过。
- 旧 Goal 与远端 JSX 断言已有行为替代。当前累计 `test:all`、App 生命周期/浏览器、
  Transcript 单测及两组浏览器回放、single-writer、repolint 已通过；不代表最终
  head 或原生平台验收。
- SettingsPanel、bridge、useController、desktop settings 和 config 通过
  设置/模型命令职责抽离，repolint 已通过。App 尺寸豁免已删除：全局 ≤800 行单模块
  上限现在覆盖每个 App 模块（合并 main-v2 后 1260 条 baseline 记录，无新增）；
  没有扩大 baseline。
- App 浏览器回放现已检查实际 WorkspacePanel、文件树、选中文件和预览 DOM，
  覆盖布局、设置页面往返及同项目会话切换。这是文件预览证据，不代表全部编辑路径。
  6项订阅及0个已登记操作仍不能代表全部剩余 App 流程。
- 内存 runner 会重建生产资源、记录源码/构建指纹、统计完整往返，每32轮采样并保存
  heap 分类和弱引用身份 cohort。最终结构已在三个独立进程完成 full/windowed/safety
  各128轮和 mixed 512轮；证据完整、操作已释放、无 page error，保留路径分类未发现
  持续的 post-baseline cohort。该分类器只是筛查门禁：它拦截持续 cohort、原生计数器
  或订阅漂移、未释放操作；heap retainer 与对照构建归因仍是记录在案的离线职责，
  自动化计数器永远无法自行完成。
- 原生 App soak、最终 Go/race/lint/CodeQL 和最终远端 head 检查仍待完成。CI 已调用
  App lifecycle/browser/memory 与完整 frontend suite；捕获的 Darwin/Chromium 筛查
  证据已 PASS，但不能替代 WebKit/WebView2 原生证据或离线 heap retainer 归因。

## 分层迁移 Runbook（已全部完成）

所有切片已按既定 owner/adapter/region 模式落地；每片一次迁完「输入捕获 →
执行 → 结果应用 → 失败 → 清理」并带 app-lifecycle 模式的确定性测试。原结构
盘点（30 个 effect、约 90 个状态/ref、44 处直接 bridge 调用、16 个大处理器）
已全部退役：

1. ✅ **Layout/Shell 生命周期（已完成）**：effects #6/#8/#11/#26/#27 与
   platform/viewport 状态迁 `lib/desktopPlatform.ts` + `store/windowChrome.ts`
   + `app-runtime/WindowChromeLifecycle.tsx`；footer ResizeObserver（#25）为
   `app-runtime/useFooterHeightLifecycle.ts`；activeTabIdRef（#22）为
   `app-runtime/activeTabMirror.ts` 模块单例；maximised 同步为
   `store/windowChrome.ts` 状态 + `useWindowsMaximisedSync` 生命周期；三个
   pointer resize 生命周期与 toggleSidebar/pulse 在 `app-runtime/useShellGeometry.ts`，
   拖拽瞬态在 `store/layout.ts`。
2. ✅ **Banner 栈（已完成）**：reclaim/lease/startupErr/错误横幅、takeover
   对话框、config 警告、provider 提示与 UpdateBanner 已迁
   `app-shell/SessionStatusBanners.tsx` + `app-runtime/useSessionBannerCommands.ts`
   + `store/overlays.ts` + `app-runtime/StartupGateLifecycle.tsx`。extension
   drain（#15）为 `app-runtime/useExtensionSurface.ts`；`app:open-settings`
   （#10）为 `app-runtime/useNativeSettingsEvent.ts`。
3. ✅ **Session actions（已完成）**：rewind/undo 在 `app-runtime/useSessionUndo.ts`
   （含 handleUndoRewind）；导出在 `app-runtime/useSessionExportCommands.ts`；
   confirmClearContext 在 `app-runtime/useSessionClearCommands.ts`；
   handleDeliveryContinue 在 `app-runtime/useDeliveryContinueCommands.ts`；
   openTurnVerification 在 `app-runtime/useTurnVerificationCommands.ts`；
   cancel/accept/disconnect/workspace-conflict/runtime-job 控制在
   `app-runtime/useSessionControlCommands.ts`。`goalSubmit.ts` 已删除：首轮 Goal
   原子激活由 controller `sendToTab` 的 `initialGoal` 分支承载
   （`SubmitInitialGoalToTabWithID`），send-failed 场景已指向
   `session-submission-lifecycle.test.tsx` 与 `goal-activation-tab-routing.test.tsx`
   的真实 owner 链路。
4. ✅ **Composer 剩余（已完成）**：handleSend/handleSteer/theme 路由在
   `app-runtime/useComposerRouter.ts`；全部插入通道（composerInsertRequestsByTab、
   selectedText、planRevisionInsert、workspaceInsertTarget、terminal 输出）在
   `app-runtime/useComposerInsertCommands.ts`；profile 投影与 patch 命令在
   `app-runtime/useComposerProfileProjection.ts`；mode/approval 轴在
   `lib/useComposerModeActions.ts`（plan/approval 记忆与 YOLO 开关已内化）。
5. ✅ **Project/Topic（已完成）**：topic summary（按 topic 身份 single-flight）
   为 `app-runtime/useTopicSummary.ts`；workspace focus 协调与 runtime
   event/ready/rebuilt 的 profile 恢复段在 `app-runtime/useRuntimeEventHandlers.ts`；
   worktree merge 协调（含 overlay 状态）在 `app-runtime/useWorktreeMergeCommands.ts`。
6. ✅ **Navigation 剩余（已完成）**：tab 关闭/重排/切换、reveal、delivery
   worktree 与 task-monitor 会话在 `app-runtime/useTabBarCommands.ts` 与
   `app-runtime/useSessionNavigationCommands.ts`（基于
   `app-runtime/useDesktopNavigation.ts`）。
7. ✅ **Preferences/Overlays（已完成）**：store 订阅面为
   `app-runtime/useAppShellStores.ts`；palette 在 `app-runtime/usePaletteCommands.tsx`；
   AppOverlayHost 属性构建在 `app-shell/overlayBuilders.ts`。
8. ✅ **组装（已完成）**：chat-pane 主区块拆为 `app-shell/ChatPaneRegion.tsx`；
   topicbar 动作栈为 `app-shell/TopicbarActionsStack.tsx` +
   `app-shell/DockToggleButton.tsx`；footer/chrome/dock 属性装配在
   `app-shell/decisionFooterBuilders.ts`、`app-shell/chromeRegionBuilders.ts`、
   `app-shell/dockRegionBuilders.ts`；`AppRuntime.tsx` 为 161 行组合根，返回树为
   `app-shell/AppRuntimeView.tsx`；模块级残余
   （setRemoteComposerProfileForSessionAction、lazy loaders）随所属模块归位。
   repolint ≤800 行单模块硬门禁通过，无 App 例外。

每个切片验收已执行：确定性测试（app-lifecycle 模式）+ tsc（两套）+ AST 分层/
hooks 门禁 + test:app-lifecycle + app-browser 回放；退役的 App 源码字符串断言
均在所属切片获得行为替代或改指所属模块源码，无宽松替代。bundle 预算按政策做了
最小 ratchet（见证据节）；goalSubmit.ts 已删除。三区域会话内换 WorkspacePanel
保真、Composer 审批覆盖下持续挂载两条不变式在每次浏览器回放中复核。

### 组装切片（8）最终状态与架构边界

最终结构：`App.tsx` 为 10 行无 bridge 组合入口；`AppRuntime.tsx`（161 行）持有
controller adapter、session identity/fence/operations、navigation surface 与
store 状态，随后调用 session 与 navigation 两个组合层并渲染 view。
`app-runtime/useAppSessionComposition.ts`（755 行）按 App body 原顺序运行
session/composer 域 hook；`app-runtime/useAppNavigationComposition.ts`（221 行）
运行 history/navigation/chrome 域；`app-shell/AppRuntimeView.tsx`（516 行）是纯
props 树。

**架构边界（已核实，不变）**：`useController` 是单实例 hook（状态不在任何
store），任何区域组件都无法自取 controller 派生态——"App ≤200 行纯组合"在当前
架构下仍不可达：需要先做 controller 状态 store 化的独立长周期史诗（超出本 PR
范围）。注意区分：该理想目标仍然开放，而本 PR 的 repolint ≤800 行单模块硬门禁
已达成。

与当前 origin/main-v2 的合并在本分支的 tab 作用域 `*ForTab` prompt 处理器内采用了
exact prompt protocol（`resolvePromptForTab` + `handlePromptFailure` +
`runtimeStatusSnapshotIsStale`）；冲突解决保留分层结构并在每个等价点采用 main-v2
的共享 helper。合并后全量 discovery runner 298/298 套件通过，包括本分支此前携带
先存失败的六个套件。

后续应继续收敛共同资源/UI 所有权并纵向迁移完整领域；不得增加局部延时 guard、
强制重挂载、清缓存或放宽验收门槛。
