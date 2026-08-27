// Run: tsx src/__tests__/remote-connect-wizard.test.tsx

import React from "react";
import { JSDOM } from "jsdom";
import { act } from "react";

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import type { AppBindings } from "../lib/bridge";
import type { RemoteDirEntry, RemoteHostView } from "../lib/types";

let passed = 0;
let failed = 0;
function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nRemote connect wizard (three steps)");
const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });

const [{ createRoot }, { RemoteConnectWizard }, { LocaleProvider }, { useRemoteStore }] = await Promise.all([
  import("react-dom/client"),
  import("../components/RemoteConnectWizard"),
  import("../lib/i18n"),
  import("../store/remote"),
]);

// Bridge call tape: every remote method appends "<name>:<detail>".
const tape: string[] = [];
const savedHosts: RemoteHostView[] = [
  { id: "gpu-box", label: "gpu-box", host: "192.168.1.10", port: 22, user: "dev", identityFile: "~/.ssh/id_ed25519", proxyJump: "", defaultWorkspace: "", serveInstall: "auto", credentialMode: "remote", useSSHConfig: false },
  { id: "pw-box", label: "pw-box", host: "10.0.0.8", port: 22, user: "ops", identityFile: "", proxyJump: "", defaultWorkspace: "", serveInstall: "auto", credentialMode: "remote", useSSHConfig: false, passwordSet: true },
];
let hostCount = 0;

function setInput(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(input, value);
  input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  input.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
}

function buttonByText(text: string): HTMLButtonElement | undefined {
  return [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === text);
}

async function flush(ticks = 20) {
  for (let i = 0; i < ticks; i++) await Promise.resolve();
}

