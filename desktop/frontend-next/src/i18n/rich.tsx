import type { ReactNode } from "react";
import { t } from "./index";

// A sentence with a number in it gets split by JSX: `这段做过的 <b>{n}</b> 处改动`
// is three nodes, and translating the pieces separately cannot work because
// English puts the number somewhere else. tx keeps the sentence whole — it is
// one key, translated as one string — and substitutes nodes into its
// placeholders afterwards.
export function tx(zh: string, vars: Record<string, ReactNode>): ReactNode[] {
  const rendered = t(zh);
  const out: ReactNode[] = [];
  const pattern = /\{(\w+)\}/g;
  let at = 0;
  let match: RegExpExecArray | null;
  let key = 0;
  while ((match = pattern.exec(rendered)) !== null) {
    if (match.index > at) out.push(rendered.slice(at, match.index));
    const name = match[1];
    // An unknown placeholder stays visible rather than vanishing: a missing
    // value is a bug worth seeing, and a blank hides it.
    out.push(name in vars ? <span key={key++}>{vars[name]}</span> : match[0]);
    at = match.index + match[0].length;
  }
  if (at < rendered.length) out.push(rendered.slice(at));
  return out;
}
