// 补全菜单的选中态：键盘走到哪一行，那一行必须是画面上唯一亮着的。
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const BOX = 'textarea[role="combobox"]';

// 选中要压过悬停,而且要往同一个方向压 —— 深色下抬亮、浅色下压暗。方向搞反
// 的填色（把 --accent-wash 直接搬过来就是）在浅色下看着还行,深色下是个洞。
const OVER = 1.4;
const FLOOR = 8;

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
page.on("pageerror", (e) => fails.push("页面异常: " + e.message));

await page.goto(PAGE, { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 20000 });

const read = () =>
  page.evaluate(() => {
    const list = document.querySelector(".slashmenu [role=listbox]");
    if (!list) return null;
    const panel = list.closest(".slashmenu");
    // 颜色一律先画出来再读。计算值的写法跟着作者写的那一种走 —— 令牌是
    // oklch(...)、color-mix 是 oklab(...)、别处是 rgb(...) —— 而把这三种的分量
    // 当成 RGB 去算,得到的数跟亮度没有关系:oklch 的第三个分量是色相角 255,
    // 按蓝色通道算出来的「亮度」比整道题都大。画布按 CSS Color 4 解析,取回来
    // 的是真正上屏的那三个字节。
    const ink = document.createElement("canvas").getContext("2d", { willReadFrequently: true });
    const rgb = (css) => {
      ink.clearRect(0, 0, 1, 1);
      ink.fillStyle = "#000";
      ink.fillStyle = css;
      ink.fillRect(0, 0, 1, 1);
      const [r, g, b, a] = ink.getImageData(0, 0, 1, 1).data;
      return [r, g, b, a / 255];
    };
    const lum = ([r, g, b]) => 0.2126 * r + 0.7152 * g + 0.0722 * b;
    const st = getComputedStyle(panel);
    const floor = lum(rgb(st.backgroundColor));
    // 悬停态没法从画面上读:键盘态下 .mi:hover:not([data-on]) 被显式抹平,而
    // 指针一落到某行,那行就成了 data-on —— 屏幕上永远不存在「一行被悬停、
    // 另一行被选中」。所以读 .mi:hover 这条规则自己声明的底色,再按令牌解析,
    // 而不是把某个令牌名写死在守卫里:换了令牌,读到的跟着换。
    const declared = () => {
      for (const sheet of document.styleSheets) {
        let rules;
        try {
          rules = sheet.cssRules;
        } catch {
          continue;
        }
        // CSSOM 的类型化取值器对 var() 一律返回空串 —— 它不是一个合法的
        // <color>,而这条规则写的正是 background: var(--float-hi)。
        for (const r of rules) {
          if (r.selectorText !== ".mi:hover") continue;
          const v = r.style?.getPropertyValue("background") || r.style?.getPropertyValue("background-color");
          if (v) return v;
        }
      }
      return null;
    };
    const hoverFill = (() => {
      const v = declared();
      if (!v) return null;
      const m = /^var\((--[\w-]+)\)$/.exec(v.trim());
      return lum(rgb(m ? getComputedStyle(panel).getPropertyValue(m[1]) : v)) - floor;
    })();
    return {
      listId: list.id,
      kb: list.hasAttribute("data-kb"),
      caret: document.querySelector('[role="combobox"]').getAttribute("aria-activedescendant"),
      hover: hoverFill,
      rows: [...list.querySelectorAll("button.mi")].map((b) => {
        const s = getComputedStyle(b);
        const c = rgb(s.backgroundColor);
        return {
          on: b.hasAttribute("data-on"),
          fill: c[3] === 0 ? null : lum(c) - floor,
          // The bar is measured as a structure rather than as "has a shadow":
          // it is a painted mark at the left edge, under 4px wide, and it stays
          // that whichever way it is drawn — a shadow can be anything.
          rail: (() => {
            const m = getComputedStyle(b, "::before");
            const w = parseFloat(m.width);
            const painted = m.backgroundColor && !/rgba\(0, 0, 0, 0\)|transparent/.test(m.backgroundColor);
            if (m.content !== "none" && painted && w > 0 && w <= 4 && parseFloat(m.left) <= 1) return true;
            return /inset\s+\d/.test(s.boxShadow);
          })(),
        };
      }),
    };
  });

const park = async (i) => {
  const b = await page.locator(".slashmenu button.mi").nth(i).boundingBox();
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  await page.waitForTimeout(80);
};

const key = async (k) => {
  await page.keyboard.press(k);
  await page.waitForTimeout(80);
};

async function suite(tag, token, park1, park2) {
  await page.click(BOX);
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.press("Backspace");
  await page.type(BOX, token, { delay: 20 });
  await page.waitForSelector(".slashmenu button.mi", { timeout: 5000 });
  await page.waitForTimeout(150);

  let s = await read();
  check(`${tag} 菜单开着,只有一行选中`, s && s.rows.filter((r) => r.on).length === 1, `${s?.rows.length} 行`);

  const on = s.rows.find((r) => r.on);
  // 读不出悬停画什么,就没有可比的对照 —— 那是守卫自己坏了,不是界面通过了。
  const beat = s.hover !== null && on.fill !== null && Math.sign(on.fill) === Math.sign(s.hover) && Math.abs(on.fill) >= Math.abs(s.hover) * OVER;
  check(`${tag} 选中比悬停更重,方向一致`, beat && Math.abs(on.fill) >= FLOOR, `选中 ${on.fill?.toFixed(1)} / 悬停 ${s.hover?.toFixed(1) ?? "读不出"}`);
  check(`${tag} 选中行有那条竖杠`, on.rail);

  await key("ArrowDown");
  s = await read();
  check(`${tag} 方向键把选中挪走了`, s.rows.findIndex((r) => r.on) === 1 && s.caret === `${s.listId}-1`);

  // 指针停在别的行上,手再回到键盘 —— 这里曾经同时亮两行,
  // 而且更重的那一行是指针底下那一行,不是回车会拿走的那一行。
  await park(park1);
  s = await read();
  check(`${tag} 指针一动就接管`, !s.kb && s.rows.findIndex((r) => r.on) === park1);

  await key("ArrowDown");
  s = await read();
  const lit = s.rows.filter((r) => r.fill !== null);
  check(`${tag} 键盘接回来,指针那行不再亮`, s.kb && lit.length === 1 && lit[0].on, `亮着 ${lit.length} 行`);
  check(`${tag} 亮的是键盘走到的那一行`, s.rows.findIndex((r) => r.on) === park1 + 1);

  await park(park2);
  s = await read();
  check(`${tag} 指针再动,又归指针`, !s.kb && s.rows.findIndex((r) => r.on) === park2);
}

for (const scheme of ["light", "dark"]) {
  console.log(`\n${scheme}`);
  await page.emulateMedia({ colorScheme: scheme });
  await page.waitForTimeout(300);
  await suite(`${scheme}/斜杠`, "/", 3, 1);
  await suite(`${scheme}/引用`, "@", 2, 0);
}

await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} 项不合格:\n  ` + fails.join("\n  "));
  process.exit(1);
}
console.log("\n全部通过");
