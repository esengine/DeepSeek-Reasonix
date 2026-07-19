/** Persisted paired Reasonix nodes (LAN / Tailscale / relay identity). */

export interface PairedNode {
  id: string;
  name: string;
  baseUrl: string;
  /** Certificate / identity fingerprint fixed at pairing time. */
  fingerprint?: string;
  online?: boolean;
  pairedAt: string;
}

const STORAGE_KEY = "reasonix.mobile.pairedNodes.v1";

export function loadPairedNodes(): PairedNode[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as PairedNode[];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

export function savePairedNodes(nodes: PairedNode[]): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(nodes));
}

/**
 * Build a stable pairing URI encoded in QR codes.
 * Format: reasonix://node-pair?v=1&url=...&id=...&name=...&fp=...
 */
export function buildPairingUri(node: {
  baseUrl: string;
  id?: string;
  name?: string;
  fingerprint?: string;
}): string {
  const u = new URL("reasonix://node-pair");
  u.searchParams.set("v", "1");
  u.searchParams.set("url", node.baseUrl.replace(/\/$/, ""));
  if (node.id) u.searchParams.set("id", node.id);
  if (node.name) u.searchParams.set("name", node.name);
  if (node.fingerprint) u.searchParams.set("fp", node.fingerprint);
  return u.toString();
}

export interface ParsedPairing {
  baseUrl: string;
  id: string;
  name: string;
  fingerprint?: string;
}

/** Parse QR/paste payload: pairing URI, bare URL, or JSON. */
export function parsePairingPayload(raw: string): ParsedPairing {
  const text = raw.trim();
  if (!text) throw new Error("empty pairing payload");

  // JSON form
  if (text.startsWith("{")) {
    const j = JSON.parse(text) as {
      url?: string;
      baseUrl?: string;
      id?: string;
      name?: string;
      fingerprint?: string;
      fp?: string;
    };
    const baseUrl = (j.url || j.baseUrl || "").replace(/\/$/, "");
    if (!baseUrl) throw new Error("missing url");
    return {
      baseUrl,
      id: j.id || `node_${hashShort(baseUrl)}`,
      name: j.name || hostLabel(baseUrl),
      fingerprint: j.fingerprint || j.fp,
    };
  }

  // reasonix://node-pair?...
  if (text.startsWith("reasonix://") || text.startsWith("REASONIX://")) {
    const u = new URL(text);
    const baseUrl = (u.searchParams.get("url") || "").replace(/\/$/, "");
    if (!baseUrl) throw new Error("pairing URI missing url");
    return {
      baseUrl,
      id: u.searchParams.get("id") || `node_${hashShort(baseUrl)}`,
      name: u.searchParams.get("name") || hostLabel(baseUrl),
      fingerprint: u.searchParams.get("fp") || undefined,
    };
  }

  // Bare http(s) URL
  if (/^https?:\/\//i.test(text)) {
    const baseUrl = text.replace(/\/$/, "");
    return {
      baseUrl,
      id: `node_${hashShort(baseUrl)}`,
      name: hostLabel(baseUrl),
    };
  }

  // host:port shorthand
  if (/^[\w.-]+:\d+$/.test(text) || text === "localhost" || text.startsWith("localhost:")) {
    const baseUrl = `http://${text.replace(/\/$/, "")}`;
    return {
      baseUrl,
      id: `node_${hashShort(baseUrl)}`,
      name: hostLabel(baseUrl),
    };
  }

  throw new Error("unrecognized pairing payload");
}

function hostLabel(baseUrl: string): string {
  try {
    return new URL(baseUrl).host || baseUrl;
  } catch {
    return baseUrl;
  }
}

function hashShort(s: string): string {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0;
  return Math.abs(h).toString(36);
}

export async function probeNodeHealth(baseUrl: string): Promise<{ ok: boolean; nodeId?: string }> {
  try {
    const res = await fetch(`${baseUrl.replace(/\/$/, "")}/healthz`, {
      method: "GET",
      signal: AbortSignal.timeout(4000),
    });
    if (!res.ok) return { ok: false };
    const body = (await res.json()) as { ok?: boolean; nodeId?: string };
    return { ok: body.ok !== false, nodeId: body.nodeId };
  } catch {
    return { ok: false };
  }
}
