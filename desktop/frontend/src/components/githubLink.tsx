import type { MouseEvent as ReactMouseEvent, ReactNode } from "react";
import { ExternalLink, Mail } from "lucide-react";
import { openExternal } from "../lib/bridge";

export interface GitHubLinkInfo {
  kind: "issue" | "pull" | "commit";
  owner: string;
  repo: string;
  value: string;
  compactLabel: string;
}

export type LinkIconKind = "github" | "external" | "mail";

function linkText(children: ReactNode): string {
  if (typeof children === "string") return children;
  if (typeof children === "number") return String(children);
  if (!Array.isArray(children)) return "";
  return children
    .map((child) => typeof child === "string" || typeof child === "number" ? String(child) : "")
    .join("");
}

function githubAccessibleLabel(info: GitHubLinkInfo): string {
  const resource = info.kind === "pull"
    ? `pull request #${info.value}`
    : info.kind === "issue"
      ? `issue #${info.value}`
      : `commit ${info.compactLabel}`;
  return `GitHub ${info.owner}/${info.repo} ${resource}`;
}

export function classifyLinkIcon(href?: string): LinkIconKind | null {
  if (!href) return null;
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return null;
  }

  if (url.protocol === "mailto:") return "mail";
  if (url.protocol !== "https:" && url.protocol !== "http:") return null;
  if (url.protocol === "https:" && ["github.com", "www.github.com"].includes(url.hostname.toLowerCase())) {
    return "github";
  }
  return "external";
}

export function parseGitHubLink(href?: string): GitHubLinkInfo | null {
  if (!href) return null;
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return null;
  }
  if (url.protocol !== "https:" || !["github.com", "www.github.com"].includes(url.hostname.toLowerCase())) {
    return null;
  }

  const parts = url.pathname.split("/").filter(Boolean);
  if (parts.length !== 4) return null;
  const [owner, repo, resource, value] = parts;
  if (!owner || !repo || !value) return null;

  if (resource === "issues" && /^\d+$/.test(value)) {
    return { kind: "issue", owner, repo, value, compactLabel: `#${value}` };
  }
  if (resource === "pull" && /^\d+$/.test(value)) {
    return { kind: "pull", owner, repo, value, compactLabel: `PR #${value}` };
  }
  if (resource === "commit" && /^[0-9a-f]{7,40}$/i.test(value)) {
    return { kind: "commit", owner, repo, value, compactLabel: value.slice(0, 7) };
  }
  return null;
}

function LinkMark({ kind }: { kind: LinkIconKind }) {
  if (kind === "github") {
    return (
      <svg aria-hidden="true" fill="none" height="13" viewBox="0 0 24 24" width="13">
        <path
          d="M15 22v-4a4.8 4.8 0 0 0-1-3.5c3.3-.4 6.8-1.6 6.8-7A5.4 5.4 0 0 0 19.4 4 5 5 0 0 0 19.3.5S18.2.1 15 1.8a13.4 13.4 0 0 0-7 0C4.8.1 3.7.5 3.7.5A5 5 0 0 0 3.6 4a5.4 5.4 0 0 0-1.4 3.7c0 5.4 3.5 6.6 6.8 7A4.8 4.8 0 0 0 8 18v4"
          stroke="currentColor"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth="2"
        />
        <path
          d="M8 19c-3 .9-3-1.5-4-2"
          stroke="currentColor"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth="2"
        />
      </svg>
    );
  }
  if (kind === "mail") return <Mail aria-hidden="true" size={13} strokeWidth={2} />;
  return <ExternalLink aria-hidden="true" size={13} strokeWidth={2} />;
}

function openLink(href: string | undefined) {
  if (href) openExternal(href);
}

export function RichMarkdownLink({
  href,
  children,
}: {
  href?: string;
  children: ReactNode;
}) {
  const github = parseGitHubLink(href);
  const iconKind = classifyLinkIcon(href);
  const compactLabel = github && linkText(children) === href ? github.compactLabel : undefined;
  const accessibleLabel = github ? githubAccessibleLabel(github) : undefined;
  const handlers = {
    onClick: (event: ReactMouseEvent<HTMLAnchorElement>) => {
      event.preventDefault();
      openLink(href);
    },
    onAuxClick: (event: ReactMouseEvent<HTMLAnchorElement>) => {
      event.preventDefault();
      openLink(href);
    },
    onMouseDown: (event: ReactMouseEvent<HTMLAnchorElement>) => {
      if (event.button === 1) event.preventDefault();
    },
  };

  if (!iconKind) {
    return <a href={href} {...handlers}>{children}</a>;
  }

  return (
    <a
      aria-label={compactLabel ? accessibleLabel : undefined}
      className={`md-rich-link md-rich-link--${iconKind}`}
      href={href}
      title={github ? accessibleLabel : undefined}
      {...handlers}
    >
      <LinkMark kind={iconKind} />
      <span
        className="md-rich-link__label"
        data-display-label={compactLabel}
      >
        {children}
      </span>
    </a>
  );
}
