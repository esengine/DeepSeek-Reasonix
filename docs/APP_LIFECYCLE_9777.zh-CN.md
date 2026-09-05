# PR #9777 App 生命周期实施检查点

[English](APP_LIFECYCLE_9777.md)

## 状态与范围

本文是实施检查点，不是合并验收结论。保留现有 PR 历史，通过
`a2a4895ba` 合并主线 `86051290b`。完整 App 分层和原生平台验收仍未完成。

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

当前生产 bundle（KiB）：初始 JS 418.4 / 426.8；外壳 CSS 115.8 / 116.0；
简中60.5 / 60.6；繁中61.3 / 61.5；初始原始资源2313.5 / 2349.4。
未提高容量预算。仅设置页使用的图片控件 CSS 移入既有懒加载样式表；三语
删除了已移除密度/reasoning/fold 控件的无引用文案。旧配置字段、setter、
事件和镜像的一版兼容政策不变。

## 仍阻塞完整验收

- App 仍4582行；六个领域 owner、剩余 effect、纯组合层和删除尺寸豁免未完成。
- `test:all` 当前停在 `goal-action-errors.test.tsx` 的2项旧 JSX 字符串断言。
  应用真实迁移后的 Composer owner/交互测试替代，不能只修改期待的标点。
- repolint 仍报告集成后的 SettingsPanel、bridge、useController、desktop
  settings 和 config 尺寸超限；未扩大 baseline。
- 现有 App 浏览器回放检查的是 Sidebar 项目树，不是 WorkspacePanel/编辑器。
  6项订阅及0个已登记操作不能代表全部剩余 App 流程。
- 探针已经按弱引用身份去重，并暴露溢出、负计数；内存 runner 仍缺构建来源、
  完整预热、统一往返计数、每32轮采样及保留路径分析，旧固定数量/百分比
  断言不可作为正式验收证据。
- 最新24轮 Transcript safety 回放几何指标通过，但总 DOM/监听器计数继续
  增加。这不是 detached DOM 统计；归因尚未完成，不能仅凭计数判定泄漏
  或正常预热。
- 128/512轮、三独立进程的正式 App 长跑、原生 App soak、最终 Go/race/
  lint/CodeQL、CI 路径过滤和最终远端 head 检查均待完成。

后续应继续收敛共同资源/UI 所有权并纵向迁移完整领域；不得增加局部延时
guard、强制重挂载、清缓存或放宽验收门槛。