// Real-time wait: the ConnectRemoteHost mock holds the connecting step for a
// 20ms macrotask so the log panel renders; microtask flushes cannot cover it.
function delay(ms: number): Promise<void> {
  return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

const dirs: Record<string, Array<{ name: string; path: string; isDir: boolean }>> = {
  "/home/dev": [
    { name: "projects", path: "/home/dev/projects", isDir: true },
    { name: "notes.txt", path: "/home/dev/notes.txt", isDir: false },
  ],
  "/home/dev/projects": [{ name: "app", path: "/home/dev/projects/app", isDir: true }],
  "/home/dev/projects/web": [],
};
const slowDirectory = Promise.withResolvers<RemoteDirEntry[]>();

// Connection attempt counter: the first attempt fails (the wizard stays on
// the connecting step so its log panel stays observable); the retry succeeds.
let connectAttempts = 0;
// Platform-gate attempt counter: the first check models a Windows SSH host
// (uname reports MINGW64); later checks pass.
let platformAttempts = 0;
// Last AddRemoteHost payload, for credential-mode assertions.
let lastAddInput: RemoteHostInput | undefined;
window.go = { main: { App: {
  async RemoteHosts() {
    tape.push("RemoteHosts");
    return savedHosts.slice();
  },
  async AddRemoteHost(input: RemoteHostView & { label?: string; host?: string }) {
    lastAddInput = input as unknown as RemoteHostInput;
    tape.push(`AddRemoteHost:${input.label}:${input.host}:${(input as RemoteHostInput).credentialMode ?? ""}`);
    hostCount += 1;
    const view = { id: `new-${hostCount}`, label: String(input.label), host: String(input.host), port: 22, user: "", identityFile: "", proxyJump: "", defaultWorkspace: "", serveInstall: "npm", credentialMode: "remote", useSSHConfig: false } as RemoteHostView;
    savedHosts.push(view);
    return view;
  },
  async UpdateRemoteHost(id: string, input: RemoteHostView & { label?: string; host?: string }) {
    tape.push(`UpdateRemoteHost:${id}:${input.host}`);
    return savedHosts.find((h) => h.id === id) ?? savedHosts[0] as RemoteHostView;
  },
  async RemoteLastWorkspace() {
    tape.push("RemoteLastWorkspace");
    return "/home/dev";
  },
  async ListRemoteDir(_hostId: string, path: string) {
    tape.push(`ListRemoteDir:${path}`);
    if (path === "/slow") return slowDirectory.promise;
    if (path === "/fast") {
      return [{ name: "latest", path: "/fast/latest", isDir: true, size: 0, mtimeUnix: 0, symlink: false }];
    }
    return (dirs[path] ?? []).map((entry) => ({ ...entry, size: 0, mtimeUnix: 0, symlink: false }));
  },
  async MkdirRemote(_hostId: string, path: string) {
    tape.push(`MkdirRemote:${path}`);
    return undefined;
  },
  async ConnectRemoteHost(hostId: string) {
    tape.push(`ConnectRemoteHost:${hostId}`);
    // Hold 60ms so the connecting step mounts, then emit the kernel status
    // per attempt: first stopped+error → waitForRemoteConnection rejects
    // immediately and the wizard stays on the connecting step; later
    // attempts connected → the flow proceeds to the platform check.
    await new Promise<void>((resolve) => setTimeout(resolve, 60));
    connectAttempts += 1;
    if (connectAttempts === 1) {
      useRemoteStore.getState().applyStatus({ hostId, state: "stopped", error: "ssh: handshake failed" });
      return undefined;
    }
    useRemoteStore.getState().applyStatus({ hostId, state: "connected" });
    return undefined;
  },
  // Platform gate: the first check models a Windows SSH host (uname reports
  // MINGW64), later checks pass.
  async CheckRemotePlatform(hostId: string) {
    tape.push(`CheckRemotePlatform:${hostId}`);
    platformAttempts += 1;
    if (platformAttempts === 1) {
      throw new Error('remote host platform check failed: unsupported remote OS "MINGW64_NT-10.0-19045" (V1 supports Linux and macOS)');
    }
    return undefined;
  },
  async PickRemoteIdentityFile() {
    tape.push("PickRemoteIdentityFile");
    return "/home/dev/.ssh/id_wizard";
  },
  async OpenRemoteWorkspace(hostId: string, workspace: string) {
    tape.push(`OpenRemoteWorkspace:${hostId}:${workspace}`);
  },
  async OpenRemoteProjectTab(hostId: string, workspace: string, opts?: { newSession?: boolean }) {
    tape.push(`OpenRemoteProjectTab:${hostId}:${workspace}:${opts?.newSession === true}`);
  },
  async AddRemoteProject(hostId: string, workspace: string) {
    tape.push(`AddRemoteProject:${hostId}:${workspace}`);
    return { hostId, workspace };
  },
} as Partial<AppBindings> as AppBindings } };

function WizardHarness() {
  return (
    <LocaleProvider>
      <RemoteConnectWizard
        onRefresh={async () => { tape.push("refresh"); }}
        onClose={() => {
          tape.push("close");
        }}
      />
    </LocaleProvider>
  );
}

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => {
  root.render(<WizardHarness />);
});
await act(async () => flush());

// ── Step ① initial state: stepper rail, config form ──
const railItems = () => [...document.querySelectorAll(".remote-wizard__rail-item")];
ok(railItems().length === 3, "stepper rail lists all three steps");
ok(railItems()[0]?.className.includes("--current") === true, "step 1 is current on open");
ok(railItems().every((item) => !item.className.includes("--done")), "no step is done on open");
ok(document.querySelectorAll(".remote-wizard__seg").length === 3, "auth, download, and credential mode use segmented sliders");
const hostInput = document.querySelector<HTMLInputElement>(".remote-wizard__suggest input");
ok(Boolean(hostInput), "config step shows the host input");
ok(document.activeElement === hostInput, "opening the dialog focuses the first field");
await act(async () => {
  document.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Tab", shiftKey: true, bubbles: true }));
  await Promise.resolve();
});
ok(document.activeElement === buttonByText("Next"), "Shift+Tab from the first field wraps to the last action");
await act(async () => {
  document.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Tab", bubbles: true }));
  await Promise.resolve();
});
ok(document.activeElement === hostInput, "Tab from the last action wraps to the first field");

