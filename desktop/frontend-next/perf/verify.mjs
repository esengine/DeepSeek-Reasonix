// 验证优化没有改掉行为：跟随、脱离跟随、回到最新、卡片渲染、切页往返。
import { chromium } from "playwright";
import { fileURLToPath } from "node:url";
import { mkdirSync } from "node:fs";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = fileURLToPath(new URL("shots", import.meta.url));

mkdirSync(SHOTS, { recursive: true });
const browser = await chromium.launch();
// 判据比的是界面上那几个词，所以语种得钉住 —— 不钉就跟着运行环境走，在英文机
// 器上这一整组会全红，而红的是台架没说自己要哪一种语言。locale.mjs 早就是这么
// 开页面的。
const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
page.on("console", (m) => m.type() === "error" && fails.push("控制台错误: " + m.text()));

await page.goto(PAGE, { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 15000 });
await page.waitForTimeout(700);

// 灌 60 轮，制造一条需要滚动的记录。
await page.evaluate(async (n) => {
  const yieldFrame = () => new Promise((r) => requestAnimationFrame(r));
  for (let i = 0; i < n; i++) {
    const id = `t${i}`;
    window.__feed({ kind: "turn_started" });
    window.__feed({ kind: "tool_dispatch", tool: { id, name: "edit_file", args: JSON.stringify({ path: `pkg/f${i}.go` }) } });
    window.__feed({ kind: "tool_result", tool: { id, name: "edit_file", args: JSON.stringify({ path: `pkg/f${i}.go` }), output: "写入完成", durationMs: 210, added: 4, removed: 2 } });
    window.__feed({ kind: "text", text: `第 ${i} 段回答。\n\n- 要点一\n- 要点二\n\n` });
    window.__feed({ kind: "message" });
    window.__feed({ kind: "turn_done" });
    if (i % 10 === 9) await yieldFrame();
  }
  await yieldFrame();
}, 60);
await page.waitForTimeout(500);

const flow = page.locator("#flowScroll").first();
const geom = () => flow.evaluate((el) => ({ top: el.scrollTop, h: el.scrollHeight, c: el.clientHeight }));

// 1) 挂载的块真的画出来了，而不是留下一片占位空白。
const cards = await page.locator("#flowScroll .call").count();
const blocks = await page.locator("#flowScroll .chunk").count();
check("底部卡片已挂载", cards > 0, `${cards} 张 / ${blocks} 块`);
check("远处的块已卸载", cards < blocks * 48, `挂载 ${cards}，全挂会是 ${blocks * 48}`);
const visibleText = await flow.evaluate((el) => (el.innerText || "").trim().length);
check("可见文字非空", visibleText > 200, `${visibleText} 字`);

// 2) 灌完之后停在底部。
const g1 = await geom();
check("自动停在底部", g1.h - g1.top - g1.c < 60, `距底 ${(g1.h - g1.top - g1.c).toFixed(0)}px`);

// 3) 流式增量继续时保持跟随。
await page.evaluate(async () => {
  window.__feed({ kind: "turn_started" });
  for (let i = 0; i < 40; i++) {
    window.__feed({ kind: "text", text: "继续写下去的一段话。" });
    await new Promise((r) => requestAnimationFrame(r));
  }
});
await page.waitForTimeout(300);
const g2 = await geom();
check("流式时保持跟随", g2.h - g2.top - g2.c < 60, `距底 ${(g2.h - g2.top - g2.c).toFixed(0)}px`);
await page.screenshot({ path: `${SHOTS}/1-跟随中.png` });

