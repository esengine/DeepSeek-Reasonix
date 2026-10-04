import { cleanTitle, type NewEvent } from "./events";

function hex(bytes: ArrayBuffer): string {
  return [...new Uint8Array(bytes)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

export function equalSecret(supplied: string, expected: string): boolean {
  if (!expected || supplied.length !== expected.length) return false;
  let difference = 0;
  for (let index = 0; index < expected.length; index += 1) difference |= supplied.charCodeAt(index) ^ expected.charCodeAt(index);
  return difference === 0;
}

// GitHub signs the raw body; the signature is checked before the body is parsed.
export async function verifySignature(secret: string, rawBody: string, header: string | null): Promise<boolean> {
  if (!secret || !header || !header.startsWith("sha256=")) return false;
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const expected = `sha256=${hex(await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(rawBody)))}`;
  return equalSecret(header, expected);
}

type Json = Record<string, unknown>;
const obj = (v: unknown): Json => (v && typeof v === "object" ? (v as Json) : {});
const str = (v: unknown): string | undefined => (typeof v === "string" ? v : undefined);
const num = (v: unknown): number | undefined => (typeof v === "number" ? v : undefined);

export interface MapOptions {
  ignoreLogins: Set<string>;
  ciWorkflows: Set<string>;
}

function base(payload: Json, t: string): NewEvent {
  return { src: "github", t, repo: str(obj(payload.repository).full_name), by: str(obj(payload.sender).login) };
}

// Maps a webhook delivery to one compact event, or null when it is not one we
// act on. Only the title is free text; bodies and comments are never copied.
export function mapGithubEvent(name: string, payload: Json, opts: MapOptions): NewEvent | null {
  const action = str(payload.action) ?? "";
  const sender = obj(payload.sender);
  const login = str(sender.login) ?? "";
  const isBot = str(sender.type) === "Bot" || opts.ignoreLogins.has(login);
  const issue = obj(payload.issue);
  const pr = obj(payload.pull_request);
  switch (name) {
    case "issues":
      if (!["opened", "reopened", "closed"].includes(action)) return null;
      return { ...base(payload, `issue.${action}`), n: num(issue.number), title: cleanTitle(issue.title), url: str(issue.html_url) };
    case "issue_comment":
      if (action !== "created" || isBot) return null;
      return {
        ...base(payload, "comment"),
        n: num(issue.number),
        title: cleanTitle(issue.title),
        url: str(obj(payload.comment).html_url),
        extra: { on: issue.pull_request ? "pr" : "issue" },
      };
    case "pull_request": {
      if (!["opened", "reopened", "synchronize", "ready_for_review", "closed"].includes(action)) return null;
      const merged = pr.merged === true;
      const t = action === "closed" ? (merged ? "pr.merged" : "pr.closed") : `pr.${action}`;
      return {
        ...base(payload, t),
        n: num(pr.number),
        title: cleanTitle(pr.title),
        url: str(pr.html_url),
        extra: { draft: pr.draft === true, base: str(obj(pr.base).ref) ?? "", head: (str(obj(pr.head).sha) ?? "").slice(0, 9) },
      };
    }
    case "pull_request_review":
      if (action !== "submitted" || isBot) return null;
      return { ...base(payload, "pr.review"), n: num(pr.number), title: cleanTitle(pr.title), url: str(obj(payload.review).html_url), extra: { state: str(obj(payload.review).state) ?? "" } };
    case "pull_request_review_comment":
      if (action !== "created" || isBot) return null;
      return { ...base(payload, "pr.review_comment"), n: num(pr.number), title: cleanTitle(pr.title), url: str(obj(payload.comment).html_url) };
    case "workflow_run": {
      const run = obj(payload.workflow_run);
      if (action !== "completed" || !opts.ciWorkflows.has(str(run.name) ?? "")) return null;
      const prs = Array.isArray(run.pull_requests) ? run.pull_requests.map((p) => num(obj(p).number)).filter((x): x is number => x !== undefined) : [];
      return {
        ...base(payload, `ci.${str(run.conclusion) ?? "unknown"}`),
        n: prs[0],
        title: cleanTitle(run.name),
        url: str(run.html_url),
        extra: { branch: str(run.head_branch) ?? "", sha: (str(run.head_sha) ?? "").slice(0, 9), prs: prs.slice(0, 5).join(",") },
      };
    }
    case "discussion":
      if (action !== "created") return null;
      return { ...base(payload, "discussion.created"), n: num(obj(payload.discussion).number), title: cleanTitle(obj(payload.discussion).title), url: str(obj(payload.discussion).html_url) };
    case "discussion_comment":
      if (action !== "created" || isBot) return null;
      return { ...base(payload, "discussion.comment"), n: num(obj(payload.discussion).number), title: cleanTitle(obj(payload.discussion).title), url: str(obj(payload.comment).html_url) };
    case "release":
      if (action !== "published") return null;
      return { ...base(payload, "release.published"), title: cleanTitle(obj(payload.release).name ?? obj(payload.release).tag_name), url: str(obj(payload.release).html_url) };
    default:
      return null;
  }
}