// ── Empty host+user: footer alert names both missing fields ──
{
  const userInput = [...document.querySelectorAll<HTMLInputElement>("input")].find((i) => i.placeholder.includes("root"));
  await act(async () => {
    if (hostInput) setInput(hostInput, "");
    if (userInput) setInput(userInput, "");
    await Promise.resolve();
  });
  await act(async () => {
    buttonByText("Next")?.click();
    await Promise.resolve();
  });
  const alert = document.querySelector(".remote-wizard__footer [role='alert']");
  const text = alert?.textContent ?? "";
  ok(text.includes("Host") && text.includes("username") && text.includes("Password"), "empty form reports host, username, and password");
  ok(text.includes("⚠"), "footer alert uses the warning mark");
  await act(async () => {
    if (userInput) setInput(userInput, "root");
    await Promise.resolve();
  });
}
// ── Saved-host suggestion: focus → dropdown → prefill ──
await act(async () => {
  hostInput?.dispatchEvent(new dom.window.Event("focusin", { bubbles: true }));
  hostInput?.dispatchEvent(new dom.window.Event("focus", { bubbles: false }));
  await Promise.resolve();
});
const suggestion = document.querySelector<HTMLButtonElement>(".remote-wizard__suggest-list button");
ok(Boolean(suggestion), "focusing the host input lists saved SSH connections");
await act(async () => {
  suggestion?.dispatchEvent(new dom.window.Event("mousedown", { bubbles: true, cancelable: true }));
  await Promise.resolve();
});
ok(hostInput?.value === "192.168.1.10", "picking a suggestion prefills the host");
const keyInput = [...document.querySelectorAll<HTMLInputElement>("input")].find((i) => i.value.includes("id_ed25519"));
ok(Boolean(keyInput), "saved key auth switches the form to key mode with the identity file");
await act(async () => {
  document.querySelector<HTMLButtonElement>(".remote-wizard__pick-btn")?.click();
  await flush();
});
ok(tape.includes("PickRemoteIdentityFile"), "identity-file action uses the native desktop picker");
ok(keyInput?.value === "/home/dev/.ssh/id_wizard", "native picker returns the absolute identity-file path");

