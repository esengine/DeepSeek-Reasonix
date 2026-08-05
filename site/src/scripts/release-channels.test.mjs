import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import {
  CLI_RELEASE_ASSETS,
  cliUpgradeCommand,
  cliReleaseModel,
  desktopGitHubReleaseModel,
  desktopReleaseModel,
  fetchFirstJSON,
  releaseAssetMap,
  releaseVersionLabel,
  selectCLIRelease,
} from "./release-channels.js";

function cliAssets(tag, missing = []) {
  const skip = new Set(missing);
  return CLI_RELEASE_ASSETS.filter((name) => !skip.has(name)).map((name) => ({
    name,
    browser_download_url: `https://github.com/esengine/DeepSeek-Reasonix/releases/download/${tag}/${name}`,
    size: 42,
  }));
}

const desktopSHA256 = "a".repeat(64);

function desktopManifest(version, base) {
  const releaseBase = base || `https://dl.reasonix.io/desktop-${version}/`;
  const asset = (name) => {
    const url = releaseBase + name;
    return { url, sig: `${url}.minisig`, size: 42, sha256: desktopSHA256 };
  };
  return {
    version,
    download_page: "https://reasonix.io/?download=desktop#start",
    platforms: {
      "darwin-arm64": asset("Reasonix-darwin-arm64.zip"),
      "darwin-amd64": asset("Reasonix-darwin-amd64.zip"),
      "windows-amd64": asset("Reasonix-windows-amd64-installer.exe"),
      "windows-arm64": asset("Reasonix-windows-arm64-installer.exe"),
      "linux-amd64": asset("Reasonix-linux-amd64.tar.gz"),
    },
    native_packages: {
      "linux-amd64": asset("Reasonix-linux-amd64.deb"),
    },
    downloads: {
      "Reasonix-darwin-universal.dmg": asset("Reasonix-darwin-universal.dmg"),
      "Reasonix-windows-amd64.zip": asset("Reasonix-windows-amd64.zip"),
    },
  };
}

function desktopGitHubRelease(version = "v1.17.21") {
  const tag = `desktop-${version}`;
  const names = [
    "Reasonix-darwin-arm64.zip",
    "Reasonix-darwin-amd64.zip",
    "Reasonix-windows-amd64-installer.exe",
    "Reasonix-windows-arm64-installer.exe",
    "Reasonix-linux-amd64.tar.gz",
    "Reasonix-linux-amd64.deb",
    "Reasonix-darwin-universal.dmg",
    "Reasonix-windows-amd64.zip",
  ];
  return {
    tag_name: tag,
    draft: false,
    prerelease: false,
    assets: names.map((name) => ({
      name,
      browser_download_url: `https://github.com/esengine/DeepSeek-Reasonix/releases/download/${tag}/${name}`,
      size: 42,
    })),
  };
}

function copy(value) {
  return structuredClone(value);
}

test("CLI always emits the official upgrade command", () => {
  assert.equal(cliUpgradeCommand("stable"), "reasonix upgrade");
  assert.equal(cliUpgradeCommand("preview"), "reasonix upgrade");
  assert.equal(cliUpgradeCommand("canary"), "reasonix upgrade");
});

test("release labels use a complete version or a readable fallback", () => {
  assert.equal(releaseVersionLabel({ version: "v1.17.21" }), "v1.17.21");
  assert.equal(releaseVersionLabel({ version: "v1.18.0-preview.62" }), "v1.18.0-preview.62");
  assert.equal(releaseVersionLabel(null), "latest");
});

test("site placeholders do not render the synthetic version vlatest", async () => {
  const [home, docs, siteScript] = await Promise.all([
    readFile(new URL("../pages/index.astro", import.meta.url), "utf8"),
    readFile(new URL("../pages/docs.astro", import.meta.url), "utf8"),
    readFile(new URL("./site.js", import.meta.url), "utf8"),
  ]);
  assert.doesNotMatch(home, /v<span (?:class="rxv"|data-release-version=)/);
  assert.doesNotMatch(docs, /v<span data-release-version=/);
  assert.doesNotMatch(home, /<a[^>]*data-cli-asset=[^>]*\sdownload(?:[ >])/);
  assert.match(home, /id="download-tab-desktop"[^>]+aria-controls="download-pane-desktop"/);
  assert.match(home, /id="download-pane-cli"[^>]+role="tabpanel"[^>]+hidden/);
  assert.doesNotMatch(home, /releases\/latest\/download/);
  assert.doesNotMatch(siteScript, /releases\/latest\/download/);
  assert.doesNotMatch(siteScript, /desktopPreviewBase/);
});