// 4) 往上滚 → 脱离跟随，「回到最新」出现，新增量不再把视口拽回去。
await flow.evaluate((el) => el.scrollTo({ top: el.scrollHeight * 0.35 }));
await page.waitForTimeout(400);
const away = await geom();
const jump = page.locator("button.jump").first();
check("向上滚后按钮出现", await jump.isVisible(), `scrollTop=${away.top.toFixed(0)}`);
await page.evaluate(async () => {
  for (let i = 0; i < 30; i++) {
    window.__feed({ kind: "text", text: "又写了一些新的内容。" });
    await new Promise((r) => requestAnimationFrame(r));
  }
  window.__feed({ kind: "turn_done" });
});
await page.waitForTimeout(400);
const stay = await geom();
check("脱离跟随后不被拽回", Math.abs(stay.top - away.top) < 80, `位移 ${Math.abs(stay.top - away.top).toFixed(0)}px`);
await page.screenshot({ path: `${SHOTS}/2-已脱离跟随.png` });

// 4b) 往上滚到远处，历史块要挂载出来，且文档高度不能塌。
const tallBefore = (await geom()).h;
await flow.evaluate((el) => el.scrollTo({ top: el.scrollHeight * 0.08 }));
await page.waitForTimeout(700);
const upTop = await page.evaluate(() => {
  const el = document.querySelector("#flowScroll");
  const cards = [...el.querySelectorAll(".call")];
  const r = el.getBoundingClientRect();
  return cards.filter((c) => { const b = c.getBoundingClientRect(); return b.bottom > r.top && b.top < r.bottom; }).length;
});
check("滚到远处会挂载历史块", upTop > 0, `视口内 ${upTop} 张`);
const tallAfter = (await geom()).h;
check("滚动时文档高度稳定", Math.abs(tallAfter - tallBefore) / tallBefore < 0.15, `${tallBefore.toFixed(0)} → ${tallAfter.toFixed(0)}px`);
await page.screenshot({ path: `${SHOTS}/2b-滚到历史处.png` });

// 5) 点「回到最新」回到底部并恢复跟随。
await jump.click();
await page.waitForTimeout(500);
const back = await geom();
check("回到最新可用", back.h - back.top - back.c < 60, `距底 ${(back.h - back.top - back.c).toFixed(0)}px`);

// 6) 切到轨迹再切回，记录仍在且仍贴底。
await page.locator('[role="tab"]').nth(1).click();
await page.waitForTimeout(300);
const trajRows = await page.locator("table.traj tbody tr").count();
check("轨迹页有内容", trajRows > 50, `${trajRows} 行`);
await page.screenshot({ path: `${SHOTS}/3-轨迹页.png` });
await page.locator('[role="tab"]').nth(0).click();
await page.waitForTimeout(400);
const after = await page.locator("#flowScroll .call").count();
check("切回后卡片仍在", after > 0, `${after} 张`);
await page.screenshot({ path: `${SHOTS}/4-切回活动页.png` });

// 6.5) 右缘那条轨道不许压在内容上。它是一整块收指针的命中区，内容伸进去多少，
//      那一段里右对齐的控件就有多少按不到 —— 看得见、点不着，报上来的正是这个。
//      量的是不变式本身（内容右缘 ≤ 轨道左缘），不是某一个按钮：右对齐的控件
//      有四处，逐个断言总会漏掉下一个。
// 轨道只在有标记时才画，而标记来自用户消息 —— 先放一条进去，顺带它自己就是
// 那条轨道下面最窄的一个控件：「撤回」36px 宽，整个落在里面过。
await page.evaluate(() => {
  window.__feed({ kind: "__user", id: "u-cancel", text: "排队的这一行", pending: true });
  window.__feed({ kind: "__queued", id: "u-cancel", itemId: "q1", queued: "steer" });
});
await page.waitForTimeout(500);

const lane = await page.evaluate(() => {
  const flow = document.querySelector(".flow");
  const srail = document.querySelector(".srail");
  if (!flow || !srail) return null;
  const f = flow.getBoundingClientRect();
  const pr = parseFloat(getComputedStyle(flow).paddingRight);
  return { 内容右缘: Math.round(f.right - pr), 轨道左缘: Math.round(srail.getBoundingClientRect().x) };
});
check(
  "轨道没有压在内容上",
  lane != null && lane.内容右缘 <= lane.轨道左缘,
  lane ? `内容右缘 ${lane.内容右缘} · 轨道起于 ${lane.轨道左缘}` : "轨道或内容没渲染",
);

