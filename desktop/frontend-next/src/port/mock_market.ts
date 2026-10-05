import type { MarketDetail, MarketList, MarketOwnRequest, MarketPackage, MarketPlan, MarketPublished, MarketQuery, MarketRequest, MarketSubmission, MarketVote } from "./market";
import { HttpError } from "./http_error";
import { MockLook } from "./mock_look";

const DIGEST = "sha256:5f0c1e9a7b3d2c4e6f8a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e";

// Four rows that each read differently: pinned and installable, one source
// that expands into several skills, one already installed, and one whose
// approved version carries no digest and so installs only on the person's trust.
const PACKAGES: (MarketPackage & { source: string; pinned: boolean })[] = [
  {
    kind: "skill", handle: "nanfei892", name: "make-ui-not-ai", slug: "nanfei892/make-ui-not-ai",
    summary: "前端界面技能：从真实产品任务出发，设计并验证可用、完整、有辨识度的界面。",
    description: "适合需要从零搭一个页面、或者把现有页面改得不像模板的时候。\n\n它会先问清楚这个页面给谁用、要完成什么，再给出布局与验证步骤。",
    homepage: "", repoUrl: "https://github.com/nanfei892/ship-it-skills", tags: ["frontend", "ui-design"],
    latestVersion: "1.0.0", installCount: 2118, starCount: 41, upCount: 41, downCount: 3, approvalRate: 41 / 44, score: 0.83, verified: true, status: "active", updatedAt: "2026-09-04T02:56:03Z",
    source: "https://github.com/nanfei892/ship-it-skills/tree/3f1c2e7a9b0d4c6e8f1a2b3c4d5e6f7a8b9c0d1e/make-ui-not-ai", pinned: true,
  },
  {
    kind: "plugin", handle: "acme", name: "review-kit", slug: "acme/review-kit",
    summary: "代码评审套件：按改动范围逐块评审，只标出会出事的地方。",
    description: "三个技能 + 一个 /pr 命令。不启动任何进程，不注册钩子。",
    homepage: "", repoUrl: "https://github.com/acme/review-kit", tags: ["review", "git"],
    latestVersion: "1.4.0", installCount: 986, starCount: 12, upCount: 12, downCount: 2, approvalRate: 12 / 14, score: 0.61, verified: false, status: "active", updatedAt: "2026-09-20T10:00:00Z",
    source: "https://github.com/acme/review-kit/tree/9c8b7a6f5e4d3c2b1a0f9e8d7c6b5a4f3e2d1c0b", pinned: true,
  },
  {
    kind: "mcp", handle: "irmia", name: "irmia-devkit", slug: "irmia/irmia-devkit",
    summary: "一组对模型友好的开发工具：批量读写、结构化搜索、依赖图。",
    description: "", homepage: "", repoUrl: "https://github.com/irmia2026/irmia_devkit_mcp", tags: ["tool", "coding"],
    latestVersion: "2.7.0", installCount: 1527, starCount: 5, upCount: 5, downCount: 1, approvalRate: 5 / 6, score: 0.52, verified: false, status: "active", updatedAt: "2026-07-22T12:11:46Z",
    installed: { version: "2.7.0", contentHash: DIGEST },
    source: "irmia-devkit-mcp", pinned: true,
  },
  {
    kind: "skill", handle: "1574022644", name: "lm-studio-vision-bridge", slug: "1574022644/lm-studio-vision-bridge",
    summary: "通过本地 LM Studio 视觉模型为 agent 提供图片识别能力。",
    description: "", homepage: "", repoUrl: "https://github.com/FuchaZ/lm-studio-vision-bridge", tags: ["vision", "local"],
    latestVersion: "2.0.0", installCount: 1678, starCount: 0, upCount: 0, downCount: 0, approvalRate: null, score: 0.29, verified: false, status: "active", updatedAt: "2026-07-28T07:14:25Z",
    source: "https://github.com/FuchaZ/lm-studio-vision-bridge/blob/master/SKILL.md", pinned: false,
  },
  {
    kind: "theme", handle: "lumen", name: "dusk-harbor", slug: "lumen/dusk-harbor",
    summary: "低饱和的暮色主题：浅色、深色各一套，只含主题，不带任何技能或钩子。",
    description: "", homepage: "", repoUrl: "https://github.com/lumen/reasonix-themes", tags: ["theme", "dark"],
    latestVersion: "1.2.0", installCount: 412, starCount: 5, upCount: 5, downCount: 0, approvalRate: 1, score: 0.44, verified: false, status: "active", updatedAt: "2026-09-18T08:00:00Z",
    source: "https://github.com/lumen/reasonix-themes/tree/7d3e1c2b9a8f7e6d5c4b3a291807f6e5d4c3b2a1/dusk-harbor", pinned: true,
  },
];

