// Run: tsx src/__tests__/browser-links.test.tsx
//
// Chat link routing matrix for the built-in browser: disposition mapping,
// default open mode (builtin vs system), protocol whitelist, and the
// system-browser fallback when the companion is unavailable.

import {
  __setBrowserLinkBackendForTest,
  chatLinkDisposition,
  hrefProtocol,
  isSafeExternalProtocol,
  openChatLink,
  type BrowserLinkBackend,
} from "../lib/browserLinks";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (JSON.stringify(actual) === JSON.stringify(expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(
      `  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`,
    );
    failed += 1;
  }
}

function ok(value: unknown, label: string) {
  eq(Boolean(value), true, label);
}

function makeBackend(overrides: Partial<BrowserLinkBackend> = {}): {
  backend: BrowserLinkBackend;
  calls: string[];
} {
  const calls: string[] = [];
  const backend: BrowserLinkBackend = {
    async defaultOpenMode() {
      return "builtin";
    },
    async openInBuiltin(url, disposition) {
      calls.push(`builtin:${disposition}:${url}`);
    },
    openInSystem(url) {
      calls.push(`system:${url}`);
    },
    ...overrides,
  };
  return { backend, calls };
}

async function run(): Promise<void> {
  console.log("\nbrowser link dispositions");

  eq(chatLinkDisposition({}), "foreground", "plain click opens foreground");
  eq(chatLinkDisposition({ button: 0 }), "foreground", "left button opens foreground");
  eq(chatLinkDisposition({ button: 1 }), "background", "middle click opens background");
  eq(chatLinkDisposition({ metaKey: true }), "background", "Cmd+click opens background");
  eq(chatLinkDisposition({ ctrlKey: true }), "background", "Ctrl+click opens background");
  eq(chatLinkDisposition({ altKey: true }), "background", "Alt+click opens background");

  console.log("\nprotocol classification");

  eq(hrefProtocol("https://example.com"), "https:", "https URL protocol");
  eq(hrefProtocol("http://example.com"), "http:", "http URL protocol");
  eq(hrefProtocol("MAILTO:test@example.com"), "mailto:", "protocols normalize to lowercase");
  eq(hrefProtocol("javascript:alert(1)"), "javascript:", "javascript: parses as a protocol");
  eq(hrefProtocol("./docs/GUIDE.md"), null, "relative links have no protocol");
  eq(hrefProtocol("#section"), null, "page fragments have no protocol");
  eq(hrefProtocol("not a url"), null, "bare text has no protocol");
  ok(isSafeExternalProtocol("mailto:"), "mailto is a safe external protocol");
  ok(isSafeExternalProtocol("tel:"), "tel is a safe external protocol");
  ok(isSafeExternalProtocol("sms:"), "sms is a safe external protocol");
  eq(isSafeExternalProtocol("javascript:"), false, "javascript: is never safe");
  eq(isSafeExternalProtocol("data:"), false, "data: is never safe");
  eq(isSafeExternalProtocol("file:"), false, "file: stays on the local-path handler");
  eq(isSafeExternalProtocol("ftp:"), false, "ftp is not on the whitelist");
  eq(isSafeExternalProtocol(null), false, "null protocol is never safe");

  console.log("\nopenChatLink routing with default builtin");

  {
    const { backend, calls } = makeBackend();
    __setBrowserLinkBackendForTest(backend);
    ok(await openChatLink("https://example.com", "foreground"), "http(s) is handled");
    eq(calls, ["builtin:foreground:https://example.com"], "foreground click opens in the built-in browser");
  }
  {
    const { backend, calls } = makeBackend();
    __setBrowserLinkBackendForTest(backend);
    await openChatLink("https://example.com", "background");
    eq(calls, ["builtin:background:https://example.com"], "modifier click opens a background tab");
  }
  {
    const { backend, calls } = makeBackend();
    __setBrowserLinkBackendForTest(backend);
    await openChatLink("https://example.com", "system");
    eq(calls, ["system:https://example.com"], "explicit system disposition skips the built-in browser");
  }
  {
    const { backend, calls } = makeBackend();
    __setBrowserLinkBackendForTest(backend);
    eq(await openChatLink("tel:+10000000000", "foreground"), false, "non-http(s) returns false for the caller");
    eq(await openChatLink("javascript:alert(1)", "foreground"), false, "javascript: returns false");
    eq(await openChatLink(undefined, "foreground"), false, "undefined href returns false");
    eq(calls, [], "non-http(s) links never reach any opener");
  }

  console.log("\nopenChatLink routing with default system");

  {
    const { backend, calls } = makeBackend({
      async defaultOpenMode() {
        return "system";
      },
    });
    __setBrowserLinkBackendForTest(backend);
    await openChatLink("https://example.com", "foreground");
    eq(calls, ["system:https://example.com"], "default system mode sends plain clicks to the OS browser");
  }
  {
    const { backend, calls } = makeBackend({
      async defaultOpenMode() {
        return "system";
      },
    });
    __setBrowserLinkBackendForTest(backend);
    await openChatLink("https://example.com", "background");
    eq(calls, ["builtin:background:https://example.com"], "modifier clicks keep background-tab intent under default system");
  }

  console.log("\nfallback when the built-in browser is unavailable");

  {
    const { backend, calls } = makeBackend({
      async openInBuiltin() {
        throw new Error("component missing");
      },
    });
    __setBrowserLinkBackendForTest(backend);
    await openChatLink("https://example.com", "foreground");
    eq(calls, ["system:https://example.com"], "companion failure falls back to the system browser");
  }

  console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
  if (failed > 0) process.exit(1);
}

void run();
