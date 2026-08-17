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
  for (const view of ["documents", "analysis", "agent", "assets", "wiki", "redaction", "lifecycle", "audit", "members", "system"]) {
    assert.match(source, new RegExp(`data-nav="${view}"`));
    assert.match(source, new RegExp(`data-view="${view}"`));
  }
});

test("bounded Agent workbench exposes natural-language tasks without generic execution", () => {
  assert.match(source, /data-testid="agent-workbench"/);
  assert.match(source, /data-testid="agent-prompt"/);
  assert.match(source, /只读处理/);
  assert.match(source, /有原文依据/);
  assert.match(source, /不自动发布/);
  assert.match(source, /系统只分析文档内容，不会执行文档中夹带的命令/);
  assert.match(source, /超出当前账号权限的操作将被拒绝/);
  assert.doesNotMatch(source, /不可信证据数据|越界指令不会改变能力边界/);
  assert.match(behavior, /createAgentWorkbench/);
  assert.match(styles, /\.agent-workbench/);
  assert.doesNotMatch(source, /coding agent|代码代理/i);
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

test("IP panorama exposes a scalable semantic neural network camera", () => {
  assert.match(source, /data-testid="graph-camera-status"/);
  assert.match(source, /滚轮缩放/);
  assert.match(source, /id="graph-viewport"/);
  assert.match(behavior, /zoomGraphCameraAt/);
  assert.match(behavior, /addEventListener\("wheel"/);
  assert.match(behavior, /addEventListener\("pointerdown"/);
  assert.match(styles, /\.graph-node \.node-core/);
  assert.match(styles, /data-zoom-level="overview"/);
  assert.match(styles, /prefers-reduced-motion/);
});

test("real MinerU and DeepSeek analysis stays behind the same-origin gateway", () => {
  assert.match(source, /id="real-file-input"/);
  assert.match(source, /data-testid="real-analysis-results"/);
  assert.match(source, /文档读取完成/);
  assert.match(source, /知识提取完成/);
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

test("persistent workspace surfaces use live counts, real governance and navigable evidence", () => {
  assert.match(source, /id="nav-document-count"/);
  assert.match(source, /id="nav-redaction-count"/);
  assert.match(source, /id="lifecycle-real-governance"/);
  assert.match(source, /data-demo-lifecycle/);
  assert.match(source, /id="agent-retry"/);
  assert.match(source, /id="redaction-real-workspace"/);
  assert.match(source, /data-demo-redaction/);
  assert.match(source, /data-demo-analysis/);
  assert.match(behavior, /renderLifecycleGovernance/);
  assert.match(behavior, /renderRealRiskWorkspace/);
  assert.match(behavior, /matchPublishedEvidence/);
  assert.match(behavior, /renderAuditMetrics/);
  assert.doesNotMatch(source, /id="nav-document-count"[^>]*>17</);
  assert.doesNotMatch(source, /id="nav-redaction-count"[^>]*>23</);
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

test("optional semantic asset check is admin-facing, bounded and read-only", () => {
  assert.match(source, /data-testid="semantic-check-panel"/);
  assert.match(source, /data-testid="run-semantic-check"/);
  assert.match(source, /运行只读检查/);
  assert.match(source, /不会上传原文、自动合并资产或改写 Wiki/);
  assert.match(behavior, /fetch\("\/api\/admin\/semantic\/enrich"/);
  assert.match(behavior, /renderSemanticResult/);
  assert.match(behavior, /记录 A：/);
  assert.match(styles, /\.semantic-check-panel/);
});

test("semantic asset suggestions persist as an explicit administrator review loop", () => {
  assert.match(source, /data-testid="semantic-review-dialog"/);
  assert.match(source, /确认需治理/);
  assert.match(source, /保留独立记录/);
  assert.match(source, /不会自动合并资产或修改 Wiki/);
  assert.match(behavior, /\/api\/admin\/semantic\/reviews/);
  assert.match(behavior, /semantic-review-decision/);
  assert.match(behavior, /renderSemanticReviews/);
  assert.match(styles, /\.semantic-review-status/);
  assert.match(styles, /\.semantic-review-actions/);
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

test("member governance is independent, permission-gated and protects consequential changes", () => {
  assert.match(source, /data-testid="nav-members"/);
  assert.match(source, /data-testid="view-members"/);
  assert.match(source, /id="member-search"/);
  assert.match(source, /id="member-role-filter"/);
  assert.match(source, /id="member-status-filter"/);
  assert.match(source, /id="system-member-summary"/);
  assert.match(source, /data-testid="member-change-dialog"/);
  assert.match(source, /data-testid="my-permissions-dialog"/);
  assert.match(source, /当前版本不会自动发送邮件/);
  assert.doesNotMatch(source, /TEAM ONBOARDING|ONE-TIME ACTIVATION LINK/);
  assert.match(behavior, /view === "members"/);
  assert.match(behavior, /pendingMemberChange/);
  assert.match(behavior, /所有现有登录会话立即失效/);
  assert.match(behavior, /lastLoginAt/);
  assert.match(styles, /\.member-page-grid/);
});

test("first-use role boundaries and truthful states are wired", () => {
  assert.match(behavior, /canAnalyzeDocuments/);
  assert.match(behavior, /canReadWorkspaceAudit/);
  assert.match(behavior, /当前账号为只读成员，不能接入或分析文档/);
  assert.match(behavior, /response\.status === 403/);
  assert.match(behavior, /updateWikiChangePreview/);
  assert.match(behavior, /const unchanged = !wikiEditBaseline/);
  assert.doesNotMatch(behavior, /if \(save\.disabled\)/);
  assert.match(source, /id="wiki-change-preview"/);
  assert.match(source, /有依据条目/);
  assert.doesNotMatch(source, /Reasonix 任务语义|最大工具调用|交付门禁已执行/);
});

test("business-facing labels match the implemented capability", () => {
  assert.match(source, /敏感信息与风险线索/);
  assert.match(source, /从文档提取资产/);
  assert.match(source, /关系待复核/);
  assert.doesNotMatch(source, /生成不可逆涂黑副本/);
  assert.match(sharedPage, /安全 Wiki 阅览/);
  assert.match(sharedPage, /请联系分享发起方/);
  assert.doesNotMatch(sharedPage, /SECURE WIKI ROOM|REDACTED KNOWLEDGE EDITION|REDACTED WIKI/);
});
