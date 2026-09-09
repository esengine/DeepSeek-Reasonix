// Run: tsx src/__tests__/rich-link-context-menu.test.tsx
//
// Headless interaction tests for the context menu of web/mail rich links:
// right click (and the keyboard ContextMenu / Shift+F10 gesture) opens a menu
// whose actions transform information the link already carries — open in the
// browser, copy the original URL, copy a GitHub owner/repo reference, copy a
// mailto address without the scheme. Local file links keep their own menu and
// are covered by local-path-click-e2e.test.tsx.

import { JSDOM } from "jsdom";

const dom = new JSDOM("<!doctype html><html><body></body></html>", { url: "https://reasonix.local/" });
const { window } = dom;

// Set globals BEFORE dynamically importing React DOM so the renderer sees a
// real document (ESM imports are hoisted, hence dynamic import below).
(globalThis as Record<string, unknown>).window = window;
(globalThis as Record<string, unknown>).document = window.document;
(globalThis as Record<string, unknown>).HTMLElement = window.HTMLElement;
(globalThis as Record<string, unknown>).MouseEvent = window.MouseEvent;
(globalThis as Record<string, unknown>).KeyboardEvent = window.KeyboardEvent;
// React 19 requires this flag for act() to flush renders/pass-through events.
(globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;

// Bridge spies: BrowserOpenURL is what "open"/"compose" must call;
// ClipboardSetText is what copy actions must reach through the clipboard lib.
const browsed: string[] = [];
const clipboardWrites: string[] = [];
let clipboardSucceeds = true;
(window as unknown as Record<string, unknown>).go = { main: { App: {} } };
(window as unknown as Record<string, unknown>).runtime = {
  BrowserOpenURL: (url: string) => {
    browsed.push(url);
  },
  ClipboardSetText: async (value: string) => {
    if (!clipboardSucceeds) return false;
    clipboardWrites.push(value);
    return true;
  },
};

let passed = 0;
let failed = 0;
function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const { createElement } = await import("react");
const { act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { ToastProvider } = await import("../lib/toast");
const { RichMarkdownLink } = await import("../components/githubLink");

// Every rendered root is unmounted before the summary so the toast
// auto-dismiss timers fire against dead trees instead of raising act warnings.
const roots: Array<{ unmount: () => void }> = [];

async function renderLink(href: string, label?: string): Promise<{ anchor: HTMLAnchorElement; container: HTMLElement }> {
  const container = window.document.createElement("div");
  window.document.body.appendChild(container);
  const root = createRoot(container);
  roots.push(root);
  const link = createElement(RichMarkdownLink, { href, children: label ?? href });
  await act(async () => {
    root.render(createElement(ToastProvider, null, link));
  });
  return { anchor: container.querySelector("a") as HTMLAnchorElement, container };
}

function menuItemLabels(): string[] {
  return Array.from(window.document.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
    .map((item) => item.textContent ?? "");
}

async function openContextMenu(anchor: HTMLAnchorElement) {
  await act(async () => {
    anchor.dispatchEvent(new window.MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 40, clientY: 40 }));
  });
}

async function clickMenuItem(label: string) {
  const item = Array.from(window.document.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
    .find((candidate) => candidate.textContent?.includes(label));
  ok(item !== undefined, `menu contains a "${label}" item`);
  await act(async () => {
    item?.dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true }));
    // Flush past the async clipboard write so the toast state update lands
    // inside act() instead of leaking a warning after the assertion.
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
  });
}

async function closeWithEscape() {
  await act(async () => {
    window.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape" }));
  });
}

console.log("\nrich link context menu");

// 1. Bare GitHub pull request URL: open, copy link, copy owner/repo reference.
{
  const href = "https://github.com/esengine/DeepSeek-Reasonix/pull/6976";
  const { anchor } = await renderLink(href);
  await openContextMenu(anchor);
  const menu = window.document.querySelector('[role="menu"]');
  ok(menu !== null, "web link context menu opens on right click");
  const labels = menuItemLabels();
  ok(labels.length === 3, `pull request menu has three items (${labels.length})`);
  ok(labels.some((text) => text.includes("Open in browser")), "pull request menu can open in the browser");
  ok(labels.some((text) => text.includes("Copy link")), "pull request menu can copy the link");
  ok(labels.some((text) => text.includes("Copy reference")), "pull request menu can copy a reference");
  await clickMenuItem("Copy reference");
  ok(clipboardWrites[0] === "esengine/DeepSeek-Reasonix#6976",
    `copy reference writes the owner/repo#number form (${clipboardWrites[0]})`);
  ok(window.document.querySelector(".toast--info")?.textContent?.includes("Copied") === true,
    "a success toast confirms the copy");
  await closeWithEscape();
}

// 2. GitHub commit URL: the reference uses the short SHA form.
{
  const href = "https://github.com/esengine/DeepSeek-Reasonix/commit/ca03d4c2ec47d7fc9726affbbda04d8616f6351b";
  const { anchor } = await renderLink(href);
  await openContextMenu(anchor);
  await clickMenuItem("Copy reference");
  ok(clipboardWrites[1] === "esengine/DeepSeek-Reasonix@ca03d4c",
    `commit reference writes owner/repo@shortsha (${clipboardWrites[1]})`);
  await closeWithEscape();
}

