import Image from "next/image";
import Link from "next/link";
import type { ReactNode } from "react";

import { SearchPanel } from "@/app/components/SearchPanel";
import { baseline, navItems, repositoryUrl } from "@/app/content";

type TocItem = { href: string; label: string };

export function PortalShell({
  active,
  children,
  toc = [],
}: {
  active: string;
  children: ReactNode;
  toc?: TocItem[];
}) {
  const currentIndex = navItems.findIndex((item) => item.href === active);
  const previous = currentIndex > 0 ? navItems[currentIndex - 1] : null;
  const next = currentIndex >= 0 && currentIndex < navItems.length - 1 ? navItems[currentIndex + 1] : null;

  return (
    <div className="site-shell">
      <header className="topbar">
        <Link className="brand" href="/" aria-label="Reasonix Developer Atlas 首页">
          <span className="brand-mark">
            <Image alt="" height={28} priority src="/reasonix-logo.svg" width={28} />
          </span>
          <span>
            <strong>Reasonix</strong>
            <small>Developer Atlas</small>
          </span>
        </Link>
        <SearchPanel />
        <nav className="top-actions" aria-label="项目链接">
          <a href={`${repositoryUrl}/tree/main-v2/docs`} target="_blank" rel="noreferrer">
            设计文档
          </a>
          <a className="github-link" href={repositoryUrl} target="_blank" rel="noreferrer">
            GitHub <span aria-hidden="true">↗</span>
          </a>
        </nav>
      </header>

      <nav className="mobile-chapters" aria-label="章节导航">
        {navItems.map((item) => (
          <Link aria-current={item.href === active ? "page" : undefined} href={item.href} key={item.href}>
            {item.label}
          </Link>
        ))}
      </nav>

      <div className="portal-grid">
        <aside className="sidebar">
          <div className="sidebar-label">开发地图</div>
          <nav aria-label="主导航">
            {navItems.map((item) => (
              <Link
                aria-current={item.href === active ? "page" : undefined}
                className={item.href === active ? "active" : ""}
                href={item.href}
                key={item.href}
              >
                <span>{item.eyebrow}</span>
                <div>
                  <strong>{item.label}</strong>
                  <small>{item.description}</small>
                </div>
              </Link>
            ))}
          </nav>
          <div className="baseline-card">
            <span>DOCUMENT BASELINE</span>
            <strong>{baseline}</strong>
            <small>整理于 2026-07-15</small>
          </div>
        </aside>

        <main className="main-content">
          {children}

          <nav className="page-switcher" aria-label="相邻章节">
            {previous ? (
              <Link href={previous.href}>
                <span>← 上一章</span>
                <strong>{previous.label}</strong>
              </Link>
            ) : (
              <span />
            )}
            {next ? (
              <Link href={next.href}>
                <span>下一章 →</span>
                <strong>{next.label}</strong>
              </Link>
            ) : null}
          </nav>

          <footer>
            <p>这是一份新人导航，不替代代码、测试与 CI。发现偏差时，请以可执行证据为准并同步修正文档。</p>
            <a href={`${repositoryUrl}/issues`} target="_blank" rel="noreferrer">
              反馈文档问题 ↗
            </a>
          </footer>
        </main>

        <aside className="toc" aria-label="本页目录">
          <span>本页目录</span>
          {toc.map((item) => (
            <a href={item.href} key={item.href}>
              {item.label}
            </a>
          ))}
          <a className="toc-top" href="#top">
            ↑ 回到顶部
          </a>
        </aside>
      </div>
    </div>
  );
}

export function PageIntro({
  eyebrow,
  title,
  summary,
  meta,
}: {
  eyebrow: string;
  title: string;
  summary: string;
  meta?: string;
}) {
  return (
    <header className="page-intro" id="top">
      <div className="eyebrow">{eyebrow}</div>
      <h1>{title}</h1>
      <p>{summary}</p>
      {meta ? <div className="page-meta">{meta}</div> : null}
    </header>
  );
}

export function SectionHeading({
  id,
  kicker,
  title,
  children,
}: {
  id: string;
  kicker: string;
  title: string;
  children?: ReactNode;
}) {
  return (
    <header className="section-heading" id={id}>
      <span className="kicker">{kicker}</span>
      <h2>{title}</h2>
      {children ? <p>{children}</p> : null}
    </header>
  );
}

export function Callout({
  tone = "blue",
  title,
  children,
}: {
  tone?: "blue" | "amber" | "violet" | "green";
  title: string;
  children: ReactNode;
}) {
  return (
    <aside className={`callout ${tone}`}>
      <strong>{title}</strong>
      <div>{children}</div>
    </aside>
  );
}

export function SourceLink({ path, label }: { path: string; label?: string }) {
  return (
    <a className="source-link" href={`${repositoryUrl}/blob/main-v2/${path}`} target="_blank" rel="noreferrer">
      <code>{path}</code>
      <span>{label ?? "查看源码"} ↗</span>
    </a>
  );
}

export function Tag({ children, tone = "default" }: { children: ReactNode; tone?: string }) {
  return <span className={`tag ${tone}`}>{children}</span>;
}