{
  await act(async () => {
    if (hostInput) setInput(hostInput, "");
    await Promise.resolve();
  });
  await act(async () => {
    hostInput?.dispatchEvent(new dom.window.Event("focusin", { bubbles: true }));
    hostInput?.dispatchEvent(new dom.window.Event("focus", { bubbles: false }));
    await Promise.resolve();
  });
  const listed = [...document.querySelectorAll<HTMLButtonElement>(".remote-wizard__suggest-list button")].find((b) => b.textContent?.includes("pw-box"));
  await act(async () => {
    listed?.dispatchEvent(new dom.window.Event("mousedown", { bubbles: true, cancelable: true }));
    await Promise.resolve();
  });
  const passwordInput = document.querySelector<HTMLInputElement>(".remote-wizard__field input[type='password']");
  ok((passwordInput?.placeholder ?? "").toLowerCase().includes("saved") || (passwordInput?.placeholder ?? "").includes("已保存"), "saved password host keeps a keep-existing placeholder");
  await act(async () => {
    if (hostInput) setInput(hostInput, "");
    await Promise.resolve();
  });
  await act(async () => {
    hostInput?.dispatchEvent(new dom.window.Event("focusin", { bubbles: true }));
    hostInput?.dispatchEvent(new dom.window.Event("focus", { bubbles: false }));
    await Promise.resolve();
  });
  const gpuSuggestion = [...document.querySelectorAll<HTMLButtonElement>(".remote-wizard__suggest-list button")].find((b) => b.textContent?.includes("gpu-box"));
  await act(async () => {
    gpuSuggestion?.dispatchEvent(new dom.window.Event("mousedown", { bubbles: true, cancelable: true }));
    await Promise.resolve();
  });
}
// ── Next: first connect fails; the wizard stays on the connecting step ──
await act(async () => {
  buttonByText("Next")?.click();
  await delay(120);
  await flush();
});
ok(tape.includes("UpdateRemoteHost:gpu-box:192.168.1.10"), "next on a picked host updates it instead of adding a duplicate");
ok(!tape.some((entry) => entry.startsWith("AddRemoteHost:")), "no AddRemoteHost for a saved host");
// The failure path keeps the connecting step on screen (act has ended and
// the DOM is committed), so counting log lines here is reliable: at least
// two — connecting + failed.
{
  const logCount = document.querySelectorAll(".remote-wizard__log-line").length;
  ok(logCount >= 2, "connecting step streams the deployment log");
  const connectError = document.querySelector(".remote-wizard__connecting .remote-wizard__error");
  ok(Boolean(connectError?.textContent?.includes("ssh: handshake failed")), "failed connect surfaces the kernel error");
  ok(Boolean(buttonByText("Retry")), "retry action is offered after a failed connect");
  await act(async () => {
    document.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Tab", bubbles: true }));
    await Promise.resolve();
  });
  ok(document.activeElement === buttonByText("Back to edit"), "Tab after a step transition returns focus to the dialog");
}
// ── Retry #1: SSH connects, but the platform check rejects the host ──
await act(async () => {
  buttonByText("Retry")?.click();
  await flush();
});
ok(buttonByText("Cancel")?.disabled === true, "retry keeps the wizard busy while the connection is pending");
await act(async () => {
  await delay(120);
  await flush();
});
ok(tape.includes("CheckRemotePlatform:gpu-box"), "a connected host runs the platform check before the workspace step");
ok(railItems()[1]?.className.includes("--current") === true, "an unsupported OS keeps the wizard on the connecting step");
{
  const platformError = document.querySelector(".remote-wizard__connecting .remote-wizard__error");
  ok(Boolean(platformError?.textContent?.includes("unsupported remote OS")), "the platform failure surfaces the kernel error");
  ok(!tape.some((entry) => entry.startsWith("ListRemoteDir")), "directory browsing never starts for an unsupported host");
  ok(Boolean(buttonByText("Retry")), "retry stays available after a platform rejection");
}
// ── Retry #2: the platform check passes and lands on step ③ ──
await act(async () => {
  buttonByText("Retry")?.click();
  await delay(120);
  await flush();
});
ok(railItems()[0]?.className.includes("--done") === true, "step 1 turns done (green check) after advancing");
ok(railItems()[2]?.className.includes("--current") === true, "step 3 is current after connecting");
ok(document.querySelector<HTMLInputElement>(".remote-wizard__path-input")?.value === "/home/dev", "workspace picker starts at RemoteLastWorkspace path");
ok(Boolean([...document.querySelectorAll(".remote-wizard__dir")].find((b) => b.textContent === "projects")), "directory entries render");
ok(Boolean([...document.querySelectorAll(".remote-wizard__file")].find((row) => row.textContent?.includes("notes.txt"))), "files render in the tree next to folders");
{
  const fileRow = [...document.querySelectorAll<HTMLButtonElement>(".remote-wizard__file")].find((row) => row.textContent?.includes("notes.txt"));
  await act(async () => {
    fileRow?.click();
    await Promise.resolve();
  });
  ok(fileRow?.className.includes("--selected") === true, "clicking a file highlights the row");
  ok(document.querySelector<HTMLInputElement>(".remote-wizard__path-input")?.value === "/home/dev", "clicking a file selects its parent directory as the workspace");
}

await act(async () => {
  [...document.querySelectorAll<HTMLButtonElement>(".remote-wizard__dir")].find((b) => b.textContent === "projects")?.click();
  await flush();
});
ok(Boolean([...document.querySelectorAll(".remote-wizard__dir")].find((b) => b.textContent === "app")), "drilling into a directory lists its children");
ok(!document.querySelector(".remote-wizard__mkdir"), "workspace step has no create-folder controls");

// ── Directory race: an older slow response cannot replace the newer path ──
{
  const pathInput = document.querySelector<HTMLInputElement>(".remote-wizard__path-input");
  await act(async () => {
    if (pathInput) setInput(pathInput, "/slow");
    await Promise.resolve();
  });
  await act(async () => {
    buttonByText("Go")?.click();
    await flush();
  });
  await act(async () => {
    if (pathInput) setInput(pathInput, "/fast");
    await Promise.resolve();
  });
  await act(async () => {
    buttonByText("Go")?.click();
    await flush();
  });
  ok(Boolean([...document.querySelectorAll(".remote-wizard__dir")].find((b) => b.textContent === "latest")), "newer directory response renders first");
  await act(async () => {
    slowDirectory.resolve([{ name: "stale", path: "/slow/stale", isDir: true, size: 0, mtimeUnix: 0, symlink: false }]);
    await flush();
  });
  ok(![...document.querySelectorAll(".remote-wizard__dir")].some((b) => b.textContent === "stale"), "stale directory response cannot overwrite the latest path");
  await act(async () => {
    if (pathInput) setInput(pathInput, "/home/dev/projects");
    await Promise.resolve();
  });
}

