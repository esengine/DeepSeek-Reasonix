/**
 * Dual-project multi-tab smoke: open two workspaces, list tree, activate each.
 * Requires REASONIX_BIN (or repo bin/reasonix) with --multi-tab support.
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { ServeSupervisor } from "../lib/serveSupervisor.mjs";
import { resolveReasonixBin } from "../lib/resolveReasonixBin.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(packageRoot, "..");

async function request(baseUrl, token, p, init = {}) {
  const url = new URL(p, baseUrl.endsWith("/") ? baseUrl : baseUrl + "/");
  url.searchParams.set("token", token);
  const headers = { ...(init.headers || {}), Cookie: `reasonix_token=${token}` };
  if (init.json !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(init.json);
  }
  const res = await fetch(url.toString(), { ...init, headers, redirect: "manual" });
  const text = await res.text();
  if (!res.ok && res.status !== 204) {
    throw new Error(`${init.method || "GET"} ${p} -> ${res.status} ${text.slice(0, 200)}`);
  }
  if (!text || res.status === 204) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

async function main() {
  const bin = resolveReasonixBin({ env: process.env, repoRoot });
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "rx-dual-home-"));
  const wsA = fs.mkdtempSync(path.join(os.tmpdir(), "rx-dual-a-"));
  const wsB = fs.mkdtempSync(path.join(os.tmpdir(), "rx-dual-b-"));
  fs.writeFileSync(path.join(wsA, "a.txt"), "a");
  fs.writeFileSync(path.join(wsB, "b.txt"), "b");
  process.env.REASONIX_HOME = home;
  process.env.HOME = home;

  const supervisor = new ServeSupervisor({
    bin,
    repoRoot,
    workspace: wsA,
    crashRestartOnce: false,
  });
  const info = await supervisor.start();
  const base = info.baseUrl;
  const token = info.token;
  console.log("serve", base);

  try {
    const tabA = await request(base, token, "/tabs/open-project", {
      method: "POST",
      json: { workspaceRoot: wsA, topicId: "main", topicTitle: "A" },
    });
    const tabB = await request(base, token, "/tabs/open-project", {
      method: "POST",
      json: { workspaceRoot: wsB, topicId: "main", topicTitle: "B" },
    });
    if (!tabA?.id || !tabB?.id) throw new Error("open-project missing id");
    if (tabA.id === tabB.id) throw new Error("expected distinct tabs");

    const tabs = await request(base, token, "/tabs");
    if (!Array.isArray(tabs) || tabs.length < 2) throw new Error(`tabs=${JSON.stringify(tabs)}`);

    await request(base, token, `/tabs/${tabA.id}/activate`, { method: "POST", json: {} });
    await request(base, token, `/tabs/${tabB.id}/activate`, { method: "POST", json: {} });

    const topic = await request(base, token, "/desktop/topics", {
      method: "POST",
      json: { scope: "project", workspaceRoot: wsA, title: "Smoke Topic" },
    });
    if (!topic?.id) throw new Error("create topic failed");

    const tree = await request(base, token, "/desktop/project-tree");
    if (!Array.isArray(tree) || tree.length < 2) {
      throw new Error(`project-tree too small: ${JSON.stringify(tree)}`);
    }

    const startup = await request(base, token, "/desktop/startup-settings");
    if (!startup || typeof startup !== "object") throw new Error("startup-settings missing");

    console.log("PASS dual-project smoke", {
      tabs: tabs.length,
      tree: tree.length,
      topic: topic.id,
      workspaces: [wsA, wsB],
    });
  } finally {
    await supervisor.stop?.().catch(() => {});
    try {
      process.kill(info.pid, "SIGTERM");
    } catch {
      /* ignore */
    }
  }
}

main().catch((e) => {
  console.error("FAIL", e);
  process.exit(1);
});
