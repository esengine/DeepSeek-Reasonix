const encoder = new TextEncoder();
export const isRecord = (v: unknown): v is Record<string, unknown> => {
  if (v === null || typeof v !== "object") return false;
  const proto = Object.getPrototypeOf(v);
  return proto === Object.prototype || proto === null;
};

/** Validates bounded JSON without serializing an untrusted object graph. */
export function fitsJSON(value: unknown, limit: number): boolean {
  let remaining = limit, nodes = 4096;
  const active = new Set<object>();
  const spend = (n: number) => (remaining -= n) >= 0;
  const string = (s: string) => s.length <= remaining && spend(encoder.encode(JSON.stringify(s)).length);
  const visit = (v: unknown, depth: number): boolean => {
    if (--nodes < 0 || depth > 32) return false;
    if (typeof v === "string") return string(v);
    if (v === null || typeof v === "boolean") return spend(String(v).length);
    if (typeof v === "number") return Number.isFinite(v) && spend(String(v).length);
    if (!Array.isArray(v) && !isRecord(v)) return false;
    if (active.has(v) || !spend(2)) return false;
    active.add(v);
    const values = Array.isArray(v) ? v : Object.entries(v);
    if (values.length > nodes) return false;
    for (let i = 0; i < values.length; i++) {
      if (i > 0 && !spend(1)) return false;
      if (Array.isArray(v)) {
        if (!visit(values[i], depth + 1)) return false;
      } else {
        const [key, item] = values[i] as [string, unknown];
        if (!string(key) || !spend(1) || !visit(item, depth + 1)) return false;
      }
    }
    active.delete(v);
    return true;
  };
  return visit(value, 0);
}

export const fitsString = (value: string, limit: number) => value.length <= limit && encoder.encode(value).length <= limit;
