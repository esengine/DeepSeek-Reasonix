type SpecRecord = Record<string, unknown>;

function isRecord(value: unknown): value is SpecRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringField(obj: SpecRecord, key: string): string | null {
  const value = obj[key];
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function stringArgs(obj: SpecRecord): string[] {
  const args = obj.args;
  if (!Array.isArray(args)) return [];
  return args.filter((arg): arg is string => typeof arg === "string" && arg.length > 0);
}

export function normalizeMcpSpec(spec: unknown): string | null {
  if (typeof spec === "string") return spec;
  if (!isRecord(spec)) return null;

  const raw = stringField(spec, "raw") ?? stringField(spec, "spec");
  if (raw) return raw;

  const name = stringField(spec, "name") ?? stringField(spec, "label");
  const command = stringField(spec, "command");
  const url = stringField(spec, "url");
  const transport = stringField(spec, "transport");
  if (name && url) return `${name}=${transport === "streamable-http" ? "streamable+" : ""}${url}`;
  if (name && command) return `${name}=${[command, ...stringArgs(spec)].join(" ")}`;
  if (command) return [command, ...stringArgs(spec)].join(" ");

  return null;
}

export function mcpSpecLabel(spec: unknown): string {
  const text = normalizeMcpSpec(spec) ?? "";
  const eq = text.indexOf("=");
  return eq > 0 ? text.slice(0, eq) : text;
}

export function mcpSpecCommand(spec: unknown): string {
  const text = normalizeMcpSpec(spec) ?? "";
  const eq = text.indexOf("=");
  return eq > 0 ? text.slice(eq + 1) : text;
}
