# App 组合边界

[English](APP_SHELL.md)

App.tsx 只挂载 AppRuntime。AppRuntime 组合会话、导航和界面状态所有者，
AppRuntimeView 渲染已有共享区域。副作用和来源绑定命令保留在对应领域模块。
提取页面树必须保持 Hook 顺序、组件身份、草稿状态及命令注册，不能建立第二套
可变的当前会话权限。

入口契约禁止直接访问桥接、执行副作用或异步工作。AST 分层检查跟踪运行时导入、
重导出、别名和动态导入，拒绝领域/公共模块经传递依赖访问 App 所有者；类型边
单独处理，并通过反例验证。

上下文窗口展示辅助函数、延迟加载的子代理结果和预览卡片均独立为展示模块。
控制器保留工具输出及实时事件的紧凑结果元组，历史结果文本在延迟卡片渲染时
解析；最终展示结果和来源命令边界保持一致。

使用 `pnpm check:app-layers`、`pnpm test:all`、`pnpm test:app-lifecycle` 和
`pnpm test:app-browser` 验证这些契约。独立 App 内存工作流和原生 Transcript
检查仍是验收要求。[会话所有权](APP_SESSION_OWNERSHIP.zh-CN.md) 说明筛查协议，
以及单独保留的堆保留链/主分支对照归因要求。
