import { readdirSync } from "node:fs";

// Union of the former Linux desktop unit commands, including pnpm's pretest.
// Dedicated entrypoints remain authoritative for tests needing asset loaders.
export const ciUnitScripts = [
  "test:terminal", "test:task-monitor", "test:mcp-app", "test:workspace",
  "test:stream", "test:motion", "test:composer", "test:todo-visibility",
  "test:app-lifecycle", "test:all", "test:transcript", "test:usage-stats",
];

export function testPlan(scripts, discovered) {
  const explicit = new Map();
  let discover = false;
  function add(args) {
    if (args[0] === "node" && args[1] === "scripts/run-tests.mjs") {
      discover = true;
      return;
    }
    // Keep Node and tsx invocation modes, including custom loaders, intact.
    const testPath = args.find(arg => /^src\/.*\.(?:test\.tsx?|tsx)$/.test(arg));
    const key = testPath ?? JSON.stringify(args);
    const previous = explicit.get(key);
    if (previous && JSON.stringify(previous.args) !== JSON.stringify(args)) throw new Error(`conflicting test invocation: ${key}`);
    explicit.set(key, { key, args, serial: key.endsWith("history-performance-benchmark.tsx") });
  }
  function expand(name, stack = []) {
    if (stack.includes(name) || typeof scripts[name] !== "string") throw new Error(`invalid script dependency: ${name}`);
    for (const hook of [`pre${name}`, name, `post${name}`]) {
      if (hook !== name && !scripts[hook]) continue;
      const body = scripts[hook];
      for (const command of body.split(" && ")) {
        // Fail closed when the command grammar changes; never silently omit work.
        if (/["'|;&<>`$\n]/.test(command)) throw new Error(`unsupported CI test command: ${command}`);
        const args = command.trim().split(/\s+/);
        if (args[0] === "pnpm" && args.length === 2) expand(args[1], [...stack, name]);
        else if (["tsx", "node", "tsc"].includes(args[0])) add(args);
        else throw new Error(`unsupported CI test command: ${command}`);
      }
    }
  }
  for (const name of ciUnitScripts) expand(name);
  if (!discover) throw new Error("CI must include discovery of new frontend tests");
  for (const file of discovered.sort()) {
    const key = `src/__tests__/${file}`;
    if (!explicit.has(key)) add(["tsx", "--import", "./scripts/css-stub-register.mjs", key]);
  }
  return [...explicit.values()];
}

export function discoveredTests(directory = "src/__tests__") {
  return readdirSync(directory).filter(name => /\.test\.tsx?$/.test(name));
}
