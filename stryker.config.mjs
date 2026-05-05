// @ts-check
/** @type {import('@stryker-mutator/api/core').StrykerOptions} */
const config = {
  // Vitest runner.
  testRunner: "vitest",
  plugins: ["@stryker-mutator/vitest-runner"],

  // Ignore symlinks and large dirs that stryker can't copy.
  ignorePatterns: ["home_sessions", ".reasonix", "node_modules"],

  // Target load-bearing modules — keeps runs fast (~minutes) so contributors
  // actually run it. UI, MCP transport, renderer, and TUI primitives are
  // better tested by snapshot/integration than mutation.
  mutate: [
    "src/loop.ts",
    "src/context-manager.ts",
    "src/core/**/*.ts",
    "src/tools/shell.ts",
    "src/tools/plan-core.ts",
    "src/tools/choice.ts",
    "src/repair/**/*.ts",
  ],

  // Run only the test files that cover the mutated modules.
  testFiles: [
    "tests/loop.test.ts",
    "tests/shell-tools.test.ts",
    "tests/plan.test.ts",
    "tests/choice.test.ts",
    "tests/repair/*.test.ts",
  ],

  testRunnerNodeArgs: ["--experimental-vm-modules"],
  vitest: {
    configFile: "vitest.config.ts",
  },

  // Thresholds — fail if mutation score drops below this.
  thresholds: {
    high: 80,
    low: 60,
    break: 50,
  },

  // Concurrency; adjust based on your machine.
  concurrency: 4,

  // Clear timeout large enough for the full suite.
  timeoutMS: 60000,
};

export default config;