test("CLI strictly excludes foreign and all prereleases", () => {
  const releases = [
    { tag_name: "v1.19.0-rc.1", prerelease: true, assets: cliAssets("v1.19.0-rc.1") },
    { tag_name: "v1.18.0-preview.2", prerelease: true, assets: cliAssets("v1.18.0-preview.2") },
    { tag_name: "desktop-v1.18.0", prerelease: false, assets: cliAssets("desktop-v1.18.0") },
    { tag_name: "v1.17.21", prerelease: false, assets: cliAssets("v1.17.21") },
    { tag_name: "v1.18.0-preview.12", prerelease: true, assets: cliAssets("v1.18.0-preview.12") },
    { tag_name: "v1.18.0-preview.13", prerelease: false, assets: cliAssets("v1.18.0-preview.13") },
  ];
  assert.equal(selectCLIRelease(releases, "stable")?.tag_name, "v1.17.21");
  assert.equal(selectCLIRelease(releases, "preview")?.tag_name, "v1.17.21");
});

test("CLI selection rejects incomplete releases instead of synthesizing asset URLs", () => {
  const releases = [
    {
      tag_name: "v1.20.0",
      prerelease: false,
      assets: cliAssets("v1.20.0", ["reasonix-windows-arm64.zip", "SHA256SUMS"]),
    },
    {
      tag_name: "v1.19.5",
      prerelease: false,
      assets: cliAssets("v1.19.5"),
    },
  ];
  assert.equal(releaseAssetMap(releases[0]), null);
  assert.equal(selectCLIRelease(releases, "stable")?.tag_name, "v1.19.5");
  const model = cliReleaseModel(releases, "stable");
  assert.equal(model?.displayVersion, "1.19.5");
  assert.equal(
    model?.assets["reasonix-windows-arm64.zip"],
    "https://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.19.5/reasonix-windows-arm64.zip",
  );
  assert.equal(cliReleaseModel([releases[0]], "stable"), null);
});

test("CLI selection compares arbitrarily large numeric version components exactly", () => {
  const releases = [
    {
      tag_name: "v99999999999999999999.999.999",
      prerelease: false,
      assets: cliAssets("v99999999999999999999.999.999"),
    },
    {
      tag_name: "v100000000000000000000.0.0",
      prerelease: false,
      assets: cliAssets("v100000000000000000000.0.0"),
    },
  ];
  assert.equal(selectCLIRelease(releases, "stable")?.tag_name, "v100000000000000000000.0.0");
});

test("CLI release links are derived from the validated canonical tag", () => {
  const release = {
    tag_name: "v1.18.0",
    prerelease: false,
    html_url: "https://evil.invalid/phishing",
    assets: cliAssets("v1.18.0"),
  };
  assert.equal(
    cliReleaseModel([release], "stable")?.releaseURL,
    "https://github.com/esengine/DeepSeek-Reasonix/releases/tag/v1.18.0",
  );
  assert.equal(
    cliReleaseModel([release], "stable")?.changelogURL,
    "https://reasonix.io/changelog/",
  );
  release.release_notes_url = "https://reasonix.io/changelog/v1.18.0/";
  assert.equal(cliReleaseModel([release], "stable")?.changelogURL, release.release_notes_url);
});

test("CLI assets reject spoofed hosts and cross-tag URLs", () => {
  const valid = {
    tag_name: "v1.18.0",
    prerelease: false,
    assets: cliAssets("v1.18.0"),
  };
  const invalidURLs = [
    "https://evil.invalid/esengine/DeepSeek-Reasonix/releases/download/v1.20.0/reasonix-darwin-amd64.tar.gz",
    "https://github.com@evil.invalid/esengine/DeepSeek-Reasonix/releases/download/v1.20.0/reasonix-darwin-amd64.tar.gz",
    "http://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.20.0/reasonix-darwin-amd64.tar.gz",
    "https://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.19.0/reasonix-darwin-amd64.tar.gz",
    "https://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.20.0/reasonix-darwin-arm64.tar.gz",
  ];
  const invalid = invalidURLs.map((browser_download_url) => {
    const release = {
      tag_name: "v1.20.0",
      prerelease: false,
      assets: cliAssets("v1.20.0"),
    };
    release.assets[0].browser_download_url = browser_download_url;
    return release;
  });
  assert.equal(selectCLIRelease([...invalid, valid], "stable")?.tag_name, "v1.18.0");
});

