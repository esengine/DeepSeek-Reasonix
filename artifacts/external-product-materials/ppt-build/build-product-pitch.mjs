import fs from "node:fs/promises";
import path from "node:path";
import { Presentation, PresentationFile } from "@oai/artifact-tool";

const ROOT = "C:/品味识别/intelifar-ip-wiki-graph";
const BUILD = `${ROOT}/artifacts/external-product-materials/ppt-build`;
const RENDER = `${BUILD}/rendered`;
const FINAL = `${ROOT}/deliverables/intelifar-企业文档IP智能平台-客户与投资人演示.pptx`;

const C = {
  purple: "#6557FF",
  purple2: "#4A3CDB",
  deep: "#171827",
  deep2: "#202234",
  ink: "#202234",
  muted: "#686D80",
  light: "#F4F4FA",
  border: "#DDE0EA",
  white: "#FFFFFF",
  teal: "#18A889",
  tealLight: "#E8F8F3",
  gold: "#B47A13",
  goldLight: "#FFF4D8",
  lavender: "#ECEAFF",
};

const FONT = "Microsoft YaHei";

async function bytes(rel) {
  const b = await fs.readFile(path.join(ROOT, rel));
  return b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength);
}

async function writeBlob(file, blob) {
  await fs.writeFile(file, new Uint8Array(await blob.arrayBuffer()));
}

function addText(slide, text, pos, opts = {}) {
  const shape = slide.shapes.add({
    geometry: "textbox",
    name: opts.name,
    position: pos,
    fill: "none",
    line: { style: "solid", fill: "none", width: 0 },
  });
  shape.text = text;
  shape.text.style = {
    typeface: opts.typeface || FONT,
    fontSize: opts.fontSize || 20,
    bold: opts.bold || false,
    italic: opts.italic || false,
    color: opts.color || C.ink,
    alignment: opts.alignment || "left",
    verticalAlignment: opts.verticalAlignment || "top",
    autoFit: opts.autoFit || "shrinkText",
    wrap: "square",
    lineSpacing: opts.lineSpacing || 1.05,
    insets: opts.insets || { top: 0, right: 0, bottom: 0, left: 0 },
  };
  return shape;
}

function addRect(slide, pos, fill, opts = {}) {
  return slide.shapes.add({
    geometry: opts.geometry || "rect",
    name: opts.name,
    position: pos,
    fill,
    line: { style: "solid", fill: opts.line || fill, width: opts.lineWidth || 0 },
    borderRadius: opts.radius || 0,
    shadow: opts.shadow || "shadow-none",
  });
}

async function addImage(slide, rel, pos, alt, opts = {}) {
  return slide.images.add({
    blob: await bytes(rel),
    contentType: "image/png",
    alt,
    fit: opts.fit || "contain",
    crop: opts.crop,
    position: pos,
    geometry: "roundRect",
    borderRadius: opts.radius || 14,
  });
}

function addRule(slide, left, top, width, color = C.purple, height = 4) {
  return addRect(slide, { left, top, width, height }, color);
}

function addKicker(slide, text, dark = false) {
  addText(slide, text.toUpperCase(), { left: 72, top: 48, width: 430, height: 24 }, {
    fontSize: 14, bold: true, color: dark ? "#AFA7FF" : C.purple,
  });
}

function addSlideTitle(slide, title, subtitle, opts = {}) {
  const dark = opts.dark || false;
  addKicker(slide, opts.kicker || "INTELIFAR PRODUCT", dark);
  addText(slide, title, { left: 72, top: 82, width: opts.width || 1136, height: 62 }, {
    name: "slide-title",
    fontSize: opts.fontSize || 38,
    bold: true,
    color: dark ? C.white : C.ink,
  });
  if (subtitle) {
    addText(slide, subtitle, { left: 72, top: 151, width: opts.subtitleWidth || 1040, height: 44 }, {
      fontSize: 19,
      color: dark ? "#C9CBD7" : C.muted,
      lineSpacing: 1.15,
    });
  }
}

