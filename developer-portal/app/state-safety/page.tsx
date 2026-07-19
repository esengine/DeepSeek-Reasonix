import type { Metadata } from "next";

import { Callout, PageIntro, PortalShell, SectionHeading, SourceLink, Tag } from "@/app/components/PortalShell";

export const metadata: Metadata = { title: "状态与安全" };

const identities = [
  ["Workspace / Project", "配置、工具、权限和文件边界", "项目树、workspace 布局、项目设置", "project"],
  ["Topic", "侧边栏逻辑分组，可包含多个 session", "产品明确要求同 topic 共享的偏好", "topic"],
  ["Session", "精确 transcript 与全部 sidecar 的运行身份", "todo、历史、恢复、运行中状态、证据", "session"],
  ["Tab", "当前可见容器，可重新绑定 session", "纯 UI 瞬态状态", "tab"],
];

const sidecars = [
  ["<id>.events.jsonl", "权威重放", "append-only transcript event log"],
  ["<id>.event-index.json", "快速定位", "事件偏移与摘要索引"],
  ["<id>.jsonl.meta", "分支信息", "branch metadata"],
  ["<id>.goal-state.json", "持续目标", "active goal 状态"],
  ["<id>.ckpt/", "恢复", "checkpoint 快照"],
  ["<id>.jobs/", "后台执行", "任务产物与状态"],
  ["<id>.conflicts.jsonl", "诊断", "snapshot 冲突恢复记录"],
  ["lock / lease", "所有权", "写锁与运行所有权"],
  ["<id>.cleanup-pending.json", "延迟清理", "待物理删除标记"],
];

const memories = [
  ["Standing docs", "项目长期约束", "启动时进入稳定前缀", "REASONIX.md / AGENTS.md"],
  ["Auto-memory", "已确认长期事实", "下个 session 进前缀；本 session 走 tail", "per-project facts + MEMORY.md"],
  ["History retrieval", "找回原话与旧工具输出", "调用 history 时按需载入", "event logs / archive"],
  ["Compaction", "控制当前上下文长度", "阈值触发，原文归档", "当前 session"],
  ["Memory v5 compiler", "生成执行 IR", "真实用户回合的 transient contract", "compiler state / traces"],
  ["Goal / AutoResearch", "持续研究、验收和续跑", "Controller 合成回合", "goal / evidence / research store"],
];

