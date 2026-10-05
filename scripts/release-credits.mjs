// Resolves the #N references in release notes to the people they credit: a
// pull request's author, or for an issue the authors of the merged pull
// requests that closed it. A number that names a discussion or nothing stays
// plain with a warning; a lookup GitHub could not answer fails by code.

import { execFileSync } from "node:child_process";

export const defaultRepository = "esengine/DeepSeek-Reasonix";

export const CreditErrorCode = Object.freeze({
  noToken: "release_credits.no_token",
  unreachable: "release_credits.api_unreachable",
  status: "release_credits.api_status",
  notFound: "release_credits.ref_not_found",
  discussion: "release_credits.ref_is_discussion",
  unresolved: "release_credits.unresolved",
});

export class CreditError extends Error {
  constructor(code, message, { ref, failures } = {}) {
    super(message);
    this.name = "CreditError";
    this.code = code;
    if (ref !== undefined) this.ref = ref;
    if (failures !== undefined) this.failures = failures;
  }
}

const botLogins = new Set(["github-actions", "dependabot", "dependabot-preview"]);

export function isBotAccount(login, type) {
  if (type === "Bot") return true;
  const name = String(login).toLowerCase();
  return name.endsWith("[bot]") || botLogins.has(name);
}

// Accounts left uncredited like bots: the repository owner, plus the logins in
// RELEASE_CREDIT_EXCLUDE (comma separated). Compared case-insensitively.
export function creditExclusions(repository = defaultRepository, env = process.env) {
  const extra = String(env.RELEASE_CREDIT_EXCLUDE || "").split(",");
  const names = [repository.split("/")[0], ...extra].map((name) => name.trim().toLowerCase());
  return new Set(names.filter(Boolean));
}

export function tokenFromEnvironment(env = process.env, runGh = defaultRunGh) {
  const token = env.GH_TOKEN || env.GITHUB_TOKEN;
  if (token) return token;
  try {
    const fromGh = runGh().trim();
    if (fromGh) return fromGh;
  } catch {
    // gh missing or signed out; reported below as no_token.
  }
  throw new CreditError(
    CreditErrorCode.noToken,
    "no GitHub token: set GH_TOKEN (in CI: ${{ github.token }}), or sign in with `gh auth login`",
  );
}

function defaultRunGh() {
  return execFileSync("gh", ["auth", "token"], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] });
}

const refQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    discussion(number: $number) { number }
    issueOrPullRequest(number: $number) {
      __typename
      ... on PullRequest { author { login __typename } }
      ... on Issue {
        closedByPullRequestsReferences(first: 20, includeClosedPrs: true) {
          nodes { number merged author { login __typename } }
        }
        timelineItems(itemTypes: [CLOSED_EVENT], last: 1) {
          nodes { ... on ClosedEvent { closer { __typename ... on PullRequest { number merged author { login __typename } } } } }
        }
      }
    }
  }
}`;

// `bot` is true for every account that is never credited, excluded humans included.
function person(author, excluded) {
  if (!author) return { login: null, bot: true };
  const bot = isBotAccount(author.login, author.__typename) || excluded.has(author.login.toLowerCase());
  return { login: author.login, bot };
}

// An issue's fixers are the merged pull requests GitHub links to it as closing
// it: the one that closed it, and any linked by a closing keyword or by hand.
// A mere mention is not a fix, so cross-references are not read.
function issueFixes(issue, excluded) {
  const closer = issue.timelineItems?.nodes?.[0]?.closer;
  const candidates = [...(issue.closedByPullRequestsReferences?.nodes || [])];
  if (closer?.__typename === "PullRequest") candidates.push(closer);
  const fixes = new Map();
  for (const pull of candidates) {
    if (pull?.merged && !fixes.has(pull.number)) fixes.set(pull.number, { number: pull.number, ...person(pull.author, excluded) });
  }
  return [...fixes.values()].sort((a, b) => a.number - b.number);
}

const defaultSleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// A 5xx, a 429, a 403 that carries rate-limit headers, a GraphQL RATE_LIMITED
// error or a dropped connection is transient; anything else is final.
function retryDelay(response, payload, attempt) {
  const backoff = 1000 * 2 ** attempt;
  if (!response) return backoff;
  const header = (name) => response.headers?.get?.(name) ?? null;
  const after = header("retry-after") === null ? null : Number(header("retry-after"));
  const limited =
    response.status === 429 ||
    (response.status === 403 && (after !== null || header("x-ratelimit-remaining") === "0")) ||
    payload?.errors?.some((error) => error.type === "RATE_LIMITED");
  if (!limited && response.status < 500) return null;
  return after > 0 ? Math.min(after * 1000, 60_000) : backoff;
}

// Returns an async lookup: number -> { kind: "pull", login, bot } |
// { kind: "issue", fixes: [{ number, login, bot }] } | { kind: "discussion" } |
// { kind: "missing" }.
export function githubRefLookup({
  repository = defaultRepository,
  token,
  fetchImpl = fetch,
  sleep = defaultSleep,
  attempts = 4,
  excluded = creditExclusions(repository),
} = {}) {
  const [owner, name] = repository.split("/");
  const headers = { "Content-Type": "application/json", "User-Agent": "reasonix-release-credits" };
  if (token) headers.Authorization = `Bearer ${token}`;
  const body = (ref) => JSON.stringify({ query: refQuery, variables: { owner, name, number: ref } });

  async function query(ref) {
    for (let attempt = 0; ; attempt += 1) {
      let response = null;
      let payload = null;
      let failure;
      try {
        response = await fetchImpl("https://api.github.com/graphql", { method: "POST", headers, body: body(ref) });
        if (response.ok) payload = await response.json();
        if (response.ok && !payload.errors?.some((error) => error.type === "RATE_LIMITED")) return payload;
        const why = response.ok ? "GraphQL RATE_LIMITED" : `status ${response.status}`;
        failure = new CreditError(CreditErrorCode.status, `#${ref}: GitHub API answered ${why}`, { ref });
      } catch (error) {
        if (error instanceof CreditError) throw error;
        response = null;
        failure = new CreditError(CreditErrorCode.unreachable, `#${ref}: GitHub API unreachable (${error.message})`, { ref });
      }
      const delay = retryDelay(response, payload, attempt);
      if (delay === null || attempt + 1 >= attempts) throw failure;
      await sleep(delay);
    }
  }

  return async (ref) => {
    const payload = await query(ref);
    const repositoryNode = payload.data?.repository;
    const node = repositoryNode?.issueOrPullRequest;
    if (node?.__typename === "PullRequest") return { kind: "pull", ...person(node.author, excluded) };
    if (node?.__typename === "Issue") return { kind: "issue", fixes: issueFixes(node, excluded) };
    const errors = payload.errors || [];
    if (repositoryNode && errors.every((error) => error.type === "NOT_FOUND")) {
      return { kind: repositoryNode.discussion ? "discussion" : "missing" };
    }
    const types = errors.map((error) => error.type || "unknown").join(", ") || "empty response";
    throw new CreditError(CreditErrorCode.status, `#${ref}: GitHub GraphQL returned no node (${types})`, { ref });
  };
}

