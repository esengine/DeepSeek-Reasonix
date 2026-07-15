# Reasonix Developer Atlas

Reasonix 新参与者开发门户。它把仓库中的架构、维护与生态文档整理为任务导向的多页面网站，帮助新人从一次用户回合进入代码，再逐步理解状态、安全、桌面端、扩展和交付边界。

## 内容范围

- 总体架构与启动装配
- 一次用户回合与工具调用循环
- 会话身份、sidecar、记忆与安全门
- Guard、Safe Mode、事务修复与更新回滚
- Wails / React 桌面端
- Provider、Tool、MCP、Skill、Agent 与插件包
- 本地产品、在线服务与发布线
- 新人阅读路线、练习、测试矩阵与首个 PR
- 模块、术语和权威资料索引

当前内容基线：`main-v2 @ 988190f3`（2026-07-15）。门户用于导航，不替代代码、测试、CI 与仓库中的版本化设计文档。

## 本地运行

需要 Node.js `>=22.13.0`。

```bash
npm ci
npm run dev
```

本地地址默认为 `http://localhost:3000/`。

## 验证

```bash
npm run lint
npm test
```

`npm test` 会构建 vinext 产物，并对全部 9 条页面路由进行服务端渲染检查。

## 内容维护

1. 架构、状态身份、公开契约或验证线变化时，在同一 PR 更新源文档与对应页面。
2. 结论要保留到源码或版本化文档的链接，避免形成无法追踪的第二份事实来源。
3. 计划和候选能力必须明确标注，不要写成当前实现。
4. 大规模整理时更新内容基线，并重新运行 lint 与完整测试。

## 技术栈

- Next.js App Router API
- React 19
- vinext / Vite
- Cloudflare Sites runtime

Sites 项目标识保存在 `.openai/hosting.json`；不要手工替换或推导该 ID。