export default function StateSafetyPage() {
  return (
    <PortalShell active="/state-safety" toc={[{ href: "#identity", label: "身份矩阵" }, { href: "#sidecars", label: "会话 sidecar" }, { href: "#memory", label: "记忆机制" }, { href: "#permissions", label: "权限分层" }, { href: "#recovery", label: "Guard 与恢复" }, { href: "#review", label: "审查问题" }]}>
      <PageIntro eyebrow="03 · STATE & SAFETY" title="任何状态，都先回答“属于谁”" summary="Reasonix 的可靠性来自身份清晰、状态可恢复与安全门独立。最常见的维护事故，不是算法错误，而是把 UI 容器当成业务会话，或只操作主文件而遗漏关联状态。" meta="高风险区域 · session / permission / cache / cross-platform" />

      <section className="content-section" id="identity">
        <SectionHeading id="identity-title" kicker="IDENTITY MATRIX" title="四种身份不能互换">桌面端中的 tab 可以重新绑定 session。只要活动会话能在一个 tab 内变化，session 级状态就不能仅以 tabId 持久化。</SectionHeading>
        <div className="identity-grid">
          {identities.map(([name, meaning, state, tone]) => (
            <article className={tone} key={name}>
              <div><span className="identity-icon">{name.charAt(0)}</span><Tag>{name}</Tag></div>
              <p>{meaning}</p>
              <dl><dt>适合作为 key</dt><dd>{state}</dd></dl>
            </article>
          ))}
        </div>
        <div className="identity-rule">
          <div><span>界面容器</span><strong>tabId</strong><small>可能被重新绑定</small></div>
          <i aria-hidden="true">≠</i>
          <div><span>运行身份</span><strong>sessionPath</strong><small>精确指向 transcript + sidecars</small></div>
        </div>
        <Callout tone="amber" title="导航与异步操作的审查点">
          <p>history、todo、draft、恢复结果与异步事件到达时，重新核对当前 <code>sessionPath</code>。仅验证 <code>tabId</code> 无法防止陈旧结果写进新绑定的 session。</p>
        </Callout>
      </section>

      <section className="content-section" id="sidecars">
        <SectionHeading id="sidecars-title" kicker="SESSION FAMILY" title="一个会话不是一个 JSONL 文件">主 <code>&lt;id&gt;.jsonl</code> 是发现和兼容锚点，但事件、目标、checkpoint、后台任务、锁和清理状态都围绕同一个 session path 存在。</SectionHeading>
        <div className="sidecar-map">
          <div className="session-core"><span>SESSION ANCHOR</span><strong>&lt;id&gt;.jsonl</strong><small>discovery + compatibility</small></div>
          <div className="sidecar-grid">
            {sidecars.map(([file, role, note]) => <article key={file}><code>{file}</code><strong>{role}</strong><small>{note}</small></article>)}
          </div>
        </div>
        <SourceLink path="internal/store/session.go" label="查看统一路径 helper" />
        <Callout title="删除、fork、resume 与迁移必须成组设计">
          <p>不要手写字符串替换来寻找 sidecar，也不要只删除主 JSONL。优先复用 <code>internal/store</code> 的路径 helper，并检查 CLI、desktop、serve 与 ACP 等所有会话表面。</p>
        </Callout>
      </section>

      <section className="content-section" id="memory">
        <SectionHeading id="memory-title" kicker="CONTEXT MECHANISMS" title="六种“记忆”解决六种问题">精确原话用 history；长期确认事实才进 memory；compaction 摘要不是原始证据；Memory v5 也不是整个产品版本。</SectionHeading>
        <div className="table-wrap">
          <table>
            <thead><tr><th>机制</th><th>目的</th><th>进入上下文时机</th><th>权威来源</th></tr></thead>
            <tbody>{memories.map((row) => <tr key={row[0]}>{row.map((cell, index) => <td key={cell}>{index === 0 ? <strong>{cell}</strong> : cell}</td>)}</tr>)}</tbody>
          </table>
        </div>
      </section>

      <section className="content-section" id="permissions">
        <SectionHeading id="permissions-title" kicker="INDEPENDENT GATES" title="安全层之间是“并且”，不是“或者”">每一层回答不同问题：当前阶段允不允许、用户是否授权、项目策略是否接受、进程实际上能做什么。</SectionHeading>
        <div className="permission-stack">
          <article><span>01</span><div><strong>Plan mode</strong><p>这类动作在当前计划阶段是否合法？</p></div></article>
          <article><span>02</span><div><strong>Permission policy</strong><p>规则允许、拒绝，还是需要询问用户？</p></div></article>
          <article><span>03</span><div><strong>Guardian + human approval</strong><p>高风险动作是否通过评审，是否需要新鲜真人确认？</p></div></article>
          <article><span>04</span><div><strong>Hooks</strong><p>项目级 PreToolUse / PostToolUse 策略如何处理？</p></div></article>
          <article><span>05</span><div><strong>Sandbox</strong><p>进程、路径和操作系统最终授予什么能力？</p></div></article>
        </div>
        <Callout tone="violet" title="特殊动作永远要求新鲜批准">
          <p><code>remember</code>、<code>forget</code> 等改变长期记忆的动作不能复用旧批准。外部 MCP 的 read-only hint 也不会被默认当成可信声明。</p>
        </Callout>
      </section>

      <section className="content-section" id="recovery">
        <SectionHeading id="recovery-title" kicker="RECOVERY PLANE" title="恢复能力必须独立于故障表面">桌面壳、WebView、插件、MCP 或用户 TOML 无法启动时，恢复入口本身仍要可用。<code>reasonix-guard</code> 因此不加载这些组件，也不读取会话正文。</SectionHeading>
        <div className="ownership-grid">
          <article><span>INSPECT</span><h3>只读诊断</h3><p><code>check</code> 与默认 <code>diagnose</code> 只检查配置、派生状态和本地语义；网络探测必须显式加 <code>--network</code>。</p><code>reasonix-guard check</code></article>
          <article><span>REPAIR</span><h3>隔离并可撤销</h3><p>无法解析的配置先进入带时间戳的隔离文件；快照恢复校验 SHA-256 与 TOML，最近一次修复可用 <code>undo</code> 回退。</p><code>repair · restore · undo</code></article>
          <article><span>SAFE MODE</span><h3>最小化启动</h3><p>五分钟内连续三次未完成启动时进入原生恢复流程：使用内置配置、不恢复标签页，并禁用本次运行的外部集成。</p><code>launch --safe-mode</code></article>
          <article><span>ROLLBACK</span><h3>发布单元回滚</h3><p>更新备份覆盖桌面主程序和同批替换的 Guard / 启动器；macOS 则备份整个 App Bundle，恢复前校验全部哈希。</p><code>pending-update.json</code></article>
        </div>
        <Callout tone="amber" title="修复不等于清空用户数据">
          <p>Guard 不删除凭据 <code>.env</code>、session JSONL 或项目源码；只有显式传入 <code>--project</code> 才会隔离项目 <code>reasonix.toml</code>。安全模式也不会改写用户配置。</p>
        </Callout>
        <Callout title="健康门决定何时丢弃回滚备份">
          <p>桌面启动依次记录 <code>starting</code>、<code>ready</code>、<code>healthy</code> 与 <code>clean-exit</code>；<code>ready</code> 后还有 30 秒观察期。只有新版本健康或干净退出后，完整发布单元的备份才会被清理。</p>
        </Callout>
        <Callout tone="violet" title="AI 辅助必须显式触发，且不扩大修复权限">
          <p>离线 <code>check</code>、<code>repair</code>、<code>diagnose</code>、回滚和 Safe Mode 都不调用模型。<code>assist</code> 只发送脱敏摘要，并只接受版本化的白名单 <code>RepairPlan</code>；Host 先展示预览和 diff 再确认，计划不能执行 shell、修改凭据/会话正文或指定任意路径。</p>
        </Callout>
        <div className="source-grid two"><SourceLink path="docs/RECOVERY.zh-CN.md" label="阅读恢复与安全模式契约" /><SourceLink path="cmd/reasonix-guard/main.go" label="查看独立恢复入口" /></div>
      </section>

      <section className="content-section" id="review">
        <SectionHeading id="review-title" kicker="REVIEW QUESTIONS" title="涉及状态与安全时，PR 里回答五个问题" />
        <ol className="review-questions">
          <li><span>01</span><p>这份状态的真实身份是什么？workspace、topic、session，还是 tab？</p></li>
          <li><span>02</span><p>中断、冲突、崩溃和删除后，权威恢复来源在哪里？</p></li>
          <li><span>03</span><p>改动是否移动或绕过了 plan、permission、hook、sandbox 任一门？</p></li>
          <li><span>04</span><p>同一状态是否被多个前端、多个进程或后台任务并发访问？</p></li>
          <li><span>05</span><p>主路径失效后，恢复入口是否仍独立可用、可审计并能完整回滚？</p></li>
        </ol>
      </section>
    </PortalShell>
  );
}
