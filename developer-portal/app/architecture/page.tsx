import type { Metadata } from "next";
import Link from "next/link";

import { Callout, PageIntro, PortalShell, SectionHeading, SourceLink, Tag } from "@/app/components/PortalShell";

export const metadata: Metadata = { title: "总体架构" };

const layers = [
  { no: "L1", name: "前端 / 传输", modules: "CLI · HTTP/SSE · ACP · Bot · Wails", note: "接收输入、消费事件，不复制内核。", tone: "cyan" },
  { no: "L2", name: "装配与控制", modules: "boot.Build · control.Controller", note: "构造依赖、编排回合、暴露传输无关端口。", tone: "blue" },
  { no: "L3", name: "推理与能力", modules: "agent · provider · tool · plugin", note: "执行模型—工具循环与能力注册。", tone: "violet" },
  { no: "L4", name: "状态与安全", modules: "store · checkpoint · repair · memory · permission · sandbox", note: "保证会话与应用都可恢复、可审计、最小权限。", tone: "navy" },
  { no: "L5", name: "基础设施与交付", modules: "config · workers · site · workflows", note: "提供配置、在线服务与发布证据。", tone: "slate" },
];

const bootSteps = [
  "确定 workspace root、session 目录和额外可写路径",
  "迁移旧配置并按 root 载入用户与项目配置",
  "解析模型引用、Provider、上下文窗口与推理参数",
  "组装稳定 system prompt、standing memory 与 Skill 索引",
  "创建每次运行独立的 Tool Registry，并接入 MCP / 插件工具",
  "装配权限、审批、hooks、sandbox、history、memory 与 jobs",
  "创建 Agent；必要时用独立 planner session 包装 Coordinator",
  "创建 Controller，挂接 goal、checkpoint、Guardian 与持久化",
];

const changeMap = [
  ["对话或工具循环", "internal/control/turn_orchestrator.go", "internal/agent/agent.go", "Controller + Agent tests"],
  ["新增 Controller 能力", "internal/control/port.go", "internal/boot/boot.go", "各前端适配测试"],
  ["新增内置工具", "internal/tool/tool.go", "internal/tool/builtin/…", "schema / 权限 / 并发"],
  ["修改配置", "internal/config/load.go", "internal/config/paths.go", "优先级 + migration"],
  ["修改会话", "internal/agent/save.go", "internal/store/session.go", "恢复 / 删除 / fork / lease"],
  ["启动恢复 / Safe Mode", "cmd/reasonix-guard", "internal/repair", "只读诊断 / undo / rollback / 最小工具面"],
  ["桌面导航", "desktop/tabs.go", "useController.ts · App.tsx", "session 重绑 + 原生构建"],
  ["事件协议", "internal/event/event.go", "internal/eventwire/wire.go", "serve / desktop / TS reducer"],
];

