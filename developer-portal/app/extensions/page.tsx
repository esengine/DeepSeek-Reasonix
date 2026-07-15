import type { Metadata } from "next";

import { Callout, PageIntro, PortalShell, SectionHeading, SourceLink, Tag } from "@/app/components/PortalShell";

export const metadata: Metadata = { title: "扩展体系" };

const extensionTypes = [
  ["Provider", "连接模型协议", "编译期 factory registry", "stream、tool-call 配对、usage、能力与重试"],
  ["Built-in Tool", "内核运行能力", "编译期 builtin registry", "schema、ReadOnly、权限、plan mode、cache"],
  ["MCP Server", "外部工具集合", "运行期 per-run registry", "传输、命名空间、失败隔离、外部信任"],
  ["Skill", "按需载入的指导能力", "启动索引 + 运行时读正文", "scope、启停、前缀索引稳定"],
  ["Slash command", "用户输入展开", "command discovery", "不要承载长期领域状态"],
  ["Hook", "项目 / 用户自动化策略", "阶段化 hook runner", "阻断语义、退出码、与安全层关系"],
  ["Plugin Agent", "手动调用的子智能体 profile", "插件命名空间", "归属、命名冲突、工具与模式约束"],
];

export default function ExtensionsPage() {
  return (
    <PortalShell active="/extensions" toc={[{ href: "#chooser", label: "能力选择器" }, { href: "#provider", label: "Provider" }, { href: "#tools", label: "Tool 与 MCP" }, { href: "#skills", label: "Skill、Agent 与插件包" }, { href: "#install", label: "安装与信任" }]}>
      <PageIntro eyebrow="05 · EXTENSIONS" title="先选扩展层，再写代码" summary="Provider、内置 Tool、MCP、Skill、Command、Hook、Plugin Agent 与 package 看似都在“增加能力”，但加载时机、信任边界和缓存影响完全不同。" meta="核心判断 · 这是模型协议、执行能力、提示知识，还是分发单元？" />

      <section className="content-section" id="chooser">
        <SectionHeading id="chooser-title" kicker="EXTENSION CHOOSER" title="你要增加的到底是什么？" />
        <div className="table-wrap extension-table">
          <table><thead><tr><th>类型</th><th>解决问题</th><th>接入时机</th><th>必须验证</th></tr></thead><tbody>{extensionTypes.map((row) => <tr key={row[0]}>{row.map((cell, index) => <td key={cell}>{index === 0 ? <Tag tone={index === 0 ? "blue" : "default"}>{cell}</Tag> : cell}</td>)}</tr>)}</tbody></table>
        </div>
        <Callout title="长期状态不应藏在模板里">
          <p>如果能力要管理 goal、approval、session 或其他长期状态，它属于 Controller / domain 层。Slash command 和 Skill 可以引导使用，但不应成为隐藏状态机。</p>
        </Callout>
      </section>

      <section className="content-section" id="provider">
        <SectionHeading id="provider-title" kicker="MODEL ADAPTER" title="Provider 是协议适配，不是产品分支">新的 Provider 实现统一接口并注册 factory；上层 Agent 不应通过模型名字判断 OpenAI 或 Anthropic 行为。</SectionHeading>
        <div className="extension-path">
          <article><span>01</span><strong>实现接口</strong><code>provider.Provider</code><small>请求、流、能力</small></article><i>→</i>
          <article><span>02</span><strong>注册 factory</strong><code>provider registry</code><small>由入口空导入触发</small></article><i>→</i>
          <article><span>03</span><strong>配置解析</strong><code>boot.Build</code><small>模型引用与参数</small></article><i>→</i>
          <article><span>04</span><strong>契约测试</strong><code>stream fixtures</code><small>增量、配对、usage</small></article>
        </div>
        <div className="check-grid">
          <article><strong>流式增量</strong><p>reasoning、text 与 tool call chunk 是否正确聚合？</p></article>
          <article><strong>消息配对</strong><p>assistant tool call 与 tool result 是否满足目标协议？</p></article>
          <article><strong>能力声明</strong><p>图片、推理、上下文窗口与工具支持是否真实？</p></article>
          <article><strong>失败语义</strong><p>重试、取消、限流和不完整流如何映射为统一错误？</p></article>
        </div>
        <SourceLink path="internal/provider/provider.go" />
      </section>

      <section className="content-section" id="tools">
        <SectionHeading id="tools-title" kicker="EXECUTION CAPABILITIES" title="内置 Tool 与 MCP 最终共享 Tool 契约">内置 Tool 与二进制一起编译；MCP Server 在运行时通过 stdio 或 Streamable HTTP 暴露外部工具，并独立失败隔离。</SectionHeading>
        <div className="compare-grid">
          <article><header><Tag tone="blue">BUILT-IN</Tag><h3>内置 Tool</h3></header><ul><li>实现 <code>tool.Tool</code></li><li>由 builtin 子包注册</li><li>代码与内核一起审查、构建</li><li>明确 <code>ReadOnly()</code> 与 schema</li></ul></article>
          <article><header><Tag tone="violet">MCP</Tag><h3>外部工具</h3></header><ul><li><code>internal/plugin</code> 作为 MCP host</li><li>命名为 <code>mcp__server__tool</code></li><li>单个服务失败不拖垮 session</li><li>外部 readOnlyHint 默认不可信</li></ul></article>
        </div>
        <Callout tone="amber" title="区分被动发现与 Economy 主动连接">
          <p>Lazy MCP 新发现的 schema 延迟到下一 session 生效。Economy profile 的 <code>connect_tool_source</code> 则是明确的运行时连接：新 schema 在下一次模型请求中可见，并形成新的稳定前缀。</p>
        </Callout>
        <div className="source-grid two"><SourceLink path="internal/tool/tool.go" /><SourceLink path="internal/plugin" label="查看 MCP Host" /></div>
      </section>

      <section className="content-section" id="skills">
        <SectionHeading id="skills-title" kicker="KNOWLEDGE & DISTRIBUTION" title="Skill 是提示能力，plugin package 是分发单元">一个可安装 package 可以同时导出 Skills、slash commands、hooks、MCP servers 和手动调用的 plugin Agents。</SectionHeading>
        <div className="package-diagram">
          <div className="package-core"><span>PLUGIN PACKAGE</span><strong>manifest + pinned content</strong><small>可解析 Reasonix / Codex / Claude，并报告 full / partial / incompatible</small></div>
          <div className="package-exports"><article><strong>Skills</strong><small>名称摘要进前缀，正文按需读</small></article><article><strong>Commands</strong><small>输入展开</small></article><article><strong>Hooks</strong><small>可信策略自动化</small></article><article><strong>MCP servers</strong><small>运行时外部代码</small></article><article><strong>Agents</strong><small><code>/&lt;plugin&gt;:agent:&lt;name&gt;</code></small></article></div>
        </div>
        <p className="body-copy">面向用户的 Skill / command 可以用 <code>/&lt;plugin&gt;:&lt;name&gt;</code> 消除同名冲突；模型内部 Skill 索引仍应保持名称与描述紧凑稳定。</p>
        <Callout tone="amber" title="Claude 兼容是明示边界，不是全量等价">
          <p>适配器会转换 <code>tool_name</code>、<code>tool_input</code> 和 <code>tool_response</code>；<code>PreToolUse</code>、<code>UserPromptSubmit</code> 以及 <code>PermissionRequest</code> 有可阻断语义。<code>Stop</code> / <code>SubagentStop</code> 仍只观察不阻断，其他缺口会进入结构化兼容性报告；无任何可映射能力的非原生包会被拒绝安装。</p>
        </Callout>
        <SourceLink path="docs/PLUGIN_PACKAGES.zh-CN.md" label="查看兼容性边界" />
      </section>

      <section className="content-section" id="install">
        <SectionHeading id="install-title" kicker="INSTALL TRUST" title="远程安装先计划，再固定，再落盘">安装完整性与运行时信任是两件事。安装过程不执行第三方脚本，但启用后的 hook shell command 与 stdio MCP 可以执行外部代码。</SectionHeading>
        <ol className="numbered-flow five">
          <li><span>01</span><p>默认只生成确定性安装 plan 与风险信息</p></li>
          <li><span>02</span><p>用户明确 <code>apply=true</code> 后才执行批准计划</p></li>
          <li><span>03</span><p>GitHub 来源固定到批准时 commit，避免 HEAD 漂移</p></li>
          <li><span>04</span><p>内容进入 staging，重新解析并验证能力数量与路径</p></li>
          <li><span>05</span><p>原子替换旧安装；远程获取受 SSRF 防护</p></li>
        </ol>
        <div className="trust-split">
          <article><span>INSTALL INTEGRITY</span><h3>拿到的是批准内容吗？</h3><p>commit 固定、manifest 路径、staging 校验、原子替换。</p></article>
          <article><span>RUNTIME TRUST</span><h3>启用后允许做什么？</h3><p>hooks、stdio MCP、权限、sandbox 与项目级策略。</p></article>
        </div>
        <SourceLink path="internal/installsource" label="查看安装规划与落盘" />
      </section>
    </PortalShell>
  );
}
