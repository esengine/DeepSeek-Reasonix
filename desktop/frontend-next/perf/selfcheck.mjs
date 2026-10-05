// 守卫的守卫：往每个断言脚本里塞一条必然为假的断言，看它是不是真的能让退出码
// 变红。一条永远绿的守卫比没有守卫更糟——它替你担保了一件它其实没看的事。
import { readdirSync, readFileSync, writeFileSync, unlinkSync } from "node:fs";
import { execFileSync } from "node:child_process";

// 名单是会烂的那个：手写的那份漏了 cards、composer、intrinsic、motion、
// overflow、press、surface —— 七条从没被问过「你会红吗」的守卫。改为认这个
// 目录里每个用 fails 表达失败的脚本，新加一条就自动进来。
const HAS_FAILS = /^const fails = \[\];$/m;
const GUARDS = readdirSync("perf")
  .filter((f) => f.endsWith(".mjs") && f !== "selfcheck.mjs" && !f.startsWith("_probe_"))
  .filter((f) => HAS_FAILS.test(readFileSync(`perf/${f}`, "utf8")))
  .map((f) => f.slice(0, -4))
  .sort();
const rows = [];
for (const g of GUARDS) {
  const path = `perf/${g}.mjs`;
  const src = readFileSync(path, "utf8");
  // An exit code alone cannot tell a fired assertion from a file that does not
  // parse: both are non-zero, and a guard that never ran reads as one that works.
  try {
    execFileSync("node", ["--check", path], { stdio: "pipe" });
  } catch {
    rows.push({ 守卫: g, "塞入必假断言后的退出码": "—", 结果: "解析不了，根本没在跑 ✗" });
    continue;
  }
  // check 的定义之后立刻塞一条假断言；各脚本的签名略有出入，找它的定义行。
  // 注入点选 fails 而不是 check：check 的签名各脚本不一，而「失败了要让退出码
  // 变红」这件事，每个脚本都是靠同一个数组表达的。
  const m = src.match(/^const fails = \[\];$/m);
  if (!m) { rows.push({ 守卫: g, 结果: "找不到 fails 清单" }); continue; }
  const probe = src.slice(0, m.index + m[0].length) + '\nfails.push("__自检__");' + src.slice(m.index + m[0].length);
  const tmp = `perf/_probe_${g}.mjs`; // 跑完就删
  writeFileSync(tmp, probe);
  let code = 0;
  try {
    execFileSync("node", [tmp], { stdio: "pipe" });
  } catch (e) {
    code = e.status ?? -1;
  }
  unlinkSync(tmp);
  rows.push({ 守卫: g, "塞入必假断言后的退出码": code, 结果: code !== 0 ? "能红 ✓" : "仍然绿 ✗" });
}
console.table(rows);
const broken = rows.filter((r) => r.结果 !== "能红 ✓");
process.exit(broken.length ? 1 : 0);
