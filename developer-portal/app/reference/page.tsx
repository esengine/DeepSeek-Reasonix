import type { Metadata } from "next";

import { Callout, PageIntro, PortalShell, SectionHeading, SourceLink, Tag } from "@/app/components/PortalShell";

export const metadata: Metadata = { title: "索引与术语" };

const modules = [
  ["cmd/reasonix", "二进制入口、编译期注册", "从进程进入 CLI"],
  ["internal/cli", "参数、子命令、TUI、退出码", "用户命令分流"],
  ["internal/boot", "配置到 Controller 的 composition root", "依赖装配"],
  ["internal/control", "回合、审批、goal、session 生命周期", "前端共享行为"],
  ["internal/agent", "模型 / 工具循环、并发、compaction", "推理执行"],
  ["internal/provider", "模型请求 / 流式响应抽象与 registry", "模型协议"],
  ["internal/tool", "Tool 接口、schema 与运行期 registry", "执行能力"],
  ["internal/event", "内核事件领域模型", "事实发布"],
  ["internal/eventwire", "跨前端 JSON 事件契约", "传输序列化"],
  ["internal/store", "session path、sidecar、锁与恢复 helper", "持久化路径"],
  ["cmd/reasonix-guard", "独立诊断、修复、Safe Mode 启动与回滚入口", "桌面无法启动时的恢复面"],
  ["internal/repair", "配置隔离、快照、事务、启动健康与更新回滚", "恢复领域逻辑"],
  ["desktop", "Wails 原生壳与 React webview", "桌面产品"],
  ["site / workers / npm", "门户、在线服务与分发", "非核心生态"],
];

const glossary = [
  ["Turn", "一次由 Controller 编排的用户回合，内部可能包含多次 Provider 调用与工具执行。"],
  ["Controller", "传输无关的主控制端口，管理回合、审批、会话、goal 与能力。"],
  ["Agent", "执行模型—工具—再调用模型循环的运行器。"],
  ["Provider", "把统一模型请求映射到具体供应商协议的适配器。"],
  ["Tool", "具有稳定名称、描述、schema、ReadOnly 语义和执行方法的能力。"],
  ["MCP", "运行时连接外部工具服务的协议；在 Reasonix 中由 internal/plugin 承载。"],
  ["Sidecar", "围绕主 session path 保存的事件、目标、checkpoint、任务、锁等关联状态。"],
  ["Standing memory", "启动时进入稳定前缀、长期成立的项目指导文档。"],
  ["Compaction", "阈值触发的上下文压缩；原文归档，摘要不是原始证据。"],
  ["Cache-first", "主动保持模型可见前缀字节稳定的架构原则。"],
  ["Capability", "对真实工具或端口的可路由能力表达，仍需经过阶段与权限检查。"],
  ["Plugin package", "可分发的能力包，可包含 Skill、Command、Hook、MCP server 与手动调用的 Agent profile。"],
  ["Guard", "不加载 Wails、WebView、插件、MCP 或会话正文的独立恢复程序；安装后的桌面入口默认先经过它。"],
  ["Safe Mode", "使用内置配置、不恢复上次标签页并在本次运行禁用外部集成的最小化桌面启动方式；不会改写用户配置。"],
  ["Release unit", "更新与回滚必须共同处理的一组安装产物：Windows / Linux 的桌面程序与相关 Guard / 启动器，或 macOS 的整个 App Bundle。"],
];

const docs = [
  ["SPEC.md", "工程契约", "约束、非目标与跨模块工程规则"],
  ["docs/ARCHITECTURE.zh-CN.md", "当前架构导览", "从代码还原的主链、分层与设计理念"],
  ["docs/MAINTAINER_GUIDE.zh-CN.md", "维护指南", "环境、练习、测试矩阵与接手节奏"],
  ["docs/ECOSYSTEM.zh-CN.md", "生态地图", "扩展、Workers、站点与发布工程"],
  ["docs/TOOL_CONTRACT.zh-CN.md", "工具契约", "Tool 可见行为与兼容约束"],
  ["docs/CHECKPOINTS.md", "恢复设计", "checkpoint 生命周期与边界"],
  ["docs/RECOVERY.zh-CN.md", "故障恢复", "Guard 命令、Safe Mode、事务修复与更新回滚"],
  ["docs/SESSION_MEMORY_RETRIEVAL.md", "会话检索", "history / memory / archive 关系"],
  ["desktop/README.md", "桌面开发", "依赖、开发、测试、构建与发布"],
  ["RELEASING.md", "发布契约", "tag、产物、签名与发布步骤"],
];

