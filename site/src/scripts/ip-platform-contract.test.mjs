import test from "node:test";
import assert from "node:assert/strict";
import { access, readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const scriptsDir = path.dirname(fileURLToPath(import.meta.url));
const source = await readFile(path.join(scriptsDir, "../pages/index.astro"), "utf8");
const styles = await readFile(path.join(scriptsDir, "../styles/ip-platform.css"), "utf8");
const behavior = await readFile(path.join(scriptsDir, "ip-platform.mjs"), "utf8");
const publicShare = await readFile(path.join(scriptsDir, "public-share.mjs"), "utf8");
const activation = await readFile(path.join(scriptsDir, "invite-activation.mjs"), "utf8");
const sharedPage = await readFile(path.join(scriptsDir, "../pages/shared.astro"), "utf8");
const activationPage = await readFile(path.join(scriptsDir, "../pages/activate.astro"), "utf8");
const astroConfig = await readFile(path.join(scriptsDir, "../../astro.config.mjs"), "utf8");

test("platform uses the official intelifar brand assets and baseline color", async () => {
  assert.match(source, /intelifar-logo-dark\.png/);
  assert.match(source, /intelifar-logo\.png/);
  assert.match(styles, /--violet:\s*#635bff/i);
  await access(path.join(scriptsDir, "../../public/brand/intelifar-logo.png"));
  await access(path.join(scriptsDir, "../../public/brand/intelifar-logo-dark.png"));
});

test("all report acceptance surfaces are available from primary navigation", () => {
  for (const view of ["documents", "analysis", "assets", "wiki", "redaction", "lifecycle", "audit", "system"]) {
    assert.match(source, new RegExp(`data-nav="${view}"`));
    assert.match(source, new RegExp(`data-view="${view}"`));
  }
});

test("provenance, redaction, share and audit actions are wired", () => {
  assert.match(source, /data-open-provenance/);
  assert.match(source, /data-open-redaction-source/);
  assert.match(source, /id="share-form"/);
  assert.match(source, /id="export-audit"/);
  assert.match(behavior, /validateShare/);
  assert.match(behavior, /makeAuditEvent/);
  assert.match(behavior, /new Blob/);
});

test("responsive and accessibility contracts are present", () => {
  assert.match(source, /class="skip-link"/);
  assert.match(source, /aria-live="polite"/);
  assert.match(styles, /@media \(max-width: 660px\)/);
  assert.match(styles, /prefers-reduced-motion/);
});

test("real MinerU and DeepSeek analysis stays behind the same-origin gateway", () => {
  assert.match(source, /id="real-file-input"/);
  assert.match(source, /data-testid="real-analysis-results"/);
  assert.match(source, /MinerU LIVE/);
  assert.match(source, /DeepSeek LIVE/);
  assert.match(behavior, /fetch\("\/api\/analysis"/);
  assert.doesNotMatch(behavior, /api\.deepseek\.com|mineru\.net/);
  assert.match(behavior, /textContent = source\.quote/);
});

test("real analysis discloses parsed volume and actual DeepSeek coverage", () => {
  assert.match(source, /解析与分析范围/);
  assert.match(source, /id="real-analysis-range"/);
  assert.match(behavior, /analysisSamplingStrategy/);
  assert.match(behavior, /analysisSelectedSections/);
  assert.match(behavior, /DeepSeek 分段分析/);
  assert.match(source, /id="real-job-select"/);
  assert.match(behavior, /fetch\("\/api\/analysis"/);
  assert.match(behavior, /loadRecentRealJobs/);
});

test("administrator operations UI is wired to same-origin backup and recovery APIs", () => {
  assert.match(source, /data-testid="operations-console"/);
  assert.match(source, /data-testid="create-backup"/);
  assert.match(source, /data-testid="operations-job-list"/);
  assert.match(behavior, /fetch\("\/api\/admin\/operations"/);
  assert.match(behavior, /fetch\("\/api\/admin\/backups"/);
  assert.match(behavior, /\/retry`/);
  assert.match(styles, /\.operations-status-grid/);
});

test("member lifecycle and double-credential sharing use real same-origin services", () => {
  assert.match(source, /data-testid="team-access"/);
  assert.match(source, /data-testid="create-invitation"/);
  assert.match(source, /data-testid="share-secret-result"/);
  assert.match(behavior, /\/api\/admin\/invitations/);
  assert.match(behavior, /\/api\/admin\/members/);
  assert.match(behavior, /fetch\("\/api\/shares"/);
  assert.match(sharedPage, /data-testid="shared-wiki"/);
  assert.match(publicShare, /\/api\/public\/shares\/access/);
  assert.match(publicShare, /history\.replaceState/);
  assert.doesNotMatch(publicShare, /innerHTML|localStorage|sessionStorage/);
  assert.match(activationPage, /data-testid="activate-account"/);
  assert.match(activation, /\/api\/public\/invitations\/accept/);
  assert.match(activation, /history\.replaceState/);
  assert.match(activation, /const form = event\.currentTarget/);
  assert.match(activation, /form\.reset\(\)/);
  assert.match(sharedPage, /public-share\.mjs\?url/);
  assert.match(activationPage, /invite-activation\.mjs\?url/);
  assert.match(astroConfig, /assetsInlineLimit:\s*0/);
});