// Looks each distinct ref up once. Failures are collected, not thrown, so the
// caller can still render every ref it has and then fail with all of them. A
// discussion or a number that names nothing is a warning, not a failure.
export async function resolveCredits(refs, lookup, { concurrency = 6 } = {}) {
  const distinct = [...new Set(refs)];
  const outcomes = new Array(distinct.length);
  let next = 0;
  async function worker() {
    while (next < distinct.length) {
      const index = next++;
      try {
        outcomes[index] = { credit: await lookup(distinct[index]) };
      } catch (error) {
        if (!(error instanceof CreditError)) throw error;
        outcomes[index] = { failure: { ref: distinct[index], code: error.code, message: error.message } };
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, distinct.length) }, worker));
  const credits = new Map();
  const failures = [];
  const warnings = [];
  distinct.forEach((ref, index) => {
    const { credit, failure } = outcomes[index];
    if (failure) return failures.push(failure);
    credits.set(ref, credit);
    if (credit.kind === "discussion") {
      warnings.push({ ref, code: CreditErrorCode.discussion, message: `#${ref} is a discussion; left uncredited` });
    } else if (credit.kind === "missing") {
      warnings.push({ ref, code: CreditErrorCode.notFound, message: `#${ref} names no issue, pull request or discussion; left uncredited` });
    }
  });
  return { credits, failures, warnings };
}

export function reportWarnings(warnings, { env = process.env, write = (line) => console.error(line) } = {}) {
  const prefix = env.GITHUB_ACTIONS === "true" ? "::warning::" : "warning: ";
  for (const warning of warnings) write(`${prefix}${warning.code} ${warning.message}`);
}

export function unresolvedError(failures) {
  const lines = failures.map((failure) => `  ${failure.code} ${failure.message}`);
  return new CreditError(
    CreditErrorCode.unresolved,
    `${failures.length} reference(s) could not be credited:\n${lines.join("\n")}`,
    { failures },
  );
}

function humanLogin(person) {
  return person.login && !person.bot ? person.login : null;
}

export function creditedLogins(credit) {
  if (credit?.kind === "pull") return [humanLogin(credit)].filter(Boolean);
  if (credit?.kind === "issue") return credit.fixes.map(humanLogin).filter(Boolean);
  return [];
}

// " by @a" for a pull request, " fixed in #2 by @a and #3" for an issue.
export function creditSuffix(credit) {
  const by = (person) => (humanLogin(person) ? ` by @${person.login}` : "");
  if (credit?.kind === "pull") return by(credit);
  if (credit?.kind !== "issue" || !credit.fixes.length) return "";
  return ` fixed in ${credit.fixes.map((fix) => `#${fix.number}${by(fix)}`).join(" and ")}`;
}

// Unique human logins in the order their refs first appear.
export function contributorLogins(refs, credits) {
  const logins = [];
  const seen = new Set();
  for (const ref of refs) {
    for (const login of creditedLogins(credits.get(ref))) {
      if (isBotAccount(login) || seen.has(login.toLowerCase())) continue;
      seen.add(login.toLowerCase());
      logins.push(login);
    }
  }
  return logins;
}
