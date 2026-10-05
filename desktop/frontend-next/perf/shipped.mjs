// 产物的守卫：dist 里的东西必须比源码新。
//
// 一次构建可以成功、可以打印 ✓、而编的是别的东西 —— 这个工作树上不止一个进程
// 在动，一次 stash 把树退回 HEAD、48 秒后再还原，中间跑的构建就编了旧代码。
// 窗口随后加载它，界面整体退回上一版，而所有测试仍然是绿的：它们读的是源码。
//
// 时间戳而不是特征字符串：特征会腐烂，而「产物比源码旧」永远是同一句话。
import { readdirSync, statSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = process.env.PERF_ROOT ?? join(HERE, "..");
const DIST = process.env.PERF_DIST ?? join(ROOT, "dist");

function newest(dir) {
  let at = 0, who = "";
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (e.name === "node_modules" || e.name.startsWith(".")) continue;
    const p = join(dir, e.name);
    if (e.isDirectory()) {
      const [t, w] = newest(p);
      if (t > at) [at, who] = [t, w];
      continue;
    }
    const t = statSync(p).mtimeMs;
    if (t > at) [at, who] = [t, p];
  }
  return [at, who];
}

if (!existsSync(DIST)) {
  console.log(`没有 ${DIST}：先 pnpm build。`);
  process.exit(1);
}

const [srcAt, srcWho] = newest(join(ROOT, "src"));
const entry = join(DIST, "index.html");
if (!existsSync(entry)) {
  console.log("dist 里没有 index.html：这次构建没有产出入口。");
  process.exit(1);
}
const builtAt = statSync(entry).mtimeMs;
const when = (ms) => new Date(ms).toISOString().slice(11, 23);

console.log(`产物 ${when(builtAt)}   源码最新 ${when(srcAt)}`);
if (builtAt < srcAt) {
  console.log(`\n产物比源码旧：${srcWho.slice(ROOT.length + 1)} 在构建之后又变过。`);
  console.log("窗口现在加载的不是这棵树上的代码 —— 重新构建，再启动。");
  process.exit(1);
}
// 入口自己也要对得上：一次编了别的树的构建，index.html 会退回没有启动幕的那版。
const src = statSync(join(ROOT, "index.html")).size;
const out = statSync(entry).size;
if (out + 200 < src) {
  console.log(`\n入口缩水了：源码 ${src} 字节，产物 ${out} 字节。`);
  console.log("index.html 里的内联部分没进产物 —— 多半编的是另一棵树。");
  process.exit(1);
}
console.log("\n产物是这棵树的");
