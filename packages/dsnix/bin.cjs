#!/usr/bin/env node
const { spawn } = require("node:child_process");

const entry = require.resolve("reasonix/dist/cli/index.js");
const child = spawn(process.execPath, [entry, ...process.argv.slice(2)], {
  stdio: "inherit",
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 1);
});