// What the demo account has submitted, one row per review state a publisher sees.
const MINE: MarketPackage[] = [
  { ...PACKAGES[0], handle: "demo", slug: "demo/ship-notes", name: "ship-notes", summary: "发布说明整理技能。", status: "pending", installCount: 0, latestVersion: "0.1.0" },
  { ...PACKAGES[4], handle: "demo", slug: "demo/paper-light", name: "paper-light", summary: "纸面质感的浅色主题。", status: "active", installCount: 37, latestVersion: "1.0.1" },
  { ...PACKAGES[1], handle: "demo", slug: "demo/lint-kit", name: "lint-kit", summary: "代码检查插件。", status: "rejected", installCount: 0, latestVersion: "0.2.0" },
  { ...PACKAGES[4], handle: "demo", slug: "demo/night-desk", name: "night-desk", summary: "只给自己用的深色主题。", status: "private", installCount: 0, latestVersion: "0.1.0" },
].map(({ source: _source, pinned: _pinned, ...rest }) => ({ ...rest, installed: undefined }));

export class MockMarket extends MockLook {
  private marketInstalled = new Set<string>(["irmia/irmia-devkit"]);
  private mine = [...MINE];
  private marketVotes = new Map<string, -1 | 0 | 1>([["acme/review-kit", 1]]);
  // Voting reads its own sign-in answer so a test can draw the signed-out entry.
  marketSignedIn = true;

  async marketList(q: MarketQuery): Promise<MarketList> {
    const needle = (q.q ?? "").trim().toLowerCase();
    const rows = PACKAGES.filter((p) => (!q.kind || p.kind === q.kind) && (!q.pinned || p.pinned) &&
      (!needle || `${p.name} ${p.summary} ${p.tags.join(" ")}`.toLowerCase().includes(needle)));
    return { packages: rows.map((p) => this.view(p)), limit: 24, offset: 0 };
  }

  async marketDetail(slug: string): Promise<MarketDetail> {
    const p = PACKAGES.find((x) => x.slug === slug);
    if (!p) throw new Error("market.not_found");
    const pkg = this.view(p);
    return {
      package: pkg, pinned: p.pinned, installed: pkg.installed,
      approved: { version: p.latestVersion, source: p.source, contentHash: p.pinned ? DIGEST : "", riskLevel: "", createdAt: p.updatedAt },
    };
  }

  // review-kit plans as a source that expands into several skills, which is
  // the confirmation this tab exists to force.
  async planMarket(req: MarketRequest): Promise<MarketPlan> {
    const p = PACKAGES.find((x) => x.slug === req.slug)!;
    if (!p.pinned && !req.trust) throw new HttpError(409, "the approved version is not pinned to reviewed content", { code: "market.unpinned" });
    const base = { ok: true, status: "planned", applied: false, source: p.source, slug: p.slug, version: p.latestVersion, contentDigest: DIGEST, unreviewed: !p.pinned };
    if (p.slug === "acme/review-kit") {
      return {
        ...base, planId: "high:sha256:mock",
        actions: ["review", "risk", "pr-notes"].map((name) => ({
          kind: "skill", action: "copy_skill", status: "planned", riskLevel: "high", name,
          riskReasons: ["one source expands to 3 skills; every one of them is installed"],
        })),
      };
    }
    return {
      ...base, planId: "low:sha256:mock",
      actions: [{
        kind: p.kind === "theme" ? "plugin" : p.kind,
        action: p.kind === "theme" ? "install_plugin_package" : "copy_skill",
        status: "planned", riskLevel: "low", name: p.name,
      }],
    };
  }

