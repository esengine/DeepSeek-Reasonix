import type { Metadata } from "next";

import { CopyCommand } from "@/app/components/CopyCommand";
import { ProgressChecklist } from "@/app/components/ProgressChecklist";
import { Callout, PageIntro, PortalShell, SectionHeading, SourceLink, Tag } from "@/app/components/PortalShell";

export const metadata: Metadata = { title: "开发上手" };

const readingRoute = [
  ["README.zh-CN.md", "产品边界与用户概念"],
  ["REASONIX.md", "长期维护不变量：单一 Controller、cache-first"],
  ["cmd/reasonix/main.go", "内置注册与真正入口"],
  ["internal/cli/cli.go", "命令如何分流"],
  ["internal/boot/boot.go", "系统如何装配"],
  ["internal/control/port.go", "前端能驱动什么"],
  ["internal/control/turn_orchestrator.go", "一次回合的主流程"],
  ["internal/agent/agent.go", "模型—工具循环"],
  ["internal/provider/provider.go · internal/tool/tool.go", "两大扩展接口"],
  ["internal/event/event.go · internal/eventwire/wire.go", "输出契约"],
  ["internal/agent/save.go · internal/store/session.go", "落盘与恢复边界"],
  ["docs/RECOVERY.zh-CN.md · cmd/reasonix-guard/main.go", "独立恢复入口、Safe Mode 与发布回滚边界"],
];

const exercises = [
  ["A", "追踪普通 prompt", "画出 main → CLI → boot → Controller → Agent → Provider，并标注消息、事件与 snapshot。"],
  ["B", "追踪 edit_file", "标注 ReadOnly、plan、permission、hook、checkpoint、sandbox 与 tool result 配对。"],
  ["C", "恢复一个 session", "找齐 transcript、event log、goal、checkpoint、jobs 与 lease；比较 CLI 和 desktop。"],
  ["D", "追踪桌面事件", "从 event sink 经 eventwire、Wails、bridge、reducer 到组件，检查身份与 FIFO。"],
  ["E", "增加最小只读 Tool", "在练习分支实现并验证接口、schema、ReadOnly、注册、权限与 cache guard。"],
  ["F", "演练 Guard 恢复", "在隔离的 REASONIX_HOME 中运行 check / diagnose，追踪启动失败阈值、Safe Mode 与完整发布单元回滚测试。"],
];

const testMatrix = [
  ["Leaf helper", "同包 tests", "root vet / test"],
  ["Tool schema / 顺序", "tool + boot + contract", "root test + cache guard"],
  ["Provider stream", "provider tests", "agent + root + 授权后 smoke"],
  ["Controller / turn", "control + agent", "root + 各前端事件 / 审批"],
  ["Session / store", "agent + store + control", "resume / fork / delete / recovery"],
  ["Desktop Go", "desktop Go tests", "frontend + Wails native build"],
  ["Desktop React", "聚焦 TS test + typecheck", "frontend full + Wails + 目标平台"],
  ["Jobs / plugin 并发", "同域 tests", "go test -race ./..."],
  ["Guard / updater", "internal/repair + reasonix-guard", "desktop startup / packaging / release-unit tests"],
];