const cancelHit = await page.evaluate(() => {
  const el = document.querySelector(".pcancel");
  if (!el) return null;
  const b = el.getBoundingClientRect();
  const hit = document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2);
  return { ok: el === hit || el.contains(hit), got: hit?.className?.toString().split(" ")[0] || hit?.tagName };
});
check("「撤回」按得到", cancelHit?.ok === true, cancelHit ? `点在它身上收到的是 ${cancelHit.got}` : "没渲染");

// 6.6) 小控件的命中框要比它的长相大一圈。这几个都是 10px 字号的片，画大了会压过
//      它标注的那一行，所以长相不动、命中框往外让 —— 但让了多少得有人守着。
const reach = await page.evaluate(() => {
  const box = (sel) => {
    const e = document.querySelector(sel);
    if (!e) return null;
    const b = e.getBoundingClientRect();
    const cx = b.x + b.width / 2, cy = b.y + b.height / 2;
    const out = (dx, dy) => { let n = 0; for (; n < 24; n++) {
      const el = document.elementFromPoint(cx + dx * (b.width / 2 + n), cy + dy * (b.height / 2 + n));
      if (!(el === e || e.contains(el))) break; } return n; };
    return { 高: Math.round(b.height) + out(0, 1) + out(0, -1) };
  };
  return box(".pcancel");
});
check("「撤回」的命中框够大", (reach?.高 ?? 0) >= 28, `命中高 ${reach?.高 ?? "—"}px`);

// 6.7) 回滚菜单从转录里弹出来，而转录的卡片带 content-visibility 和一层动画残留的
//      transform，滚动容器又是 overflow:auto —— 三样都会裁它。菜单因此挂在转录外面，
//      这条断言量的是结果：四角都点得到。
await page.locator(".compose textarea").first().fill("守卫用的一句话");
await page.locator(".compose textarea").first().press("Enter");
await page.waitForTimeout(1600);
await page.locator(".rewind button").first().click();
await page.waitForTimeout(500);
const clip = await page.evaluate(() => {
  const m = document.querySelector(".rewindmenu");
  if (!m) return null;
  const b = m.getBoundingClientRect();
  const pts = [[b.x + 6, b.y + 6], [b.right - 6, b.y + 6], [b.x + 6, b.bottom - 6], [b.right - 6, b.bottom - 6]];
  const inside = pts.filter(([x, y]) => { const e = document.elementFromPoint(x, y); return m === e || m.contains(e); });
  return { 四角命中: inside.length, 高: Math.round(b.height) };
});
check("回滚菜单没有被裁", clip?.四角命中 === 4, clip ? `四角命中 ${clip.四角命中}/4` : "菜单没开");
await page.keyboard.press("Escape");
await page.waitForTimeout(300);

// 7) 审批卡这类交互卡仍然可答。
await page.evaluate(() => {
  window.__feed({ kind: "turn_started" });
  window.__feed({ kind: "approval_request", approval: { id: "a1", tool: "bash", subject: "rm -rf build/", risk: "high" } });
});
await page.waitForTimeout(400);
const apv = page.locator(".call").filter({ hasText: "bash" }).last();
check("审批卡出现", await page.locator("text=/批准|允许|拒绝/").first().isVisible().catch(() => false));