test("CLI assets require a bounded positive safe integer size", () => {
  for (const size of [0, -1, "42", 1.5, 1073741825, Number.MAX_SAFE_INTEGER + 1, NaN, undefined]) {
    const release = {
      tag_name: "v1.20.0",
      prerelease: false,
      assets: cliAssets("v1.20.0"),
    };
    release.assets[0].size = size;
    assert.equal(releaseAssetMap(release), null, `size ${String(size)} must be rejected`);
  }
});

test("CLI releases reject duplicate required asset names", () => {
  const release = {
    tag_name: "v1.20.0",
    prerelease: false,
    assets: cliAssets("v1.20.0"),
  };
  release.assets.push({ ...release.assets[0] });
  assert.equal(releaseAssetMap(release), null);
});

test("Desktop manifests accept only official versions and old or unified asset bases", () => {
  const preview = desktopManifest("v1.18.0-preview.62");
  assert.equal(desktopReleaseModel(preview, "stable"), null);
  assert.equal(desktopReleaseModel(preview, "preview"), null);

  const stableR2 = desktopManifest("v1.18.0");
  assert.equal(desktopReleaseModel(stableR2, "stable")?.version, "v1.18.0");
  const githubBase =
    "https://github.com/esengine/DeepSeek-Reasonix/releases/download/desktop-v1.18.0/";
  const stableGitHub = desktopManifest("v1.18.0", githubBase);
  assert.equal(
    desktopReleaseModel(stableGitHub, "stable")?.assets["Reasonix-linux-amd64.deb"],
    `${githubBase}Reasonix-linux-amd64.deb`,
  );
  const unifiedBase = "https://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.18.0/";
  assert.equal(desktopReleaseModel(desktopManifest("v1.18.0", unifiedBase))?.assets["Reasonix-linux-amd64.deb"], `${unifiedBase}Reasonix-linux-amd64.deb`);
});

test("Desktop manifests reject hostile URLs and incomplete integrity metadata", () => {
  const cases = [
    ["malicious host", (manifest) => {
      const url = "https://evil.invalid/desktop-v1.18.0-preview.62/Reasonix-darwin-arm64.zip";
      Object.assign(manifest.platforms["darwin-arm64"], { url, sig: `${url}.minisig` });
    }],
    ["userinfo", (manifest) => {
      const url = "https://dl.reasonix.io@evil.invalid/desktop-v1.18.0-preview.62/Reasonix-darwin-arm64.zip";
      Object.assign(manifest.platforms["darwin-arm64"], { url, sig: `${url}.minisig` });
    }],
    ["http", (manifest) => {
      const url = "http://dl.reasonix.io/desktop-v1.18.0-preview.62/Reasonix-darwin-arm64.zip";
      Object.assign(manifest.platforms["darwin-arm64"], { url, sig: `${url}.minisig` });
    }],
    ["wrong channel path", (manifest) => {
      const url = "https://dl.reasonix.io/preview/Reasonix-darwin-arm64.zip";
      Object.assign(manifest.platforms["darwin-arm64"], { url, sig: `${url}.minisig` });
    }],
    ["wrong filename", (manifest) => {
      const url = "https://dl.reasonix.io/desktop-v1.18.0-preview.62/Reasonix-darwin-amd64.zip";
      Object.assign(manifest.platforms["darwin-arm64"], { url, sig: `${url}.minisig` });
    }],
    ["missing platform", (manifest) => {
      delete manifest.platforms["windows-arm64"];
    }],
    ["missing website download", (manifest) => {
      delete manifest.downloads["Reasonix-darwin-universal.dmg"];
    }],
    ["invalid website download", (manifest) => {
      manifest.downloads["Reasonix-windows-amd64.zip"].size = 0;
    }],
    ["bad SHA", (manifest) => {
      manifest.platforms["darwin-arm64"].sha256 = "A".repeat(64);
    }],
    ["zero size", (manifest) => {
      manifest.platforms["darwin-arm64"].size = 0;
    }],
    ["size above release maximum", (manifest) => {
      manifest.platforms["darwin-arm64"].size = 1073741825;
    }],
    ["string size", (manifest) => {
      manifest.platforms["darwin-arm64"].size = "42";
    }],
    ["missing size", (manifest) => {
      delete manifest.platforms["darwin-arm64"].size;
    }],
    ["bad signature URL", (manifest) => {
      manifest.platforms["darwin-arm64"].sig += "?mirror=1";
    }],
    ["wrong download page", (manifest) => {
      manifest.download_page = "https://evil.invalid/download";
    }],
  ];

  for (const [name, mutate] of cases) {
    const manifest = copy(desktopManifest("v1.18.0-preview.62"));
    mutate(manifest);
    assert.equal(desktopReleaseModel(manifest, "preview"), null, name);
  }

  const crossVersion = desktopManifest(
    "v1.18.0",
    "https://dl.reasonix.io/desktop-v1.17.9/",
  );
  assert.equal(desktopReleaseModel(crossVersion, "stable"), null);
});

