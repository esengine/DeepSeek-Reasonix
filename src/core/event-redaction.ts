const SECRET_KEY_RE =
  /(secret|token|password|passphrase|api[-_]?key|authorization|cookie|credential|passwd|pwd)/i;

export function redactEventValue<T>(value: T): T {
  return redactUnknown(value, null) as T;
}

function redactUnknown(value: unknown, key: string | null): unknown {
  if (Array.isArray(value)) return value.map((item) => redactUnknown(item, null));
  if (value && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [childKey, childValue] of Object.entries(value)) {
      out[childKey] = redactUnknown(childValue, childKey);
    }
    return out;
  }
  if (typeof value === "string") {
    if ((key && SECRET_KEY_RE.test(key)) || /^Bearer\s+/i.test(value)) return "[redacted]";
  }
  return value;
}
