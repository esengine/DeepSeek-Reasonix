import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  compareDistTagVersions,
  publishPackages,
} from "./publish.mjs";

const candidateSha = "a".repeat(40);

function splitPackageSpec(spec) {
  const separator = spec.lastIndexOf("@");
  return [spec.slice(0, separator), spec.slice(separator + 1)];
}

function fixture(
  t,
  version = "1.5.0-canary.42",
  { forbidCleanup = false, visibilityDelayReads = 0 } = {},
) {
  const root = mkdtempSync(join(tmpdir(), "reasonix-npm-publish-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));

  const packages = ["@reasonix/cli-linux-x64", "reasonix"].map((name, index) => {
    const dir = join(root, `package-${index}`);
    mkdirSync(dir);
    writeFileSync(
      join(dir, "package.json"),
      `${JSON.stringify({ name, version, reasonixCandidateSha: candidateSha })}\n`,
    );
    return { name, dir };
  });
  const registry = new Map(
    packages.map(({ name }) => [name, { versions: new Map(), tags: new Map() }]),
  );
  const calls = [];
  const hiddenReads = new Map();

  function packageState(name) {
    if (!registry.has(name)) {
      registry.set(name, { versions: new Map(), tags: new Map() });
    }
    return registry.get(name);
  }

  function runner(args, { cwd, missingOk = false } = {}) {
    calls.push({ args: [...args], cwd });
    if (args[0] === "view" && args[2]?.startsWith("dist-tags.")) {
      const state = registry.get(args[1]);
      if (!state) return missingOk ? null : "";
      const value = state.tags.get(args[2].slice("dist-tags.".length));
      return value ? JSON.stringify(value) : "";
    }
    if (args[0] === "view") {
      const [name, requestedVersion] = splitPackageSpec(args[1]);
      const packageSpec = `${name}@${requestedVersion}`;
      const remainingHiddenReads = hiddenReads.get(packageSpec) ?? 0;
      if (remainingHiddenReads > 0) {
        hiddenReads.set(packageSpec, remainingHiddenReads - 1);
        return missingOk ? null : "";
      }
      const metadata = registry.get(name)?.versions.get(requestedVersion);
      return metadata ? JSON.stringify(metadata) : missingOk ? null : "";
    }
    if (args[0] === "publish") {
      const pkg = JSON.parse(readFileSync(join(cwd, "package.json"), "utf8"));
      const state = packageState(pkg.name);
      if (state.versions.has(pkg.version)) {
        throw new Error(`version already exists: ${pkg.name}@${pkg.version}`);
      }
      state.versions.set(pkg.version, {
        name: pkg.name,
        version: pkg.version,
        reasonixCandidateSha: pkg.reasonixCandidateSha,
        gitHead: pkg.reasonixCandidateSha,
      });
      state.tags.set(args[args.indexOf("--tag") + 1], pkg.version);
      hiddenReads.set(`${pkg.name}@${pkg.version}`, visibilityDelayReads);
      return "";
    }
    if (args[0] === "dist-tag" && args[1] === "add") {
      const [name, requestedVersion] = splitPackageSpec(args[2]);
      assert.ok(packageState(name).versions.has(requestedVersion));
      packageState(name).tags.set(args[3], requestedVersion);
      return "";
    }
    if (args[0] === "dist-tag" && args[1] === "rm") {
      if (forbidCleanup) {
        throw new Error("npm dist-tag failed with exit code 1: npm error code E403");
      }
      packageState(args[2]).tags.delete(args[3]);
      return "";
    }
    throw new Error(`unsupported fake npm invocation: ${args.join(" ")}`);
  }

  function publish({ attempts = 2 } = {}) {
    const options = {
      packages,
      version,
      candidateSha,
      runner,
      sleep: () => {},
      log: () => {},
    };
    if (attempts !== null) options.attempts = attempts;
    return publishPackages(options);
  }

  function addVersion(name, publishedVersion = version, sha = candidateSha) {
    packageState(name).versions.set(publishedVersion, {
      name,
      version: publishedVersion,
      reasonixCandidateSha: sha,
      gitHead: sha,
    });
  }

  return { packages, registry, calls, publish, addVersion };
}

test("reuses a fully published npm candidate without republishing", (t) => {
  const fx = fixture(t);
  for (const { name } of fx.packages) {
    fx.addVersion(name);
    fx.registry.get(name).tags.set("canary", "1.5.0-canary.42");
  }

  assert.deepEqual(fx.publish(), {
    distTag: "canary",
    version: "1.5.0-canary.42",
  });
  assert.equal(fx.calls.filter(({ args }) => args[0] === "publish").length, 0);
});

test("fills a partially published package set before advancing canary", (t) => {
  const fx = fixture(t);
  fx.addVersion(fx.packages[0].name);
  for (const { name } of fx.packages) {
    fx.registry.get(name).tags.set("canary", "1.5.0-canary.41");
  }

  fx.publish();

  const publishes = fx.calls.filter(({ args }) => args[0] === "publish");
  assert.equal(publishes.length, 1);
  assert.equal(publishes[0].cwd, fx.packages[1].dir);
  for (const { name } of fx.packages) {
    assert.equal(fx.registry.get(name).tags.get("canary"), "1.5.0-canary.42");
    assert.equal(fx.registry.get(name).tags.has("canary-staging"), false);
  }
});

test("waits through multi-minute npm registry visibility lag", (t) => {
  const fx = fixture(t, "1.5.0-canary.42", { visibilityDelayReads: 18 });

  assert.doesNotThrow(() => fx.publish({ attempts: null }));
  for (const { name } of fx.packages) {
    assert.equal(fx.registry.get(name).tags.get("canary"), "1.5.0-canary.42");
  }
});

test("rejects an immutable package owned by another candidate", (t) => {
  const fx = fixture(t);
  fx.addVersion(fx.packages[0].name, "1.5.0-canary.42", "b".repeat(40));

  assert.throws(
    () => fx.publish(),
    /belongs to candidate b{40}, expected a{40}/,
  );
  assert.equal(fx.calls.filter(({ args }) => args[0] === "publish").length, 0);
});

test("stale recovery publishes missing packages without rolling canary back", (t) => {
  const fx = fixture(t);
  fx.addVersion(fx.packages[0].name);
  for (const { name } of fx.packages) {
    fx.addVersion(name, "1.5.0-canary.43");
    fx.registry.get(name).tags.set("canary", "1.5.0-canary.43");
  }

  fx.publish();

  assert.ok(
    fx.registry
      .get(fx.packages[1].name)
      .versions.has("1.5.0-canary.42"),
  );
  for (const { name } of fx.packages) {
    assert.equal(fx.registry.get(name).tags.get("canary"), "1.5.0-canary.43");
  }
  assert.equal(
    fx.calls.some(
      ({ args }) =>
        args[0] === "dist-tag" &&
        args[1] === "add" &&
        args[3] === "canary",
    ),
    false,
  );
});

test("accepts legacy packages whose gitHead proves the candidate", (t) => {
  const fx = fixture(t);
  for (const { name } of fx.packages) {
    fx.registry.get(name).versions.set("1.5.0-canary.42", {
      name,
      version: "1.5.0-canary.42",
      gitHead: candidateSha,
    });
    fx.registry.get(name).tags.set("canary", "1.5.0-canary.42");
  }

  assert.doesNotThrow(() => fx.publish());
});

test("does not fail a completed publish when npm forbids staging cleanup", (t) => {
  const fx = fixture(t, "1.5.0-canary.42", { forbidCleanup: true });

  assert.doesNotThrow(() => fx.publish());
  for (const { name } of fx.packages) {
    assert.equal(fx.registry.get(name).tags.get("canary"), "1.5.0-canary.42");
    assert.equal(
      fx.registry.get(name).tags.get("canary-staging"),
      "1.5.0-canary.42",
    );
  }
});

test("compares channel versions without integer truncation", () => {
  assert.equal(
    compareDistTagVersions(
      "canary",
      "1.5.0-canary.100000000000000000000",
      "1.5.0-canary.99999999999999999999",
    ),
    1,
  );
  assert.equal(
    compareDistTagVersions("latest", "2.0.0", "10.0.0"),
    -1,
  );
  assert.equal(
    compareDistTagVersions("next", "1.5.0-rc.10", "1.5.0-rc.2"),
    1,
  );
});