  async installMarket(req: MarketRequest): Promise<MarketPlan> {
    const plan = await this.planMarket(req);
    if (plan.unreviewed && req.digest !== plan.contentDigest) throw new HttpError(400, "an unreviewed install needs the digest of the preview it confirms", { code: "market.unpreviewed" });
    this.marketInstalled.add(req.slug);
    return { ...plan, status: "done", applied: true, actions: plan.actions?.map((a) => ({ ...a, status: "done" })) };
  }

  async publishMarket(sub: MarketSubmission): Promise<MarketPublished> {
    const pkg: MarketPackage = {
      kind: sub.kind, handle: "demo", name: sub.name, slug: `demo/${sub.name}`, summary: sub.summary ?? "", description: sub.description ?? "",
      homepage: "", repoUrl: sub.repoUrl ?? "", tags: sub.tags ?? [], latestVersion: sub.version || "0.1.0", installCount: 0, starCount: 0, upCount: 0, downCount: 0, approvalRate: null, score: 0,
      verified: false, status: sub.visibility === "private" ? "private" : "pending", updatedAt: new Date().toISOString(),
    };
    this.mine.unshift(pkg);
    return { package: pkg, created: true, version: pkg.latestVersion };
  }

  async myMarket(): Promise<MarketPackage[]> {
    return [...this.mine];
  }

  async planOwnMarket(req: MarketOwnRequest): Promise<MarketPlan> {
    const p = this.mine.find((x) => x.slug === req.slug);
    if (!p) throw new Error("market.not_yours");
    return {
      ok: true, status: "planned", applied: false, slug: p.slug, version: p.latestVersion, contentDigest: DIGEST, unreviewed: true,
      planId: "low:sha256:own",
      actions: [{ kind: p.kind === "theme" ? "plugin" : p.kind, action: "install_plugin_package", status: "planned", riskLevel: "low", name: p.name }],
    };
  }

  async installOwnMarket(req: MarketOwnRequest): Promise<MarketPlan> {
    if (req.digest !== DIGEST) throw new Error("market.unpreviewed");
    const plan = await this.planOwnMarket(req);
    this.mine = this.mine.map((p) => (p.slug === req.slug ? { ...p, installed: { version: p.latestVersion, contentHash: DIGEST } } : p));
    return { ...plan, status: "done", applied: true, actions: plan.actions?.map((a) => ({ ...a, status: "done" })) };
  }

  async submitMarket(slug: string): Promise<MarketPackage> {
    const i = this.mine.findIndex((x) => x.slug === slug && x.status === "private");
    if (i < 0) throw new Error("market.not_private");
    this.mine[i] = { ...this.mine[i]!, status: "pending" };
    return this.mine[i]!;
  }

  async marketMyVote(slug: string): Promise<MarketVote> {
    if (!this.marketSignedIn) return { signedIn: false, value: 0 };
    return this.tally(slug, this.marketVotes.get(slug) ?? 0);
  }

  async voteMarket(slug: string, value: -1 | 0 | 1): Promise<MarketVote> {
    if (!this.marketSignedIn) throw new Error("market.signed_out");
    this.marketVotes.set(slug, value);
    return this.tally(slug, value);
  }

  private tally(slug: string, value: -1 | 0 | 1): MarketVote {
    const p = PACKAGES.find((x) => x.slug === slug);
    if (!p) throw new Error("market.not_found");
    const was = slug === "acme/review-kit" ? 1 : 0;
    const up = p.upCount - (was === 1 ? 1 : 0) + (value === 1 ? 1 : 0);
    const down = p.downCount + (value === -1 ? 1 : 0);
    return { signedIn: true, value, upCount: up, downCount: down, approvalRate: up + down ? up / (up + down) : null, canVote: true, own: false, emailVerified: true };
  }

  private view(p: (typeof PACKAGES)[number]): MarketPackage {
    const { source: _source, ...rest } = p;
    const installed = this.marketInstalled.has(p.slug) ? { version: p.latestVersion, contentHash: DIGEST } : undefined;
    return { ...rest, installed };
  }
}