export default function DevelopPage() {
  return (
    <PortalShell active="/develop" toc={[{ href: "#environment", label: "环境隔离" }, { href: "#route", label: "阅读路线" }, { href: "#exercises", label: "纵向练习" }, { href: "#tests", label: "测试矩阵" }, { href: "#first-pr", label: "首个 PR" }, { href: "#progress", label: "接手进度" }]}>
      <PageIntro eyebrow="07 · GET STARTED" title="不要读完代码，再开始动手" summary="用可验证的纵向路径建立维护能力：先跑通入口，再跟踪一个回合，随后理解状态与安全，最后完成一个范围小、证据完整的 PR。" meta="目标 · 能定位层级、追踪主链、选择验证矩阵、识别四类高风险" />

      <section className="content-section" id="environment">
        <SectionHeading id="environment-title" kicker="SAFE ENVIRONMENT" title="第一步：隔离开发状态">开发版默认不应读取或污染你日常使用的配置、credentials、sessions、cache、Skills、commands、hooks 与桌面 tab state。</SectionHeading>
        <div className="environment-card">
          <div><Tag tone="blue">LINUX / macOS</Tag><CopyCommand command="export REASONIX_HOME=/tmp/reasonix-dev" /><CopyCommand command="go run ./cmd/reasonix --help" /></div>
          <div><Tag tone="violet">WINDOWS POWERSHELL</Tag><CopyCommand command={'$env:REASONIX_HOME = "$env:TEMP\\reasonix-dev"'} /><CopyCommand command="go run ./cmd/reasonix --help" /></div>
        </div>
        <Callout tone="amber" title="先看 go.mod，再安装工具链">
          <p>根 <code>go.mod</code> 是版本事实来源。先运行 <code>go version</code> 与 <code>go env GOTOOLCHAIN GOOS GOARCH</code>；若无法识别 <code>toolchain</code> 指令，是本机 Go 过旧，还没有进入项目编译阶段。</p>
        </Callout>
      </section>

      <section className="content-section" id="route">
        <SectionHeading id="route-title" kicker="FIRST PASS" title="第一遍只走主链">这一遍只回答：谁创建谁、一次回合经过谁、事实由谁持久化。遇到分支先标记，不要立即展开。</SectionHeading>
        <ol className="reading-route">
          {readingRoute.map(([file, reason], index) => <li key={file}><span>{String(index + 1).padStart(2, "0")}</span><div><code>{file}</code><p>{reason}</p></div></li>)}
        </ol>
        <div className="time-route">
          <article><strong>90 min</strong><span>建立项目地图</span><p>首页 → 架构 → 入口 / boot / port</p></article>
          <article><strong>½ day</strong><span>跟踪一个回合</span><p>orchestrator → Agent → Provider / Tool → Store</p></article>
          <article><strong>1 day</strong><span>完成纵向练习</span><p>在测试与日志中验证自己的调用图</p></article>
        </div>
      </section>

      <section className="content-section" id="exercises">
        <SectionHeading id="exercises-title" kicker="VERTICAL EXERCISES" title="五条练习，把目录变成调用链">每条练习都要留下自己的流程图、断点 / 日志位置和可复现命令。只阅读不输出，往往会高估理解程度。</SectionHeading>
        <div className="exercise-list">
          {exercises.map(([letter, title, note]) => <article key={letter}><span>{letter}</span><div><h3>{title}</h3><p>{note}</p></div></article>)}
        </div>
        <Callout tone="violet" title="恢复练习使用独立状态目录">
          <p>先阅读 <code>docs/RECOVERY.zh-CN.md</code>，再让手工检查只接触练习状态；<code>check</code> 默认只读，只有 <code>repair</code>、<code>--apply</code> 或显式项目修复会产生变更。</p>
          <CopyCommand command="export REASONIX_HOME=/tmp/reasonix-recovery-lab" />
          <CopyCommand command="go run ./cmd/reasonix-guard check --root ." />
        </Callout>
      </section>

      <section className="content-section" id="tests">
        <SectionHeading id="tests-title" kicker="PROPORTIONAL VALIDATION" title="按改动半径选验证，不要只跑一个命令">最小聚焦测试提供快速反馈；合并前扩展测试证明相邻边界。嵌套模块与独立 Node / Worker 表面必须分别检查。</SectionHeading>
        <div className="table-wrap"><table><thead><tr><th>改动</th><th>最小反馈</th><th>合并前验证</th></tr></thead><tbody>{testMatrix.map((row) => <tr key={row[0]}>{row.map((cell, index) => <td key={cell}>{index === 0 ? <strong>{cell}</strong> : <code>{cell}</code>}</td>)}</tr>)}</tbody></table></div>
        <div className="command-cluster">
          <div><span>ROOT KERNEL</span><CopyCommand command="make build && make vet && make test" /></div>
          <div><span>FOCUSED</span><CopyCommand command="go test ./internal/control/ -run TestName -v" /></div>
          <div><span>CONCURRENCY</span><CopyCommand command="go test -race ./..." /></div>
          <div><span>RECOVERY</span><CopyCommand command="go test ./internal/repair ./cmd/reasonix-guard" /><CopyCommand command={'cd desktop && go test . -run \'Test(UpdateSiblingNamesCoverEveryReplacedEntryPoint|DesktopPackagesUseGuardAsDefaultLauncher|ShutdownDoesNotBlessStartupBeforeReady)\''} /></div>
        </div>
      </section>

      <section className="content-section" id="first-pr">
        <SectionHeading id="first-pr-title" kicker="FIRST PULL REQUEST" title="第一个 PR 要小，但必须完整">优先选择已有测试接缝、没有 schema / 持久化迁移的小问题。先补失败测试，再修改实现。</SectionHeading>
        <div className="pr-flow">
          <article><span>01</span><strong>确定边界</strong><p>写清产品面、层级、状态身份与不在范围内的内容。</p></article>
          <article><span>02</span><strong>建立失败证据</strong><p>聚焦测试或最小复现先证明问题存在。</p></article>
          <article><span>03</span><strong>最小修复</strong><p>通用行为进入共享层，不在单一前端复制。</p></article>
          <article><span>04</span><strong>扩展验证</strong><p>按矩阵覆盖缓存、安全、会话、平台与嵌套模块。</p></article>
          <article><span>05</span><strong>交接说明</strong><p>记录改了什么、如何验证、仍未证明什么。</p></article>
        </div>
        <div className="pr-checklist">
          <label><input type="checkbox" disabled /><span />改动落在正确层，没有复制通用逻辑</label>
          <label><input type="checkbox" disabled /><span />Tool / Provider / event wire 可见契约已同步</label>
          <label><input type="checkbox" disabled /><span />Cache、权限、session 与跨平台影响已评估</label>
          <label><input type="checkbox" disabled /><span />根模块与 desktop / Node 独立验证</label>
          <label><input type="checkbox" disabled /><span /><code>git diff --check</code> 通过，无生成物与无关噪声</label>
        </div>
      </section>

      <section className="content-section" id="progress">
        <ProgressChecklist />
        <div className="source-grid two"><SourceLink path="docs/MAINTAINER_GUIDE.zh-CN.md" label="阅读完整维护指南" /><SourceLink path="docs/RECOVERY.zh-CN.md" label="阅读恢复与安全模式" /><SourceLink path="CONTRIBUTING.md" label="查看贡献约定" /></div>
      </section>
    </PortalShell>
  );
}
