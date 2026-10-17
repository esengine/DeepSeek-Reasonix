import { since } from "./wallet";

export function SessionAge({ at }: { at?: number }) {
  if (!at) return null;
  const date = new Date(at);
  if (!Number.isFinite(date.valueOf())) return null;
  const iso = date.toISOString();
  return <time className="sessmeta sessage" dateTime={iso} title={date.toLocaleString()}>{since(iso, Date.now())}</time>;
}
