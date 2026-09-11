import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { signMacOS } from "./sign-macos.mjs";

test("signing requires an explicit app and identity", async () => {
  await assert.rejects(signMacOS("fixture.app"), /identity are required/);
});

test("signs both architectures of resource sidecars and framework binaries before sealing", {
  skip: process.platform !== "darwin",
}, async () => {
  const root = mkdtempSync(join(tmpdir(), "reasonix-signing-test-"));
  try {
    const app = join(root, "Reasonix.app");
    const plist = (executable, type) => `<?xml version="1.0"?><plist version="1.0"><dict>
      <key>CFBundleIdentifier</key><string>io.reasonix.fixture.${executable}</string>
      <key>CFBundleExecutable</key><string>${executable}</string>
      <key>CFBundlePackageType</key><string>${type}</string>
      <key>CFBundleVersion</key><string>1</string></dict></plist>`;
    const put = (name, contents) => {
      mkdirSync(dirname(join(app, name)), { recursive: true });
      writeFileSync(join(app, name), contents);
    };
    put("Contents/Info.plist", plist("Reasonix", "APPL"));
    put("Contents/Frameworks/Electron Framework.framework/Versions/A/Resources/Info.plist", plist("Electron Framework", "FMWK"));
    put("Contents/Frameworks/Squirrel.framework/Versions/A/Resources/Info.plist", plist("Squirrel", "FMWK"));
    const binaries = [
      "Contents/MacOS/Reasonix",
      "Contents/MacOS/reasonix-desktop",
      "Contents/Resources/service/reasonix",
      "Contents/Resources/service/reasonix-desktop",
      "Contents/Frameworks/Electron Framework.framework/Versions/A/Electron Framework",
      "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libffmpeg.dylib",
      "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libvk_swiftshader.dylib",
      "Contents/Frameworks/Squirrel.framework/Versions/A/Squirrel",
      "Contents/Frameworks/Squirrel.framework/Versions/A/Resources/ShipIt",
    ];
    const source = join(root, "fixture.c");
    writeFileSync(source, "int main(void) { return 0; }\n");
    for (const relative of binaries) {
      const file = join(app, relative);
      mkdirSync(dirname(file), { recursive: true });
      const dylib = relative.endsWith(".dylib") || /\/(Electron Framework|Squirrel)$/.test(relative);
      execFileSync("clang", ["-arch", "arm64", "-arch", "x86_64", ...(dylib ? ["-dynamiclib"] : []), source, "-o", file]);
      execFileSync("codesign", ["--remove-signature", file]);
      if (relative.endsWith(".dylib")) chmodSync(file, 0o644);
    }
    // Valid versioned framework symlinks are essential to exercise real seals.
    for (const name of ["Electron Framework", "Squirrel"]) {
      const framework = join(app, "Contents/Frameworks", `${name}.framework`);
      execFileSync("ln", ["-s", "A", join(framework, "Versions/Current")]);
      execFileSync("ln", ["-s", `Versions/Current/${name}`, join(framework, name)]);
      execFileSync("ln", ["-s", "Versions/Current/Resources", join(framework, "Resources")]);
    }
    // Reproduce the original false positive: bundle verification passes while
    // code stored as a resource still has no signature.
    execFileSync("codesign", ["--force", "--deep", "--options", "runtime", "-s", "-", app]);
    execFileSync("codesign", ["--verify", "--deep", "--strict", app]);
    assert.notEqual(spawnSync("codesign", ["--verify", join(app, binaries[2])]).status, 0);

    // The control above signs MacOS siblings. Start the new signer from fully
    // unsigned code so it must order those siblings before the main app seal.
    for (const relative of binaries) {
      execFileSync("codesign", ["--remove-signature", join(app, relative)]);
    }

    await signMacOS(app, "-");
    for (const relative of binaries) {
      for (const arch of ["arm64", "x86_64"]) {
        const result = spawnSync("codesign", ["--display", "--verbose=4", "--arch", arch, join(app, relative)], { encoding: "utf8" });
        assert.equal(result.status, 0, `${relative} (${arch}): ${result.stderr}`);
        assert.match(result.stderr, /flags=.*\(.*runtime.*\)/, `${relative} (${arch})`);
      }
      execFileSync("codesign", ["--verify", "--strict", "--all-architectures", join(app, relative)]);
    }
    execFileSync("codesign", ["--verify", "--deep", "--strict", "--all-architectures", app]);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