function addFooter(slide, index, dark = false) {
  addText(slide, "intelifar  ·  企业文档 IP 智能平台", { left: 72, top: 686, width: 420, height: 18 }, {
    fontSize: 11, bold: true, color: dark ? "#8E91A3" : "#969AAB",
  });
  addText(slide, String(index).padStart(2, "0"), { left: 1160, top: 684, width: 48, height: 18 }, {
    fontSize: 11, bold: true, color: dark ? "#8E91A3" : "#969AAB", alignment: "right",
  });
}

function setNotes(slide, lines) {
  slide.speakerNotes.textFrame.setText([
    ...lines,
    "",
    "[Sources]",
    "- 本演示中的产品能力与截图来自 intelifar 本地工作空间及自动化验收产物。",
  ]);
  slide.speakerNotes.setVisible(true);
}

function addValueLine(slide, y, number, title, body, accent = C.purple) {
  addText(slide, number, { left: 90, top: y, width: 86, height: 52 }, {
    fontSize: 38, bold: true, color: accent,
  });
  addText(slide, title, { left: 190, top: y + 1, width: 320, height: 34 }, {
    fontSize: 24, bold: true, color: C.ink,
  });
  addText(slide, body, { left: 190, top: y + 38, width: 430, height: 50 }, {
    fontSize: 17, color: C.muted, lineSpacing: 1.16,
  });
  addRule(slide, 90, y + 91, 530, C.border, 1);
}