export default function ArchitecturePage() {
  return (
    <PortalShell active="/architecture" toc={[{ href: "#model", label: "最小心智模型" }, { href: "#layers", label: "五层架构" }, { href: "#principles", label: "设计原则" }, { href: "#bootstrap", label: "启动装配" }, { href: "#change-map", label: "改动落点" }]}>
      <PageIntro eyebrow="01 · ARCHITECTURE" title="一个 Agent 内核，多种前端，一条独立恢复路径" summary="CLI、HTTP/SSE、ACP、Bot 与 Wails 共同驱动 Controller / Agent 内核；应用无法正常启动时，reasonix-guard 经由隔离的 repair 域诊断和恢复。" meta="当前实现基线 · main-v2 @ 988190f3" />

      <section className="content-section" id="model">
        <SectionHeading id="model-title" kicker="MINIMUM MODEL" title="先记住五个角色">不用背目录；先理解谁负责装配、谁负责编排、谁执行循环、谁发布事实、谁保存状态。</SectionHeading>
        <div className="architecture-chain">
          <article><Tag>入口</Tag><strong>Frontend</strong><small>提交输入、呈现事件</small></article>
          <i>→</i>
          <article><Tag tone="blue">装配</Tag><strong>boot.Build</strong><small>唯一 composition root</small></article>
          <i>→</i>
          <article><Tag tone="violet">编排</Tag><strong>Controller</strong><small>回合、审批、会话</small></article>
          <i>→</i>
          <article><Tag tone="green">执行</Tag><strong>Agent</strong><small>模型—工具循环</small></article>
          <i>↔</i>
          <article><Tag tone="amber">状态</Tag><strong>Session</strong><small>日志、sidecar、恢复</small></article>
        </div>
        <Callout title="边界判断">
          <p>如果通用行为需要在 CLI、桌面端和 Serve 各写一次，它大概率放错了层。共享行为应优先进入 <code>internal/control</code> 或更低层，前端只做适配。</p>
        </Callout>
        <Callout tone="amber" title="Guard 不是另一个前端">
          <p><code>cmd/reasonix-guard</code> 不加载 Wails / WebView、插件、MCP、hooks 或 session 正文。它是独立恢复进程，为正常 composition root 失败时保留一条更小的信任路径。</p>
        </Callout>
      </section>

      <section className="content-section" id="layers">
        <SectionHeading id="layers-title" kicker="FIVE LAYERS" title="依赖应由外向内收敛">越靠上越接近交互与传输，越靠下越接近状态、安全和基础设施。跨层调用应通过明确端口，而非让 UI 直接穿透到底层。</SectionHeading>
        <div className="layer-stack">
          {layers.map((layer) => (
            <article className={layer.tone} key={layer.no}>
              <span>{layer.no}</span>
              <div><strong>{layer.name}</strong><code>{layer.modules}</code></div>
              <p>{layer.note}</p>
            </article>
          ))}
        </div>
        <div className="source-grid two">
          <SourceLink path="internal/control/port.go" label="查看前端端口" />
          <SourceLink path="internal/boot/boot.go" label="查看装配入口" />
        </div>
      </section>

      <section className="content-section" id="principles">
        <SectionHeading id="principles-title" kicker="DESIGN PRINCIPLES" title="代码里已经做出的取舍" />
        <div className="decision-grid">
          <article><span>01</span><h3>装配与执行分离</h3><p><code>boot.Build</code> 只构造依赖；Controller 与 Agent 承担长期运行行为。</p></article>
          <article><span>02</span><h3>接口与 Registry 驱动</h3><p>Provider 和 Tool 通过接口与注册表扩展，避免按名字增长的大型分支。</p></article>
          <article><span>03</span><h3>类型化事件</h3><p>内核发布 <code>event.Event</code>，<code>eventwire</code> 统一跨前端 JSON 形状。</p></article>
          <article><span>04</span><h3>Cache-first</h3><p>模型可见的 system prompt、工具顺序和 schema 是性能与行为契约。</p></article>
          <article><span>05</span><h3>分层安全</h3><p>plan mode、permission、Guardian、hooks 与 sandbox 互不替代。</p></article>
          <article><span>06</span><h3>分层恢复</h3><p>会话依靠 event log、snapshot 与 checkpoint；应用启动依靠 Guard、事务修复、undo 和更新回滚。</p></article>
        </div>
      </section>

      <section className="content-section" id="bootstrap">
        <SectionHeading id="bootstrap-title" kicker="COMPOSITION ROOT" title="正常模式下，boot.Build 如何把系统装起来">新依赖先判断属于启动期装配，还是回合期行为。前者进入 composition root，后者进入 Controller / Agent 的领域协作者。</SectionHeading>
        <ol className="numbered-flow">
          {bootSteps.map((step, index) => <li key={step}><span>{String(index + 1).padStart(2, "0")}</span><p>{step}</p></li>)}
        </ol>
        <Callout tone="violet" title="注册时机影响缓存">
          <p>内置 Provider 与 Tool 由子包 <code>init()</code> 自注册；MCP 工具在运行期进入 per-run registry。工具名称、顺序、描述或 schema 的变化都应做 cache-impact 审查。</p>
        </Callout>
        <Callout tone="amber" title="Safe Mode 故意跳过正常装配">
          <p>Safe Mode 使用内置配置，跳过用户/项目 TOML 与配置、memory、session 迁移及物理清理；禁用 Skills、MCP、hooks、外部集成与 Memory v5，也不恢复 tabs。这是有意缩小能力面，不是普通配置 profile。</p>
        </Callout>
      </section>

      <section className="content-section" id="change-map">
        <SectionHeading id="change-map-title" kicker="CHANGE MAP" title="我要改功能，先从哪里读？">这张表给出最初落点，不代表只需要改这些文件。跨层变更仍要沿调用链检查事件、状态和验证面。</SectionHeading>
        <div className="table-wrap">
          <table>
            <thead><tr><th>需求</th><th>先读</th><th>继续追踪</th><th>验证重点</th></tr></thead>
            <tbody>{changeMap.map((row) => <tr key={row[0]}>{row.map((cell, index) => <td key={cell}>{index === 0 ? cell : <code>{cell}</code>}</td>)}</tr>)}</tbody>
          </table>
        </div>
        <div className="next-callout">
          <div><span>NEXT</span><h3>现在沿着一次真实回合继续</h3><p>架构图只有跟运行时结合才会变成可维护的心智模型。</p></div>
          <Link className="button primary" href="/runtime">进入运行时回合 →</Link>
        </div>
      </section>
    </PortalShell>
  );
}
