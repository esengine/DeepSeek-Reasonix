import assert from "node:assert/strict";
import { sep } from "node:path";
import { test } from "node:test";
import { reasonixHome } from "./home.js";

const unix = (env: NodeJS.ProcessEnv, platform: NodeJS.Platform = "darwin") =>
  reasonixHome({ env, platform, homedir: () => "/home/fallback", cwd: () => "/work" });

test("REASONIX_HOME wins, with ~ and ${VAR} expansion and relative paths made absolute", () => {
  assert.equal(unix({ REASONIX_HOME: "/data/rx", HOME: "/home/u" }), "/data/rx");
  assert.equal(unix({ REASONIX_HOME: "  /data/rx/  ", HOME: "/home/u" }), "/data/rx");
  assert.equal(unix({ REASONIX_HOME: "~/rx", HOME: "/home/u" }), "/home/u/rx");
  assert.equal(unix({ REASONIX_HOME: "~", HOME: "/home/u" }), "/home/u");
  assert.equal(unix({ REASONIX_HOME: "${BASE}/rx", BASE: "/srv", HOME: "/home/u" }), "/srv/rx");
  assert.equal(unix({ REASONIX_HOME: "${MISSING:-/opt}/rx", HOME: "/home/u" }), "/opt/rx");
  assert.equal(unix({ REASONIX_HOME: "rel/rx", HOME: "/home/u" }), "/work/rel/rx");
});

test("macOS and Linux default to ~/.reasonix from $HOME, then the passwd home", () => {
  assert.equal(unix({ HOME: "/home/u" }), "/home/u/.reasonix");
  assert.equal(unix({ HOME: "/home/u" }, "linux"), "/home/u/.reasonix");
  assert.equal(unix({}), "/home/fallback/.reasonix");
});

test("Windows uses %APPDATA%\\reasonix with the Roaming fallback", () => {
  const win = (env: NodeJS.ProcessEnv) => reasonixHome({ env, platform: "win32", homedir: () => "", cwd: () => "C:\\work" });
  assert.equal(win({ APPDATA: "C:\\Users\\u\\AppData\\Roaming" }), ["C:\\Users\\u\\AppData\\Roaming", "reasonix"].join(sep));
  assert.equal(win({ USERPROFILE: "C:\\Users\\u" }), ["C:\\Users\\u", "AppData", "Roaming", "reasonix"].join(sep));
});
