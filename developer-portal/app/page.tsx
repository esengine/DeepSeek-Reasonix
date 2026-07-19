import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";

import { CopyCommand } from "@/app/components/CopyCommand";
import { Callout, PortalShell, Tag } from "@/app/components/PortalShell";
import { navItems, repositoryUrl } from "@/app/content";

export const metadata: Metadata = { title: "项目入口" };

const routes = [
  {
    title: "我要理解一次请求",
    description: "从 CLI/桌面入口跟到模型流、工具调用与会话落盘。",
    href: "/runtime",
    tag: "核心必读",
  },
  {
    title: "我要修改桌面界面",
    description: "先辨认 React、Wails 绑定、Go 控制器和会话状态的归属。",
    href: "/desktop",
    tag: "跨语言",
  },
  {
    title: "我要增加新能力",
    description: "判断需求属于 Provider、Tool、MCP、Skill、Agent 还是插件包。",
    href: "/extensions",
    tag: "扩展点",
  },
  {
    title: "我要排查启动或更新故障",
    description: "区分 session checkpoint 与应用级 Guard、Safe Mode、修复和回滚。",
    href: "/state-safety#recovery",
    tag: "恢复路径",
  },
  {
    title: "我要提交第一个 PR",
    description: "按改动范围选择测试线，完成一个可验证的小闭环。",
    href: "/develop",
    tag: "动手路径",
  },
];

export default function Home() {
  return (
    <PortalShell active="/" toc={[{ href: "#map", label: "项目地图" }, { href: "#routes", label: "任务入口" }, { href: "#truth", label: "维护原则" }, { href: "#quickstart", label: "本地启动" }]}>
      <section className="home-hero" id="top">
        <div className="hero-copy">
          <div className="eyebrow">REASONIX · MAINTAINER ONBOARDING</div>
          <h1>
            从一次用户回合，
            <span>理解整个项目。</span>
          </h1>
          <p>
            这不是目录清单，而是一张面向开发任务的可跳转地图。先建立全局心智模型，再沿着运行时主链路进入你要维护的模块。
          </p>
          <div className="hero-actions">
            <Link className="button primary" href="/architecture">
              开始阅读 <span aria-hidden="true">→</span>
            </Link>
            <a className="button secondary" href={repositoryUrl} target="_blank" rel="noreferrer">
              打开仓库 <span aria-hidden="true">↗</span>
            </a>
          </div>
          <div className="hero-facts" aria-label="项目事实">
            <div><strong>Go</strong><span>核心编排</span></div>
            <div><strong>Wails + React</strong><span>桌面端</span></div>
            <div><strong>CLI + Guard</strong><span>运行与恢复入口</span></div>
          </div>
        </div>
        <div className="hero-visual" aria-label="Reasonix 分层架构示意">
          <Image alt="Reasonix 开发架构抽象地图" fill priority sizes="(max-width: 900px) 100vw, 44vw" src="/og.png" />
          <div className="visual-caption">
            <span>01 — 07</span>
            <strong>一张持续更新的开发地图</strong>
          </div>
        </div>
      </section>

      <section className="content-section" id="map">
        <header className="section-heading compact">
          <span className="kicker">SYSTEM MAP</span>
          <h2>先记住这条主链路</h2>
          <p>几乎所有维护任务，都可以从这条链路找到最初的落点。</p>
        </header>
        <div className="turn-ribbon" aria-label="Reasonix 核心运行链路">
          <Link href="/architecture#bootstrap"><span>01</span><strong>入口装配</strong><small>CLI / Desktop</small></Link>
          <i aria-hidden="true">→</i>
          <Link href="/runtime#turn"><span>02</span><strong>回合编排</strong><small>Agent loop</small></Link>
          <i aria-hidden="true">→</i>
          <Link href="/runtime#tools"><span>03</span><strong>模型与工具</strong><small>Stream / Calls</small></Link>
          <i aria-hidden="true">→</i>
          <Link href="/state-safety#sidecars"><span>04</span><strong>状态落盘</strong><small>Session / Sidecar</small></Link>
        </div>
        <Callout title="维护时的最短判断">
          <p>先问：“改动发生在主链路的哪一步？它读取和写入的是谁的状态？失败后由哪一层负责恢复？” 这三个问题通常比从目录名猜职责更可靠。</p>
        </Callout>
      </section>

      <section className="content-section" id="routes">
        <header className="section-heading compact">
          <span className="kicker">CHOOSE YOUR ROUTE</span>
          <h2>从你要完成的任务出发</h2>
          <p>无需线性读完所有文档。先选一条路线，再回到架构页补齐上下文。</p>
        </header>
        <div className="route-grid">
          {routes.map((route, index) => (
            <Link href={route.href} key={route.title}>
              <div className="route-index">0{index + 1}</div>
              <Tag tone={index === 0 ? "blue" : "default"}>{route.tag}</Tag>
              <h3>{route.title}</h3>
              <p>{route.description}</p>
              <span className="route-arrow" aria-hidden="true">↗</span>
            </Link>
          ))}
        </div>
      </section>

      <section className="content-section" id="truth">
        <header className="section-heading compact">
          <span className="kicker">MAINTENANCE CONTRACT</span>
          <h2>接手项目时，先接受三个事实</h2>
        </header>
        <div className="principle-grid">
          <article>
            <span>01</span>
            <h3>证据有优先级</h3>
            <p>实际代码与测试高于解释性文档；CI 与构建脚本高于口头流程。文档冲突时，要修正文档而不是解释冲突。</p>
          </article>
          <article>
            <span>02</span>
            <h3>状态必须有身份</h3>
            <p>界面容器不等于业务会话。活动会话能在同一 tab 中切换时，业务状态应以 sessionPath 或 topic identity 隔离。</p>
          </article>
          <article>
            <span>03</span>
            <h3>验证按产品面分线</h3>
            <p>根目录 Go 测试不能证明嵌套 Wails 桌面应用可构建；前端检查也不能替代目标系统的原生构建。</p>
          </article>
        </div>
      </section>

      <section className="content-section quickstart" id="quickstart">
        <div>
          <span className="kicker">LOCAL QUICKSTART</span>
          <h2>把文档与代码同时打开</h2>
          <p>建议先跑 CLI，随后用调试器或日志跟踪一个最小回合。桌面端是独立的原生应用工作区，需要对应系统的工具链。</p>
        </div>
        <div className="command-stack">
          <CopyCommand command="go test ./..." />
          <CopyCommand command="go run ./cmd/reasonix --help" />
          <CopyCommand command="cd desktop && pnpm install" />
        </div>
      </section>

      <section className="chapter-strip" aria-label="全部章节">
        {navItems.slice(1).map((item) => (
          <Link href={item.href} key={item.href}>
            <span>{item.eyebrow}</span>
            <strong>{item.label}</strong>
            <small>{item.description}</small>
          </Link>
        ))}
      </section>
    </PortalShell>
  );
}
