#!/usr/bin/env node
// Pre-step for `npm run dev`: build the Rust renderer once before tsx
// hands off. Silent no-op when cargo isn't installed (TS-only contributors)
// or when the source tree isn't present (published install). The resolver
// then picks the freshly-built target/release binary instead of falling
// back to `cargo run` (slower per-invocation overhead) or a stale binary.

import { spawnSync } from "node:child_process";

const cargoCheck = spawnSync("cargo", ["--version"], { stdio: "ignore" });
if (cargoCheck.status !== 0) {
  process.exit(0);
}

const build = spawnSync("cargo", ["build", "--release", "--quiet", "--bin", "reasonix-render"], {
  stdio: "inherit",
});

if (build.status !== 0) {
  process.stderr.write(
    "▲ cargo build for reasonix-render failed — the resolver will fall back to `cargo run` (slower) or the Node TUI.\n",
  );
}
process.exit(0);
