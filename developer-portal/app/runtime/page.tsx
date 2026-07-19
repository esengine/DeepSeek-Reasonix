import type { Metadata } from "next";

import { Callout, PageIntro, PortalShell, SectionHeading, SourceLink, Tag } from "@/app/components/PortalShell";

export const metadata: Metadata = { title: "运行时回合" };

const turnSteps = [
  ["01", "提交", "Frontend → Controller", "解析命令、引用、显示文本和 transient tail。"],
  ["02", "准备", "turnOrchestrator", "建立 checkpoint，运行 PromptSubmit hook，标记 in-flight。"],
  ["03", "推理", "Agent → Provider", "发送消息与当前 profile 的 tool schema，持续接收 reasoning、text、tool calls。"],
  ["04", "分派", "Agent → Tool gates", "对工具调用做循环保护、安全门控、调度与执行。"],
  ["05", "续推", "Tool results → Provider", "按原调用顺序追加 tool message，再进入下一轮模型调用。"],
  ["06", "收束", "Agent → Controller", "执行 readiness / compaction 检查并返回回合结果。"],
  ["07", "落盘", "Controller → Store", "保存 transcript、snapshot、goal 与 activity，并发布 TurnDone。"],
];

const gates = [
  ["循环保护", "存在性、重复成功、stale edit anchor"],
  ["阶段合法性", "plan mode；外部 MCP 的 readOnlyHint 默认不可信"],
  ["真实目标", "proxy / capability tool 解析后再次检查"],
  ["交付约束", "delivery profile 验收条件"],
  ["用户授权", "permission → Guardian → 必要时真人确认"],
  ["项目策略", "PreToolUse hook"],
  ["可恢复性", "写操作前建立 pre-edit checkpoint"],
  ["实际执行", "工具自身路径约束 + OS sandbox"],
  ["结果治理", "PostToolUse、证据、脱敏、截断、事件"],
];

