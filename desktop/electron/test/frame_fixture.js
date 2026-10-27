"use strict";
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { seedHome } = require("./livekit");

function frameFixture() {
  const root = path.resolve(__dirname, "../../..");
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "rx-frame-home-"));
  const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "rx-frame-ws-"));
  process.env.REASONIX_HOME = home;
  seedHome(home, "http://127.0.0.1:9", workspace);
  const plugin = path.join(home, "plugins/task-board");
  fs.cpSync(path.join(root, "sdk/go/examples/frameextension"), plugin, { recursive: true });
  fs.mkdirSync(path.join(plugin, "bin"));
  execFileSync("go", ["build", "-o", path.join(plugin, "bin/task-board.exe"), "./examples/frameextension"], { cwd: path.join(root, "sdk/go") });
  fs.writeFileSync(path.join(home, "plugin-packages.json"), JSON.stringify({ version: 1, plugins: [
    { name: "task-board", root: "plugins/task-board", version: "0.1.0", enabled: true },
  ] }));
  // Observes the document's real port without replacing its SDK example handler.
  fs.appendFileSync(path.join(plugin, "frames/board.html"), `<script>
    window.probe = { received: [] };
    addEventListener('message', event => {
      if (event.data?.type !== 'connect' || !event.ports[0]) return;
      probe.port = event.ports[0];
      probe.port.addEventListener('message', message => probe.received.push(message.data));
    });
  </script>`);
  return { home, workspace };
}

module.exports = { frameFixture };