// 7b) 计划的门不是工具的门。内核给计划留了三个互不相同的结局
//     （control.PlanDecisionAction），而这张卡过去用的是工具审批那套词：
//     「允许这一次 / 这一类不再问 / 拒绝」—— 其中「这一类不再问」对计划没有意义
//     （内核对这道门拒绝任何记住的授权），而真正想要的那个出路（继续规划、把计划
//     改掉）藏在「拒绝」这个词后面，谁也认不出来。
await page.evaluate(() => [...document.querySelectorAll(".compose button")].find((b) => b.textContent.trim() === "计划")?.click());
await page.waitForTimeout(700);
const planOn = await page.evaluate(() => document.querySelector(".app")?.dataset.plan);
check("进得了计划模式", planOn === "on", `data-plan=${planOn}`);
await page.evaluate(() => {
  window.__feed({ kind: "turn_started" });
  window.__feed({ kind: "approval_request", approval: { id: "plan1", tool: "exit_plan_mode", subject: "", kind: "plan", fresh: true } });
});
await page.waitForTimeout(600);
const gate = await page.evaluate(() => {
  const card = [...document.querySelectorAll(".apv")].pop();
  return {
    btns: [...(card?.querySelectorAll(".apv-ft .btn") ?? [])].map((b) => b.textContent.trim()),
    note: card?.querySelector(".apv-note")?.textContent?.trim() ?? "",
  };
});
check("计划有三条出路", gate.btns.length === 3, gate.btns.join(" / ") || "没有按钮");
check("其中一条是改计划", gate.btns.some((x) => x.includes("修改")), gate.btns.join(" / "));
check("不给计划发会话授权", !gate.btns.some((x) => x.includes("不再问")), gate.btns.join(" / "));
check("卡上说清了改计划怎么改", gate.note.includes("计划模式"), gate.note || "没有这句话");

// 「修改计划」留在计划模式里 —— 这就是「改计划」这件事本身
await page.evaluate(() => [...document.querySelectorAll(".apv-ft .btn")].find((b) => b.textContent.trim().includes("修改"))?.click());
await page.waitForTimeout(800);
check(
  "改计划后仍在计划模式",
  (await page.evaluate(() => document.querySelector(".app")?.dataset.plan)) === "on",
);

// 批准之后必须离开计划模式：内核不会自己关这个标志，聊天 TUI 是在自己这一侧关的，
// Studio 过去没关 —— 于是执行完这一轮，下一轮又回去规划。
await page.evaluate(() => {
  window.__feed({ kind: "approval_request", approval: { id: "plan2", tool: "exit_plan_mode", subject: "", kind: "plan", fresh: true } });
});
await page.waitForTimeout(600);
await page.evaluate(() => [...document.querySelectorAll(".apv-ft .btn")].find((b) => b.textContent.trim() === "开始执行")?.click());
await page.waitForTimeout(900);
check(
  "开始执行后离开计划模式",
  (await page.evaluate(() => document.querySelector(".app")?.dataset.plan)) === "off",
);
await page.screenshot({ path: `${SHOTS}/5-审批卡.png` });

// 8) 会话树开得很大时：只列最近的，能展开全部，能折叠，行仍可点。
await page.goto(`${PAGE}?ws=6&sess=200`, { waitUntil: "networkidle" });
await page.waitForSelector(".wsnode", { timeout: 20000 });
await page.waitForTimeout(600);
const firstWs = page.locator(".wsnode").first();
check("大树只列最近的", (await firstWs.locator(".sessrow").count()) === 30, `${await firstWs.locator(".sessrow").count()} 行`);
check("有「全部显示」", await firstWs.locator("button.sessmore").isVisible());
await firstWs.locator("button.sessmore").click();
await page.waitForTimeout(400);
check("展开后列全", (await firstWs.locator(".sessrow").count()) === 200, `${await firstWs.locator(".sessrow").count()} 行`);
await firstWs.locator("button.twist").click();
await page.waitForTimeout(300);
check("折叠后收起", (await firstWs.locator(".sessrow").count()) === 0);
await page.screenshot({ path: `${SHOTS}/6-大会话树.png` });
const second = page.locator(".wsnode").nth(1).locator(".sessrow").first();
await second.click();
await page.waitForTimeout(800);
check("会话行可点开", (await page.locator(".pane").count()) >= 1, `${await page.locator(".pane").count()} 个面板`);

await browser.close();
console.log(fails.length ? `\n失败 ${fails.length} 项：\n- ${fails.join("\n- ")}` : "\n全部通过");
process.exit(fails.length ? 1 : 0);