async function build() {
  await fs.mkdir(RENDER, { recursive: true });
  await fs.mkdir(path.dirname(FINAL), { recursive: true });
  const p = Presentation.create({ slideSize: { width: 1280, height: 720 } });

  // 1 — Cover
  {
    const s = p.slides.add();
    s.background.fill = C.deep;
    await addImage(s, "site/public/brand/intelifar-logo-dark.png",
      { left: 72, top: 54, width: 250, height: 60 }, "intelifar 标志");
    addRule(s, 72, 158, 94, C.purple, 5);
    addText(s, "让每份长文档，\n成为企业可以持续增长的知识资产", { left: 72, top: 198, width: 930, height: 174 }, {
      name: "deck-title", fontSize: 55, bold: true, color: C.white, lineSpacing: 1.0,
    });
    addText(s, "企业文档 IP 智能分析与 Wiki 管理平台", { left: 76, top: 407, width: 650, height: 42 }, {
      fontSize: 24, color: "#B5B0FF", bold: true,
    });
    addText(s, "客户与投资人演示  ·  2026", { left: 76, top: 470, width: 440, height: 30 }, {
      fontSize: 16, color: "#9296A8",
    });
    addText(s, "可检索  /  可追溯  /  可治理  /  可安全复用", { left: 76, top: 606, width: 660, height: 30 }, {
      fontSize: 18, color: "#D9DAE4",
    });
    addRect(s, { left: 1008, top: 96, width: 138, height: 516 }, C.purple, { radius: 69 });
    addText(s, "IP\nINTELLIGENCE", { left: 1012, top: 245, width: 130, height: 130 }, {
      fontSize: 17, bold: true, color: C.white, alignment: "center", verticalAlignment: "middle", lineSpacing: 1.0,
    });
    setNotes(s, ["开场只讲一个判断：企业文档的价值不在存储数量，而在能否被可信地复用。"]);
  }

  // 2 — Problem
  {
    const s = p.slides.add();
    s.background.fill = C.deep;
    addSlideTitle(s, "企业最重要的知识，仍被锁在长文档里", "文件可以找到，结论却很难被复核、连接和安全复用。", {
      dark: true, kicker: "THE KNOWLEDGE GAP",
    });
    const items = [
      ["01", "知识散落", "同一技术、产品或客户信息分布在多种格式与多个版本中。"],
      ["02", "证据断裂", "摘要与回答无法稳定回到原文位置，重要判断缺少复核入口。"],
      ["03", "关系不可见", "依赖、复用、冲突与影响范围主要存在于少数专家记忆中。"],
      ["04", "治理滞后", "权限、审批、分享和离职交接常在知识生成之后补做。"],
    ];
    items.forEach((it, i) => {
      const y = 244 + i * 96;
      addText(s, it[0], { left: 88, top: y, width: 72, height: 40 }, { fontSize: 27, bold: true, color: "#A99FFF" });
      addText(s, it[1], { left: 182, top: y, width: 220, height: 34 }, { fontSize: 24, bold: true, color: C.white });
      addText(s, it[2], { left: 420, top: y + 2, width: 740, height: 56 }, { fontSize: 18, color: "#C7CAD7", lineSpacing: 1.16 });
      if (i < 3) addRule(s, 182, y + 73, 978, "#343646", 1);
    });
    addFooter(s, 2, true);
    setNotes(s, ["不要把问题说成“缺 AI”。真正缺的是从文档到正式知识的连续责任链。"]);
  }

  // 3 — Promise and product proof
  {
    const s = p.slides.add();
    s.background.fill = C.light;
    addSlideTitle(s, "intelifar 把文档变成有来源、有关系、有责任人的 IP 资产", "不是增加一个聊天框，而是建立一条可以治理的企业知识链。", {
      kicker: "PRODUCT PROMISE", subtitleWidth: 590, width: 600,
    });
    addValueLine(s, 244, "01", "找得到", "跨文档搜索已经发布的资产、Wiki 与原文依据。", C.purple);
    addValueLine(s, 346, "02", "信得过", "关键结论带来源位置、摘录、版本与校验信息。", C.teal);
    addValueLine(s, 448, "03", "管得住", "角色、审批、密级、分享、审计和备份进入同一工作空间。", C.gold);
    await addImage(s, "artifacts/real-ui-port/01-real-ui-home-4388.png",
      { left: 676, top: 92, width: 532, height: 505 }, "intelifar 真实工作空间指挥台", { fit: "cover", crop: { left: 0.03, top: 0, right: 0, bottom: 0 } });
    addText(s, "真实产品界面 · 4388 工作空间", { left: 790, top: 614, width: 320, height: 24 }, {
      fontSize: 13, color: C.muted, alignment: "center",
    });
    addFooter(s, 3);
    setNotes(s, ["右侧为当前真实 UI；强调它不是概念设计。", "来源：artifacts/real-ui-port/01-real-ui-home-4388.png"]);
  }

  // 4 — Workflow
  {
    const s = p.slides.add();
    s.background.fill = C.white;
    addSlideTitle(s, "一条闭环，把“读文档”升级为“经营知识资产”", "智能能力负责提取与建议，权限和人工复核决定什么进入正式知识。", { kicker: "END-TO-END FLOW" });
    const steps = [
      ["01", "接入", "授权文档\n安全预检"],
      ["02", "解析", "章节表格\n版面结构"],
      ["03", "提取", "资产风险\n关系草案"],
      ["04", "复核", "证据权属\n密级审批"],
      ["05", "发布", "资产 Wiki\n版本留痕"],
      ["06", "复用", "搜索网络\nAgent 分享"],
    ];
    steps.forEach((st, i) => {
      const x = 72 + i * 190;
      addText(s, st[0], { left: x, top: 258, width: 60, height: 34 }, { fontSize: 18, bold: true, color: C.purple });
      addText(s, st[1], { left: x, top: 304, width: 150, height: 40 }, { fontSize: 28, bold: true, color: C.ink });
      addText(s, st[2], { left: x, top: 358, width: 150, height: 70 }, { fontSize: 17, color: C.muted, lineSpacing: 1.18 });
      addRule(s, x, 449, 150, i < 3 ? C.purple : C.teal, 5);
      if (i < 5) addText(s, "→", { left: x + 154, top: 330, width: 30, height: 40 }, { fontSize: 24, bold: true, color: "#A5A8B6", alignment: "center" });
    });
    addRect(s, { left: 72, top: 524, width: 1136, height: 86 }, C.lavender, { radius: 14 });
    addText(s, "正式知识的门槛", { left: 98, top: 545, width: 220, height: 28 }, { fontSize: 20, bold: true, color: C.purple2 });
    addText(s, "证据可回溯  ·  人工可复核  ·  权限可执行  ·  变更可审计", { left: 340, top: 545, width: 800, height: 30 }, { fontSize: 20, bold: true, color: C.ink });
    addFooter(s, 4);
    setNotes(s, ["闭环中每一步都能找到负责人和状态；模型输出不直接等同于正式知识。"]);
  }

  // 5 — Evidence Wiki
  {
    const s = p.slides.add();
    s.background.fill = C.light;
    addSlideTitle(s, "每个重要结论，都能沿证据链回到原文", "资产、Wiki、版本与来源定位共同回答：这项知识从哪里来，为什么可以相信。", { kicker: "PROVENANCE-FIRST WIKI" });
    addText(s, "结论", { left: 88, top: 250, width: 120, height: 36 }, { fontSize: 26, bold: true, color: C.purple });
    addText(s, "形成可阅读、可搜索、可版本化的 Wiki 页面", { left: 88, top: 293, width: 470, height: 52 }, { fontSize: 19, color: C.ink });
    addRule(s, 88, 367, 470, C.border, 1);
    addText(s, "依据", { left: 88, top: 398, width: 120, height: 36 }, { fontSize: 26, bold: true, color: C.teal });
    addText(s, "记录原始文档、章节或页码、摘录与内容校验信息", { left: 88, top: 441, width: 470, height: 62 }, { fontSize: 19, color: C.ink, lineSpacing: 1.18 });
    addText(s, "审批前正式版本保持不变", { left: 88, top: 548, width: 380, height: 30 }, { fontSize: 17, bold: true, color: C.muted });
    await addImage(s, "artifacts/screenshots/14-real-published-wiki.png",
      { left: 616, top: 214, width: 592, height: 412 }, "已发布 Wiki 与原文资源并列核对", { fit: "cover", crop: { left: 0.17, top: 0.02, right: 0, bottom: 0.05 } });
    addFooter(s, 5);
    setNotes(s, ["演示时点击“查看原文依据”是最有说服力的动作。", "来源：artifacts/screenshots/14-real-published-wiki.png"]);
  }

  // 6 — Graph
  {
    const s = p.slides.add();
    s.background.fill = C.deep;
    addSlideTitle(s, "知识不再只是页面，而是一张可以缩放和追问的 IP 网络", "从全局识别核心资产，从局部判断依赖、复用路径和变更影响。", { dark: true, kicker: "IP PANORAMA" });
    await addImage(s, "artifacts/ip-asset-graph/06-neural-panorama.png",
      { left: 72, top: 214, width: 812, height: 425 }, "可缩放 IP 资产神经网络全景", { fit: "cover", crop: { left: 0.13, top: 0.17, right: 0.02, bottom: 0.02 } });
    const points = [
      ["缩小", "识别知识密集区与孤立资产"],
      ["放大", "查看权属、密级、证据与来源"],
      ["聚焦", "保留一跳关系评估影响范围"],
      ["复核", "建议关系经人工确认后才生效"],
    ];
    points.forEach((pt, i) => {
      const y = 235 + i * 94;
      addText(s, pt[0], { left: 930, top: y, width: 95, height: 32 }, { fontSize: 23, bold: true, color: "#AFA7FF" });
      addText(s, pt[1], { left: 930, top: y + 34, width: 266, height: 48 }, { fontSize: 17, color: "#D0D2DC", lineSpacing: 1.14 });
    });
    addFooter(s, 6, true);
    setNotes(s, ["网络只展示当前账号有权查看的资产；待复核关系不会被自动当作事实。", "来源：artifacts/ip-asset-graph/06-neural-panorama.png"]);
  }

  // 7 — Agent
  {
    const s = p.slides.add();
    s.background.fill = C.white;
    addSlideTitle(s, "自然语言负责泛化，确定性边界负责企业交付", "IP 任务助手处理盘点、影响分析、证据核查、对比、风险和尽调，但不成为通用执行代理。", { kicker: "BOUNDED IP AGENT" });
    addText(s, "它能做什么", { left: 72, top: 238, width: 260, height: 34 }, { fontSize: 24, bold: true, color: C.purple });
    addText(s, "搜索授权资产\n检查关系与依赖\n核对原文证据\n生成结构化交付\n提出 Wiki 草案建议", { left: 72, top: 286, width: 320, height: 196 }, { fontSize: 19, color: C.ink, lineSpacing: 1.35 });
    addText(s, "它不会做什么", { left: 72, top: 518, width: 260, height: 34 }, { fontSize: 24, bold: true, color: C.gold });
    addText(s, "不执行代码、系统命令、任意联网、自动发布、删除或权限修改", { left: 72, top: 560, width: 360, height: 68 }, { fontSize: 18, color: C.muted, lineSpacing: 1.18 });
    await addImage(s, "artifacts/ip-agent/02-grounded-delivery.png",
      { left: 500, top: 212, width: 708, height: 427 }, "受控 IP 任务交付界面", { fit: "contain" });
    addFooter(s, 7);
    setNotes(s, ["受控 Agent 的差异是：允许复杂任务，但每一步仍在封闭工具、当前角色和证据门槛之内。", "来源：artifacts/ip-agent/02-grounded-delivery.png"]);
  }

  // 8 — Governance
  {
    const s = p.slides.add();
    s.background.fill = C.light;
    addSlideTitle(s, "权限、审批和留痕，让知识资产真正进入组织流程", "治理不是上线后的附加项，而是从文档接入到对外分享的默认路径。", { kicker: "GOVERNANCE BY DESIGN" });
    await addImage(s, "artifacts/user-guide-review/06-operation-records.png",
      { left: 72, top: 218, width: 690, height: 420 }, "intelifar 操作记录与完整性状态", { fit: "cover", crop: { left: 0.03, top: 0.08, right: 0.03, bottom: 0.08 } });
    const pts = [
      ["角色", "所有者、管理员、编辑者、阅读成员和外部收件人各有清晰边界。"],
      ["审批", "发布、关系、权属密级和语义建议保留人工决定。"],
      ["分享", "脱敏 Wiki 使用链接与独立访问码，支持有效期和撤销。"],
      ["连续性", "Wiki 版本、任务、操作记录和已验证备份持久保存。"],
    ];
    pts.forEach((pt, i) => {
      const y = 222 + i * 98;
      addText(s, pt[0], { left: 816, top: y, width: 110, height: 32 }, { fontSize: 23, bold: true, color: i % 2 ? C.teal : C.purple });
      addText(s, pt[1], { left: 816, top: y + 34, width: 378, height: 58 }, { fontSize: 17, color: C.ink, lineSpacing: 1.16 });
    });
    addFooter(s, 8);
    setNotes(s, ["这里强调“谁能做什么”与“做过什么”都可以被解释。", "来源：artifacts/user-guide-review/06-operation-records.png"]);
  }

  // 9 — Semantic quality
  {
    const s = p.slides.add();
    s.background.fill = C.white;
    addSlideTitle(s, "知识库增长之后，系统继续发现重复、冲突和治理欠账", "本地语义体检把算法建议转化为管理员待办；正式资产仍由业务人员决定。", { kicker: "KNOWLEDGE QUALITY" });
    addText(s, "发现", { left: 82, top: 254, width: 130, height: 36 }, { fontSize: 28, bold: true, color: C.purple });
    addText(s, "疑似重复\n信息不一致\n来源链缺口", { left: 82, top: 305, width: 210, height: 132 }, { fontSize: 21, color: C.ink, lineSpacing: 1.3 });
    addText(s, "→", { left: 292, top: 338, width: 56, height: 60 }, { fontSize: 38, bold: true, color: C.border, alignment: "center" });
    addText(s, "判断", { left: 366, top: 254, width: 130, height: 36 }, { fontSize: 28, bold: true, color: C.teal });
    addText(s, "确认需治理\n保留独立记录\n留下处理说明", { left: 366, top: 305, width: 230, height: 132 }, { fontSize: 21, color: C.ink, lineSpacing: 1.3 });
    addRect(s, { left: 82, top: 488, width: 514, height: 102 }, C.goldLight, { radius: 14 });
    addText(s, "不会自动合并、删除或改写 Wiki", { left: 108, top: 516, width: 462, height: 34 }, { fontSize: 20, bold: true, color: C.gold, alignment: "center" });
    await addImage(s, "artifacts/semantica-phase-2/07-real-action-center.png",
      { left: 650, top: 210, width: 558, height: 382 }, "真实待办中心中的语义资产建议", { fit: "cover", crop: { left: 0.12, top: 0.08, right: 0.01, bottom: 0.06 } });
    addFooter(s, 9);
    setNotes(s, ["Semantica 在本地运行，只接收经过权限和字段限制的资产投影。", "来源：artifacts/semantica-phase-2/07-real-action-center.png"]);
  }

  // 10 — Architecture
  {
    const s = p.slides.add();
    s.background.fill = C.deep;
    addSlideTitle(s, "模型可以替换，企业知识和治理责任始终掌握在本地", "分层架构把外部智能服务、正式知识存储和业务审批明确分开。", { dark: true, kicker: "TECHNOLOGY ARCHITECTURE" });
    const layers = [
      ["01  交互层", "Web 工作空间：文档、分析、资产、Wiki、Agent 与治理", "#2B2D40"],
      ["02  业务层", "权限校验 · 任务编排 · 发布审批 · 版本 · 分享 · 审计 · 备份", "#343052"],
      ["03  智能层", "MinerU 文档解析 · DeepSeek 结构化分析与任务规划 · Semantica 本地治理", "#40366C"],
      ["04  知识层", "文档 · IP 资产 · 原文证据 · Wiki · 版本 · 关系网络", "#4A3C83"],
    ];
    layers.forEach((layer, i) => {
      const y = 222 + i * 92;
      addRect(s, { left: 72, top: y, width: 958, height: 72 }, layer[2], { radius: 12 });
      addText(s, layer[0], { left: 98, top: y + 20, width: 210, height: 32 }, { fontSize: 22, bold: true, color: "#B9B3FF" });
      addText(s, layer[1], { left: 318, top: y + 20, width: 680, height: 34 }, { fontSize: 18, color: C.white });
    });
    addRect(s, { left: 1060, top: 222, width: 148, height: 348 }, C.teal, { radius: 16 });
    addText(s, "贯穿安全边界", { left: 1078, top: 248, width: 112, height: 42 }, { fontSize: 20, bold: true, color: C.white, alignment: "center" });
    addText(s, "同源网关\n工作空间隔离\n文件预检\n限流\n最小字段传递\n操作留痕", { left: 1078, top: 316, width: 112, height: 212 }, { fontSize: 17, color: C.white, alignment: "center", lineSpacing: 1.25 });
    addText(s, "API 密钥只在服务端读取，不进入浏览器", { left: 72, top: 608, width: 630, height: 28 }, { fontSize: 18, bold: true, color: "#B9B3FF" });
    addFooter(s, 10, true);
    setNotes(s, ["外部模型承担计算，本地系统承担权限、持久化、正式知识和责任链。", "技术实现依据：README.md、site/server、integrations/semantica。"]);
  }

  // 11 — Moat
  {
    const s = p.slides.add();
    s.background.fill = C.light;
    addSlideTitle(s, "差异不在某一个模型，而在完整闭环和长期积累", "解析、证据、关系、治理决定和任务反馈共同形成客户专属的知识网络。", { kicker: "PRODUCT MOAT" });
    const moat = [
      ["结构", "版面解析 + 专属 Schema", "从可读文本走向可治理 IP 对象"],
      ["可信", "结论 + 原文证据", "让生成内容可核查、可版本化"],
      ["关系", "知识网络 + 人工治理", "让网络增长而不被算法建议污染"],
      ["执行", "自然语言 + 封闭工具", "允许复杂任务同时保留企业可控性"],
    ];
    moat.forEach((m, i) => {
      const x = 76 + i * 286;
      addText(s, m[0], { left: x, top: 232, width: 200, height: 46 }, { fontSize: 35, bold: true, color: i % 2 ? C.teal : C.purple });
      addRule(s, x, 295, 224, i % 2 ? C.teal : C.purple, 4);
      addText(s, m[1], { left: x, top: 326, width: 224, height: 62 }, { fontSize: 22, bold: true, color: C.ink, lineSpacing: 1.12 });
      addText(s, m[2], { left: x, top: 412, width: 224, height: 86 }, { fontSize: 18, color: C.muted, lineSpacing: 1.18 });
    });
    addRect(s, { left: 76, top: 548, width: 1130, height: 78 }, C.tealLight, { radius: 14 });
    addText(s, "更多授权文档 → 更多人工复核 → 更完整的知识网络 → 更高的组织复用价值", { left: 104, top: 572, width: 1072, height: 30 }, { fontSize: 22, bold: true, color: C.ink, alignment: "center" });
    addFooter(s, 11);
    setNotes(s, ["投资视角：随着文档、关系和治理反馈积累，客户价值来自专属知识网络，而非单次模型调用。"]);
  }

  // 12 — Adoption
  {
    const s = p.slides.add();
    s.background.fill = C.white;
    addSlideTitle(s, "从一组真实资料开始，用可验收结果决定是否扩大", "无需一次迁移全部知识；先验证提取、来源与治理三项结果。", { kicker: "CUSTOMER ADOPTION", width: 620, subtitleWidth: 620 });
    const steps = [
      ["01", "选择样本", "一组已授权、价值明确、包含版本与关系的真实资料"],
      ["02", "定义目标", "资产盘点、技术尽调、项目复用、客户交付或风险核查"],
      ["03", "联合验证", "结构、证据、关系、权限和任务交付是否满足要求"],
      ["04", "逐步扩展", "增加文档量、使用团队与治理深度"],
    ];
    steps.forEach((st, i) => {
      const y = 224 + i * 94;
      addText(s, st[0], { left: 72, top: y, width: 60, height: 30 }, { fontSize: 18, bold: true, color: C.purple });
      addText(s, st[1], { left: 148, top: y - 2, width: 170, height: 36 }, { fontSize: 24, bold: true, color: C.ink });
      addText(s, st[2], { left: 324, top: y, width: 348, height: 58 }, { fontSize: 17, color: C.muted, lineSpacing: 1.14 });
      if (i < 3) addRule(s, 148, y + 70, 524, C.border, 1);
    });
    await addImage(s, "artifacts/screenshots/02-document-intake.png",
      { left: 726, top: 136, width: 482, height: 476 }, "文档接入与业务分类界面", { fit: "cover", crop: { left: 0.05, top: 0.03, right: 0.05, bottom: 0.02 } });
    addFooter(s, 12);
    setNotes(s, ["客户试点建议以业务验收问题为中心，不以模型分数或演示效果作为唯一标准。", "来源：artifacts/screenshots/02-document-intake.png"]);
  }

  // 13 — Proof
  {
    const s = p.slides.add();
    s.background.fill = C.light;
    addSlideTitle(s, "这不是概念图：核心链路已经通过真实服务与 UI 验证", "当前版本已完成全量自动化测试、真实解析与模型链路、本地语义检查和多角色 E2E。", { kicker: "VALIDATION EVIDENCE" });
    addText(s, "170", { left: 82, top: 232, width: 260, height: 100 }, { fontSize: 72, bold: true, color: C.purple });
    addText(s, "自动化测试通过", { left: 86, top: 333, width: 260, height: 34 }, { fontSize: 22, bold: true, color: C.ink });
    addText(s, "50", { left: 386, top: 232, width: 200, height: 100 }, { fontSize: 72, bold: true, color: C.teal });
    addText(s, "构建页面", { left: 390, top: 333, width: 180, height: 34 }, { fontSize: 22, bold: true, color: C.ink });
    addText(s, "0", { left: 650, top: 232, width: 160, height: 100 }, { fontSize: 72, bold: true, color: C.gold });
    addText(s, "高危依赖漏洞", { left: 654, top: 333, width: 220, height: 34 }, { fontSize: 22, bold: true, color: C.ink });
    addRule(s, 82, 404, 734, C.border, 1);
    addText(s, "真实链路", { left: 82, top: 438, width: 180, height: 34 }, { fontSize: 25, bold: true, color: C.purple2 });
    addText(s, "MinerU + DeepSeek 同源服务端链路\nSemantica：8 项资产识别 4 条疑似重复候选\n真实 UI 控制台：0 错误", { left: 82, top: 487, width: 660, height: 104 }, { fontSize: 19, color: C.ink, lineSpacing: 1.25 });
    await addImage(s, "artifacts/real-ui-port/05-system-persistent-4388.png",
      { left: 858, top: 206, width: 350, height: 392 }, "真实系统状态与持久化服务界面", { fit: "cover", crop: { left: 0.08, top: 0.03, right: 0.04, bottom: 0.03 } });
    addFooter(s, 13);
    setNotes(s, ["验证时间：2026-08-12。", "来源：npm test、npm run build、npm audit、artifacts/e2e-report.md、artifacts/semantica-phase-2/report.md。"]);
  }

  // 14 — Close
  {
    const s = p.slides.add();
    s.background.fill = C.deep;
    await addImage(s, "site/public/brand/intelifar-logo-dark.png",
      { left: 72, top: 54, width: 250, height: 60 }, "intelifar 标志");
    addText(s, "带一组真实资料，\n验证它能否成为企业的下一批知识资产", { left: 72, top: 186, width: 960, height: 150 }, {
      fontSize: 48, bold: true, color: C.white, lineSpacing: 1.02,
    });
    const qs = [
      ["提取得准", "关键资产与关系是否符合业务专家判断"],
      ["来源说得清", "每项重要结论是否能回到正确原文"],
      ["治理管得住", "权限、审批、分享和审计是否符合责任链"],
    ];
    qs.forEach((q, i) => {
      const x = 76 + i * 360;
      addText(s, `0${i + 1}`, { left: x, top: 414, width: 60, height: 34 }, { fontSize: 18, bold: true, color: "#AFA7FF" });
      addText(s, q[0], { left: x, top: 458, width: 290, height: 36 }, { fontSize: 25, bold: true, color: C.white });
      addText(s, q[1], { left: x, top: 506, width: 300, height: 64 }, { fontSize: 17, color: "#C9CBD7", lineSpacing: 1.15 });
    });
    addRule(s, 76, 614, 1130, C.purple, 4);
    addText(s, "预约产品演示  /  申请联合验证", { left: 76, top: 642, width: 580, height: 32 }, { fontSize: 20, bold: true, color: "#B9B3FF" });
    addText(s, "intelifar", { left: 1080, top: 642, width: 126, height: 28 }, { fontSize: 18, bold: true, color: C.white, alignment: "right" });
    setNotes(s, ["收尾不做泛泛感谢，直接邀请对方用一组真实资料验证三项结果。"]);
  }

  // Export every slide and structural evidence
  for (const [i, slide] of p.slides.items.entries()) {
    const n = String(i + 1).padStart(2, "0");
    await writeBlob(`${RENDER}/slide-${n}.png`, await p.export({ slide, format: "png", scale: 1 }));
    const layout = await slide.export({ format: "layout" });
    await fs.writeFile(`${RENDER}/slide-${n}.layout.json`, await layout.text());
  }
  await writeBlob(`${BUILD}/deck-montage.webp`, await p.export({ format: "webp", montage: true, scale: 1 }));
  const inspect = await p.inspect({ kind: "slide,textbox,shape,image,notes", maxChars: 30000 });
  await fs.writeFile(`${BUILD}/inspect.ndjson`, inspect.ndjson);
  const pptx = await PresentationFile.exportPptx(p);
  await pptx.save(FINAL);
  console.log(FINAL);
}

build().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});