export default function ReferencePage() {
  return (
    <PortalShell active="/reference" toc={[{ href: "#authority", label: "权威层级" }, { href: "#modules", label: "模块索引" }, { href: "#glossary", label: "术语表" }, { href: "#documents", label: "文档导航" }, { href: "#maintain", label: "维护本网站" }]}>
      <PageIntro eyebrow="REF · REFERENCE" title="遇到冲突，回到可执行证据" summary="这页不是要替代源码，而是帮助你快速找到正确的事实来源。目录名提供线索，测试与调用链才确认真实职责。" meta="查找建议 · package comment → 同目录 tests → 调用者 → CI" />

      <section className="content-section" id="authority">
        <SectionHeading id="authority-title" kicker="SOURCE OF TRUTH" title="资料权威层级">越靠上越接近可执行事实。下层资料可以解释动机，但不能覆盖当前代码、测试与 CI 的行为。</SectionHeading>
        <ol className="authority-stack">
          <li><span>01</span><div><Tag tone="green">EXECUTABLE</Tag><strong>代码 · 测试 · CI workflow</strong><p>当前实现和自动化证明。</p></div></li>
          <li><span>02</span><div><Tag tone="blue">CONTRACT</Tag><strong>SPEC · TOOL_CONTRACT</strong><p>明确的工程与可见行为契约。</p></div></li>
          <li><span>03</span><div><Tag tone="violet">CURRENT MAP</Tag><strong>Architecture · Maintainer Guide</strong><p>按当前代码整理的导航。</p></div></li>
          <li><span>04</span><div><Tag>USER DOCS</Tag><strong>README · CLI · Guide · Config paths</strong><p>产品表面与用户行为。</p></div></li>
          <li><span>05</span><div><Tag>DESIGN RECORD</Tag><strong>子系统设计文档</strong><p>解释历史与动机，可能包含旧计划或 open questions。</p></div></li>
          <li><span>06</span><div><Tag>RELEASE EVIDENCE</Tag><strong>Releasing · Checklists</strong><p>特定发布线、版本和审计记录。</p></div></li>
        </ol>
        <Callout tone="amber" title="不要静默接受漂移">
          <p>实现与文档不一致时，在同一 PR 修正文档，或明确记录为什么代码需要回到既有契约。子系统 v5 / v6 不代表整个 Reasonix 产品版本。</p>
        </Callout>
      </section>

      <section className="content-section" id="modules">
        <SectionHeading id="modules-title" kicker="MODULE INDEX" title="核心目录速查" />
        <div className="module-index">
          {modules.map(([path, role, use]) => <article key={path}><code>{path}</code><p>{role}</p><span>{use}</span></article>)}
        </div>
      </section>

      <section className="content-section" id="glossary">
        <SectionHeading id="glossary-title" kicker="GLOSSARY" title="项目术语，不按字面猜" />
        <dl className="glossary">
          {glossary.map(([term, definition]) => <div key={term}><dt>{term}</dt><dd>{definition}</dd></div>)}
        </dl>
      </section>

      <section className="content-section" id="documents">
        <SectionHeading id="documents-title" kicker="DEEP DIVES" title="按主题继续深入">本门户给出导航和判断框架；细节、边界条件与维护命令以仓库中的版本化文档为准。</SectionHeading>
        <div className="document-list">
          {docs.map(([path, type, note]) => <SourceLink key={path} path={path} label={`${type} · ${note}`} />)}
        </div>
      </section>

      <section className="content-section" id="maintain">
        <SectionHeading id="maintain-title" kicker="KEEP IT CURRENT" title="维护这张地图的规则" />
        <div className="maintain-rules">
          <article><span>01</span><h3>跟行为一起改</h3><p>架构、身份、验证线或公开契约变化时，在同一 PR 更新对应页面与源文档。</p></article>
          <article><span>02</span><h3>链接到证据</h3><p>页面保留源文件入口；不要只复制结论而切断读者继续追踪的路径。</p></article>
          <article><span>03</span><h3>标明基线</h3><p>大规模整理时记录分支、提交与日期，避免把历史快照伪装成永远当前。</p></article>
          <article><span>04</span><h3>不写未验证承诺</h3><p>计划、候选能力与发布预期必须明确标注，不能混入“当前已实现”。</p></article>
        </div>
      </section>
    </PortalShell>
  );
}
