"use strict";
// Drives computer use end to end in the real shell: a scripted model asks the
// real kernel to read and operate a real application through the helper this
// shell hands the kernel, and the checks read what that application received.
// Run with `pnpm computer-live` on macOS, with Accessibility and Screen Recording
// granted to whatever launches it; it needs no network.
const os = require("node:os");
const path = require("node:path");
const fs = require("node:fs");
const { execFileSync, spawnSync } = require("node:child_process");
const { app, screen } = require("electron");
const { checker, listen, scriptedModel, seedHome, settledWindow, until, wait } = require("./livekit");

const BUNDLE = "io.reasonix.test.computer-target";
const { check, failures } = checker();

// buildTarget puts the probe application into a bundle, so it runs with an
// identity like any other application, and opens it in the background.
function buildTarget(dir) {
  const macos = path.join(dir, "Target.app", "Contents", "MacOS");
  fs.mkdirSync(macos, { recursive: true });
  fs.writeFileSync(path.join(dir, "Target.app", "Contents", "Info.plist"), `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>${BUNDLE}</string>
<key>CFBundleExecutable</key><string>target</string>
<key>CFBundleName</key><string>Computer Target</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>`);
  const source = path.join(__dirname, "..", "..", "..", "internal", "computer", "testdata", "target.swift");
  const built = spawnSync("swiftc", ["-O", source, "-o", path.join(macos, "target")], { stdio: "inherit" });
  if (built.status !== 0) throw new Error("the probe application did not build");
  const log = path.join(dir, "target.log");
  execFileSync("open", ["-g", "-n", path.join(dir, "Target.app"), "--args", log]);
  return { log, binary: path.join(macos, "target") };
}

const logged = (log) => (fs.existsSync(log) ? fs.readFileSync(log, "utf8") : "");

function operatingModel() {
  return scriptedModel((tools) => {
    const last = tools.length ? tools[tools.length - 1] : "";
    const ref = (role, name) => (last.match(new RegExp(`- ${role} "${name}" \\[(a\\d+)\\]`)) || [])[1];
    switch (tools.length) {
      case 0:
        return { name: "computer_read", arguments: { what: "apps" } };
      case 1:
        return { name: "computer_read", arguments: { what: "snapshot", app: BUNDLE } };
      case 2:
        return { name: "computer_act", arguments: { app: BUNDLE, steps: [
          { action: "set_value", ref: ref("textField", "Probe field"), text: "from-studio" },
          { action: "click", ref: ref("button", "Probe button") },
          { action: "focus", ref: ref("textField", "Probe field") },
          { action: "type", text: " 李雷" },
        ] } };
      case 3:
        return { name: "computer_read", arguments: { what: "screenshot", app: BUNDLE } };
      case 4:
        return { name: "computer_act", arguments: { app: BUNDLE, steps: [{ action: "pointer_click", x: 100, y: 180 }] } };
      default:
        return null;
    }
  });
}

const frontmost = () => execFileSync("lsappinfo", ["front"]).toString().trim();

// answering keeps saying what a person would to whatever the kernel asks, in
// order, and records how many it was asked. A turn is blocked while one waits,
// so it runs alongside the turn rather than after it.
function answering(client, base, answers) {
  const asked = [];
  let stopped = false;
  const loop = (async () => {
    while (!stopped) {
      const status = await client.json("GET", `${base}/status`).catch(() => null);
      for (const decision of (status && status.decisions) || []) {
        if (asked.includes(decision.id)) continue;
        asked.push(decision.id);
        const answer = answers[asked.length - 1] || { allow: false };
        await client.request("POST", `${base}/approve`, { id: decision.id, allow: answer.allow, session: !!answer.session, persist: false });
      }
      await wait(100);
    }
  })();
  return { asked, stop: async () => { stopped = true; await loop; } };
}

async function main() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-computer-live-"));
  let target = null;
  try {
    target = buildTarget(dir);
    await until("the probe application", async () => logged(target.log).includes("ready"), 15000);
    const { handler, requests } = operatingModel();
    const model = await listen(handler);
    const home = path.join(dir, "home");
    const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "rx-computer-ws-"));
    process.env.REASONIX_HOME = home;
    // Not yolo: yolo answers every prompt, and what this run reads is which
    // prompts a person is given.
    seedHome(home, model.url, workspace, { toolApproval: "auto" });
    const { current } = require("../src/main.js");

    await settledWindow();
    const { client } = current();
    const runtimes = await until("a pane", async () => {
      const list = await client.json("GET", "/runtimes");
      return Array.isArray(list) && list.length ? list : null;
    });
    await wait(500);
    const pointer = screen.getCursorScreenPoint();
    const front = frontmost();
    // The application is granted for the session; the pointer is refused.
    // The application is granted for the session, twice: listing what is
    // running, then this application. Operating it rides that grant. The
    // pointer does not, and that third question is the one refused.
    const person = answering(client, runtimes[0].base, [
      { allow: true, session: true },
      { allow: true, session: true },
      { allow: false },
    ]);
    await client.request("POST", `${runtimes[0].base}/submit`, { input: "put from-studio 李雷 into the probe, then click the drawn view" });
    await until("the scripted turn", async () => requests.length >= 6, 90000);
    await person.stop();

    const results = (requests[5].messages || []).filter((m) => m.role === "tool").map((m) => String(m.content || ""));
    check("the kernel was given computer use", results.length === 5, results.length);
    check("the model saw the probe among the applications", (results[0] || "").includes(`${BUNDLE} — `), (results[0] || "").slice(0, 400));
    check("the snapshot carried refs", /textField "Probe field" \[a\d+\]/.test(results[1] || ""), (results[1] || "").slice(0, 400));
    check("every step ran", (results[2] || "").includes("Completed 4 of 4 step(s)."), (results[2] || "").slice(0, 600));
    await until("the input in the application", async () => logged(target.log).includes("text from-studio 李雷"), 10000).catch(() => {});
    const received = logged(target.log);
    check("the button was pressed", received.includes("button pressed"), received);
    check("the text arrived", received.includes("text from-studio 李雷"), received);
    check("the pointer is asked for although the application is already granted", person.asked.length === 3, person.asked);
    check("a refused pointer never reached the application", !received.includes("mouseDown"), received);
    check("the model was told the pointer was refused", /declin|refus|denied/i.test(results[4] || ""), (results[4] || "").slice(0, 300));
    const after = screen.getCursorScreenPoint();
    check("the person's pointer did not move", after.x === pointer.x && after.y === pointer.y, { pointer, after });
    check("the frontmost application did not change", frontmost() === front, { front, now: frontmost() });
  } catch (err) {
    check("the run completed", false, err.message);
  } finally {
    if (target) spawnSync("pkill", ["-f", target.binary]);
  }
  process.stdout.write(failures.length ? `${failures.length} check(s) failed\n` : "all checks passed\n");
  app.exit(failures.length ? 1 : 0);
}

app.whenReady().then(() => setTimeout(main, 0));