test("Desktop Stable falls back to a complete exact GitHub Latest release", () => {
  const release = desktopGitHubRelease();
  const model = desktopGitHubReleaseModel(release);
  assert.equal(model?.version, "v1.17.21");
  assert.equal(
    model?.assets["Reasonix-darwin-universal.dmg"],
    "https://github.com/esengine/DeepSeek-Reasonix/releases/download/desktop-v1.17.21/Reasonix-darwin-universal.dmg",
  );

  const invalid = [
    { ...release, prerelease: true },
    { ...release, tag_name: "v1.17.21" },
    { ...release, assets: release.assets.slice(1) },
    { ...release, assets: [...release.assets, { ...release.assets[0] }] },
  ];
  const spoofed = copy(release);
  spoofed.assets[0].browser_download_url =
    "https://github.com.evil.invalid/esengine/DeepSeek-Reasonix/releases/download/desktop-v1.17.21/Reasonix-darwin-arm64.zip";
  invalid.push(spoofed);
  for (const candidate of invalid) assert.equal(desktopGitHubReleaseModel(candidate), null);
});

test("release JSON fetch falls back in order", async () => {
  const calls = [];
  const result = await fetchFirstJSON(["https://one.invalid", "https://two.invalid"], async (url) => {
    calls.push(url);
    if (url.includes("one")) return { ok: false, status: 503, statusText: "Unavailable" };
    return { ok: true, json: async () => ({ version: "v1.2.3" }) };
  });
  assert.deepEqual(calls, ["https://one.invalid", "https://two.invalid"]);
  assert.deepEqual(result, { version: "v1.2.3" });
});

test("release JSON fetch skips a successful response that fails channel validation", async () => {
  const calls = [];
  const result = await fetchFirstJSON(
    ["https://one.invalid", "https://two.invalid"],
    async (url) => {
      calls.push(url);
      return {
        ok: true,
        json: async () => ({ version: url.includes("one") ? "v1.2.3-canary.1" : "v1.2.3-preview.1" }),
      };
    },
    (payload) => payload.version === "v1.2.3-preview.1",
  );
  assert.deepEqual(calls, ["https://one.invalid", "https://two.invalid"]);
  assert.deepEqual(result, { version: "v1.2.3-preview.1" });
});

test("Desktop JSON fetch continues after an invalid 200 response", async () => {
  const invalid = desktopManifest("v1.18.0");
  invalid.platforms["darwin-arm64"].size = 0;
  const valid = desktopManifest("v1.17.21");
  const calls = [];
  const result = await fetchFirstJSON(
    ["https://one.invalid", "https://two.invalid"],
    async (url) => {
      calls.push(url);
      return { ok: true, json: async () => url.includes("one") ? invalid : valid };
    },
    (payload) => Boolean(desktopReleaseModel(payload)),
  );
  assert.deepEqual(calls, ["https://one.invalid", "https://two.invalid"]);
  assert.equal(result.version, "v1.17.21");
});
