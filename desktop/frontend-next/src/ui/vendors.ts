// vendors.ts — one account, however many doors it answers on.
//
// Two config entries at one host are one service reached under two protocols,
// not two services. Both the connection list and the model list group on that,
// so they have to agree on what counts as the same host and what to call it.

export const KIND_LABEL: Record<string, string> = {
  openai: "OpenAI 兼容",
  anthropic: "Anthropic 兼容",
  responses: "Responses",
  typesafe: "决策 · System One",
  extension: "扩展",
};

// The account a route belongs to. Host alone is not enough: a relay reached
// with two different keys is two accounts — personal beside work, or two
// tenants of the same gateway — and merging them would show one balance and one
// key state for both.
export function accountKey(host: string, keyEnv?: string): string {
  return `${host}\u0000${(keyEnv ?? "").trim()}`;
}

export function hostOf(baseUrl: string): string {
  try {
    return new URL(baseUrl).hostname.toLowerCase();
  } catch {
    return baseUrl.trim().toLowerCase();
  }
}

// The name a person would use for the account. "api.deepseek.com" is deepseek.
export function vendorLabel(host: string): string {
  const bare = host.replace(/^(www|api|open|gateway)\./, "");
  const first = bare.split(".")[0];
  return first || host;
}

// The config name we fill in for a new source, so nobody has to invent one.
// Derived from vendorLabel and nothing else: one derivation is what keeps "is
// this still the name we generated" answerable later.
export function derivedName(host: string): string {
  return vendorLabel(host).replace(/[^a-zA-Z0-9._-]/g, "-") || "custom";
}

// Mirrors providerNameRE in internal/frontend/serve/providers.go: the name is a
// config table name and the head of every "<provider>/<model>" ref.
const SOURCE_NAME = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;
export const sourceNameUsable = (name: string): boolean => SOURCE_NAME.test(name.trim());

export function nameFrom(baseUrl: string): string {
  try {
    return derivedName(new URL(baseUrl).hostname.toLowerCase());
  } catch {
    return "custom";
  }
}

// The name to put on an account. A display name the user set wins outright.
// Otherwise a name the user typed is the answer: a curated entry carries our
// preset id, a new source is prefilled with derivedName, and uniqueName's -2,
// -3 … suffix is ours too — so anything left is a name somebody chose on
// purpose. Only when no entry has one does the shared host speak.
export function accountLabel(host: string, entries: { name: string; preset?: boolean; displayName?: string }[]): string {
  const shown = entries.find((e) => e.displayName?.trim())?.displayName?.trim();
  if (shown) return shown;
  const auto = derivedName(host).toLowerCase();
  const own = entries.find((e) => !e.preset && e.name.trim().replace(/-\d+$/, "").toLowerCase() !== auto);
  return own ? own.name.trim() : vendorLabel(host);
}

// Two accounts on one host called the same thing tell the user nothing about
// which is which. Only then does the config entry's own name earn a place on
// screen; two labels the user already told apart are left as they are.
export function disambiguate<T extends { host: string; label: string; hint: string }>(items: T[]): T[] {
  const seen = new Map<string, number>();
  const key = (it: T) => `${it.host}\u0000${it.label}`;
  for (const it of items) seen.set(key(it), (seen.get(key(it)) ?? 0) + 1);
  return items.map((it) =>
    (seen.get(key(it)) ?? 0) > 1 && it.hint && it.hint !== it.label
      ? { ...it, label: `${it.label} · ${it.hint}` }
      : it,
  );
}
