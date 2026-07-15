import type { Metadata } from "next";

import { CopyCommand } from "@/app/components/CopyCommand";
import { Callout, PageIntro, PortalShell, SectionHeading, SourceLink, Tag } from "@/app/components/PortalShell";

export const metadata: Metadata = { title: "桌面端" };

const keyFiles = [
  ["cmd/reasonix-guard/main.go", "恢复入口", "不加载 Wails / WebView 的启动、诊断、修复与回滚程序"],
  ["internal/repair/startup.go · update.go", "恢复状态", "启动健康、Safe Mode 阈值与完整发布单元回滚"],
  ["desktop/main.go", "原生壳", "窗口、embed 前端产物与 Wails 生命周期"],
  ["desktop/app.go", "命令面", "绑定给前端的方法；一个 App 管理多个 workspace tabs"],
  ["desktop/tabs.go", "运行身份", "tab / topic / session / runtime 与 Controller 装配"],
  ["frontend/src/lib/bridge.ts", "桥接 seam", "Wails bindings、runtime events 与 browser mock"],
  ["frontend/src/lib/useController.ts", "前端状态机", "每 tab 的事件 reducer 与运行状态"],
  ["frontend/src/App.tsx", "页面编排", "布局、导航与功能面组合"],
];

export default function DesktopPage() {
  return (
    <PortalShell active="/desktop" toc={[{ href: "#model", label: "桌面模型" }, { href: "#bridge", label: "Wails 桥" }, { href: "#files", label: "关键文件" }, { href: "#ownership", label: "状态归属" }, { href: "#recovery", label: "恢复启动链" }, { href: "#validation", label: "验证矩阵" }]}>
      <PageIntro eyebrow="04 · DESKTOP" title="原生壳，不是第二套 Agent" summary="Wails 将同一个 Go Controller 直接绑定给 React webview：Go 方法进入，类型化事件返回，中间没有 HTTP 跳转。桌面端的难点在跨语言契约、运行身份和目标系统原生构建。" meta="desktop/ 是独立 Go module · Wails v2 + React + TypeScript" />

      <section className="content-section" id="model">
        <SectionHeading id="model-title" kicker="NATIVE SHELL" title="共享内核，隔离构建">嵌套 module 让 Wails / CGO 构建与根 CLI 的静态二进制保证分离，同时仍可导入 <code>reasonix/internal/*</code>。</SectionHeading>
        <div className="desktop-stack">
          <article className="webview"><Tag tone="blue">WEBVIEW</Tag><strong>React + TypeScript</strong><span>bridge.ts · useController.ts · App.tsx</span></article>
          <div><span>bound methods ↓</span><span>↑ typed events</span></div>
          <article className="wails"><Tag tone="violet">WAILS</Tag><strong>App + WorkspaceTab + eventSink</strong><span>desktop/app.go · tabs.go · main.go</span></article>
          <div><span>boot options ↓</span><span>↑ event.Event</span></div>
          <article className="kernel"><Tag tone="green">SHARED KERNEL</Tag><strong>boot.Build → control.Controller</strong><span>与 CLI 共用 Provider、Tool、Gate、Session</span></article>
        </div>
        <Callout title="构建边界">
          <p>根目录 <code>go test ./...</code> 会跳过嵌套的 <code>desktop/</code> module。它通过，只能证明根 Go 内核；不能证明桌面 Go、前端或 Wails 原生产物。</p>
        </Callout>
      </section>

      <section className="content-section" id="bridge">
        <SectionHeading id="bridge-title" kicker="BRIDGE CONTRACT" title="一条桥，两种运行环境">原生运行时使用 Wails 生成绑定和事件；浏览器开发模式由 <code>bridge.ts</code> mock 同一份事件契约。</SectionHeading>
        <div className="bridge-flow">
          <article><span>01</span><strong>React action</strong><small>Submit / Cancel / Approve…</small></article><i>→</i>
          <article><span>02</span><strong>bridge.ts</strong><small>唯一前端桥接 seam</small></article><i>→</i>
          <article><span>03</span><strong>App method</strong><small>Wails bound Go method</small></article><i>→</i>
          <article><span>04</span><strong>Controller</strong><small>共享内核行为</small></article>
        </div>
        <div className="bridge-flow reverse">
          <article><span>08</span><strong>UI reducer</strong><small>按 tab / session 消费</small></article><i>←</i>
          <article><span>07</span><strong>agent:event</strong><small>runtime.EventsOn</small></article><i>←</i>
          <article><span>06</span><strong>eventSink</strong><small>附加 tabId，保持 FIFO</small></article><i>←</i>
          <article><span>05</span><strong>eventwire</strong><small>统一 JSON 形状</small></article>
        </div>
        <Callout tone="amber" title="事件必须走同一个 FIFO 队列">
          <p><code>runtime:rebuilt</code> 等控制事件也不能绕过队列。否则前端可能先看到新 runtime，再收到旧 runtime 的残余事件。</p>
        </Callout>
      </section>

      <section className="content-section" id="files">
        <SectionHeading id="files-title" kicker="KEY FILES" title="八组文件建立桌面心智模型" />
        <div className="file-cards">
          {keyFiles.map(([file, role, note]) => <article key={file}><code>{file}</code><Tag>{role}</Tag><p>{note}</p></article>)}
        </div>
      </section>

      <section className="content-section" id="ownership">
        <SectionHeading id="ownership-title" kicker="STATE OWNERSHIP" title="新增前端状态前，先选正确归属">不要默认丢进 React store。状态的生命周期决定它应该留在组件、布局、会话，还是 Go runtime。</SectionHeading>
        <div className="ownership-grid">
          <article><span>COMPONENT</span><h3>组件局部</h3><p>hover、临时展开、未提交输入等短命 UI 状态。</p><code>useState</code></article>
          <article><span>LAYOUT</span><h3>持久布局</h3><p>面板尺寸、主题、纯展示偏好，可随 workspace 或用户保存。</p><code>layout/settings store</code></article>
          <article><span>SESSION</span><h3>会话状态</h3><p>todo、history、运行态、证据，必须跟随精确 sessionPath。</p><code>session identity</code></article>
          <article><span>RUNTIME</span><h3>内核事实</h3><p>审批、goal、checkpoint 与执行状态由 Controller 负责。</p><code>Go controller</code></article>
        </div>
        <Callout tone="violet" title="异步结果需要二次验明身份">
          <p>事件到达、history 加载或导航恢复完成时，确认结果仍属于当前 tab 绑定的 <code>sessionPath</code>。Windows 路径还要考虑大小写与规范化语义。</p>
        </Callout>
      </section>

      <section className="content-section" id="recovery">
        <SectionHeading id="recovery-title" kicker="GUARDED LAUNCH" title="安装后的默认入口先经过 Guard">桌面快捷方式和 macOS Bundle 先进入 <code>reasonix-guard</code>；恢复层独立判断失败安装、启动健康与 Safe Mode，再决定是否启动 Wails 主程序。</SectionHeading>
        <div className="bridge-flow">
          <article><span>01</span><strong>Guard / launcher</strong><small>不加载 WebView</small></article><i>→</i>
          <article><span>02</span><strong>恢复检查</strong><small>失败安装 · crash loop</small></article><i>→</i>
          <article><span>03</span><strong>Desktop startup</strong><small>normal / Safe Mode</small></article><i>→</i>
          <article><span>04</span><strong>健康确认</strong><small>ready + 30s → healthy</small></article>
        </div>
        <Callout tone="amber" title="回滚单位不是单个桌面二进制">
          <p>Windows / Linux 的更新事务同时记录主程序与同批替换的 Guard、启动器、更新 helper；macOS 记录整个 App Bundle。任何目标越界、目录外备份、缺失哈希或恢复不完整都会被拒绝，避免新旧版本混装后继续启动。</p>
        </Callout>
      </section>

      <section className="content-section" id="validation">
        <SectionHeading id="validation-title" kicker="NATIVE VALIDATION" title="四条验证线，缺一条都不完整">浏览器 mock 适合快速迭代 UI，但不能验证原生绑定；前端测试不能证明 Go 嵌套模块；原生构建必须在目标 OS 工具链执行；恢复与更新还要证明完整发布单元契约。</SectionHeading>
        <div className="validation-lanes">
          <article><span>01</span><h3>Frontend</h3><CopyCommand command="pnpm --dir desktop/frontend build" /><CopyCommand command="pnpm --dir desktop/frontend test:all" /><p>覆盖 TypeScript、组件、事件 reducer 与样式契约。</p></article>
          <article><span>02</span><h3>Nested Go</h3><CopyCommand command="cd desktop && go vet ./... && go test ./..." /><p>覆盖 App、tab/session 语义、平台路径与 Go 绑定面。</p></article>
          <article><span>03</span><h3>Native Wails</h3><CopyCommand command="cd desktop && wails build -nocolour" /><p>在 Windows / macOS / Linux 对应原生环境证明真实产物。</p></article>
          <article><span>04</span><h3>Guard / recovery</h3><CopyCommand command="go test ./internal/repair ./cmd/reasonix-guard" /><CopyCommand command={'cd desktop && go test . -run \'Test(UpdateSiblingNamesCoverEveryReplacedEntryPoint|DesktopPackagesUseGuardAsDefaultLauncher|ShutdownDoesNotBlessStartupBeforeReady)\''} /><p>覆盖 Guard CLI、Safe Mode 阈值、启动健康，以及发布包和回滚单元的一致性。</p></article>
        </div>
        <div className="source-grid two"><SourceLink path="desktop/README.md" label="查看完整开发说明" /><SourceLink path="docs/RECOVERY.zh-CN.md" label="查看恢复契约" /><SourceLink path=".github/workflows/release-desktop.yml" label="查看发布构建事实" /></div>
      </section>
    </PortalShell>
  );
}