// 3. External web URL: no reference action; copy and open go to the right places.
{
  const href = "https://example.com/a/very/long/documentation/path";
  const { anchor } = await renderLink(href);
  await openContextMenu(anchor);
  const labels = menuItemLabels();
  ok(labels.length === 2, `external menu has two items (${labels.length})`);
  ok(labels.every((text) => !text.includes("Copy reference")), "external menu has no reference action");
  await clickMenuItem("Copy link");
  ok(clipboardWrites[2] === href, `copy link writes the original URL (${clipboardWrites[2]})`);
  await openContextMenu(anchor);
  await clickMenuItem("Open in browser");
  ok(browsed[0] === href, "open in browser sends the URL to the system browser");
  await closeWithEscape();
}

// 4. mailto link: compose opens the mail handler; copy strips the scheme.
{
  const href = "mailto:support@example.com";
  const { anchor } = await renderLink(href);
  await openContextMenu(anchor);
  const labels = menuItemLabels();
  ok(labels.some((text) => text.includes("Compose email")), "mailto menu offers compose email");
  ok(labels.some((text) => text.includes("Copy email address")), "mailto menu offers copy address");
  await clickMenuItem("Copy email address");
  ok(clipboardWrites[3] === "support@example.com",
    `copy email address strips the mailto: scheme (${clipboardWrites[3]})`);
  await openContextMenu(anchor);
  await clickMenuItem("Compose email");
  ok(browsed[1] === href, "compose email hands the mailto URL to the system handler");
  await closeWithEscape();
}

// Copy only the decoded recipient address, not compose parameters.
for (const [href, expected] of [
  ["mailto:support@example.com?subject=Help&body=Hello", "support@example.com"],
  ["MAILTO:support@example.com", "support@example.com"],
  ["mailto:hello%2Btag@example.com", "hello+tag@example.com"],
  ["mailto:one@example.com,two@example.com?cc=other@example.com", "one@example.com,two@example.com"],
  ["mailto:bad%ZZ@example.com?subject=Help", "bad%ZZ@example.com"],
  ["mailto:?subject=Help", ""],
]) {
  const { anchor } = await renderLink(href);
  await openContextMenu(anchor);
  ok(menuItemLabels().some((text) => text.includes("Compose email")), "mail protocol classification is consistent");
  await clickMenuItem("Copy email address");
  ok(clipboardWrites.at(-1) === expected, `address copy for ${href}`);
  await closeWithEscape();
}
clipboardWrites.splice(4);

// 5. Keyboard gesture: ContextMenu key and Shift+F10 open the menu, Escape closes.
{
  const href = "https://github.com/esengine/DeepSeek-Reasonix/issues/6856";
  const { anchor } = await renderLink(href);
  anchor.focus();
  await act(async () => {
    anchor.dispatchEvent(new window.KeyboardEvent("keydown", { key: "ContextMenu", bubbles: true, cancelable: true }));
  });
  ok(window.document.querySelector('[role="menu"]') !== null, "ContextMenu key opens the menu");
  ok(window.document.activeElement?.textContent?.includes("Open in browser"), "keyboard menu focuses first action");
  await closeWithEscape();
  ok(window.document.querySelector('[role="menu"]') === null, "Escape closes the menu");
  ok(window.document.activeElement === anchor, "Escape returns focus to link");
  await act(async () => {
    anchor.dispatchEvent(new window.KeyboardEvent("keydown", { key: "F10", shiftKey: true, bubbles: true, cancelable: true }));
  });
  ok(window.document.querySelector('[role="menu"]') !== null, "Shift+F10 opens the menu");
  await closeWithEscape();
}

// 6. Explicit Markdown labels are irrelevant to the menu: the copy actions
// still write the original URL and the owner/repo reference.
{
  const href = "https://github.com/esengine/DeepSeek-Reasonix/issues/6856";
  const { anchor } = await renderLink(href, "Readable issue title");
  await openContextMenu(anchor);
  await clickMenuItem("Copy link");
  ok(clipboardWrites[4] === href, "explicit label does not change what copy link writes");
  await closeWithEscape();
}

// 7. Relative links (no icon kind) keep the plain anchor: no menu, no
// preventDefault, so the webview default behavior is untouched.
{
  const { anchor } = await renderLink("./docs/GUIDE.md", "Guide");
  let defaultPrevented = false;
  await act(async () => {
    const event = new window.MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 10, clientY: 10 });
    anchor.dispatchEvent(event);
    defaultPrevented = event.defaultPrevented;
  });
  ok(defaultPrevented === false, "relative link right click is not intercepted");
  ok(window.document.querySelector('[role="menu"]') === null, "relative link opens no context menu");
}

// 8. Clipboard failure surfaces an error toast instead of a success toast.
{
  clipboardSucceeds = false;
  const { anchor } = await renderLink("https://example.com/copy-failure");
  await openContextMenu(anchor);
  await clickMenuItem("Copy link");
  ok(window.document.querySelector(".toast--error")?.textContent?.includes("Could not copy") === true,
    "a failed copy shows an error toast");
  await closeWithEscape();
  clipboardSucceeds = true;
}

await act(async () => {
  for (const root of roots) root.unmount();
});

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
