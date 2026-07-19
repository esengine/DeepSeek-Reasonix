import type { Metadata } from "next";

import { CopyCommand } from "@/app/components/CopyCommand";
import { Callout, PageIntro, PortalShell, SectionHeading, SourceLink, Tag } from "@/app/components/PortalShell";

export const metadata: Metadata = { title: "生态与交付" };

const surfaces = [
  ["Root Go module", "本地核心", "CLI、Agent 内核、多前端共享领域逻辑", "必需"],
  ["desktop/", "本地产品", "Wails 原生壳与 React UI", "桌面需要"],
  ["site/", "Web 门户", "官网、下载、账户 / 论坛 / registry UI", "可选"],
  ["workers/accounts", "云服务", "身份、session、邮箱验证、device auth 服务端", "可选"],
  ["workers/forum", "云服务", "社区内容、角色与 trust", "可选"],
  ["workers/crash-report", "云服务", "报告、指标、更新网关、registry 与管理面", "可选"],
  ["npm/", "分发", "平台二进制选择与 npm 安装渠道", "可选"],
];

export default function EcosystemPage() {
  return (
    <PortalShell active="/ecosystem" toc={[{ href: "#boundary", label: "本地 / 云端边界" }, { href: "#services", label: "服务关系" }, { href: "#delivery", label: "发布线" }, { href: "#ci", label: "CI 覆盖" }, { href: "#validate", label: "独立验证" }]}>
      <PageIntro eyebrow="06 · ECOSYSTEM" title="本地内核可以独立运行" summary="官网、账户、论坛和 registry 都不是本地 Agent 启动依赖。维护生态时，先确认自己正在改本地产品、在线服务，还是分发链路。" meta="三条产品发布线 · CLI / npm / Desktop" />

      <section className="content-section" id="boundary">
        <SectionHeading id="boundary-title" kicker="SYSTEM BOUNDARY" title="不要把仓库等同于一个部署单元">同一仓库包含多个模块、运行环境和交付责任。某个 Worker 不可用，不等于本地 CLI 内核不能运行。</SectionHeading>
        <div className="ecosystem-map">
          <article className="local"><Tag tone="green">LOCAL PRODUCT</Tag><h3>CLI + Desktop</h3><p>共享 Go kernel，接入本地配置、会话、Skills、MCP 与 plugin packages。</p><div><span>CLI / TUI</span><span>Wails Desktop</span><span>Shared Kernel</span></div></article>
          <div className="optional-link"><span>可选在线能力</span><i>⇄</i></div>
          <article className="cloud"><Tag tone="blue">WEB & CLOUD</Tag><h3>Site + Workers</h3><p>门户组合账户、社区、报告、更新与 registry 服务。</p><div><span>Accounts</span><span>Forum</span><span>Gateway</span></div></article>
          <div className="distribution"><Tag tone="violet">DISTRIBUTION</Tag><span>Go releases</span><span>npm packages</span><span>Desktop GitHub / R2</span></div>
        </div>
        <div className="table-wrap"><table><thead><tr><th>表面</th><th>分类</th><th>主要用途</th><th>本地 Agent</th></tr></thead><tbody>{surfaces.map((row) => <tr key={row[0]}>{row.map((cell, index) => <td key={cell}>{index === 0 ? <code>{cell}</code> : cell}</td>)}</tr>)}</tbody></table></div>
        <Callout tone="amber" title="服务端存在 ≠ 客户端已支持">
          <p>accounts Worker 已有 device flow 服务端，但当前根 CLI / desktop 没有对应客户端调用。文档和产品说明不能把服务端 API 写成已完成的本地登录能力。</p>
        </Callout>
      </section>

      <section className="content-section" id="services">
        <SectionHeading id="services-title" kicker="SERVICE RELATIONSHIPS" title="身份只有一个权威">Accounts 保存身份；Forum 委托认证并只保存社区领域数据；Crash-report Worker 聚合多个在线子域。</SectionHeading>
        <div className="service-graph">
          <article className="site-node"><strong>Astro Site</strong><small>门户与 UI 组合层</small></article>
          <div className="service-branches"><span>↓</span><span>↓</span><span>↓</span></div>
          <div className="service-nodes">
            <article><Tag tone="blue">AUTHORITY</Tag><strong>Accounts</strong><small>账户 · Web session · email · device auth</small></article>
            <article><Tag>DELEGATED</Tag><strong>Forum</strong><small>转 Bearer 调 accounts /me</small></article>
            <article><Tag tone="violet">GATEWAY</Tag><strong>Crash / Registry</strong><small>报告 · 指标 · 更新 · registry · 运维</small></article>
          </div>
        </div>
        <Callout title="Crash-report Worker 的故障半径较大">
          <p>修改时写清路由子域、D1 binding、环境变量、认证方式与数据保留策略。registry 元数据和 source pointer 在云端，真正获取与安装仍由本地 <code>install_source</code> 完成。</p>
        </Callout>
      </section>

      <section className="content-section" id="delivery">
        <SectionHeading id="delivery-title" kicker="RELEASE LANES" title="三条发布线，三个 tag 命名空间">普通 PR 的“代码可构建”不等于真实发布已验证；签名、公证、secrets、受保护 environment 与外部 R2 状态属于发布流程。</SectionHeading>
        <div className="release-lanes">
          <article><Tag tone="blue">CLI</Tag><h3>v&lt;semver&gt;</h3><code>release.yml + GoReleaser</code><p>archives · checksums · Homebrew</p></article>
          <article><Tag tone="violet">NPM</Tag><h3>npm-v&lt;semver&gt;</h3><code>release-npm.yml + build.mjs</code><p>JS shim · 6 platform packages</p></article>
          <article><Tag tone="green">DESKTOP</Tag><h3>desktop-v&lt;semver&gt;</h3><code>release-desktop.yml</code><p>桌面 + Guard/启动器 · 签名 · manifest · R2</p></article>
        </div>
        <p className="body-copy">Wails / CGO 产物使用各目标系统的原生 runner，不能从 Linux 一次性交叉编译全部 GUI 产物。稳定版、RC 和 canary 对 dist-tag 与 R2 pointer 的影响，以当前 workflow 和 <code>RELEASING.md</code> 为准。</p>
        <Callout tone="amber" title="桌面更新以完整发布单元为边界">
          <p>Windows / Linux 的回滚覆盖桌面主程序及安装器同批替换的 Guard / 启动器二进制，macOS 覆盖整个 App Bundle。备份只有在新版本进入 <code>healthy</code> 或干净退出后才清理；失败安装会由 helper 留下标记，让 Guard 在下次启动前立即恢复，而不是等待再次崩溃。</p>
        </Callout>
      </section>

      <section className="content-section" id="ci">
        <SectionHeading id="ci-title" kicker="CI EVIDENCE" title="CI 已证明什么，又没有证明什么" />
        <div className="ci-columns">
          <article><header><span className="status-dot stable" /><h3>已有主门禁</h3></header><ul><li>根 Go：Linux / macOS / Windows vet、build、test</li><li>Linux race、lint 与 cache guard</li><li>Desktop：Linux 前端 build、Go vet / lint / build / test</li><li>Windows 路径语义 tests</li><li>Site 安全敏感 Node tests</li></ul></article>
          <article><header><span className="status-dot dynamic" /><h3>当前覆盖缺口</h3></header><ul><li>Desktop PR CI 未运行 frontend test:all</li><li>accounts / forum 未进入主 PR typecheck gate</li><li>crash Worker 检查主要位于部署 workflow</li><li>Worker 数据库 migration 不随 deploy 自动应用</li><li>前端新测试需手动确认加入显式 suite</li></ul></article>
        </div>
        <Callout tone="violet" title="缺少自动证明，不等于已经错误">
          <p>把缺口作为风险标签与人工验证依据。高收益改进是把 frontend / Worker 检查移入 PR gate，并建立数据库 migration 台账和兼容矩阵。</p>
        </Callout>
      </section>

      <section className="content-section" id="validate">
        <SectionHeading id="validate-title" kicker="INDEPENDENT CHECKS" title="每个产品面单独验证" />
        <div className="validation-lanes ecosystem-validation">
          <article><span>ROOT</span><h3>Go kernel</h3><CopyCommand command="make vet && make test && make build" /></article>
          <article><span>SITE</span><h3>Astro portal</h3><CopyCommand command="cd site && npm ci && npm test && npm run build" /></article>
          <article><span>WORKERS</span><h3>Cloudflare</h3><CopyCommand command="cd workers/crash-report && npm ci && npm run typecheck && npm test" /></article>
          <article><span>RECOVERY</span><h3>Release unit</h3><CopyCommand command="go test ./internal/repair ./cmd/reasonix-guard" /><CopyCommand command="cd desktop && go test ./..." /></article>
        </div>
        <p className="footnote">数据库 migration、Worker deploy、真实 Provider E2E 和发布 workflow 会访问外部状态、使用 secret 或产生费用，执行前需单独确认授权、环境与回滚路径。</p>
        <div className="source-grid two"><SourceLink path="docs/ECOSYSTEM.zh-CN.md" label="阅读完整生态地图" /><SourceLink path="docs/RECOVERY.zh-CN.md" label="阅读恢复与回滚契约" /><SourceLink path="RELEASING.md" label="阅读发布契约" /></div>
      </section>
    </PortalShell>
  );
}