export default function RuntimePage() {
  return (
    <PortalShell active="/runtime" toc={[{ href: "#turn", label: "一次回合" }, { href: "#tools", label: "工具循环" }, { href: "#progress", label: "计划与进度" }, { href: "#gates", label: "安全门" }, { href: "#cache", label: "提示缓存" }, { href: "#debug", label: "调试路径" }]}>
      <PageIntro eyebrow="02 · RUNTIME" title="一次回合，就是系统的主干" summary="从用户输入到 TurnDone，Controller 负责交互编排，Agent 负责模型—工具循环。所有前端都围绕同一套类型化事件观察这条主干。" meta="建议边读边打开 turn_orchestrator.go 与 agent.go" />

      <section className="content-section" id="turn">
        <SectionHeading id="turn-title" kicker="ONE TURN" title="七步走完一次用户回合">回合并不是一次模型请求。只要模型继续产生工具调用，Agent 就会把结果追加到消息中并再次请求模型。</SectionHeading>
        <div className="timeline">
          {turnSteps.map(([no, title, owner, note]) => (
            <article key={no}>
              <span>{no}</span>
              <div><Tag>{owner}</Tag><h3>{title}</h3><p>{note}</p></div>
            </article>
          ))}
        </div>
        <div className="source-grid three">
          <SourceLink path="internal/control/turn_orchestrator.go" />
          <SourceLink path="internal/agent/agent.go" />
          <SourceLink path="internal/control/controller.go" />
        </div>
      </section>

      <section className="content-section" id="tools">
        <SectionHeading id="tools-title" kicker="TOOL LOOP" title="并行执行，但顺序不漂移">只读工具可以加速，写工具必须保持因果顺序。<code>ReadOnly()</code> 因而是调度与权限语义，而不是一个展示标签。</SectionHeading>
        <div className="execution-lanes">
          <article>
            <header><Tag tone="green">READ-ONLY LANE</Tag><strong>最多 8 个并发</strong></header>
            <div className="lane-calls"><span>read A</span><span>read B</span><span>read C</span></div>
            <p>连续、已知且明确只读的调用可以组成并行批次。</p>
          </article>
          <article>
            <header><Tag tone="amber">ORDERED LANE</Tag><strong>严格串行</strong></header>
            <div className="lane-calls ordered"><span>write A</span><i>→</i><span>unknown</span><i>→</i><span>write B</span></div>
            <p>写调用、未知调用和混合边界按模型给出的顺序执行。</p>
          </article>
        </div>
        <Callout title="事件顺序仍然稳定">
          <p>内部即使并发，结果事件与写回模型的 tool results 仍按原始调用顺序排列。新增 Tool 时错误声明为只读，会同时引入越权与竞态风险。</p>
        </Callout>
      </section>

      <section className="content-section" id="progress">
        <SectionHeading id="progress-title" kicker="SERIAL PROGRESS" title="Todo 是两级串行状态机">它不是任意改写的清单。Host 通过连续性规则保护当前项和已完成前缀，防止模型用“重写计划”逃避当前工作。</SectionHeading>
        <div className="check-grid">
          <article><strong>唯一活动项</strong><p>整张表最多一个 <code>in_progress</code>；孤立的 level-1 子步骤会被拒绝。</p></article>
          <article><strong>先子步骤，后阶段</strong><p>两级计划先逐个激活 level-1；子项未完成时，level-0 phase 保持 <code>pending</code>。</p></article>
          <article><strong>连续性守卫</strong><p>当前项不能被删除、替换或退回 pending；已完成前缀不能插入或重排。</p></article>
          <article><strong>Host 负责推进</strong><p><code>complete_step</code> 签收当前项，由 host 选中下一项并合成新的 todo 事件。</p></article>
        </div>
        <Callout tone="amber" title="自适应进度租约取代隐藏步数上限">
          <p>当有活动 Todo 且连续 8 个 tool-call rounds 无进展时，Agent 收到提醒；到 16 轮时暂停并保留工作。<code>[agent].max_steps</code> 与 <code>planner_max_steps</code> 已退役；CLI <code>--max-steps</code> 和 <code>[bot].max_steps</code> 仍是显式运行预算。</p>
        </Callout>
        <Callout title="Plan mode 的 Economy 工作流是窄化能力">
          <p>规划阶段只连接 <code>todo_write</code>。获批退出 plan mode 后，需要重连 workflow 能力，才会暴露 <code>complete_step</code>。</p>
        </Callout>
        <div className="source-grid three"><SourceLink path="internal/evidence/evidence.go" /><SourceLink path="internal/tool/builtin/todo.go" /><SourceLink path="internal/tool/builtin/completestep.go" /></div>
      </section>

      <section className="content-section" id="gates">
        <SectionHeading id="gates-title" kicker="SAFETY PIPELINE" title="一次工具调用要经过九道门">“已经批准”不是全局通行证。每一层解决不同问题，任何一层放行都不能绕开后续约束。</SectionHeading>
        <div className="gate-pipeline">
          {gates.map(([name, note], index) => (
            <article key={name}><span>{String(index + 1).padStart(2, "0")}</span><div><strong>{name}</strong><small>{note}</small></div></article>
          ))}
        </div>
        <div className="safety-equation" aria-label="安全层级关系">
          <span>Plan mode</span><b>+</b><span>Permission</span><b>+</b><span>Hooks</span><b>+</b><span>Sandbox</span><b>=</b><strong>可控执行</strong>
        </div>
      </section>

      <section className="content-section" id="cache">
        <SectionHeading id="cache-title" kicker="CACHE-FIRST" title="模型可见内容也是公共契约">Reasonix 尽量保持输入前缀字节稳定。改变工具 schema 或 system prompt 的位置，既影响行为，也影响成本和延迟。</SectionHeading>
        <div className="cache-columns">
          <article>
            <header><span className="status-dot stable" />稳定前缀 <Tag tone="green">低频变化</Tag></header>
            <ul><li>base system prompt</li><li>工具名、顺序、描述与 schema</li><li>启动时折叠的 standing memory</li><li>Skill 名称与摘要索引</li><li>输出风格等低频设置</li></ul>
          </article>
          <article>
            <header><span className="status-dot dynamic" />回合尾部 <Tag tone="violet">允许变化</Tag></header>
            <ul><li>plan mode marker</li><li>goal / todo 提醒</li><li>本回合 reference 内容</li><li>mid-session memory 更新提示</li><li>自动计划、恢复或执行提示</li></ul>
          </article>
        </div>
        <Callout tone="amber" title="Cache-impact 审查触发器">
          <p>修改 <code>boot</code>、<code>tool</code>、<code>provider</code>、<code>config</code>、<code>memory</code>、<code>skill</code> 或 <code>outputstyle</code> 时，检查是否改变稳定前缀。临时状态优先由 <code>control.Compose</code> 注入 turn tail。</p>
        </Callout>
        <Callout title="三种 work profile 的 schema 语义不同">
          <p>Balanced 在同版本、同配置下保持前缀稳定；Delivery 通过稳定的 <code>use_capability</code> 代理 MCP；Economy 每次成功 <code>connect_tool_source</code> 都会在下一次请求加入新 schema，形成一个新缓存前缀，此后再保持稳定。</p>
        </Callout>
      </section>

      <section className="content-section" id="debug">
        <SectionHeading id="debug-title" kicker="DEBUG PATH" title="模型没有调用工具，按这个顺序查" />
        <ol className="debug-list">
          <li><span>1</span><div><strong>注册</strong><p>工具是否进入 per-run registry，二进制入口是否保留内置包空导入？</p></div></li>
          <li><span>2</span><div><strong>可见性</strong><p>名称、描述和 canonical schema 是否稳定并真实出现在 Provider 请求中？</p></div></li>
          <li><span>3</span><div><strong>阶段与权限</strong><p>plan mode、capability routing 或 permission 是否在模型执行前阻断？</p></div></li>
          <li><span>4</span><div><strong>Provider 配对</strong><p>增量 tool call 是否正确聚合，assistant/tool message 是否成对追加？</p></div></li>
          <li><span>5</span><div><strong>事件与结果</strong><p>调用是否已经发生，只是前端遗漏或错误解析了类型化事件？</p></div></li>
        </ol>
      </section>
    </PortalShell>
  );
}
