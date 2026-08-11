// browserLinks routes chat markdown http(s) links into the built-in browser
// window. Ordinary clicks open a foreground tab in the current chat's tab
// group; modifier clicks (Cmd/Ctrl/Alt) or the middle button open background
// tabs; "system" disposition goes straight to the OS browser. When the user
// chose "system browser" as the default open mode, plain foreground clicks go
// to the OS browser too — modifier clicks still mean "background tab in the
// built-in browser". file:// links keep their local-path handler and non-http
// protocols are either handed to the OS opener (safe whitelist) or dropped
// (javascript:, data:, unknown schemes) — only the http(s) chain is touched
// here.
import { app, openExternal } from "./bridge";

export type BrowserLinkDisposition = "foreground" | "background" | "system";

// chatLinkDisposition maps a pointer event onto the disposition contract.
export function chatLinkDisposition(event: {
  metaKey?: boolean;
  ctrlKey?: boolean;
  altKey?: boolean;
  button?: number;
}): BrowserLinkDisposition {
  if (event.button === 1) return "background"; // middle click
  if (event.metaKey || event.ctrlKey || event.altKey) return "background";
  return "foreground";
}

// hrefProtocol returns the lowercased URL protocol (with colon), or null when
// href is not an absolute URL (relative links, fragments, bare text).
export function hrefProtocol(href: string): string | null {
  try {
    return new URL(href).protocol.toLowerCase();
  } catch {
    return null;
  }
}

// SAFE_EXTERNAL_PROTOCOLS are schemes that may hand off to the OS opener. This
// is the allowlist: javascript:, data:, and any unknown scheme are never
// opened anywhere.
const SAFE_EXTERNAL_PROTOCOLS = new Set([
  "mailto:",
  "tel:",
  "sms:",
  "geo:",
  "maps:",
  "facetime:",
  "skype:",
]);

export function isSafeExternalProtocol(protocol: string | null): boolean {
  return protocol !== null && SAFE_EXTERNAL_PROTOCOLS.has(protocol);
}

export interface BrowserLinkBackend {
  /** Resolves the user's default open mode: "builtin" | "system". */
  defaultOpenMode(): Promise<string>;
  openInBuiltin(url: string, disposition: "foreground" | "background"): Promise<void>;
  openInSystem(url: string): void;
}

const liveBackend: BrowserLinkBackend = {
  async defaultOpenMode(): Promise<string> {
    try {
      const settings = await app.GetBrowserSettings();
      return settings.defaultOpenMode === "system" ? "system" : "builtin";
    } catch {
      return "builtin";
    }
  },
  async openInBuiltin(url, disposition) {
    await app.OpenBrowserURL("", url, disposition);
  },
  openInSystem(url) {
    openExternal(url);
  },
};

// backend is swappable for tests; production always uses liveBackend.
let backend: BrowserLinkBackend = liveBackend;

export function __setBrowserLinkBackendForTest(b: BrowserLinkBackend): void {
  backend = b;
}

// openChatLink opens an http(s) link in the built-in browser for the active
// chat (the host binds the ownerId). When the built-in browser is unavailable
// (component missing, crashed, or disabled) it falls back to the system
// browser so a link never dies silently. Returns false when the URL is not an
// http(s) link and the caller should keep its own handling.
export async function openChatLink(
  href: string | undefined,
  disposition: BrowserLinkDisposition,
): Promise<boolean> {
  if (!href) return false;
  if (!/^https?:\/\//i.test(href)) return false;
  if (disposition === "system") {
    backend.openInSystem(href);
    return true;
  }
  // The default open mode applies to plain foreground clicks: users who chose
  // the system browser never trigger the companion. Modifier clicks keep
  // their explicit background-tab intent.
  if (disposition === "foreground" && (await backend.defaultOpenMode()) === "system") {
    backend.openInSystem(href);
    return true;
  }
  try {
    await backend.openInBuiltin(href, disposition);
    return true;
  } catch {
    // Companion unavailable: explicit system-browser fallback. The status
    // surface (settings) still offers install/repair and retry.
    backend.openInSystem(href);
    return true;
  }
}