// ── Finish: pin, open an in-app session tab, then refresh the tree ──
await act(async () => {
  buttonByText("Connect and open")?.click();
  await flush();
});
ok(tape.includes("OpenRemoteProjectTab:gpu-box:/home/dev/projects:true"), "finish opens the selected workspace in a new remote session tab");
ok(tape.includes("AddRemoteProject:gpu-box:/home/dev/projects"), "finish pins the selected remote workspace");
ok(tape.indexOf("AddRemoteProject:gpu-box:/home/dev/projects") < tape.indexOf("OpenRemoteProjectTab:gpu-box:/home/dev/projects:true"), "the workspace is pinned before its session tab opens");
ok(tape.indexOf("OpenRemoteProjectTab:gpu-box:/home/dev/projects:true") < tape.indexOf("refresh"), "the project tree refreshes after the session tab opens");
ok(tape.includes("close"), "wizard closes after a successful finish");

await act(async () => root.unmount());

// ── Second harness: brand-new host goes through AddRemoteHost ──
const secondRootEl = document.createElement("div");
document.body.appendChild(secondRootEl);
const secondRoot = createRoot(secondRootEl);
await act(async () => {
  secondRoot.render(<WizardHarness />);
});
await act(async () => flush());
const newHostInput = document.querySelector<HTMLInputElement>(".remote-wizard__suggest input");
const newUserInput = [...document.querySelectorAll<HTMLInputElement>("input")].find((i) => i.placeholder.includes("root"));
const newPasswordInput = document.querySelector<HTMLInputElement>(".remote-wizard__field input[type='password']");
await act(async () => {
  if (newHostInput) setInput(newHostInput, "10.9.8.7");
  if (newUserInput) setInput(newUserInput, "root");
  await Promise.resolve();
});
await act(async () => {
  if (newPasswordInput) setInput(newPasswordInput, "s3cret");
  await Promise.resolve();
});
{
  // Credential mode: a segmented control mirroring the download method;
  // picking local-proxy must ride the AddRemoteHost payload.
  const segButtons = Array.from(document.querySelectorAll<HTMLButtonElement>(".remote-wizard__field .provider-add-segmented__item"));
  const localProxy = segButtons.find((b) => b.textContent?.includes("本机") || b.textContent?.includes("this computer"));
  ok(Boolean(localProxy), "wizard host form offers the credential-mode segmented control");
  await act(async () => {
    localProxy?.click();
    await Promise.resolve();
  });
  ok(localProxy?.className.includes("--active") === true, "local-proxy segment highlights when selected");
}
await act(async () => {
  buttonByText("Next")?.click();
  await flush();
});
ok(tape.some((entry) => entry.startsWith("AddRemoteHost:10.9.8.7:10.9.8.7")), "a new host is added (label defaults to the host)");
ok(lastAddInput?.credentialMode === "local-proxy", `AddRemoteHost carries the chosen credential mode (got ${lastAddInput?.credentialMode})`);

await act(async () => secondRoot.unmount());

// ── Merged finish: source contract for overlapping workspaces ──
const here = dirname(fileURLToPath(import.meta.url));
const wizardSource = readFileSync(resolve(here, "../components/RemoteConnectWizard.tsx"), "utf8");
ok(
  /const canonical = project\.merged \? project\.workspace : target;/.test(wizardSource) &&
    /OpenRemoteProjectTab\(hostId, canonical, \{ newSession: true \}\)/.test(wizardSource),
  "a merged finish opens the tab on the canonical workspace",
);
ok(
  /if \(!project\.merged\) \{[\s\S]*?RemoveRemoteProject\(hostId, target\)/.test(wizardSource),
  "rollback only removes a pin the wizard actually added (a merge owns none)",
);
ok(
  /onMerged\?\.\(t\("remoteWizard\.mergedProject"/.test(wizardSource),
  "a merged finish notifies through onMerged",
);

dom.window.close();
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
