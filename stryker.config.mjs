// @ts-check
/** @type {import('@stryker-mutator/api/core').StrykerOptions} */
const config = {
  // Vitest runner.
  testRunner: "vitest",
  plugins: ["@stryker-mutator/vitest-runner"],

  // Ignore symlinks and large dirs that stryker can't copy.
  ignorePatterns: ["home_sessions", ".reasonix", "node_modules"],

  // Only mutate the files we changed (focus on PauseGate + tool integrations).
  mutate: [
    "src/core/pause-gate.ts",
    "src/tools/shell.ts",
    "src/tools/plan-core.ts",
    "src/tools/choice.ts",
    "src/loop.ts",
  ],

  // Run only relevant test files for the mutated code.
  testRunnerNodeArgs: ["--experimental-vm-modules"],
  vitest: {
    configFile: "vitest.config.ts",
  },

  // Only run tests relevant to the mutated files.
  testFiles: [
    "tests/pause-gate.test.ts",
    "tests/shell-tools.test.ts",
    "tests/plan.test.ts",
    "tests/choice.test.ts",
  ],

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
