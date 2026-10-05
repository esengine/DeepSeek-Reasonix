"use strict";

// What a page in the agent's browser may become. The agent's own requests pass
// the kernel's policy first; this is the window's answer for everything else a
// page can reach — a link, a redirect, a script, the person typing an address.
// The kernel's own origin is never one of them: its routes act on the session.
function guestNavigationAllowed(raw, kernelOrigin) {
  let url;
  try {
    url = new URL(String(raw));
  } catch {
    return false;
  }
  if (url.origin === kernelOrigin) return false;
  return url.protocol === "http:" || url.protocol === "https:" || url.href === "about:blank";
}

// What the person typed, as the address to load and the one to fall back to.
// Only "scheme://" or about: names a scheme: "intranet:8080" is a host and a
// port. A host that cannot be on the public internet is read as http, the way
// Chromium exempts it from upgrading; any other bare host tries https first
// and falls back to http, since an intranet site may serve only http.
function typedAddress(raw) {
  const text = String(raw || "").trim();
  if (!text) return { url: "", fallback: "" };
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(text) || /^about:/i.test(text)) return { url: text, fallback: "" };
  let host;
  try {
    host = new URL("http://" + text).hostname;
  } catch {
    return { url: "https://" + text, fallback: "" };
  }
  if (privateHost(host)) return { url: "http://" + text, fallback: "" };
  return { url: "https://" + text, fallback: "http://" + text };
}

// privateHost reads the address's structure, never its spelling: a loopback,
// private, link-local or shared IP literal, localhost, or a name with no dot,
// which no public resolver answers for.
function privateHost(host) {
  const h = host.toLowerCase();
  if (h.startsWith("[")) {
    const v6 = h.slice(1, -1);
    return v6 === "::1" || /^f[cd][0-9a-f]{2}:/.test(v6) || /^fe[89ab][0-9a-f]:/.test(v6);
  }
  const v4 = h.match(/^(\d+)\.(\d+)\.(\d+)\.(\d+)$/);
  if (v4) {
    const [a, b] = [Number(v4[1]), Number(v4[2])];
    return (
      a === 10 ||
      a === 127 ||
      (a === 172 && b >= 16 && b <= 31) ||
      (a === 192 && b === 168) ||
      (a === 169 && b === 254) ||
      (a === 100 && b >= 64 && b <= 127)
    );
  }
  return h === "localhost" || h.endsWith(".localhost") || !h.includes(".");
}

module.exports = { guestNavigationAllowed, typedAddress, privateHost };
